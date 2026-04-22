package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bthe0/pigeon/internal/proto"
)

func TestServeHTTP_IgnoresSpoofedForwardedProto(t *testing.T) {
	s := New(Config{Token: "tok", Domain: "tun.example.com"})
	fwd := &forward{
		id:         "f1",
		protocol:   proto.ProtoHTTP,
		publicAddr: "app.tun.example.com",
		expose:     "https",
	}
	s.sessions.Store("http:app.tun.example.com", fwd)

	req := httptest.NewRequest(http.MethodGet, "http://app.tun.example.com/", nil)
	req.Host = "app.tun.example.com"
	req.RemoteAddr = "203.0.113.10:1234"
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusMovedPermanently)
	}
}
