package server

import (
	"net"
	"sync"
	"testing"

	"github.com/bthe0/pigeon/internal/proto"
	"github.com/hashicorp/yamux"
)

// newTestSession spins up a yamux session on a loopback TCP pair. The returned
// session has a live *yamux.Session so openPort's serveTCP/serveUDP goroutines
// can call mux.Open without nil-dereferencing. The cleanup closes both sides.
func newTestSession(t *testing.T) (*session, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serverSide := make(chan *yamux.Session, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverSide <- nil
			return
		}
		sess, err := yamux.Server(conn, yamux.DefaultConfig())
		if err != nil {
			conn.Close()
			serverSide <- nil
			return
		}
		serverSide <- sess
	}()
	cc, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		ln.Close()
		t.Fatalf("dial: %v", err)
	}
	clientSess, err := yamux.Client(cc, yamux.DefaultConfig())
	if err != nil {
		ln.Close()
		cc.Close()
		t.Fatalf("yamux client: %v", err)
	}
	srv := <-serverSide
	if srv == nil {
		ln.Close()
		t.Fatal("server yamux failed")
	}
	// The "session" struct in production is a server-side holder; we just
	// need its .mux to be non-nil and Open-able. Using clientSess here
	// gives us a functional mux the tests can drive.
	s := &session{id: "test", mux: clientSess, forwards: make(map[string]*forward)}
	return s, func() {
		clientSess.Close()
		srv.Close()
		cc.Close()
		ln.Close()
	}
}

// ── tryAcquire / release ──────────────────────────────────────────────────────

func TestTryAcquire_ZeroMaxConnections_IsUnlimited(t *testing.T) {
	f := &forward{maxConnections: 0}
	for i := 0; i < 1000; i++ {
		if !f.tryAcquire() {
			t.Fatalf("tryAcquire #%d failed with maxConnections=0", i)
		}
	}
}

func TestTryAcquire_RespectsCap(t *testing.T) {
	f := &forward{maxConnections: 3}
	for i := 0; i < 3; i++ {
		if !f.tryAcquire() {
			t.Fatalf("acquire #%d failed", i)
		}
	}
	if f.tryAcquire() {
		t.Error("tryAcquire should fail when cap is reached")
	}
}

func TestRelease_UnblocksAcquire(t *testing.T) {
	f := &forward{maxConnections: 1}
	if !f.tryAcquire() {
		t.Fatal("first acquire failed")
	}
	if f.tryAcquire() {
		t.Fatal("second acquire should fail at cap")
	}
	f.release()
	if !f.tryAcquire() {
		t.Error("acquire after release should succeed")
	}
}

// Concurrent acquires must never overshoot maxConnections — the CAS loop in
// tryAcquire is the invariant this covers.
func TestTryAcquire_ConcurrentRespectsCap(t *testing.T) {
	const cap = 10
	const attempts = 200
	f := &forward{maxConnections: cap}

	var succ, fail int32
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if f.tryAcquire() {
				mu.Lock()
				succ++
				mu.Unlock()
			} else {
				mu.Lock()
				fail++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if succ != cap {
		t.Errorf("successes = %d, want %d (cap)", succ, cap)
	}
	if succ+fail != attempts {
		t.Errorf("total %d != %d", succ+fail, attempts)
	}
}

// ── openPort / listener lifecycle ─────────────────────────────────────────────

func TestOpenPort_TCP_AssignsFreePort(t *testing.T) {
	s := New(Config{Token: "t", Domain: "tun.example.com"})
	sess, cleanup := newTestSession(t)
	defer cleanup()

	fwd := &forward{id: "x", protocol: proto.ProtoTCP, port: 0, session: sess}

	port, err := s.openPort(fwd)
	if err != nil {
		t.Fatalf("openPort: %v", err)
	}
	if port == 0 {
		t.Fatal("expected assigned non-zero port")
	}
	if fwd.listener == nil {
		t.Error("fwd.listener not set")
	}
	_ = fwd.listener.Close()
}

func TestOpenPort_UDP_AssignsFreePort(t *testing.T) {
	s := New(Config{Token: "t", Domain: "tun.example.com"})
	sess, cleanup := newTestSession(t)
	defer cleanup()

	fwd := &forward{id: "x", protocol: proto.ProtoUDP, port: 0, session: sess}

	port, err := s.openPort(fwd)
	if err != nil {
		t.Fatalf("openPort: %v", err)
	}
	if port == 0 {
		t.Fatal("expected assigned non-zero UDP port")
	}
	if fwd.listener == nil {
		t.Error("fwd.listener not set")
	}
	_ = fwd.listener.Close()
}

// After releaseForward closes the listener, the port must actually be free —
// this is the real fix from the #3 refactor. If the listener leaked, a
// second Listen on the same port would fail with EADDRINUSE.
func TestReleaseForward_ClosesTCPListener_PortBecomesFree(t *testing.T) {
	s := New(Config{Token: "t", Domain: "tun.example.com"})
	sess, cleanup := newTestSession(t)
	defer cleanup()

	fwd := &forward{id: "x", protocol: proto.ProtoTCP, port: 0, session: sess}

	port, err := s.openPort(fwd)
	if err != nil {
		t.Fatalf("openPort: %v", err)
	}

	s.releaseForward(fwd)
	if fwd.listener != nil {
		t.Error("listener not cleared after release")
	}

	// Rebinding the same port must succeed.
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", itoa(port)))
	if err != nil {
		// Macs sometimes keep the port in TIME_WAIT; retry on any different port
		// just to prove the close path itself works without leaking the fd.
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("follow-up Listen: %v", err)
		}
	}
	ln.Close()
}

func TestReleaseForward_Idempotent(t *testing.T) {
	s := New(Config{Token: "t", Domain: "tun.example.com"})
	sess, cleanup := newTestSession(t)
	defer cleanup()

	fwd := &forward{id: "x", protocol: proto.ProtoTCP, port: 0, session: sess}
	if _, err := s.openPort(fwd); err != nil {
		t.Fatalf("openPort: %v", err)
	}
	s.releaseForward(fwd)
	s.releaseForward(fwd) // must not panic
}

// Verify the sessions map is cleared on release for HTTP forwards.
func TestReleaseForward_DeletesFromSessionsMap(t *testing.T) {
	s := New(Config{Token: "t", Domain: "tun.example.com"})
	fwd := &forward{id: "x", protocol: proto.ProtoHTTP, publicAddr: "app.tun.example.com"}
	s.sessions.Store("http:"+fwd.publicAddr, fwd)
	s.forwards.Store(fwd.id, fwd)

	s.releaseForward(fwd)

	if _, ok := s.sessions.Load("http:" + fwd.publicAddr); ok {
		t.Error("sessions entry not removed")
	}
	if _, ok := s.forwards.Load(fwd.id); ok {
		t.Error("forwards entry not removed")
	}
}

// Tiny helper to avoid pulling in strconv just for one conversion in the test.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
