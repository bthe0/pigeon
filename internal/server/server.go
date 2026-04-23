// Package server implements the public-facing tunnel server. It accepts
// yamux-multiplexed connections from pigeon clients, registers per-client
// forwards (HTTP subdomains or TCP/UDP ports), and reverse-proxies public
// traffic back through the matching client stream.
//
// The package is split into separate files by concern:
//
//	server.go     — config, Server struct, lifecycle (New/Start)
//	control.go    — yamux control plane: auth + forward registration
//	http.go       — public HTTP/HTTPS reverse proxy
//	listeners.go  — TCP/UDP port listeners for non-HTTP forwards
//	password.go   — per-tunnel password auth + rate limiting
//	pages.go      — HTML templates for status + password prompt pages
//	log.go        — traffic logging
//	visitor_enrich.go — request geo/browser enrichment
package server

import (
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config holds server configuration.
type Config struct {
	ControlAddr string // e.g. ":2222"
	HTTPAddr    string // e.g. ":80"
	HTTPSAddr   string // e.g. ":443"
	Token       string
	Domain      string // base domain for auto subdomains, e.g. "tun.example.com"
	CertDir     string // directory to store ACME certs
	CertFile    string // path to TLS cert PEM (overrides ACME)
	KeyFile     string // path to TLS key PEM (overrides ACME)
	LogFile     string // path to traffic log file, "" = stdout

	// TrustedProxyCIDRs lists CIDRs whose X-Forwarded-Proto header is trusted
	// when determining whether a request arrived over TLS. Requests from any
	// other source are only considered secure when r.TLS is set.
	TrustedProxyCIDRs []string

	// OnForwardRegistered is called whenever an HTTP tunnel domain is registered.
	// Used in local-dev mode to add /etc/hosts entries dynamically.
	OnForwardRegistered func(domain string)
}

// Server is the pigeon tunnel server.
type Server struct {
	cfg            Config
	sessions       sync.Map // domain/port-key → *session
	forwards       sync.Map // forward id → *forward
	logger         *log.Logger
	logFile        io.WriteCloser
	geoCache       sync.Map // ip -> geoInfo
	geoPauseUntil  atomic.Int64
	passwordFails  sync.Map // "fwdID:ip" -> *passwordRateEntry (tunnel password)
	authFails      sync.Map // source IP -> *passwordRateEntry (control-plane auth)
	trustedProxies []*net.IPNet
}

// New creates a new Server.
func New(cfg Config) *Server {
	s := &Server{cfg: cfg}

	var w io.Writer = os.Stdout
	if cfg.LogFile != "" {
		f, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err == nil {
			s.logFile = f
			w = f
		}
	}
	s.logger = log.New(w, "", 0)

	for _, c := range cfg.TrustedProxyCIDRs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, n, err := net.ParseCIDR(c); err == nil {
			s.trustedProxies = append(s.trustedProxies, n)
		} else if ip := net.ParseIP(c); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			mask := net.CIDRMask(bits, bits)
			s.trustedProxies = append(s.trustedProxies, &net.IPNet{IP: ip, Mask: mask})
		} else {
			log.Printf("warning: ignoring invalid TrustedProxyCIDR %q", c)
		}
	}
	return s
}

// Start starts all listeners. Blocks until one of them returns an error.
func (s *Server) Start() error {
	errCh := make(chan error, 3)

	go s.cleanupLoop()
	go func() { errCh <- s.serveControl() }()
	go func() { errCh <- s.serveHTTP() }()
	if s.cfg.HTTPSAddr != "" && s.cfg.Domain != "" {
		go func() { errCh <- s.serveHTTPS() }()
	}

	return <-errCh
}

// cleanupLoop periodically prunes stale rate-limit entries so the maps don't
// grow unbounded. Stale = no activity within passwordLockDuration.
func (s *Server) cleanupLoop() {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for range t.C {
		cutoff := time.Now().Add(-passwordLockDuration)
		prune := func(m *sync.Map) {
			m.Range(func(k, v any) bool {
				e := v.(*passwordRateEntry)
				e.mu.Lock()
				stale := !e.lastSeen.IsZero() && e.lastSeen.Before(cutoff)
				e.mu.Unlock()
				if stale {
					m.Delete(k)
				}
				return true
			})
		}
		prune(&s.passwordFails)
		prune(&s.authFails)
	}
}

