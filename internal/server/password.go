package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"sync"
	"time"
)

type passwordRateEntry struct {
	mu          sync.Mutex
	count       int
	lockedUntil time.Time
	lastSeen    time.Time
}

const (
	passwordMaxFails     = 10
	passwordLockDuration = 15 * time.Minute

	// Control-plane auth limits — more generous than tunnel password limits
	// because legitimate reconnect storms during network flaps should not lock
	// a client out, but sustained token guessing must still hit a wall.
	authMaxFails     = 20
	authLockDuration = 15 * time.Minute
)

// recordFail atomically increments a failure counter for key in m. When the
// counter reaches maxFails the entry is locked for lockDur from the current
// time; subsequent increments extend the lock only if they push count past
// maxFails again after a reset.
func recordFail(m *sync.Map, key string, maxFails int, lockDur time.Duration) {
	v, _ := m.LoadOrStore(key, &passwordRateEntry{})
	e := v.(*passwordRateEntry)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.count++
	e.lastSeen = time.Now()
	if e.count >= maxFails {
		e.lockedUntil = time.Now().Add(lockDur)
	}
}

func (s *Server) isPasswordRateLimited(key string) bool {
	v, _ := s.passwordFails.LoadOrStore(key, &passwordRateEntry{})
	e := v.(*passwordRateEntry)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastSeen = time.Now()
	return e.count >= passwordMaxFails && time.Now().Before(e.lockedUntil)
}

func (s *Server) recordPasswordFail(key string) {
	recordFail(&s.passwordFails, key, passwordMaxFails, passwordLockDuration)
}

func (s *Server) clearPasswordFails(key string) {
	s.passwordFails.Delete(key)
}

func (s *Server) isAuthRateLimited(ip string) bool {
	v, _ := s.authFails.LoadOrStore(ip, &passwordRateEntry{})
	e := v.(*passwordRateEntry)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastSeen = time.Now()
	return e.count >= authMaxFails && time.Now().Before(e.lockedUntil)
}

func (s *Server) recordAuthFail(ip string) {
	recordFail(&s.authFails, ip, authMaxFails, authLockDuration)
}

func (s *Server) clearAuthFails(ip string) {
	s.authFails.Delete(ip)
}

// authorizeTunnelPassword validates the per-tunnel HTTP password. It accepts
// either basic auth (for CLI/API callers) or a signed cookie set by a prior
// successful form login. On POST it validates a submitted password and
// either redirects with the cookie or re-renders the password page with an
// error message. Returns true when the request is authorized.
func (s *Server) authorizeTunnelPassword(w http.ResponseWriter, r *http.Request, fwd *forward) bool {
	if _, pass, ok := r.BasicAuth(); ok && pass == fwd.httpPassword {
		return true
	}
	if cookie, err := r.Cookie(passwordCookieName(fwd)); err == nil && cookie.Value == passwordCookieValue(s.cfg.Token, fwd) {
		return true
	}

	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	rateKey := fwd.id + ":" + ip

	var errorMessage string
	if r.Method == http.MethodPost {
		if s.isPasswordRateLimited(rateKey) {
			writePasswordPage(w, pageVariant(fwd.unavailablePage), "Password required", "This tunnel is protected. Enter the password to continue.", "Too many failed attempts. Try again later.")
			return false
		}
		if err := r.ParseForm(); err == nil {
			password := r.Form.Get("pigeon_password")
			switch {
			case password == "":
				errorMessage = "Password is required."
			case len(password) < 4:
				errorMessage = "Password must be at least 4 characters."
			case len(password) > 128:
				errorMessage = "Password must be 128 characters or fewer."
			case password != fwd.httpPassword:
				s.recordPasswordFail(rateKey)
				errorMessage = "Incorrect password."
			default:
				s.clearPasswordFails(rateKey)
				http.SetCookie(w, &http.Cookie{
					Name:     passwordCookieName(fwd),
					Value:    passwordCookieValue(s.cfg.Token, fwd),
					Path:     "/",
					HttpOnly: true,
					Secure:   s.requestIsSecure(r),
					SameSite: http.SameSiteLaxMode,
				})
				w.Header().Set("Cache-Control", "no-store")
				http.Redirect(w, r, r.URL.RequestURI(), http.StatusSeeOther)
				return false
			}
		}
	}

	writePasswordPage(w, pageVariant(fwd.unavailablePage), "Password required", "This tunnel is protected. Enter the password to continue.", errorMessage)
	return false
}

func passwordCookieName(fwd *forward) string {
	return "pigeon_auth_" + fwd.id
}

func passwordCookieValue(token string, fwd *forward) string {
	sum := sha256.Sum256([]byte(token + ":" + fwd.id + ":" + fwd.httpPassword))
	return hex.EncodeToString(sum[:])
}
