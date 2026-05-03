package client

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"embed"

	"github.com/bthe0/pigeon/internal/proto"
)

//go:embed web/dist/*
var webFS embed.FS

// AgentVersion is set at build time or by main before starting the web interface.
var AgentVersion = "dev"

func OpenBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		fmt.Printf("Error opening browser: %v\n", err)
	}
}

// sessionIdleTimeout bounds how long a dashboard session survives between
// requests. The session's lastSeen is refreshed on every authed hit.
const sessionIdleTimeout = 30 * 24 * time.Hour

// sessionStore holds dashboard session IDs in memory. Session IDs are random
// 32-byte values (hex-encoded) issued on successful login; they do not derive
// from the password, so rotating the password, logging out, or restarting the
// daemon all invalidate outstanding cookies. The previous implementation used
// sha256(password) — a cookie that never changed until the password did —
// which meant a captured cookie was equivalent to permanent access.
type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]time.Time // id → lastSeen
}

func newSessionStore() *sessionStore {
	s := &sessionStore{sessions: make(map[string]time.Time)}
	go s.gcLoop()
	return s
}

func (s *sessionStore) gcLoop() {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for range t.C {
		cutoff := time.Now().Add(-sessionIdleTimeout)
		s.mu.Lock()
		for id, seen := range s.sessions {
			if seen.Before(cutoff) {
				delete(s.sessions, id)
			}
		}
		s.mu.Unlock()
	}
}

func (s *sessionStore) issue() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(buf[:])
	s.mu.Lock()
	s.sessions[id] = time.Now()
	s.mu.Unlock()
	return id, nil
}

// validate reports whether id names a live session and, as a side effect,
// bumps its lastSeen timestamp so active use extends the idle window.
// Constant-time lookup is not required here because the sync-map key is
// the caller-supplied cookie and `map` lookup already runs in constant
// time over key length; the secret isn't the id itself but the presence of
// a matching row in the map.
func (s *sessionStore) validate(id string) bool {
	if id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	seen, ok := s.sessions[id]
	if !ok {
		return false
	}
	if time.Since(seen) > sessionIdleTimeout {
		delete(s.sessions, id)
		return false
	}
	s.sessions[id] = time.Now()
	return true
}

func (s *sessionStore) revoke(id string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

// configStore caches the active config in memory and auto-refreshes on an
// mtime change so writes made by the daemon (via OnAddr → SaveConfig) become
// visible to the web handlers without a server restart.
//
// The store tolerates a missing config file — cfg is nil until either an
// Update call writes one or the user runs `pigeon init`. Handlers must
// nil-check before dereferencing.
type configStore struct {
	mu    sync.RWMutex
	cfg   *Config
	mtime time.Time
}

func newConfigStore() *configStore {
	s := &configStore{}
	s.Load() // populate cfg + mtime if the file exists
	return s
}

// Load returns the active config. If the file on disk has a newer mtime than
// the cached copy, the cache is refreshed before returning. This keeps the
// web UI in sync with daemon-side writes (server-assigned public addresses,
// in particular) without requiring an explicit plumb.
func (s *configStore) Load() *Config {
	path, err := ConfigPath()
	if err != nil {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.cfg
	}

	info, statErr := os.Stat(path)
	s.mu.RLock()
	cached := s.cfg
	cachedMtime := s.mtime
	s.mu.RUnlock()

	if statErr != nil {
		return cached
	}
	if cached != nil && !info.ModTime().After(cachedMtime) {
		return cached
	}

	cfg, err := LoadConfig()
	if err != nil {
		return cached
	}
	s.mu.Lock()
	s.cfg = cfg
	s.mtime = info.ModTime()
	s.mu.Unlock()
	return cfg
}

// Update mutates the cached config via fn, persists to disk, and replaces the
// cached pointer. If no config is loaded yet, fn runs against an empty
// Config — letting the first Update initialise the file. The mtime is bumped
// so the next Load sees the fresh copy without re-reading disk.
func (s *configStore) Update(fn func(*Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var working Config
	if s.cfg != nil {
		working = *s.cfg
		working.Forwards = append([]ForwardRule(nil), s.cfg.Forwards...)
	}

	if err := fn(&working); err != nil {
		return err
	}
	if err := SaveConfig(&working); err != nil {
		return err
	}
	s.cfg = &working
	if path, perr := ConfigPath(); perr == nil {
		if info, serr := os.Stat(path); serr == nil {
			s.mtime = info.ModTime()
		}
	}
	return nil
}

// allowMethod returns 405 for any method other than `method` and otherwise
// delegates to h. Used for single-method endpoints where relying on Go 1.22
// method-aware routing would let the "/" file-server catch-all swallow
// mismatches as 404.
func allowMethod(method string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h(w, r)
	}
}

// bindIsLoopback reports whether addr ("host:port") is bound to a loopback
// interface. An empty or wildcard host (":8080", "0.0.0.0:8080", "[::]:8080")
// counts as non-loopback since the dashboard becomes reachable beyond the
// local machine.
func bindIsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return host == "localhost"
}

func StartWebInterface(addr string, openBrowser bool) error {
	store := newConfigStore()
	sessions := newSessionStore()

	if !bindIsLoopback(addr) {
		log.Printf("WARNING: web dashboard bound to %s — session cookie and API token traverse plaintext HTTP over the network. Bind to 127.0.0.1 or front with TLS.", addr)
	}

	mux := http.NewServeMux()
	var handler http.Handler

	localDist := filepath.Join("internal", "client", "web", "dist")
	if _, err := os.Stat(localDist); err == nil {
		fmt.Printf("Serving web interface from local folder: %s\n", localDist)
		handler = http.FileServer(http.Dir(localDist))
	} else {
		subFS, err := fs.Sub(webFS, "web/dist")
		if err != nil {
			return err
		}
		handler = http.FileServer(http.FS(subFS))
	}

	mux.Handle("/", handler)

	// stateChanging reports whether the method can mutate server state. We
	// treat anything other than GET/HEAD/OPTIONS as potentially dangerous and
	// require a CSRF header for cookie-authenticated callers.
	stateChanging := func(method string) bool {
		switch method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			return false
		}
		return true
	}

	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			cfg := store.Load()
			if cfg == nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			if cookie, err := r.Cookie("pigeon_session"); err == nil {
				if sessions.validate(cookie.Value) {
					// Session-cookie callers must include a custom header on
					// state-changing requests. Browsers cannot set arbitrary
					// headers cross-origin without a CORS preflight (which
					// this server does not answer), so this blocks the
					// classic form-POST CSRF path even if SameSite enforcement
					// lapses.
					if stateChanging(r.Method) && r.Header.Get("X-Pigeon-CSRF") == "" {
						http.Error(w, "missing X-Pigeon-CSRF header", http.StatusForbidden)
						return
					}
					h(w, r)
					return
				}
			}

			token := r.Header.Get("Authorization")
			if strings.HasPrefix(token, "Bearer ") {
				token = strings.TrimPrefix(token, "Bearer ")
			}
			if token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(cfg.Token)) == 1 {
				// Bearer-token callers are non-browser; cross-site cookie-ride
				// attacks don't apply, so we don't force the CSRF header on
				// them.
				h(w, r)
				return
			}

			http.Error(w, "unauthorized", http.StatusUnauthorized)
		}
	}

	noCache := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			h(w, r)
		}
	}

	// ── API routes ───────────────────────────────────────────────────────────
	// Single-method endpoints use allowMethod to emit proper 405s. Multi-method
	// endpoints (/api/forwards, /api/forwards/{id}) use Go 1.22 method-aware
	// patterns — the mux returns 405 automatically when the path is known but
	// the method isn't.

	mux.HandleFunc("/api/config", auth(noCache(allowMethod(http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		cfg := store.Load()
		forwards := append([]ForwardRule(nil), cfg.Forwards...)
		if metrics, _ := GetMetrics(); metrics != nil {
			for i := range forwards {
				if m, ok := metrics[forwards[i].ID]; ok {
					forwards[i].RequestCount = m.Requests
					forwards[i].ByteCount = m.Bytes
				}
			}
		}
		resp := map[string]interface{}{
			"server":      cfg.Server,
			"token":       cfg.Token,
			"local_dev":   cfg.LocalDev,
			"base_domain": cfg.BaseDomain,
			"web_addr":    cfg.WebAddr,
			"forwards":    forwards,
			"version":     AgentVersion,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))))

	mux.HandleFunc("/api/login", noCache(allowMethod(http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		cfg := store.Load()
		if cfg == nil {
			http.Error(w, "not initialised", http.StatusInternalServerError)
			return
		}
		if cfg.DashboardPassword == "" ||
			subtle.ConstantTimeCompare([]byte(p.Password), []byte(cfg.DashboardPassword)) != 1 {
			log.Printf("dashboard login rejected from %s", r.RemoteAddr)
			http.Error(w, "invalid password", http.StatusUnauthorized)
			return
		}
		sid, err := sessions.issue()
		if err != nil {
			http.Error(w, "session error", http.StatusInternalServerError)
			return
		}
		log.Printf("dashboard login accepted from %s", r.RemoteAddr)

		http.SetCookie(w, &http.Cookie{
			Name:     "pigeon_session",
			Value:    sid,
			Path:     "/",
			HttpOnly: true,
			Secure:   requestIsSecure(r),
			MaxAge:   int(sessionIdleTimeout / time.Second),
			SameSite: http.SameSiteLaxMode,
		})
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})))

	mux.HandleFunc("/api/logout", noCache(allowMethod(http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie("pigeon_session"); err == nil {
			sessions.revoke(cookie.Value)
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "pigeon_session",
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   requestIsSecure(r),
			MaxAge:   -1,
			SameSite: http.SameSiteLaxMode,
		})
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})))

	mux.HandleFunc("GET /api/logs", auth(noCache(func(w http.ResponseWriter, r *http.Request) {
		filter := r.URL.Query().Get("filter")
		logs, err := FetchRecentLogs(filter, 100)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(logs)
	})))

	mux.HandleFunc("DELETE /api/logs", auth(noCache(func(w http.ResponseWriter, r *http.Request) {
		if err := ClearLogs(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("logs cleared via dashboard")
		w.WriteHeader(http.StatusNoContent)
	})))

	mux.HandleFunc("/api/inspector", auth(noCache(allowMethod(http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		filter := r.URL.Query().Get("filter")
		entries, err := FetchRecentInspectorEntries(100, filter)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries)
	}))))

	mux.HandleFunc("/api/inspector/replay", auth(noCache(allowMethod(http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		handleReplay(w, r, store)
	}))))

	// /api/forwards supports POST; /api/forwards/{id} supports DELETE and PUT.
	// Register both with method-aware patterns so the mux auto-emits 405 for
	// unsupported methods.
	mux.HandleFunc("POST /api/forwards", auth(noCache(func(w http.ResponseWriter, r *http.Request) {
		var rule ForwardRule
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if rule.ID == "" {
			rule.ID = proto.RandomID(8)
		}
		if err := store.Update(func(cfg *Config) error { return cfg.AddForward(rule) }); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		log.Printf("forward added via web: %s %s → %s", rule.ID, rule.Protocol, rule.LocalAddr)
		DaemonReload()
		w.WriteHeader(http.StatusCreated)
	})))

	mux.HandleFunc("DELETE /api/forwards/{id}", auth(noCache(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		err := store.Update(func(cfg *Config) error {
			if !cfg.RemoveForward(id) {
				return fmt.Errorf("not found")
			}
			return nil
		})
		if err != nil {
			status := http.StatusInternalServerError
			if err.Error() == "not found" {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}
		log.Printf("forward removed via web: %s", id)
		DaemonReload()
		w.WriteHeader(http.StatusOK)
	})))

	mux.HandleFunc("PUT /api/forwards/{id}", auth(noCache(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var rule ForwardRule
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		err := store.Update(func(cfg *Config) error {
			// Preserve server-assigned public addr and existing expose mode when not provided.
			for _, f := range cfg.Forwards {
				if f.ID == id {
					rule.PublicAddr = f.PublicAddr
					if rule.Expose == "" {
						rule.Expose = f.Expose
					}
					break
				}
			}
			return cfg.UpdateForward(id, rule)
		})
		if err != nil {
			status := http.StatusInternalServerError
			if err.Error() == "forward not found" {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}
		log.Printf("forward updated via web: %s (%s → %s)", id, rule.Protocol, rule.LocalAddr)
		DaemonReload()
		w.WriteHeader(http.StatusOK)
	})))

	mux.HandleFunc("/api/restart", auth(noCache(allowMethod(http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		exe, err := os.Executable()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// The web server runs inside this daemon, so a synchronous stop+start
		// kills our own response mid-flight — the dashboard sees "Failed to
		// fetch". Spawn a detached helper that waits long enough for the 200
		// to flush, then bounces us and starts a fresh daemon. Setsid keeps
		// the helper alive after we exit.
		script := fmt.Sprintf("sleep 1; %q daemon stop; sleep 1; %q daemon start", exe, exe)
		cmd := exec.Command("/bin/sh", "-c", script)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("daemon restart scheduled via dashboard")
		w.WriteHeader(http.StatusAccepted)
	}))))

	// Daemon tunneling state: pause keeps the dashboard alive but stops the
	// reconnect loop; resume kicks it back into Connect. Distinct from
	// /api/restart which bounces the entire process.
	mux.HandleFunc("GET /api/daemon/state", auth(noCache(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"paused": DaemonIsPaused()})
	})))
	mux.HandleFunc("POST /api/daemon/stop", auth(noCache(func(w http.ResponseWriter, r *http.Request) {
		DaemonPause()
		log.Printf("tunneling paused via dashboard")
		w.WriteHeader(http.StatusOK)
	})))
	mux.HandleFunc("POST /api/daemon/start", auth(noCache(func(w http.ResponseWriter, r *http.Request) {
		DaemonResume()
		log.Printf("tunneling resumed via dashboard")
		w.WriteHeader(http.StatusOK)
	})))

	mux.HandleFunc("/api/token/validate", auth(noCache(allowMethod(http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		cfg := store.Load()
		if cfg == nil {
			http.Error(w, "not initialised", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := ValidateToken(cfg); err != nil {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))))

	mux.HandleFunc("/api/config/export", auth(noCache(allowMethod(http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		cfg := store.Load()
		if cfg == nil {
			http.Error(w, "not initialised", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="pigeon-config.json"`)
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(cfg)
	}))))

	mux.HandleFunc("/api/config/import", auth(noCache(allowMethod(http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			http.Error(w, "could not read body", http.StatusBadRequest)
			return
		}
		var incoming Config
		if err := json.Unmarshal(body, &incoming); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if incoming.Server == "" {
			http.Error(w, "config missing required field: server", http.StatusBadRequest)
			return
		}
		if incoming.Token == "" {
			http.Error(w, "config missing required field: token", http.StatusBadRequest)
			return
		}
		if err := store.Update(func(cfg *Config) error {
			*cfg = incoming
			return nil
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("config imported via dashboard (%d forwards)", len(incoming.Forwards))
		DaemonReload()
		w.WriteHeader(http.StatusOK)
	}))))

	fmt.Printf("Web interface running on http://%s\n", addr)
	if openBrowser {
		go func() {
			time.Sleep(500 * time.Millisecond)
			OpenBrowser("http://" + addr)
		}()
	}

	return http.ListenAndServe(addr, mux)
}

func requestIsSecure(r *http.Request) bool {
	return r.TLS != nil
}

// replayRequest is the dashboard's POST body. It mirrors the inspector entry
// fields the user wants to fire again. Domain identifies the target tunnel;
// the request loops back through the public URL so it shows up in the
// inspector again (and exercises any auth / IP rules in front of the tunnel).
type replayRequest struct {
	ForwardID    string            `json:"forward_id"`
	Domain       string            `json:"domain"`
	Method       string            `json:"method"`
	Path         string            `json:"path"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         string            `json:"body,omitempty"`
	BodyEncoding string            `json:"body_encoding,omitempty"`
	HTTPPassword string            `json:"http_password,omitempty"` // optional override for password-protected tunnels
}

type replayResponse struct {
	Status      int               `json:"status"`
	DurationMs  int               `json:"duration_ms"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        string            `json:"body,omitempty"`
	Truncated   bool              `json:"truncated,omitempty"`
	Error       string            `json:"error,omitempty"`
}

func handleReplay(w http.ResponseWriter, r *http.Request, store *configStore) {
	var p replayRequest
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if p.Method == "" || p.Path == "" {
		http.Error(w, "method and path required", http.StatusBadRequest)
		return
	}

	// Locate the tunnel so we can resolve its public URL and figure out
	// whether to dial https or http. Replays must target a known forward —
	// firing arbitrary requests through the dashboard would turn it into an
	// open proxy.
	cfg := store.Load()
	if cfg == nil {
		http.Error(w, "not initialised", http.StatusInternalServerError)
		return
	}
	rule := findReplayTarget(cfg, p)
	if rule == nil {
		http.Error(w, "tunnel not found", http.StatusNotFound)
		return
	}
	publicHost := rule.PublicAddr
	if publicHost == "" {
		publicHost = rule.Domain
	}
	if publicHost == "" {
		http.Error(w, "tunnel has no public address yet", http.StatusConflict)
		return
	}
	// Wildcard tunnels (*.foo.example.com) need a concrete host. Use the
	// caller-supplied Domain when it matches the wildcard suffix; otherwise
	// substitute "replay" as the leading label.
	if strings.HasPrefix(publicHost, "*.") {
		suffix := strings.TrimPrefix(publicHost, "*.")
		if p.Domain != "" && strings.HasSuffix(p.Domain, "."+suffix) {
			publicHost = p.Domain
		} else {
			publicHost = "replay." + suffix
		}
	}

	scheme := "https"
	if rule.Expose == "http" {
		scheme = "http"
	}
	url := scheme + "://" + publicHost + p.Path

	var body io.Reader
	if p.Body != "" {
		switch p.BodyEncoding {
		case "base64":
			b, err := base64.StdEncoding.DecodeString(p.Body)
			if err != nil {
				http.Error(w, "invalid base64 body", http.StatusBadRequest)
				return
			}
			body = bytes.NewReader(b)
		default:
			body = strings.NewReader(p.Body)
		}
	}

	req, err := http.NewRequestWithContext(r.Context(), p.Method, url, body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for k, v := range p.Headers {
		// These hop-by-hop / framing headers must reflect the new transport,
		// not the captured ones — leaving them in causes content-length
		// mismatches and TE conflicts.
		switch strings.ToLower(k) {
		case "host", "content-length", "transfer-encoding", "connection":
			continue
		}
		req.Header.Set(k, v)
	}
	if p.HTTPPassword != "" {
		req.SetBasicAuth("pigeon", p.HTTPPassword)
	}

	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.LocalDev}} //nolint:gosec
	client := &http.Client{Timeout: 30 * time.Second, Transport: tr}

	start := time.Now()
	resp, err := client.Do(req)
	dur := int(time.Since(start).Milliseconds())
	if err != nil {
		json.NewEncoder(w).Encode(replayResponse{DurationMs: dur, Error: err.Error()})
		return
	}
	defer resp.Body.Close()

	const replayBodyCap = proto.MaxCapturedBodyBytes
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, replayBodyCap+1))
	truncated := false
	if len(bodyBytes) > replayBodyCap {
		bodyBytes = bodyBytes[:replayBodyCap]
		truncated = true
	}
	out := replayResponse{
		Status:     resp.StatusCode,
		DurationMs: dur,
		Headers:    make(map[string]string, len(resp.Header)),
		Truncated:  truncated,
	}
	for k, v := range resp.Header {
		out.Headers[k] = strings.Join(v, ", ")
	}
	out.Body = string(bodyBytes)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func findReplayTarget(cfg *Config, p replayRequest) *ForwardRule {
	for i := range cfg.Forwards {
		f := &cfg.Forwards[i]
		if !f.Protocol.IsHTTPLike() {
			continue
		}
		if p.ForwardID != "" && f.ID == p.ForwardID {
			return f
		}
		if p.Domain == "" {
			continue
		}
		if f.PublicAddr == p.Domain || f.Domain == p.Domain {
			return f
		}
		// Wildcard match: *.suffix matches one-label-deep host like leaf.suffix.
		host := strings.TrimPrefix(f.PublicAddr, "*.")
		if strings.HasPrefix(f.PublicAddr, "*.") && strings.HasSuffix(p.Domain, "."+host) {
			return f
		}
	}
	return nil
}

