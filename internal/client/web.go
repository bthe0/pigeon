package client

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"embed"
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

func StartWebInterface(addr string, openBrowser bool) error {
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

	// Auth middleware
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			cfg, err := LoadConfig()
			if err != nil {
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

	mux.HandleFunc("/api/config", auth(noCache(func(w http.ResponseWriter, r *http.Request) {
		cfg, err := LoadConfig()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		metrics, _ := GetMetrics()
		if metrics != nil {
			for i := range cfg.Forwards {
				if m, ok := metrics[cfg.Forwards[i].ID]; ok {
					cfg.Forwards[i].RequestCount = m.Requests
					cfg.Forwards[i].ByteCount = m.Bytes
				}
			}
		}

		resp := map[string]interface{}{
			"server":      cfg.Server,
			"local_dev":   cfg.LocalDev,
			"base_domain": cfg.BaseDomain,
			"web_addr":    cfg.WebAddr,
			"forwards":    cfg.Forwards,
			"version":     AgentVersion,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})))

	mux.HandleFunc("/api/login", noCache(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var p struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		cfg, err := LoadConfig()
		if err != nil {
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
	}))

	mux.HandleFunc("/api/logout", noCache(func(w http.ResponseWriter, r *http.Request) {
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
	}))

	mux.HandleFunc("/api/logs", auth(noCache(func(w http.ResponseWriter, r *http.Request) {
		filter := r.URL.Query().Get("filter")
		logs, err := FetchRecentLogs(filter, 100)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(logs)
	})))

	mux.HandleFunc("/api/inspector", auth(noCache(func(w http.ResponseWriter, r *http.Request) {
		filter := r.URL.Query().Get("filter")
		entries, err := FetchRecentInspectorEntries(100, filter)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries)
	})))

	mux.HandleFunc("/api/forwards", auth(noCache(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			var rule ForwardRule
			if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			cfg, _ := LoadConfig()
			if err := cfg.AddForward(rule); err != nil {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			if err := SaveConfig(cfg); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			DaemonReload()
			w.WriteHeader(http.StatusCreated)
			return
		}
	})))

	mux.HandleFunc("/api/forwards/", auth(noCache(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/forwards/")
		cfg, _ := LoadConfig()

		if r.Method == "DELETE" {
			if !cfg.RemoveForward(id) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if err := SaveConfig(cfg); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			DaemonReload()
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == "PUT" {
			var rule ForwardRule
			if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			for _, f := range cfg.Forwards {
				if f.ID == id {
					rule.PublicAddr = f.PublicAddr
					if rule.Expose == "" {
						rule.Expose = f.Expose
					}
					break
				}
			}
			if err := cfg.UpdateForward(id, rule); err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			if err := SaveConfig(cfg); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			DaemonReload()
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == "PATCH" {
			var patch map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			found := false
			for i := range cfg.Forwards {
				if cfg.Forwards[i].ID == id {
					if v, ok := patch["expose"].(string); ok {
						cfg.Forwards[i].Expose = v
					}
					if v, ok := patch["disabled"].(bool); ok {
						cfg.Forwards[i].Disabled = v
					}
					found = true
					break
				}
			}
			if !found {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if err := SaveConfig(cfg); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			DaemonReload()
			w.WriteHeader(http.StatusOK)
			return
		}
	})))

	mux.HandleFunc("/api/restart", auth(noCache(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_ = DaemonStop()
		time.Sleep(500 * time.Millisecond)
		if err := DaemonStart(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})))

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
	if r.TLS != nil {
		return true
	}
	if !strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
