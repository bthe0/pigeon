package client

import (
	"bufio"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bthe0/pigeon/internal/proto"
)

// handleStaticStream serves files in response to a single request read from
// the stream. We drive it with a net.Pipe so we can write a request and read
// back the response without running a real server.
func TestHandleStaticStream_ServesFile(t *testing.T) {
	root := t.TempDir()
	want := []byte("hello, world\n")
	if err := os.WriteFile(filepath.Join(root, "hi.txt"), want, 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	c := &Client{cfg: &Config{}}
	rule := &ForwardRule{ID: "s1", Protocol: proto.ProtoStatic, StaticRoot: root}

	clientSide, serverSide := net.Pipe()

	go func() {
		req, _ := http.NewRequest("GET", "/hi.txt", nil)
		req.Host = "static.example.com"
		_ = req.Write(clientSide)
	}()

	done := make(chan struct{})
	go func() {
		c.handleStaticStream(serverSide, rule, proto.StreamHeader{Protocol: proto.ProtoStatic, RemoteAddr: "127.0.0.1:1"})
		close(done)
	}()

	resp, err := http.ReadResponse(bufio.NewReader(clientSide), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	got := make([]byte, len(want)+10)
	n, _ := resp.Body.Read(got)
	if string(got[:n]) != string(want) {
		t.Errorf("body = %q, want %q", got[:n], want)
	}
	clientSide.Close()
	serverSide.Close()
	<-done
}

func TestHandleStaticStream_404OnMissingFile(t *testing.T) {
	root := t.TempDir()
	c := &Client{cfg: &Config{}}
	rule := &ForwardRule{ID: "s1", Protocol: proto.ProtoStatic, StaticRoot: root}

	clientSide, serverSide := net.Pipe()
	go func() {
		req, _ := http.NewRequest("GET", "/nope.txt", nil)
		req.Host = "static.example.com"
		_ = req.Write(clientSide)
	}()

	done := make(chan struct{})
	go func() {
		c.handleStaticStream(serverSide, rule, proto.StreamHeader{Protocol: proto.ProtoStatic})
		close(done)
	}()

	resp, err := http.ReadResponse(bufio.NewReader(clientSide), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
	clientSide.Close()
	serverSide.Close()
	<-done
}

// Wildcard normalisation must keep the leading "*." intact when expanding a
// bare label using the configured base domain.
func TestNormalizeForward_PreservesWildcard(t *testing.T) {
	cfg := &Config{BaseDomain: "tun.example.com"}
	rule := &ForwardRule{Protocol: proto.ProtoHTTP, Domain: "*.preview"}
	cfg.normalizeForward(rule)
	if rule.Domain != "*.preview.tun.example.com" {
		t.Errorf("domain = %q, want *.preview.tun.example.com", rule.Domain)
	}
}

// Quiet the import linter — strings is used for the static body assertion.
var _ = strings.HasPrefix
