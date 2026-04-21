package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthorizeTunnelPassword_Bypass(t *testing.T) {
	s := New(Config{Token: "test-token"})
	fwd := &forward{id: "test", httpPassword: "secret"}

	t.Run("no password", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		if s.authorizeTunnelPassword(w, req, fwd) {
			t.Fatal("expected false, got true")
		}
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", w.Code)
		}
	})

	t.Run("query param correct", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/?pigeon_password=secret", nil)
		w := httptest.NewRecorder()
		if !s.authorizeTunnelPassword(w, req, fwd) {
			t.Fatal("expected true, got false")
		}
	})

	t.Run("query param incorrect", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/?pigeon_password=wrong", nil)
		w := httptest.NewRecorder()
		if s.authorizeTunnelPassword(w, req, fwd) {
			t.Fatal("expected false, got true")
		}
	})

	t.Run("basic auth correct", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.SetBasicAuth("anyuser", "secret")
		w := httptest.NewRecorder()
		if !s.authorizeTunnelPassword(w, req, fwd) {
			t.Fatal("expected true, got false")
		}
	})

	t.Run("basic auth incorrect", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.SetBasicAuth("anyuser", "wrong")
		w := httptest.NewRecorder()
		if s.authorizeTunnelPassword(w, req, fwd) {
			t.Fatal("expected false, got true")
		}
	})
}
