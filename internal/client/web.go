package client

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

func sessionValue(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}

// configStore caches the active config in memory so every API request doesn't
// re-read ~/.pigeon/config.json from disk. Saves go through Update which
// persists to disk and refreshes the cached copy atomically.
//
// The store tolerates a missing config file — cfg is simply nil until either
// an Update call writes one or the user runs `pigeon init`. Handlers must
// nil-check before dereferencing.
type configStore struct {
	mu  sync.RWMutex
	cfg *Config
}

func newConfigStore() *configStore {
	s := &configStore{}
	if cfg, err := LoadConfig(); err == nil {
		s.cfg = cfg
	}
	return s
}

func (s *configStore) Load() *Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// Update mutates the cached config via fn, persists to disk, and replaces the
// cached pointer. If no config is loaded yet, fn runs against an empty
// Config — letting the first Update initialise the file.
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

func StartWebInterface(addr string, openBrowser bool) error {
	store := newConfigStore()

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

	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			cfg := store.Load()
			if cfg == nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			if cookie, err := r.Cookie("pigeon_session"); err == nil {
				if cookie.Value == sessionValue(cfg.DashboardPassword) {
					h(w, r)
					return
				}
			}

			token := r.Header.Get("Authorization")
			if strings.HasPrefix(token, "Bearer ") {
				token = strings.TrimPrefix(token, "Bearer ")
			}
			if token != "" && token == cfg.Token {
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
		if p.Password == "" || p.Password != cfg.DashboardPassword {
			http.Error(w, "invalid password", http.StatusUnauthorized)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "pigeon_session",
			Value:    sessionValue(cfg.DashboardPassword),
			Path:     "/",
			HttpOnly: true,
			Secure:   requestIsSecure(r),
			MaxAge:   86400 * 30, // 30 days
			SameSite: http.SameSiteLaxMode,
		})
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})))

	mux.HandleFunc("/api/logout", noCache(allowMethod(http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("/api/logs", auth(noCache(allowMethod(http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		filter := r.URL.Query().Get("filter")
		logs, err := FetchRecentLogs(filter, 100)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(logs)
	}))))

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
		DaemonReload()
		w.WriteHeader(http.StatusOK)
	})))

	mux.HandleFunc("/api/restart", auth(noCache(allowMethod(http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		_ = DaemonStop()
		time.Sleep(500 * time.Millisecond)
		if err := DaemonStart(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
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
