package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
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
	LogFile     string // path to traffic log file, "" = stdout
}

type forward struct {
	id         string
	protocol   proto.Protocol
	localAddr  string
	publicAddr string
	domain     string
	port       int
	session    *session
}

type session struct {
	id       string
	mux      *yamux.Session
	forwards map[string]*forward // id → forward
	mu       sync.RWMutex
}

// Server is the pigeon tunnel server.
type Server struct {
	cfg      Config
	sessions sync.Map   // domain/port-key → *session
	forwards sync.Map   // forward id → *forward
	logger   *log.Logger
	logFile  io.WriteCloser
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
	sess := &session{id: clientID, mux: mux, forwards: make(map[string]*forward)}
	proto.Write(ctrl, proto.Message{Type: proto.MsgAuthAck, Payload: proto.AuthAckPayload{ClientID: clientID}})
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
				proto.Write(ctrl, proto.Message{Type: proto.MsgError, Payload: proto.ErrorPayload{Message: err.Error()}})
				continue
			}
			publicAddr, err := s.registerForward(sess, &p)
			if err != nil {
				proto.Write(ctrl, proto.Message{Type: proto.MsgError, Payload: proto.ErrorPayload{Message: err.Error()}})
				continue
			}
			proto.Write(ctrl, proto.Message{Type: proto.MsgForwardAck, Payload: proto.ForwardAckPayload{ID: p.ID, PublicAddr: publicAddr}})

		case proto.MsgForwardRemove:
			var p proto.ForwardRemovePayload
			if err := proto.DecodePayload(msg, &p); err == nil {
				s.removeForward(sess, p.ID)
			}

		case proto.MsgPing:
			proto.Write(ctrl, proto.Message{Type: proto.MsgPong})
		}
	}
}

// ── Forward management ─────────────────────────────────────────────────────────

func (s *Server) registerForward(sess *session, p *proto.ForwardPayload) (string, error) {
	fwd := &forward{
		id:        p.ID,
		protocol:  p.Protocol,
		localAddr: p.LocalAddr,
		session:   sess,
		domain:    p.Domain,
		port:      p.RemotePort,
	}

	switch p.Protocol {
	case proto.ProtoHTTP:
		domain := p.Domain
		if domain == "" {
			domain = randomID(8) + "." + s.cfg.Domain
		}
		fwd.publicAddr = domain
		s.sessions.Store("http:"+domain, fwd)

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
		s.sessions.Delete("http:" + fwd.domain)
		s.forwards.Delete(id)
		log.Printf("[%s] removed forward %s", sess.id, id)
	}
}

func (s *Server) cleanupSession(sess *session) {
	sess.mu.RLock()
	defer sess.mu.RUnlock()
	for _, fwd := range sess.forwards {
		s.sessions.Delete("http:" + fwd.domain)
		s.forwards.Delete(fwd.id)
	}
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
	m := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(s.cfg.Domain, "*."+s.cfg.Domain),
		Cache:      autocert.DirCache(s.cfg.CertDir),
	}
	srv := &http.Server{
		Addr:    s.cfg.HTTPSAddr,
		Handler: s,
		TLSConfig: &tls.Config{
			GetCertificate: m.GetCertificate,
		},
	}
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
		http.Error(w, "tunnel not found for "+host, http.StatusBadGateway)
		return
	}
	fwd := v.(*forward)

	stream, err := fwd.session.mux.Open()
	if err != nil {
		http.Error(w, "tunnel unavailable", http.StatusBadGateway)
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

	target, _ := url.Parse("http://" + host)
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.Transport = &connTransport{conn: stream}
	rp.ServeHTTP(w, r)

	s.logTraffic(fwd, r.RemoteAddr, "HTTP", r.Method+" "+r.URL.Path, 0)
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
