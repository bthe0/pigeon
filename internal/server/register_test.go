package server

import (
	"testing"

	"github.com/bthe0/pigeon/internal/proto"
)

// ── registerForward domain validation ─────────────────────────────────────────

func TestRegisterForward_RandomSubdomainWhenEmpty(t *testing.T) {
	s := New(Config{Token: "tok", Domain: "tun.example.com"})
	sess := &session{id: "s1", forwards: make(map[string]*forward)}

	p := &proto.ForwardPayload{
		ID:       "fwd1",
		Protocol:  proto.ProtoHTTP,
		LocalAddr: "127.0.0.1:8080",
	}
	addr, err := s.registerForward(sess, p)
	if err != nil {
		t.Fatalf("registerForward: %v", err)
	}
	if addr == "" {
		t.Fatal("expected a public addr, got empty string")
	}
	// The assigned domain must be a subdomain of tun.example.com.
	if !isSubdomain(addr, "tun.example.com") {
		t.Errorf("addr %q is not a subdomain of tun.example.com", addr)
	}
}

func TestRegisterForward_CustomDomain_Valid(t *testing.T) {
	s := New(Config{Token: "tok", Domain: "tun.example.com"})
	sess := &session{id: "s1", forwards: make(map[string]*forward)}

	p := &proto.ForwardPayload{
		ID:       "fwd2",
		Protocol:  proto.ProtoHTTP,
		LocalAddr: "127.0.0.1:8080",
		Domain:   "myapp.tun.example.com",
	}
	addr, err := s.registerForward(sess, p)
	if err != nil {
		t.Fatalf("registerForward with valid domain: %v", err)
	}
	if addr != "myapp.tun.example.com" {
		t.Errorf("addr = %q, want myapp.tun.example.com", addr)
	}
}

func TestRegisterForward_CustomDomain_Invalid(t *testing.T) {
	s := New(Config{Token: "tok", Domain: "tun.example.com"})
	sess := &session{id: "s1", forwards: make(map[string]*forward)}

	p := &proto.ForwardPayload{
		ID:       "fwd3",
		Protocol:  proto.ProtoHTTP,
		LocalAddr: "127.0.0.1:8080",
		Domain:   "evil.com",
	}
	_, err := s.registerForward(sess, p)
	if err == nil {
		t.Fatal("expected error for domain outside base domain, got nil")
	}
}

func TestRegisterForward_NoDomainValidation_WhenNoDomain(t *testing.T) {
	// If no base domain is configured, any domain is accepted.
	s := New(Config{Token: "tok"})
	sess := &session{id: "s1", forwards: make(map[string]*forward)}

	p := &proto.ForwardPayload{
		ID:       "fwd4",
		Protocol:  proto.ProtoHTTP,
		LocalAddr: "127.0.0.1:8080",
		Domain:   "anything.com",
	}
	_, err := s.registerForward(sess, p)
	if err != nil {
		t.Fatalf("unexpected error when no base domain set: %v", err)
	}
}

func isSubdomain(subdomain, base string) bool {
	if subdomain == base {
		return true
	}
	suffix := "." + base
	if len(subdomain) > len(suffix) && subdomain[len(subdomain)-len(suffix):] == suffix {
		return true
	}
	return false
}
