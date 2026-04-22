package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

// ── Cookie-based auth ──────────────────────────────────────────────────────────

func TestAuthorizeTunnelPassword_CookieAuth(t *testing.T) {
	s := New(Config{Token: "test-token"})
	fwd := &forward{id: "myfwd", httpPassword: "hunter2"}

	cookieName := passwordCookieName(fwd)
	cookieValue := passwordCookieValue(s.cfg.Token, fwd)

	t.Run("valid cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: cookieValue})
		w := httptest.NewRecorder()
		if !s.authorizeTunnelPassword(w, req, fwd) {
			t.Fatal("expected true with valid cookie")
		}
	})

	t.Run("wrong cookie value", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: "badvalue"})
		w := httptest.NewRecorder()
		if s.authorizeTunnelPassword(w, req, fwd) {
			t.Fatal("expected false with invalid cookie value")
		}
	})

	t.Run("wrong cookie name", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "other_cookie", Value: cookieValue})
		w := httptest.NewRecorder()
		if s.authorizeTunnelPassword(w, req, fwd) {
			t.Fatal("expected false with wrong cookie name")
		}
	})
}

// ── Password form validation ───────────────────────────────────────────────────

func TestAuthorizeTunnelPassword_FormValidation(t *testing.T) {
	s := New(Config{Token: "tok"})
	fwd := &forward{id: "f1", httpPassword: "correct-password"}

	postForm := func(password string) *httptest.ResponseRecorder {
		body := url.Values{"pigeon_password": {password}}.Encode()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		s.authorizeTunnelPassword(w, req, fwd)
		return w
	}

	t.Run("empty password shows error", func(t *testing.T) {
		w := postForm("")
		if !strings.Contains(w.Body.String(), "Password is required") {
			t.Errorf("expected 'Password is required' in body, got: %s", w.Body.String())
		}
	})

	t.Run("too short shows error", func(t *testing.T) {
		w := postForm("ab")
		if !strings.Contains(w.Body.String(), "at least 4") {
			t.Errorf("expected 'at least 4' in body, got: %s", w.Body.String())
		}
	})

	t.Run("too long shows error", func(t *testing.T) {
		w := postForm(strings.Repeat("x", 129))
		if !strings.Contains(w.Body.String(), "128 characters") {
			t.Errorf("expected '128 characters' in body, got: %s", w.Body.String())
		}
	})

	t.Run("wrong password shows error", func(t *testing.T) {
		w := postForm("wrong-password!")
		if !strings.Contains(w.Body.String(), "Incorrect password") {
			t.Errorf("expected 'Incorrect password' in body, got: %s", w.Body.String())
		}
	})
}

// ── Correct password sets cookie and redirects ─────────────────────────────────

func TestAuthorizeTunnelPassword_CorrectPasswordRedirects(t *testing.T) {
	s := New(Config{Token: "tok"})
	fwd := &forward{id: "f1", httpPassword: "correct-password"}

	body := url.Values{"pigeon_password": {"correct-password"}}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/some/path", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	result := s.authorizeTunnelPassword(w, req, fwd)
	if result {
		t.Fatal("expected false (redirect, not pass-through)")
	}
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", w.Code, http.StatusSeeOther)
	}
	// A cookie must be set.
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected Set-Cookie header after successful login")
	}
	found := false
	for _, c := range cookies {
		if c.Name == passwordCookieName(fwd) {
			found = true
			if c.Value != passwordCookieValue(s.cfg.Token, fwd) {
				t.Errorf("cookie value mismatch: got %q", c.Value)
			}
		}
	}
	if !found {
		t.Errorf("auth cookie %q not set", passwordCookieName(fwd))
	}
}

// ── Rate limiting ──────────────────────────────────────────────────────────────

func TestAuthorizeTunnelPassword_RateLimiting(t *testing.T) {
	s := New(Config{Token: "tok"})
	fwd := &forward{id: "rl", httpPassword: "secret"}

	postWrong := func() {
		body := url.Values{"pigeon_password": {"badpassword"}}.Encode()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		s.authorizeTunnelPassword(w, req, fwd)
	}

	// Submit passwordMaxFails wrong attempts.
	for i := 0; i < passwordMaxFails; i++ {
		postWrong()
	}

	// Next attempt should be rate-limited.
	body := url.Values{"pigeon_password": {"badpassword"}}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.authorizeTunnelPassword(w, req, fwd)

	if !strings.Contains(w.Body.String(), "Too many failed attempts") {
		t.Errorf("expected rate-limit message, got: %s", w.Body.String())
	}
}

func TestPasswordCookieValue_IsDeterministic(t *testing.T) {
	fwd := &forward{id: "x", httpPassword: "pw"}
	v1 := passwordCookieValue("token", fwd)
	v2 := passwordCookieValue("token", fwd)
	if v1 != v2 {
		t.Errorf("passwordCookieValue not deterministic: %q != %q", v1, v2)
	}
}

func TestPasswordCookieValue_DiffersOnTokenChange(t *testing.T) {
	fwd := &forward{id: "x", httpPassword: "pw"}
	v1 := passwordCookieValue("token1", fwd)
	v2 := passwordCookieValue("token2", fwd)
	if v1 == v2 {
		t.Error("passwordCookieValue should differ when token changes")
	}
}
