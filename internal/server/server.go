package server

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bthe0/pigeon/internal/proto"
	"github.com/hashicorp/yamux"
	"golang.org/x/crypto/acme/autocert"
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

	// OnForwardRegistered is called whenever an HTTP tunnel domain is registered.
	// Used in local-dev mode to add /etc/hosts entries dynamically.
	OnForwardRegistered func(domain string)
}

type forward struct {
	id         string
	protocol   proto.Protocol
	localAddr  string
	publicAddr string
	domain     string
	port       int
	expose     string // "both" | "http" | "https"
	httpPassword string
	maxConnections int
	unavailablePage string
	activeConns atomic.Int32
	session    *session
}

type session struct {
	id       string
	mux      *yamux.Session
	ctrl     net.Conn
	forwards map[string]*forward // id → forward
	mu       sync.RWMutex
	writeMu  sync.Mutex
}

func (s *session) writeMessage(msg proto.Message) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return proto.Write(s.ctrl, msg)
}

// Server is the pigeon tunnel server.
type Server struct {
	cfg      Config
	sessions sync.Map   // domain/port-key → *session
	forwards sync.Map   // forward id → *forward
	logger   *log.Logger
	logFile  io.WriteCloser
	geoCache sync.Map // ip -> geoInfo
	geoPauseUntil atomic.Int64
}

// New creates a new Server.
func New(cfg Config) *Server {
	s := &Server{cfg: cfg}

	var w io.Writer = os.Stdout
	if cfg.LogFile != "" {
		f, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err == nil {
			s.logFile = f
			w = f
		}
	}
	s.logger = log.New(w, "", 0)
	return s
}

// Start starts all listeners.
func (s *Server) Start() error {
	errCh := make(chan error, 3)

	go func() { errCh <- s.serveControl() }()
	go func() { errCh <- s.serveHTTP() }()
	if s.cfg.HTTPSAddr != "" && s.cfg.Domain != "" {
		go func() { errCh <- s.serveHTTPS() }()
	}

	return <-errCh
}

// ── Control plane ──────────────────────────────────────────────────────────────

func (s *Server) serveControl() error {
	ln, err := net.Listen("tcp", s.cfg.ControlAddr)
	if err != nil {
		return fmt.Errorf("control listen %s: %w", s.cfg.ControlAddr, err)
	}
	log.Printf("Control listening on %s", s.cfg.ControlAddr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handleClient(conn)
	}
}

func (s *Server) handleClient(conn net.Conn) {
	defer conn.Close()

	mux, err := yamux.Server(conn, yamux.DefaultConfig())
	if err != nil {
		return
	}
	defer mux.Close()

	// First stream = control channel
	ctrl, err := mux.Accept()
	if err != nil {
		return
	}

	// Auth
	msg, err := proto.Read(ctrl)
	if err != nil || msg.Type != proto.MsgAuth {
		proto.Write(ctrl, proto.Message{Type: proto.MsgError, Payload: proto.ErrorPayload{Message: "expected auth"}})
		return
	}
	var auth proto.AuthPayload
	if err := proto.DecodePayload(msg, &auth); err != nil || auth.Token != s.cfg.Token {
		proto.Write(ctrl, proto.Message{Type: proto.MsgError, Payload: proto.ErrorPayload{Message: "invalid token"}})
		return
	}

	clientID := randomID(8)
	sess := &session{id: clientID, mux: mux, ctrl: ctrl, forwards: make(map[string]*forward)}
	sess.writeMessage(proto.Message{Type: proto.MsgAuthAck, Payload: proto.AuthAckPayload{ClientID: clientID}})
	log.Printf("[%s] client connected from %s", clientID, conn.RemoteAddr())

	defer func() {
		s.cleanupSession(sess)
		log.Printf("[%s] client disconnected", clientID)
	}()

	// Control loop
	for {
		msg, err := proto.Read(ctrl)
		if err != nil {
			return
		}
		switch msg.Type {
		case proto.MsgForwardAdd:
			var p proto.ForwardPayload
			if err := proto.DecodePayload(msg, &p); err != nil {
				sess.writeMessage(proto.Message{Type: proto.MsgError, Payload: proto.ErrorPayload{Message: err.Error()}})
				continue
			}
			publicAddr, err := s.registerForward(sess, &p)
			if err != nil {
				sess.writeMessage(proto.Message{Type: proto.MsgError, Payload: proto.ErrorPayload{Message: err.Error()}})
				continue
			}
			sess.writeMessage(proto.Message{Type: proto.MsgForwardAck, Payload: proto.ForwardAckPayload{ID: p.ID, PublicAddr: publicAddr}})

		case proto.MsgForwardRemove:
			var p proto.ForwardRemovePayload
			if err := proto.DecodePayload(msg, &p); err == nil {
				s.removeForward(sess, p.ID)
			}

		case proto.MsgPing:
			sess.writeMessage(proto.Message{Type: proto.MsgPong})
		}
	}
}

// ── Forward management ─────────────────────────────────────────────────────────

func (s *Server) registerForward(sess *session, p *proto.ForwardPayload) (string, error) {
	fwd := &forward{
		id:              p.ID,
		protocol:        p.Protocol,
		localAddr:       p.LocalAddr,
		session:         sess,
		domain:          p.Domain,
		port:            p.RemotePort,
		expose:          p.Expose,
		httpPassword:    p.HTTPPassword,
		maxConnections:  p.MaxConnections,
		unavailablePage: p.UnavailablePage,
	}

	switch p.Protocol {
	case proto.ProtoHTTP, proto.ProtoHTTPS:
		domain := p.Domain
		if domain == "" {
			domain = randomID(8) + "." + s.cfg.Domain
		}
		fwd.publicAddr = domain
		s.sessions.Store("http:"+domain, fwd)
		if s.cfg.OnForwardRegistered != nil {
			s.cfg.OnForwardRegistered(domain)
		}

	case proto.ProtoTCP, proto.ProtoUDP:
		port, err := s.openPort(fwd)
		if err != nil {
			return "", err
		}
		fwd.port = port
		fwd.publicAddr = fmt.Sprintf("%s:%d", s.cfg.Domain, port)
	}

	sess.mu.Lock()
	sess.forwards[p.ID] = fwd
	sess.mu.Unlock()
	s.forwards.Store(p.ID, fwd)

	log.Printf("[%s] forward %s %s → %s", sess.id, p.Protocol, fwd.publicAddr, p.LocalAddr)
	return fwd.publicAddr, nil
}

func (s *Server) removeForward(sess *session, id string) {
	sess.mu.Lock()
	fwd, ok := sess.forwards[id]
	delete(sess.forwards, id)
	sess.mu.Unlock()

	if ok {
		s.sessions.Delete("http:" + fwd.publicAddr)
		s.forwards.Delete(id)
		log.Printf("[%s] removed forward %s", sess.id, id)
	}
}

func (s *Server) cleanupSession(sess *session) {
	sess.mu.RLock()
	defer sess.mu.RUnlock()
	for _, fwd := range sess.forwards {
		s.sessions.Delete("http:" + fwd.publicAddr)
		s.forwards.Delete(fwd.id)
	}
}

func (f *forward) tryAcquire() bool {
	if f.maxConnections <= 0 {
		f.activeConns.Add(1)
		return true
	}
	for {
		current := f.activeConns.Load()
		if int(current) >= f.maxConnections {
			return false
		}
		if f.activeConns.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func (f *forward) release() {
	f.activeConns.Add(-1)
}

// ── Port listeners (TCP/UDP) ───────────────────────────────────────────────────

func (s *Server) openPort(fwd *forward) (int, error) {
	port := fwd.port

	switch fwd.protocol {
	case proto.ProtoTCP:
		addr := fmt.Sprintf(":%d", port)
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return 0, fmt.Errorf("tcp listen %s: %w", addr, err)
		}
		if port == 0 {
			port = ln.Addr().(*net.TCPAddr).Port
		}
		go s.serveTCP(ln, fwd)

	case proto.ProtoUDP:
		addr := fmt.Sprintf(":%d", port)
		pc, err := net.ListenPacket("udp", addr)
		if err != nil {
			return 0, fmt.Errorf("udp listen %s: %w", addr, err)
		}
		if port == 0 {
			port = pc.LocalAddr().(*net.UDPAddr).Port
		}
		go s.serveUDP(pc, fwd)
	}
	return port, nil
}

func (s *Server) serveTCP(ln net.Listener, fwd *forward) {
	defer ln.Close()
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			if !fwd.tryAcquire() {
				return
			}
			defer fwd.release()
			stream, err := fwd.session.mux.Open()
			if err != nil {
				return
			}
			defer stream.Close()
			if err := proto.WriteStreamHeader(stream, proto.StreamHeader{
				ForwardID:  fwd.id,
				RemoteAddr: conn.RemoteAddr().String(),
				Protocol:   proto.ProtoTCP,
			}); err != nil {
				return
			}
			s.logTraffic(fwd, conn.RemoteAddr().String(), "TCP", "CONNECT", 0)
			proxy(conn, stream)
		}()
	}
}

func (s *Server) serveUDP(pc net.PacketConn, fwd *forward) {
	defer pc.Close()
	// One persistent yamux stream per UDP forward for simplicity
	stream, err := fwd.session.mux.Open()
	if err != nil {
		return
	}
	defer stream.Close()
	if err := proto.WriteStreamHeader(stream, proto.StreamHeader{
		ForwardID: fwd.id,
		Protocol:  proto.ProtoUDP,
	}); err != nil {
		return
	}

	// Server → client: read datagrams, frame them
	go func() {
		buf := make([]byte, 65535)
		enc := json.NewEncoder(stream)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			enc.Encode(udpFrame{Addr: addr.String(), Data: buf[:n]})
			s.logTraffic(fwd, addr.String(), "UDP", "IN", n)
		}
	}()

	// Client → server: read framed datagrams, send them
	dec := json.NewDecoder(stream)
	for {
		var frame udpFrame
		if err := dec.Decode(&frame); err != nil {
			return
		}
		addr, err := net.ResolveUDPAddr("udp", frame.Addr)
		if err != nil {
			continue
		}
		pc.WriteTo(frame.Data, addr)
		s.logTraffic(fwd, frame.Addr, "UDP", "OUT", len(frame.Data))
	}
}

type udpFrame struct {
	Addr string `json:"addr"`
	Data []byte `json:"data"`
}

// ── HTTP serving ───────────────────────────────────────────────────────────────

func (s *Server) serveHTTP() error {
	log.Printf("HTTP listening on %s", s.cfg.HTTPAddr)
	return http.ListenAndServe(s.cfg.HTTPAddr, s)
}

func (s *Server) serveHTTPS() error {
	srv := &http.Server{
		Addr:    s.cfg.HTTPSAddr,
		Handler: s,
		// Disable HTTP/2: the backend yamux transport only speaks HTTP/1.1,
		// and the Go stdlib auto-negotiates HTTP/2 on TLS which breaks the proxy.
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}
	if s.cfg.CertFile != "" && s.cfg.KeyFile != "" {
		log.Printf("HTTPS listening on %s (self-signed)", s.cfg.HTTPSAddr)
		return srv.ListenAndServeTLS(s.cfg.CertFile, s.cfg.KeyFile)
	}
	m := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(s.cfg.Domain, "*."+s.cfg.Domain),
		Cache:      autocert.DirCache(s.cfg.CertDir),
	}
	srv.TLSConfig = &tls.Config{GetCertificate: m.GetCertificate}
	log.Printf("HTTPS listening on %s", s.cfg.HTTPSAddr)
	return srv.ListenAndServeTLS("", "")
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	v, ok := s.sessions.Load("http:" + host)
	if !ok {
		writeStatusPage(w, http.StatusBadGateway, "default", "tunnel not found", "No active tunnel is currently registered for "+host+".")
		return
	}
	fwd := v.(*forward)

	isTLS := r.TLS != nil
	switch fwd.expose {
	case "https":
		if !isTLS {
			http.Redirect(w, r, "https://"+host+r.RequestURI, http.StatusMovedPermanently)
			return
		}
	case "http":
		if isTLS {
			writeStatusPage(w, http.StatusNotFound, pageVariant(fwd.unavailablePage), "HTTPS disabled", "This tunnel is only available over plain HTTP.")
			return
		}
	}

	if fwd.httpPassword != "" {
		if !s.authorizeTunnelPassword(w, r, fwd) {
			return
		}
	}

	if !fwd.tryAcquire() {
		writeStatusPage(w, http.StatusTooManyRequests, pageVariant(fwd.unavailablePage), "Too many connections", "This tunnel reached its maximum number of active connections.")
		return
	}
	defer fwd.release()

	stream, err := fwd.session.mux.Open()
	if err != nil {
		writeStatusPage(w, http.StatusBadGateway, pageVariant(fwd.unavailablePage), "Tunnel unavailable", "The tunnel is online, but the upstream connection is currently unavailable.")
		return
	}
	defer stream.Close()

	if err := proto.WriteStreamHeader(stream, proto.StreamHeader{
		ForwardID:  fwd.id,
		RemoteAddr: r.RemoteAddr,
		Protocol:   proto.ProtoHTTP,
	}); err != nil {
		http.Error(w, "tunnel write error", http.StatusBadGateway)
		return
	}

	start := time.Now()
	requestHeaders := headerToMap(r.Header)
	clientAddr := clientIP(r.RemoteAddr, r.Header)
	browser, osName := browserAndOS(r.UserAgent())
	geo := s.lookupGeo(clientAddr)
	target, _ := url.Parse("http://" + host)
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.Transport = &connTransport{conn: stream}
	var responseHeaders map[string]string
	rp.ModifyResponse = func(resp *http.Response) error {
		responseHeaders = headerToMap(resp.Header)
		return nil
	}
	rp.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		writeStatusPage(rw, http.StatusBadGateway, pageVariant(fwd.unavailablePage), "Tunnel unavailable", "The tunnel could not reach the local service right now.")
	}
	cw := &captureWriter{ResponseWriter: w, status: http.StatusOK}
	rp.ServeHTTP(cw, r)

	fwd.session.writeMessage(proto.Message{
		Type: proto.MsgInspectorEvent,
		Payload: proto.InspectorEventPayload{
			Time:            time.Now().Format(time.RFC3339),
			ForwardID:       fwd.id,
			Domain:          fwd.publicAddr,
			RemoteAddr:      clientAddr,
			Method:          r.Method,
			Path:            r.URL.RequestURI(),
			Status:          cw.status,
			DurationMs:      int(time.Since(start).Milliseconds()),
			Bytes:           cw.bytes,
			City:            geo.City,
			Country:         geo.Country,
			CountryCode:     geo.CountryCode,
			Latitude:        geo.Latitude,
			Longitude:       geo.Longitude,
			Browser:         browser,
			OS:              osName,
			RequestHeaders:  requestHeaders,
			ResponseHeaders: responseHeaders,
		},
	})

	s.logTraffic(fwd, r.RemoteAddr, "HTTP", fmt.Sprintf("%s %s %d %dms", r.Method, r.URL.Path, cw.status, time.Since(start).Milliseconds()), cw.bytes)
}

// connTransport implements http.RoundTripper using an existing net.Conn.
type connTransport struct {
	conn net.Conn
}

func (t *connTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Use a one-shot http.Transport that dials our existing conn
	tr := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return t.conn, nil
		},
	}
	return tr.RoundTrip(req)
}

type captureWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *captureWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *captureWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func headerToMap(header http.Header) map[string]string {
	if len(header) == 0 {
		return nil
	}
	out := make(map[string]string, len(header))
	for k, v := range header {
		out[k] = strings.Join(v, ", ")
	}
	return out
}

func pageVariant(variant string) string {
	switch variant {
	case "terminal", "minimal":
		return variant
	default:
		return "default"
	}
}

func (s *Server) authorizeTunnelPassword(w http.ResponseWriter, r *http.Request, fwd *forward) bool {
	if cookie, err := r.Cookie(passwordCookieName(fwd)); err == nil && cookie.Value == passwordCookieValue(s.cfg.Token, fwd) {
		return true
	}

	var errorMessage string
	if r.Method == http.MethodPost {
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
				errorMessage = "Incorrect password."
			default:
				http.SetCookie(w, &http.Cookie{
					Name:     passwordCookieName(fwd),
					Value:    passwordCookieValue(s.cfg.Token, fwd),
					Path:     "/",
					HttpOnly: true,
					Secure:   r.TLS != nil,
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

func writeStatusPage(w http.ResponseWriter, status int, variant, title, message string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	title = htmlEscape(title)
	message = htmlEscape(message)
	switch pageVariant(variant) {
	case "terminal":
		fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title><style>body{margin:0;background:#08110c;color:#7CFFB2;font:16px/1.6 ui-monospace,SFMono-Regular,Menlo,monospace;display:grid;place-items:center;min-height:100vh}main{max-width:720px;padding:32px;border:1px solid #184c2c;background:#0d1711;box-shadow:0 0 0 1px #0f2a19 inset}h1{margin:0 0 8px;font-size:28px}p{margin:0;color:#b7ffd2}.code{margin-top:18px;color:#62d891}</style></head><body><main><h1>%s</h1><p>%s</p><div class="code">status=%d</div></main></body></html>`, title, title, message, status)
	case "minimal":
		fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title><style>body{margin:0;font:16px/1.5 Inter,system-ui,sans-serif;background:#f7f7f7;color:#111;display:grid;place-items:center;min-height:100vh}main{max-width:560px;background:#fff;border:1px solid #e5e5e5;padding:32px}h1{margin:0 0 8px;font-size:26px}p{margin:0;color:#666}</style></head><body><main><h1>%s</h1><p>%s</p></main></body></html>`, title, title, message)
	default:
		fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title><style>:root{--bg:#0b0d0c;--panel:#111412;--border:#222522;--text:#d8ddd9;--text-dim:#9ba39c;--accent:#00e87a}body{margin:0;background:radial-gradient(circle at top,#162119,#0b0d0c 60%%);color:var(--text);font:16px/1.5 Inter,system-ui,sans-serif;display:grid;place-items:center;min-height:100vh;padding:24px}main{width:min(620px,100%%);background:rgba(17,20,18,.96);border:1px solid var(--border);padding:32px;border-radius:18px;box-shadow:0 12px 40px rgba(0,0,0,.25)}.eyebrow{font-size:11px;letter-spacing:.12em;text-transform:uppercase;color:var(--accent);margin-bottom:12px}.status{font:600 13px ui-monospace,SFMono-Regular,Menlo,monospace;color:var(--text-dim);margin-top:16px}h1{margin:0 0 10px;font-size:30px}.msg{margin:0;color:var(--text-dim)}</style></head><body><main><div class="eyebrow">Pigeon Tunnel</div><h1>%s</h1><p class="msg">%s</p><div class="status">HTTP %d</div></main></body></html>`, title, title, message, status)
	}
}

func writePasswordPage(w http.ResponseWriter, variant, title, message, errMsg string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	title = htmlEscape(title)
	message = htmlEscape(message)
	errMsg = htmlEscape(errMsg)
	switch pageVariant(variant) {
	case "terminal":
		fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title><style>body{margin:0;background:#08110c;color:#7CFFB2;font:16px/1.6 ui-monospace,SFMono-Regular,Menlo,monospace;display:grid;place-items:center;min-height:100vh}main{max-width:720px;padding:32px;border:1px solid #184c2c;background:#0d1711;box-shadow:0 0 0 1px #0f2a19 inset}h1{margin:0 0 8px;font-size:28px}p{margin:0 0 18px;color:#b7ffd2}.err{margin:0 0 14px;color:#ff8e8e}input,button{font:inherit;border:1px solid #2a6d45;background:#0a120d;color:#7CFFB2;padding:10px 12px}button{cursor:pointer}.row{display:flex;gap:8px;flex-wrap:wrap}</style></head><body><main><h1>%s</h1><p>%s</p>%s<form method="post"><div class="row"><input autofocus type="password" name="pigeon_password" placeholder="Password" /><button type="submit">Unlock</button></div></form></main></body></html>`, title, title, message, renderInlineError("err", errMsg))
	case "minimal":
		fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title><style>body{margin:0;font:16px/1.5 Inter,system-ui,sans-serif;background:#f7f7f7;color:#111;display:grid;place-items:center;min-height:100vh}main{max-width:560px;background:#fff;border:1px solid #e5e5e5;padding:32px}h1{margin:0 0 8px;font-size:26px}p{margin:0 0 18px;color:#666}.err{margin:0 0 14px;color:#c62828}input,button{font:inherit;padding:10px 12px;border:1px solid #d5d5d5}.row{display:flex;gap:8px;flex-wrap:wrap}button{cursor:pointer;background:#111;color:#fff}</style></head><body><main><h1>%s</h1><p>%s</p>%s<form method="post"><div class="row"><input autofocus type="password" name="pigeon_password" placeholder="Password" /><button type="submit">Unlock</button></div></form></main></body></html>`, title, title, message, renderInlineError("err", errMsg))
	default:
		fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title><style>:root{--bg:#0b0d0c;--panel:#111412;--border:#222522;--text:#d8ddd9;--text-dim:#9ba39c;--accent:#00e87a;--red:#ff5d5d}body{margin:0;background:radial-gradient(circle at top,#162119,#0b0d0c 60%%);color:var(--text);font:16px/1.5 Inter,system-ui,sans-serif;display:grid;place-items:center;min-height:100vh;padding:24px}main{width:min(620px,100%%);background:rgba(17,20,18,.96);border:1px solid var(--border);padding:32px;border-radius:18px;box-shadow:0 12px 40px rgba(0,0,0,.25)}.eyebrow{font-size:11px;letter-spacing:.12em;text-transform:uppercase;color:var(--accent);margin-bottom:12px}h1{margin:0 0 10px;font-size:30px}.msg{margin:0 0 18px;color:var(--text-dim)}.err{margin:0 0 14px;color:var(--red)}.row{display:flex;gap:8px;flex-wrap:wrap}input,button{font:inherit;padding:11px 12px;border-radius:10px}input{min-width:220px;background:var(--panel);border:1px solid var(--border);color:var(--text)}button{border:none;background:var(--accent);color:#000;font-weight:700;cursor:pointer}</style></head><body><main><div class="eyebrow">Protected Tunnel</div><h1>%s</h1><p class="msg">%s</p>%s<form method="post"><div class="row"><input autofocus type="password" name="pigeon_password" placeholder="Password" /><button type="submit">Unlock</button></div></form></main></body></html>`, title, title, message, renderInlineError("err", errMsg))
	}
}

func renderInlineError(className, errMsg string) string {
	if errMsg == "" {
		return ""
	}
	return fmt.Sprintf(`<div class="%s">%s</div>`, className, errMsg)
}

func htmlEscape(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	)
	return replacer.Replace(s)
}

// ── Logging ────────────────────────────────────────────────────────────────────

type LogEntry struct {
	Time       string `json:"time"`
	ForwardID  string `json:"forward_id"`
	Domain     string `json:"domain"`
	RemoteAddr string `json:"remote_addr"`
	Protocol   string `json:"protocol"`
	Action     string `json:"action"`
	Bytes      int    `json:"bytes,omitempty"`
}

func (s *Server) logTraffic(fwd *forward, remoteAddr, protocol, action string, bytes int) {
	entry := LogEntry{
		Time:       time.Now().Format(time.RFC3339),
		ForwardID:  fwd.id,
		Domain:     fwd.publicAddr,
		RemoteAddr: remoteAddr,
		Protocol:   protocol,
		Action:     action,
		Bytes:      bytes,
	}
	b, _ := json.Marshal(entry)
	s.logger.Println(string(b))
}

// ── Helpers ────────────────────────────────────────────────────────────────────

func proxy(a, b io.ReadWriter) {
	done := make(chan struct{}, 2)
	cp := func(dst io.Writer, src io.Reader) {
		io.Copy(dst, src)
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
}

const idChars = "abcdefghijklmnopqrstuvwxyz0123456789"

func randomID(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = idChars[rand.Intn(len(idChars))]
	}
	return string(b)
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

// domainFromHost strips port from a host header.
func domainFromHost(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return strings.ToLower(h)
	}
	return strings.ToLower(host)
}
