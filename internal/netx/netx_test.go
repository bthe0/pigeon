package netx

import (
	"bytes"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestCopy_MovesAllBytes(t *testing.T) {
	src := bytes.NewReader([]byte("hello world"))
	var dst bytes.Buffer
	Copy(&dst, src)
	if dst.String() != "hello world" {
		t.Errorf("got %q, want %q", dst.String(), "hello world")
	}
}

// proxyPair returns two connected in-process net.Conns plus their peers.
// a1<->a2 are endpoints; Proxy is exercised by feeding a1 data, expecting it
// at b2 (and vice versa).
func proxyPair(t *testing.T) (client net.Conn, server net.Conn, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			done <- nil
			return
		}
		done <- c
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		ln.Close()
		t.Fatalf("dial: %v", err)
	}
	s := <-done
	if s == nil {
		c.Close()
		ln.Close()
		t.Fatal("accept failed")
	}
	return c, s, func() {
		c.Close()
		s.Close()
		ln.Close()
	}
}

func TestProxy_BidirectionalAndWaitsForBothSides(t *testing.T) {
	// Topology: (client1) <-> (server1)  --Proxy-->  (server2) <-> (client2)
	client1, server1, cleanup1 := proxyPair(t)
	defer cleanup1()
	client2, server2, cleanup2 := proxyPair(t)
	defer cleanup2()

	var returned atomic.Bool
	go func() {
		Proxy(server1, server2)
		returned.Store(true)
	}()

	// client1 → server1 → Proxy → server2 → client2
	if _, err := client1.Write([]byte("ping")); err != nil {
		t.Fatalf("write client1: %v", err)
	}
	buf := make([]byte, 4)
	client2.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(client2, buf); err != nil {
		t.Fatalf("read client2: %v", err)
	}
	if string(buf) != "ping" {
		t.Errorf("client2 got %q, want ping", buf)
	}

	// Reverse direction.
	if _, err := client2.Write([]byte("pong")); err != nil {
		t.Fatalf("write client2: %v", err)
	}
	client1.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(client1, buf); err != nil {
		t.Fatalf("read client1: %v", err)
	}
	if string(buf) != "pong" {
		t.Errorf("client1 got %q, want pong", buf)
	}

	// Tearing one side should cause Proxy to close both and return.
	client1.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if returned.Load() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Proxy did not return after peer close")
}
