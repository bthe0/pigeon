package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type passwordRateEntry struct {
	mu          sync.Mutex
	count       int
	lockedUntil time.Time
	lastSeen    time.Time
}

// cookieSigningSecret is the HMAC key for tunnel-password cookies. A fresh
// random value is generated at process startup so all previously-issued cookies
// become invalid on server restart. Captured cookies no longer grant permanent
// access the way the old sha256(token:id:pwd) scheme did.
var cookieSigningSecret = func() []byte {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Extremely unlikely; fail loudly rather than returning a predictable key.
		panic(fmt.Sprintf("cookie secret: %v", err))
	}
	return b[:]
}()

// passwordCookieMaxAge bounds how long a tunnel password cookie stays valid
// after it was issued, independent of the browser's cookie expiry.
const passwordCookieMaxAge = 24 * time.Hour

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
	if _, pass, ok := r.BasicAuth(); ok &&
		subtle.ConstantTimeCompare([]byte(pass), []byte(fwd.httpPassword)) == 1 {
		return true
	}
	if cookie, err := r.Cookie(passwordCookieName(fwd)); err == nil &&
		passwordCookieValidate(cookie.Value, s.cfg.Token, fwd, time.Now()) {
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
			case subtle.ConstantTimeCompare([]byte(password), []byte(fwd.httpPassword)) != 1:
				s.recordPasswordFail(rateKey)
				errorMessage = "Incorrect password."
			default:
				s.clearPasswordFails(rateKey)
				http.SetCookie(w, &http.Cookie{
					Name:     passwordCookieName(fwd),
					Value:    passwordCookieIssue(s.cfg.Token, fwd, time.Now()),
					Path:     "/",
					HttpOnly: true,
					Secure:   s.requestIsSecure(r),
					MaxAge:   int(passwordCookieMaxAge / time.Second),
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

// passwordCookieIssue returns a freshly minted, time-bounded cookie value.
// Format: "<unix-timestamp>.<hex(hmac-sha256(secret, ts|token|id|pwd))>".
// Validating against the ambient cookieSigningSecret means:
//   - Captured cookies expire (see passwordCookieMaxAge).
//   - Cookies do not survive a server restart (the secret is regenerated).
//   - Rotating the server token or the tunnel password invalidates prior cookies.
func passwordCookieIssue(token string, fwd *forward, now time.Time) string {
	ts := strconv.FormatInt(now.Unix(), 10)
	mac := passwordCookieMAC(ts, token, fwd)
	return ts + "." + hex.EncodeToString(mac)
}

// passwordCookieValidate parses and verifies a cookie issued by
// passwordCookieIssue against the current signing secret and freshness bound.
func passwordCookieValidate(value, token string, fwd *forward, now time.Time) bool {
	dot := strings.IndexByte(value, '.')
	if dot <= 0 || dot == len(value)-1 {
		return false
	}
	ts := value[:dot]
	sigHex := value[dot+1:]
	issued, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	// Reject future-dated and long-stale timestamps.
	age := now.Sub(time.Unix(issued, 0))
	if age < -time.Minute || age > passwordCookieMaxAge {
		return false
	}
	provided, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	expected := passwordCookieMAC(ts, token, fwd)
	return hmac.Equal(provided, expected)
}

func passwordCookieMAC(ts, token string, fwd *forward) []byte {
	h := hmac.New(sha256.New, cookieSigningSecret)
	// Separator bytes chosen to avoid ambiguity between fields that can
	// legitimately contain colons (forward id is random lowercase, but token
	// and password are user-supplied).
	h.Write([]byte(ts))
	h.Write([]byte{0})
	h.Write([]byte(token))
	h.Write([]byte{0})
	h.Write([]byte(fwd.id))
	h.Write([]byte{0})
	h.Write([]byte(fwd.httpPassword))
	return h.Sum(nil)
}
