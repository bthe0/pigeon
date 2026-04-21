package client

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	_ "embed"

	"github.com/bthe0/pigeon/internal/proto"
)

//go:embed web_index.html
var indexHTML []byte

func randomID(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	rand.Read(b)
	for i := 0; i < n; i++ {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

func StartWebInterface(addr string) error {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write(indexHTML)
	})

	http.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		cfg, err := LoadConfig()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(cfg)
	})

	http.HandleFunc("/api/forwards", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Protocol   proto.Protocol `json:"protocol"`
			LocalAddr  string         `json:"local_addr"`
			Domain     string         `json:"domain"`
			RemotePort int            `json:"remote_port"`
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
			ID:         randomID(8),
			Protocol:   req.Protocol,
			LocalAddr:  req.LocalAddr,
			Domain:     req.Domain,
			RemotePort: req.RemotePort,
		}
		if err := cfg.AddForward(rule); err != nil {
			http.Error(w, string(err.Error()), http.StatusBadRequest)
			return
		}
		if err := SaveConfig(cfg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/api/forwards/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
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

		if !cfg.RemoveForward(id) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err := SaveConfig(cfg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
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

	fmt.Printf("Web interface running on http://%s\n", addr)
	return http.ListenAndServe(addr, nil)
}
