package server

import (
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/bthe0/pigeon/internal/proto"
	"github.com/hashicorp/yamux"
)

type forward struct {
	id              string
	protocol        proto.Protocol
	localAddr       string
	publicAddr      string
	domain          string
	port            int
	expose          string // "http" | "https"; default "https"
	httpPassword    string
	maxConnections  int
	unavailablePage string
	captureBodies   bool
	allowedIPs      []*net.IPNet // empty = allow all
	activeConns     atomic.Int32
	session         *session
	listener        io.Closer // TCP listener or UDP packet conn; nil for HTTP forwards
}

// allows reports whether ip is permitted to reach this forward. An empty
// allowedIPs list means no restriction. An unparseable ip string is denied
// (defense in depth; should not happen for accepted connections).
func (f *forward) allows(ip string) bool {
	if len(f.allowedIPs) == 0 {
		return true
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range f.allowedIPs {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
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

	srcIP, _, _ := net.SplitHostPort(conn.RemoteAddr().String())

	if srcIP != "" && s.isAuthRateLimited(srcIP) {
		log.Printf("rejecting auth from %s: rate limited", srcIP)
		return
	}

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
		if srcIP != "" {
			s.recordAuthFail(srcIP)
		}
		proto.Write(ctrl, proto.Message{Type: proto.MsgError, Payload: proto.ErrorPayload{Message: "expected auth"}})
		return
	}
	var auth proto.AuthPayload
	if err := proto.DecodePayload(msg, &auth); err != nil || auth.Token != s.cfg.Token {
		if srcIP != "" {
			s.recordAuthFail(srcIP)
		}
		proto.Write(ctrl, proto.Message{Type: proto.MsgError, Payload: proto.ErrorPayload{Message: "authentication failed"}})
		return
	}
	if srcIP != "" {
		s.clearAuthFails(srcIP)
	}

	clientID := proto.RandomID(8)
	sess := &session{id: clientID, mux: mux, ctrl: ctrl, forwards: make(map[string]*forward)}
	sess.writeMessage(proto.Message{Type: proto.MsgAuthAck, Payload: proto.AuthAckPayload{
		ClientID:   clientID,
		BaseDomain: s.cfg.Domain,
	}})
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
	// Static forwards have no local TCP service — the daemon serves files
	// in-process — so the host:port requirement only applies to the others.
	if p.Protocol != proto.ProtoStatic {
		if p.LocalAddr == "" {
			return "", fmt.Errorf("local address required")
		}
		if _, port, err := net.SplitHostPort(p.LocalAddr); err != nil || port == "" {
			return "", fmt.Errorf("invalid local address %q: must be host:port", p.LocalAddr)
		} else if _, err := strconv.Atoi(port); err != nil {
			return "", fmt.Errorf("invalid local address %q: port must be numeric", p.LocalAddr)
		}
	}

	allowed, err := parseAllowedIPs(p.AllowedIPs)
	if err != nil {
		return "", err
	}

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
		captureBodies:   p.CaptureBodies,
		allowedIPs:      allowed,
	}

	if p.Protocol.IsHTTPLike() {
		domain := p.Domain
		if domain == "" {
			if s.cfg.Domain == "" {
				return "", fmt.Errorf("cannot auto-assign subdomain: server has no base domain configured")
			}
			domain = proto.RandomID(8) + "." + s.cfg.Domain
		} else if s.cfg.Domain != "" {
			// Wildcards must terminate at the configured base domain. We only
			// accept *one* leading wildcard label (`*.user.example.com`) to keep
			// matching predictable and avoid surprising shadowing of nested
			// tunnels (`a.b.user.example.com` should not be swallowed).
			check := strings.TrimPrefix(domain, "*.")
			if !strings.HasSuffix(check, "."+s.cfg.Domain) && check != s.cfg.Domain {
				return "", fmt.Errorf("domain %q is not a subdomain of %s", domain, s.cfg.Domain)
			}
		}
		fwd.publicAddr = domain
		if strings.HasPrefix(domain, "*.") {
			suffix := strings.TrimPrefix(domain, "*.")
			if strings.Contains(suffix, "*") {
				return "", fmt.Errorf("only one leading wildcard label is allowed in %q", domain)
			}
			s.wildcards.Store(suffix, fwd)
		} else {
			s.sessions.Store("http:"+domain, fwd)
		}
		if s.cfg.OnForwardRegistered != nil {
			s.cfg.OnForwardRegistered(domain)
		}

	} else { // TCP / UDP
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

// parseAllowedIPs normalises a list of IP / CIDR strings into *net.IPNet
// values. A bare address is upgraded to its single-host CIDR (/32 or /128).
func parseAllowedIPs(entries []string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, raw := range entries {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if _, n, err := net.ParseCIDR(s); err == nil {
			out = append(out, n)
			continue
		}
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, fmt.Errorf("invalid IP or CIDR %q in allowed_ips", raw)
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return out, nil
}

func (s *Server) removeForward(sess *session, id string) {
	sess.mu.Lock()
	fwd, ok := sess.forwards[id]
	delete(sess.forwards, id)
	sess.mu.Unlock()

	if ok {
		s.releaseForward(fwd)
		log.Printf("[%s] removed forward %s", sess.id, id)
	}
}

func (s *Server) cleanupSession(sess *session) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	for _, fwd := range sess.forwards {
		s.releaseForward(fwd)
	}
	sess.forwards = nil
}

// releaseForward removes a forward from the global registry and closes any
// TCP/UDP listener it owns. Safe to call multiple times.
func (s *Server) releaseForward(fwd *forward) {
	if strings.HasPrefix(fwd.publicAddr, "*.") {
		s.wildcards.Delete(strings.TrimPrefix(fwd.publicAddr, "*."))
	} else {
		s.sessions.Delete("http:" + fwd.publicAddr)
	}
	s.forwards.Delete(fwd.id)
	if fwd.listener != nil {
		_ = fwd.listener.Close()
		fwd.listener = nil
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
