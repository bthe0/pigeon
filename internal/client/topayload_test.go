package client_test

import (
	"testing"

	"github.com/bthe0/pigeon/internal/client"
	"github.com/bthe0/pigeon/internal/proto"
)

func TestToPayload_CopiesAllFields(t *testing.T) {
	r := client.ForwardRule{
		ID:              "abc12345",
		Protocol:        proto.ProtoTCP,
		LocalAddr:       "localhost:5432",
		Domain:          "",
		RemotePort:      2200,
		Expose:          "https",
		HTTPPassword:    "s3cret",
		MaxConnections:  5,
		UnavailablePage: "terminal",
	}
	p := r.ToPayload()
	if p.ID != r.ID || p.Protocol != r.Protocol || p.LocalAddr != r.LocalAddr {
		t.Errorf("core fields not copied: %+v", p)
	}
	if p.RemotePort != r.RemotePort || p.Expose != r.Expose ||
		p.HTTPPassword != r.HTTPPassword || p.MaxConnections != r.MaxConnections ||
		p.UnavailablePage != r.UnavailablePage {
		t.Errorf("auxiliary fields not copied: %+v", p)
	}
}

// HTTP tunnels reuse PublicAddr when Domain is empty so the URL stays stable
// across reconnects — this is the core invariant of sendForwardAdd.
func TestToPayload_HTTPWithoutDomain_FallsBackToPublicAddr(t *testing.T) {
	r := client.ForwardRule{
		Protocol:   proto.ProtoHTTP,
		LocalAddr:  "localhost:3000",
		Domain:     "",
		PublicAddr: "myapp.example.com",
	}
	if got := r.ToPayload().Domain; got != "myapp.example.com" {
		t.Errorf("Domain = %q, want fallback to PublicAddr", got)
	}
}

func TestToPayload_HTTPSWithoutDomain_FallsBackToPublicAddr(t *testing.T) {
	r := client.ForwardRule{
		Protocol:   proto.ProtoHTTPS,
		PublicAddr: "secure.example.com",
	}
	if got := r.ToPayload().Domain; got != "secure.example.com" {
		t.Errorf("Domain = %q, want %q", got, "secure.example.com")
	}
}

func TestToPayload_ExplicitDomainTakesPrecedence(t *testing.T) {
	r := client.ForwardRule{
		Protocol:   proto.ProtoHTTP,
		Domain:     "new.example.com",
		PublicAddr: "old.example.com",
	}
	if got := r.ToPayload().Domain; got != "new.example.com" {
		t.Errorf("Domain = %q, want explicit %q", got, "new.example.com")
	}
}

// TCP/UDP forwards must not inherit PublicAddr as a Domain — that field is
// HTTP-only on the wire and would be interpreted as a rejected subdomain.
func TestToPayload_TCP_DoesNotFallBackToPublicAddr(t *testing.T) {
	r := client.ForwardRule{
		Protocol:   proto.ProtoTCP,
		PublicAddr: "example.com:5432",
	}
	if got := r.ToPayload().Domain; got != "" {
		t.Errorf("Domain = %q, want empty for TCP", got)
	}
}

func TestToPayload_UDP_DoesNotFallBackToPublicAddr(t *testing.T) {
	r := client.ForwardRule{
		Protocol:   proto.ProtoUDP,
		PublicAddr: "example.com:7777",
	}
	if got := r.ToPayload().Domain; got != "" {
		t.Errorf("Domain = %q, want empty for UDP", got)
	}
}
