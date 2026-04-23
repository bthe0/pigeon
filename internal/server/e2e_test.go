package server

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bthe0/pigeon/internal/netx"
	"github.com/bthe0/pigeon/internal/proto"
	"github.com/hashicorp/yamux"
)

// openCtrlStream simulates the production handshake where the client opens
// the first yamux stream as the control channel. It returns the server-side
// ctrl conn (suitable for sess.ctrl) and spins up a goroutine that drains
// whatever the server writes to it (inspector events, pong replies, …) so
// writes from ServeHTTP don't block.
func openCtrlStream(t *testing.T, srv, cli *yamux.Session) net.Conn {
	t.Helper()
	cliCtrl, err := cli.Open()
	if err != nil {
		t.Fatalf("cli.Open (ctrl): %v", err)
	}
	go io.Copy(io.Discard, cliCtrl)
	srvCtrl, err := srv.Accept()
	if err != nil {
		t.Fatalf("srv.Accept (ctrl): %v", err)
	}
	return srvCtrl
}

// newYamuxPair returns (serverMux, clientMux). One side is the pigeon server,
// the other is the pigeon client. This gives us a real multiplexed connection
// to exercise ServeHTTP's stream-open + header-write path end to end.
func newYamuxPair(t *testing.T) (server, client *yamux.Session, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			accepted <- nil
			return
		}
		accepted <- c
	}()
	clientConn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		ln.Close()
		t.Fatalf("dial: %v", err)
	}
	serverConn := <-accepted
	if serverConn == nil {
		clientConn.Close()
		ln.Close()
		t.Fatal("accept failed")
	}

	srvSess, err := yamux.Server(serverConn, yamux.DefaultConfig())
	if err != nil {
		t.Fatalf("yamux.Server: %v", err)
	}
	cliSess, err := yamux.Client(clientConn, yamux.DefaultConfig())
	if err != nil {
		t.Fatalf("yamux.Client: %v", err)
	}
	return srvSess, cliSess, func() {
		srvSess.Close()
		cliSess.Close()
		serverConn.Close()
		clientConn.Close()
		ln.Close()
	}
}

// runClientAcceptLoop mirrors what the real pigeon client does: accept yamux
// streams, read the stream header, and proxy them to the local backend.
// Returns a stop function that tears down the goroutine.
func runClientAcceptLoop(t *testing.T, mux *yamux.Session, backend string) func() {
	t.Helper()
	stopped := make(chan struct{})
	go func() {
		for {
			stream, err := mux.Accept()
			if err != nil {
				close(stopped)
				return
			}
			go func(s net.Conn) {
				defer s.Close()
				if _, err := proto.ReadStreamHeader(s); err != nil {
					return
				}
				local, err := net.DialTimeout("tcp", backend, 2*time.Second)
				if err != nil {
					return
				}
				defer local.Close()
				netx.Proxy(s, local)
			}(stream)
		}
	}()
	return func() {
		mux.Close()
		<-stopped
	}
}

// ── The test ──────────────────────────────────────────────────────────────────

func TestE2E_HTTPProxy_RoundTripsRequestAndResponse(t *testing.T) {
	// Local HTTP backend — the "user's local service".
	var hits atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Echo-Path", r.URL.Path)
		fmt.Fprintf(w, "hello from backend, you said %q", r.URL.Path)
	}))
	defer backend.Close()
	u, _ := url.Parse(backend.URL)
	backendHostPort := u.Host

	// Yamux pair: one side lives on the server, the other on the client.
	srvMux, cliMux, cleanupMux := newYamuxPair(t)
	defer cleanupMux()

	ctrl := openCtrlStream(t, srvMux, cliMux)

	// Client accept loop that dials the backend for every incoming stream.
	stopClient := runClientAcceptLoop(t, cliMux, backendHostPort)
	defer stopClient()

	// Register a forward on the server so ServeHTTP can route by Host.
	const host = "myapp.tun.example.com"
	s := New(Config{Token: "tok", Domain: "tun.example.com"})
	sess := &session{id: "e2e", mux: srvMux, ctrl: ctrl, forwards: map[string]*forward{}}
	fwd := &forward{
		id:         "fwd1",
		protocol:   proto.ProtoHTTP,
		publicAddr: host,
		expose:     "http",
		session:    sess,
	}
	sess.forwards[fwd.id] = fwd
	s.sessions.Store("http:"+host, fwd)

	// Fire a request at the public face of the tunnel.
	req := httptest.NewRequest(http.MethodGet, "http://"+host+"/greet", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %q", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "hello from backend") {
		t.Errorf("body = %q, want backend response", body)
	}
	if resp.Header.Get("X-Echo-Path") != "/greet" {
		t.Errorf("X-Echo-Path = %q, want /greet", resp.Header.Get("X-Echo-Path"))
	}
	if hits.Load() != 1 {
		t.Errorf("backend hits = %d, want 1", hits.Load())
	}
}

// maxConnections on the forward must be enforced: once at capacity, further
// requests get the Too Many Connections status page rather than being proxied.
func TestE2E_HTTPProxy_RespectsMaxConnections(t *testing.T) {
	// Backend blocks so the first in-flight request holds the slot.
	block := make(chan struct{})
	released := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
		fmt.Fprintln(w, "ok")
		close(released)
	}))
	defer backend.Close()
	u, _ := url.Parse(backend.URL)

	srvMux, cliMux, cleanupMux := newYamuxPair(t)
	defer cleanupMux()
	ctrl := openCtrlStream(t, srvMux, cliMux)
	stopClient := runClientAcceptLoop(t, cliMux, u.Host)
	defer stopClient()

	const host = "cap.tun.example.com"
	s := New(Config{Token: "tok", Domain: "tun.example.com"})
	sess := &session{id: "capacity", mux: srvMux, ctrl: ctrl, forwards: map[string]*forward{}}
	fwd := &forward{
		id:             "cap1",
		protocol:       proto.ProtoHTTP,
		publicAddr:     host,
		expose:         "http",
		session:        sess,
		maxConnections: 1,
	}
	sess.forwards[fwd.id] = fwd
	s.sessions.Store("http:"+host, fwd)

	// First request — takes the slot, blocks in backend.
	firstDone := make(chan *http.Response, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "http://"+host+"/first", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		firstDone <- rec.Result()
	}()

	// Give the first request time to grab the slot.
	time.Sleep(100 * time.Millisecond)

	// Second request — should be rejected at the cap.
	req2 := httptest.NewRequest(http.MethodGet, "http://"+host+"/second", nil)
	req2.Host = host
	rec2 := httptest.NewRecorder()
	s.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second request status = %d, want 429", rec2.Code)
	}

	// Unblock the first request and drain.
	close(block)
	select {
	case <-released:
	case <-time.After(2 * time.Second):
		t.Fatal("first request never completed")
	}
	<-firstDone
}
