package client

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"embed"

	"github.com/bthe0/pigeon/internal/proto"
)

//go:embed web/dist/*
var webFS embed.FS

func randomID(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	rand.Read(b)
	for i := 0; i < n; i++ {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

// AgentVersion is set at build time or by main before starting the web interface.
var AgentVersion = "dev"

func openBrowser(url string) {
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

func StartWebInterface(addr string) error {
	var handler http.Handler

	// Try serving from local filesystem first (useful for dev)
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

	http.Handle("/", handler)

	noCache := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			h(w, r)
		}
	}

	http.HandleFunc("/api/config", noCache(func(w http.ResponseWriter, r *http.Request) {
		cfg, err := LoadConfig()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Inject real metrics from logs
		metrics, _ := GetMetrics()
		if metrics != nil {
			for i := range cfg.Forwards {
				if m, ok := metrics[cfg.Forwards[i].ID]; ok {
					cfg.Forwards[i].RequestCount = m.Requests
					cfg.Forwards[i].ByteCount = m.Bytes
				}
			}
		}

		type configResponse struct {
			*Config
			Version string `json:"version"`
		}
		json.NewEncoder(w).Encode(configResponse{Config: cfg, Version: AgentVersion})
	}))

	http.HandleFunc("/api/logs", noCache(func(w http.ResponseWriter, r *http.Request) {
		filter := r.URL.Query().Get("filter")
		logs, err := FetchRecentLogs(filter, 100)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(logs)
	}))

	http.HandleFunc("/api/inspector", noCache(func(w http.ResponseWriter, r *http.Request) {
		filter := r.URL.Query().Get("filter")
		entries, err := FetchRecentInspectorEntries(100, filter)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries)
	}))

	http.HandleFunc("/api/forwards", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Protocol       proto.Protocol `json:"protocol"`
			LocalAddr      string         `json:"local_addr"`
			Domain         string         `json:"domain"`
			RemotePort     int            `json:"remote_port"`
			Expose         string         `json:"expose"`
			HTTPPassword   string         `json:"http_password"`
			MaxConnections int            `json:"max_connections"`
			UnavailablePage string         `json:"unavailable_page"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		cfg, err := LoadConfig()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		rule := ForwardRule{
			ID:              randomID(8),
			Protocol:        req.Protocol,
			LocalAddr:       req.LocalAddr,
			Domain:          req.Domain,
			RemotePort:      req.RemotePort,
			Expose:          req.Expose,
			HTTPPassword:    req.HTTPPassword,
			MaxConnections:  req.MaxConnections,
			UnavailablePage: req.UnavailablePage,
		}
		if err := cfg.AddForward(rule); err != nil {
			http.Error(w, string(err.Error()), http.StatusBadRequest)
			return
		}
		if err := SaveConfig(cfg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		DaemonReload()
		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/api/forwards/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/api/forwards/"):]
		if id == "" {
			http.Error(w, "missing id", http.StatusBadRequest)
			return
		}

		cfg, err := LoadConfig()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

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
			rule.ID = id
			// preserve server-assigned public address across edits
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

		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	http.HandleFunc("/api/restart", func(w http.ResponseWriter, r *http.Request) {
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
	})

	url := fmt.Sprintf("http://%s", addr)
	fmt.Printf("Web interface running on %s\n", url)
	go func() {
		time.Sleep(500 * time.Millisecond)
		openBrowser(url)
	}()

	return http.ListenAndServe(addr, nil)
}

