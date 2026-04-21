package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bthe0/pigeon/internal/server"
)

// ── New ────────────────────────────────────────────────────────────────────────

func TestNew_NonNilWithLogFile(t *testing.T) {
	s := server.New(server.Config{
		ControlAddr: ":0",
		HTTPAddr:    ":0",
		Token:       "tok",
		Domain:      "tun.example.com",
	})
	if s == nil {
		t.Fatal("expected non-nil server")
	}
}

func TestNew_WithLogFile(t *testing.T) {
	dir := t.TempDir()
	logPath := dir + "/traffic.log"

	s := server.New(server.Config{
		ControlAddr: ":0",
		HTTPAddr:    ":0",
		Token:       "tok",
		Domain:      "tun.example.com",
		LogFile:     logPath,
	})
	if s == nil {
		t.Fatal("expected non-nil server with LogFile")
	}
}

// ── ServeHTTP — no registered tunnel ──────────────────────────────────────────

func TestServeHTTP_UnknownHost_Returns502(t *testing.T) {
	s := server.New(server.Config{
		Token:  "tok",
		Domain: "tun.example.com",
	})

	req := httptest.NewRequest(http.MethodGet, "http://unknown.tun.example.com/", nil)
	req.Host = "unknown.tun.example.com"
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status code: got %d want %d", w.Code, http.StatusBadGateway)
	}
	if !strings.Contains(w.Body.String(), "tunnel not found") {
		t.Errorf("body: got %q, expected 'tunnel not found'", w.Body.String())
	}
}

func TestServeHTTP_HostWithPort_Returns502(t *testing.T) {
	s := server.New(server.Config{
		Token:  "tok",
		Domain: "tun.example.com",
	})

	req := httptest.NewRequest(http.MethodGet, "http://unknown.tun.example.com:8080/path", nil)
	req.Host = "unknown.tun.example.com:8080"
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status code: got %d want %d", w.Code, http.StatusBadGateway)
	}
}

func TestServeHTTP_EmptyHost_Returns502(t *testing.T) {
	s := server.New(server.Config{
		Token:  "tok",
		Domain: "tun.example.com",
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = ""
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status code: got %d want %d", w.Code, http.StatusBadGateway)
	}
}

// ── Config fields are preserved ────────────────────────────────────────────────

func TestServerConfig_Fields(t *testing.T) {
	cfg := server.Config{
		ControlAddr: ":2222",
		HTTPAddr:    ":80",
		HTTPSAddr:   ":443",
		Token:       "my-token",
		Domain:      "tun.example.com",
		CertDir:     "/tmp/certs",
		LogFile:     "",
	}

	s := server.New(cfg)
	if s == nil {
		t.Fatal("New returned nil")
	}
	// Config is embedded — we just verify construction doesn't panic.
}
