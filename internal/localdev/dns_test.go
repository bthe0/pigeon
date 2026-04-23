package localdev

import (
	"net"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func buildQuery(t *testing.T, name string, qType dnsmessage.Type) []byte {
	t.Helper()
	msg := dnsmessage.Message{
		Header: dnsmessage.Header{ID: 42, RecursionDesired: true},
		Questions: []dnsmessage.Question{{
			Name:  dnsmessage.MustNewName(name),
			Type:  qType,
			Class: dnsmessage.ClassINET,
		}},
	}
	b, err := msg.Pack()
	if err != nil {
		t.Fatalf("pack query: %v", err)
	}
	return b
}

// localPacketConn wraps the server side of a net.Pipe-style packet exchange
// by binding the DNS handler to a real loopback socket on a random port.
func startDNSOnRandomPort(t *testing.T, domain string) net.PacketConn {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		buf := make([]byte, 512)
		for {
			pc.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, src, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			// Run handler synchronously to keep the test simple.
			handleDNS(pc, src, append([]byte(nil), buf[:n]...), domain)
		}
	}()
	return pc
}

func TestHandleDNS_ResolvesMatchingDomain(t *testing.T) {
	server := startDNSOnRandomPort(t, "pigeon.local")
	defer server.Close()

	client, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("client listen: %v", err)
	}
	defer client.Close()

	q := buildQuery(t, "app.pigeon.local.", dnsmessage.TypeA)
	if _, err := client.WriteTo(q, server.LocalAddr()); err != nil {
		t.Fatalf("write query: %v", err)
	}

	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 512)
	n, _, err := client.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var resp dnsmessage.Message
	if err := resp.Unpack(buf[:n]); err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if !resp.Header.Response || !resp.Header.Authoritative {
		t.Errorf("response flags = %+v", resp.Header)
	}
	if len(resp.Answers) != 1 {
		t.Fatalf("answers = %d, want 1", len(resp.Answers))
	}
	a, ok := resp.Answers[0].Body.(*dnsmessage.AResource)
	if !ok {
		t.Fatalf("answer body = %T, want AResource", resp.Answers[0].Body)
	}
	if a.A != [4]byte{127, 0, 0, 1} {
		t.Errorf("A = %v, want 127.0.0.1", a.A)
	}
}

func TestHandleDNS_IgnoresNonMatchingDomain(t *testing.T) {
	server := startDNSOnRandomPort(t, "pigeon.local")
	defer server.Close()

	client, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("client listen: %v", err)
	}
	defer client.Close()

	q := buildQuery(t, "example.com.", dnsmessage.TypeA)
	if _, err := client.WriteTo(q, server.LocalAddr()); err != nil {
		t.Fatalf("write: %v", err)
	}

	client.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 512)
	n, _, err := client.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var resp dnsmessage.Message
	if err := resp.Unpack(buf[:n]); err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if len(resp.Answers) != 0 {
		t.Errorf("expected no answers for non-matching domain, got %d", len(resp.Answers))
	}
}
