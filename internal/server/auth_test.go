package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
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
	cookieValue := passwordCookieIssue(s.cfg.Token, fwd, time.Now())

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

	t.Run("stale cookie rejected", func(t *testing.T) {
		stale := passwordCookieIssue(s.cfg.Token, fwd, time.Now().Add(-2*passwordCookieMaxAge))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: stale})
		w := httptest.NewRecorder()
		if s.authorizeTunnelPassword(w, req, fwd) {
			t.Fatal("expected false for stale cookie")
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
			if !passwordCookieValidate(c.Value, s.cfg.Token, fwd, time.Now()) {
				t.Errorf("issued cookie failed validation: %q", c.Value)
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

func TestPasswordCookie_FreshlyIssuedValidates(t *testing.T) {
	fwd := &forward{id: "x", httpPassword: "pw"}
	now := time.Now()
	v := passwordCookieIssue("token", fwd, now)
	if !passwordCookieValidate(v, "token", fwd, now) {
		t.Errorf("freshly-issued cookie should validate; got rejected: %q", v)
	}
}

func TestPasswordCookie_RejectsOnTokenChange(t *testing.T) {
	fwd := &forward{id: "x", httpPassword: "pw"}
	now := time.Now()
	v := passwordCookieIssue("token1", fwd, now)
	if passwordCookieValidate(v, "token2", fwd, now) {
		t.Error("cookie signed under token1 should not validate under token2")
	}
}

func TestPasswordCookie_RejectsOnPasswordChange(t *testing.T) {
	now := time.Now()
	v := passwordCookieIssue("t", &forward{id: "x", httpPassword: "old"}, now)
	if passwordCookieValidate(v, "t", &forward{id: "x", httpPassword: "new"}, now) {
		t.Error("cookie signed under old password should not validate under new")
	}
}

func TestPasswordCookie_ExpiresAfterMaxAge(t *testing.T) {
	fwd := &forward{id: "x", httpPassword: "pw"}
	past := time.Now().Add(-passwordCookieMaxAge - time.Minute)
	v := passwordCookieIssue("token", fwd, past)
	if passwordCookieValidate(v, "token", fwd, time.Now()) {
		t.Error("cookie older than passwordCookieMaxAge should be rejected")
	}
}
