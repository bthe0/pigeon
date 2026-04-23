package client

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bthe0/pigeon/internal/proto"
)

// startUDPEcho runs a UDP echo server on a random loopback port until stop is
// closed. It returns the listen address.
func startUDPEcho(t *testing.T) (addr string, stop func()) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 2048)
		for {
			pc.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, src, err := pc.ReadFrom(buf)
			if err != nil {
				select {
				case <-done:
					return
				default:
					continue
				}
			}
			pc.WriteTo(buf[:n], src)
		}
	}()
	return pc.LocalAddr().String(), func() {
		close(done)
		pc.Close()
	}
}

// streamPair returns two connected TCP endpoints on loopback. Used instead of
// net.Pipe because the JSON decoder in handleUDPStream needs reliable
// deadline-bearing reads, which net.Pipe doesn't provide.
func streamPair(t *testing.T) (a, b net.Conn, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serverCh := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			serverCh <- nil
			return
		}
		serverCh <- c
	}()
	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		ln.Close()
		t.Fatalf("dial: %v", err)
	}
	server := <-serverCh
	if server == nil {
		client.Close()
		ln.Close()
		t.Fatal("accept failed")
	}
	return client, server, func() {
		client.Close()
		server.Close()
		ln.Close()
	}
}

// newTestClient returns a bare *Client suitable for invoking handleUDPStream.
// HOME is redirected so logTraffic's disk writes don't pollute the dev box.
func newTestClient(t *testing.T) *Client {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".pigeon", "logs"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return &Client{cfg: &Config{}, logger: log.New(io.Discard, "", 0)}
}

func TestHandleUDPStream_RoundTripsSingleClient(t *testing.T) {
	echoAddr, stopEcho := startUDPEcho(t)
	defer stopEcho()

	handlerSide, testSide, cleanup := streamPair(t)
	defer cleanup()

	c := newTestClient(t)
	rule := &ForwardRule{ID: "f1", LocalAddr: echoAddr, Protocol: proto.ProtoUDP}

	go c.handleUDPStream(handlerSide, rule)

	// Simulate server sending an inbound datagram stamped with an external addr.
	enc := json.NewEncoder(testSide)
	if err := enc.Encode(proto.UDPFrame{Addr: "203.0.113.5:4444", Data: []byte("hello")}); err != nil {
		t.Fatalf("encode: %v", err)
	}

	// The echo server replies, the client-side stamps with the same extAddr.
	testSide.SetReadDeadline(time.Now().Add(2 * time.Second))
	dec := json.NewDecoder(testSide)
	var got proto.UDPFrame
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if got.Addr != "203.0.113.5:4444" {
		t.Errorf("reply Addr = %q, want the original external address", got.Addr)
	}
	if string(got.Data) != "hello" {
		t.Errorf("reply Data = %q, want %q", got.Data, "hello")
	}
}

// The NAT-table invariant: two different external clients must get replies
// stamped with *their* address, not each other's. A per-client local socket
// is what makes that work.
func TestHandleUDPStream_IsolatesReplies_PerExternalClient(t *testing.T) {
	echoAddr, stopEcho := startUDPEcho(t)
	defer stopEcho()

	handlerSide, testSide, cleanup := streamPair(t)
	defer cleanup()

	c := newTestClient(t)
	rule := &ForwardRule{ID: "f2", LocalAddr: echoAddr, Protocol: proto.ProtoUDP}
	go c.handleUDPStream(handlerSide, rule)

	enc := json.NewEncoder(testSide)

	// Two different external clients, each with a payload that identifies
	// which client it came from so we can verify the reply stamping.
	enc.Encode(proto.UDPFrame{Addr: "10.0.0.1:1111", Data: []byte("from-A")})
	enc.Encode(proto.UDPFrame{Addr: "10.0.0.2:2222", Data: []byte("from-B")})

	received := map[string]string{}
	var mu sync.Mutex
	dec := json.NewDecoder(testSide)
	deadline := time.Now().Add(2 * time.Second)
	for len(received) < 2 && time.Now().Before(deadline) {
		testSide.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		var f proto.UDPFrame
		if err := dec.Decode(&f); err != nil {
			break
		}
		mu.Lock()
		received[f.Addr] = string(f.Data)
		mu.Unlock()
	}

	if received["10.0.0.1:1111"] != "from-A" {
		t.Errorf("client A reply = %q, want from-A", received["10.0.0.1:1111"])
	}
	if received["10.0.0.2:2222"] != "from-B" {
		t.Errorf("client B reply = %q, want from-B", received["10.0.0.2:2222"])
	}
}

// Invalid local address in the rule should make handleUDPStream bail cleanly
// rather than spin or panic.
func TestHandleUDPStream_InvalidLocalAddr_ExitsCleanly(t *testing.T) {
	handlerSide, testSide, cleanup := streamPair(t)
	defer cleanup()

	c := newTestClient(t)
	rule := &ForwardRule{ID: "bad", LocalAddr: "not-a-host-port", Protocol: proto.ProtoUDP}

	done := make(chan struct{})
	go func() {
		c.handleUDPStream(handlerSide, rule)
		close(done)
	}()

	// If the handler is already done there's nothing to read; closing our
	// side forces it to return if it hasn't yet.
	testSide.Close()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("handleUDPStream did not return on invalid local addr")
	}
}
