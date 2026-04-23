package server

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bthe0/pigeon/internal/proto"
)

// ── parseAllowedIPs ───────────────────────────────────────────────────────────

func TestParseAllowedIPs_AcceptsBareIPAndCIDR(t *testing.T) {
	nets, err := parseAllowedIPs([]string{"10.0.0.1", "192.168.0.0/16", " 2001:db8::1 "})
	if err != nil {
		t.Fatalf("parseAllowedIPs: %v", err)
	}
	if len(nets) != 3 {
		t.Fatalf("want 3 nets, got %d", len(nets))
	}
	// Bare IPs should round-trip as host-mask CIDRs.
	if !nets[0].Contains(net.ParseIP("10.0.0.1")) || nets[0].Contains(net.ParseIP("10.0.0.2")) {
		t.Error("bare IP should be a /32 host route")
	}
	if !nets[1].Contains(net.ParseIP("192.168.5.5")) {
		t.Error("/16 CIDR should contain 192.168.5.5")
	}
}

func TestParseAllowedIPs_RejectsGarbage(t *testing.T) {
	if _, err := parseAllowedIPs([]string{"not-an-ip"}); err == nil {
		t.Fatal("expected error for invalid input")
	}
}

func TestParseAllowedIPs_SkipsBlankEntries(t *testing.T) {
	nets, err := parseAllowedIPs([]string{"", "  ", "10.0.0.0/8"})
	if err != nil {
		t.Fatalf("parseAllowedIPs: %v", err)
	}
	if len(nets) != 1 {
		t.Fatalf("blank entries should be skipped; got %d nets", len(nets))
	}
}

// ── forward.allows ────────────────────────────────────────────────────────────

func TestForwardAllows_EmptyListAllowsAll(t *testing.T) {
	f := &forward{}
	if !f.allows("203.0.113.5") {
		t.Error("empty allowlist should permit any IP")
	}
}

func TestForwardAllows_RejectsOutsideCIDR(t *testing.T) {
	nets, _ := parseAllowedIPs([]string{"10.0.0.0/8"})
	f := &forward{allowedIPs: nets}
	if f.allows("192.168.1.1") {
		t.Error("192.168.1.1 should not match 10.0.0.0/8")
	}
	if !f.allows("10.5.5.5") {
		t.Error("10.5.5.5 should match 10.0.0.0/8")
	}
}

func TestForwardAllows_DeniesUnparseableIP(t *testing.T) {
	nets, _ := parseAllowedIPs([]string{"10.0.0.0/8"})
	f := &forward{allowedIPs: nets}
	if f.allows("not-an-ip") {
		t.Error("unparseable IP should be denied when allowlist is non-empty")
	}
}

// ── ServeHTTP IP allowlist ────────────────────────────────────────────────────

// IP allowlist must short-circuit before the password / max-conn checks so a
// blocked IP never sees the password page (which leaks tunnel existence).
func TestServeHTTP_IPAllowlist_Blocks(t *testing.T) {
	s := New(Config{Token: "t", Domain: "tun.example.com"})
	nets, _ := parseAllowedIPs([]string{"10.0.0.0/8"})
	fwd := &forward{
		id:         "fwd",
		protocol:   proto.ProtoHTTP,
		publicAddr: "app.tun.example.com",
		expose:     "http", // skip the HTTPS redirect path
		allowedIPs: nets,
	}
	s.sessions.Store("http:app.tun.example.com", fwd)

	req := httptest.NewRequest("GET", "http://app.tun.example.com/", nil)
	req.RemoteAddr = "203.0.113.5:5555"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for blocked IP, got %d", w.Code)
	}
}

// ── Wildcard routing ──────────────────────────────────────────────────────────

func TestRegisterForward_Wildcard_StoresInWildcardMap(t *testing.T) {
	s := New(Config{Token: "t", Domain: "tun.example.com"})
	sess := &session{id: "s1", forwards: make(map[string]*forward)}

	p := &proto.ForwardPayload{
		ID:        "wfwd",
		Protocol:  proto.ProtoHTTP,
		LocalAddr: "127.0.0.1:8080",
		Domain:    "*.preview.tun.example.com",
	}
	if _, err := s.registerForward(sess, p); err != nil {
		t.Fatalf("registerForward wildcard: %v", err)
	}
	if _, ok := s.wildcards.Load("preview.tun.example.com"); !ok {
		t.Error("wildcard not stored under suffix key")
	}
	if _, ok := s.sessions.Load("http:*.preview.tun.example.com"); ok {
		t.Error("wildcard should not be stored in exact-match map")
	}
}

func TestRegisterForward_Wildcard_RejectsMultipleStars(t *testing.T) {
	s := New(Config{Token: "t", Domain: "tun.example.com"})
	sess := &session{id: "s1", forwards: make(map[string]*forward)}

	p := &proto.ForwardPayload{
		ID: "bad", Protocol: proto.ProtoHTTP, LocalAddr: "127.0.0.1:8080",
		Domain: "*.foo.*.tun.example.com",
	}
	if _, err := s.registerForward(sess, p); err == nil || !strings.Contains(err.Error(), "one leading wildcard") {
		t.Fatalf("expected 'one leading wildcard' error, got %v", err)
	}
}

func TestLookupHTTPForward_WildcardOneLevelOnly(t *testing.T) {
	s := New(Config{Token: "t", Domain: "tun.example.com"})
	fwd := &forward{id: "f", protocol: proto.ProtoHTTP, publicAddr: "*.preview.tun.example.com"}
	s.wildcards.Store("preview.tun.example.com", fwd)

	if got := s.lookupHTTPForward("foo.preview.tun.example.com"); got != fwd {
		t.Errorf("one-label-deep host should match wildcard")
	}
	// Two-label-deep must NOT match — that's the user's chosen scope.
	if got := s.lookupHTTPForward("a.b.preview.tun.example.com"); got != nil {
		t.Errorf("nested host should not match one-level wildcard, got %v", got)
	}
}

func TestReleaseForward_DeletesFromWildcardMap(t *testing.T) {
	s := New(Config{Token: "t", Domain: "tun.example.com"})
	fwd := &forward{id: "f", protocol: proto.ProtoHTTP, publicAddr: "*.preview.tun.example.com"}
	s.wildcards.Store("preview.tun.example.com", fwd)
	s.forwards.Store(fwd.id, fwd)

	s.releaseForward(fwd)

	if _, ok := s.wildcards.Load("preview.tun.example.com"); ok {
		t.Error("wildcard not removed on release")
	}
}

// ── Static forwards ───────────────────────────────────────────────────────────

func TestRegisterForward_Static_DoesNotRequireLocalAddr(t *testing.T) {
	s := New(Config{Token: "t", Domain: "tun.example.com"})
	sess := &session{id: "s1", forwards: make(map[string]*forward)}

	p := &proto.ForwardPayload{
		ID:       "sfwd",
		Protocol: proto.ProtoStatic,
		Domain:   "docs.tun.example.com",
	}
	addr, err := s.registerForward(sess, p)
	if err != nil {
		t.Fatalf("registerForward static: %v", err)
	}
	if addr != "docs.tun.example.com" {
		t.Errorf("addr = %q, want docs.tun.example.com", addr)
	}
}

// ── Body capture helpers ──────────────────────────────────────────────────────

func TestCaptureBody_TruncatesAndReplaysFull(t *testing.T) {
	in := strings.NewReader(strings.Repeat("a", 1024))
	got, truncated, replacement := captureBody(noopCloser{in}, 100)
	if !truncated {
		t.Error("expected truncated=true when body exceeds cap")
	}
	if len(got) != 100 {
		t.Errorf("captured len = %d, want 100", len(got))
	}
	// The replacement must still produce the full 1024 bytes.
	all := make([]byte, 0, 1024)
	buf := make([]byte, 256)
	for {
		n, err := replacement.Read(buf)
		all = append(all, buf[:n]...)
		if err != nil {
			break
		}
	}
	if len(all) != 1024 {
		t.Errorf("replayed len = %d, want 1024", len(all))
	}
}

func TestCaptureBody_ShortBodyNotTruncated(t *testing.T) {
	in := strings.NewReader("hi")
	got, truncated, _ := captureBody(noopCloser{in}, 100)
	if truncated {
		t.Error("expected truncated=false for short body")
	}
	if string(got) != "hi" {
		t.Errorf("got %q, want %q", got, "hi")
	}
}

type noopCloser struct{ *strings.Reader }

func (noopCloser) Close() error { return nil }
