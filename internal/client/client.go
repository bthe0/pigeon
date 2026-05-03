package client

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bthe0/pigeon/internal/netx"
	"github.com/bthe0/pigeon/internal/proto"
	"github.com/hashicorp/yamux"
)

// Client manages a single connection to the pigeon server.
type Client struct {
	cfg          *Config
	forwardIndex atomic.Pointer[map[string]*ForwardRule] // read by accept goroutines, swapped by control loop
	mux          *yamux.Session
	ctrl         net.Conn
	logger       *log.Logger
	logFile      io.WriteCloser
	inspector    *InspectorWriter
	OnAddr       func(id, publicAddr string) // called when a forward is acknowledged
}

// New creates a new Client.
func New(cfg *Config) (*Client, error) {
	dir, err := LogDir()
	if err != nil {
		return nil, err
	}
	logPath := filepath.Join(dir, time.Now().Format("2006-01-02")+".ndjson")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}

	iw, _ := NewInspectorWriter()
	c := &Client{cfg: cfg, logFile: f, inspector: iw}
	c.rebuildForwardIndex()
	c.logger = log.New(io.MultiWriter(os.Stdout, f), "", 0)
	return c, nil
}

func (c *Client) rebuildForwardIndex() {
	idx := make(map[string]*ForwardRule, len(c.cfg.Forwards))
	for i := range c.cfg.Forwards {
		fwd := &c.cfg.Forwards[i]
		idx[fwd.ID] = fwd
	}
	c.forwardIndex.Store(&idx)
}

func (c *Client) lookupForward(id string) *ForwardRule {
	idx := c.forwardIndex.Load()
	if idx == nil {
		return nil
	}
	return (*idx)[id]
}

// Connect dials the server, authenticates, and registers all forwards.
func (c *Client) Connect() error {
	conn, err := net.DialTimeout("tcp", c.cfg.Server, 10*time.Second)
	if err != nil {
		return fmt.Errorf("dial %s: %w", c.cfg.Server, err)
	}

	mux, err := yamux.Client(conn, yamux.DefaultConfig())
	if err != nil {
		conn.Close()
		return fmt.Errorf("yamux: %w", err)
	}

	// Open control stream
	ctrl, err := mux.Open()
	if err != nil {
		mux.Close()
		return fmt.Errorf("open control stream: %w", err)
	}

	// Auth
	if err := proto.Write(ctrl, proto.Message{
		Type:    proto.MsgAuth,
		Payload: proto.AuthPayload{Token: c.cfg.Token},
	}); err != nil {
		mux.Close()
		return err
	}

	msg, err := proto.Read(ctrl)
	if err != nil {
		mux.Close()
		return err
	}
	if msg.Type == proto.MsgError {
		var e proto.ErrorPayload
		proto.DecodePayload(msg, &e)
		mux.Close()
		return fmt.Errorf("auth rejected: %s", e.Message)
	}
	if msg.Type != proto.MsgAuthAck {
		mux.Close()
		return fmt.Errorf("unexpected message: %s", msg.Type)
	}
	var ack proto.AuthAckPayload
	proto.DecodePayload(msg, &ack)
	log.Printf("Connected as %s", ack.ClientID)

	// Persist the base domain discovered from the server
	if ack.BaseDomain != "" && c.cfg.BaseDomain != ack.BaseDomain {
		c.cfg.BaseDomain = ack.BaseDomain
		_ = SaveConfig(c.cfg)
	}

	c.mux = mux
	c.ctrl = ctrl
	c.rebuildForwardIndex()

	// Register all configured forwards
	for _, rule := range c.cfg.Forwards {
		if rule.Disabled {
			log.Printf("forward %s is disabled, skipping", rule.ID)
			continue
		}
		if err := c.sendForwardAdd(rule); err != nil {
			log.Printf("forward %s: %v", rule.ID, err)
		}
	}

	// Accept loop for server-opened streams (incoming connections)
	go c.acceptLoop()

	// Control read loop
	return c.controlLoop()
}

func (c *Client) controlLoop() error {
	for {
		msg, err := proto.Read(c.ctrl)
		if err != nil {
			return err
		}
		switch msg.Type {
		case proto.MsgForwardAck:
			var p proto.ForwardAckPayload
			if err := proto.DecodePayload(msg, &p); err == nil {
				log.Printf("forward ready: %s → %s", p.ID, p.PublicAddr)
				if c.OnAddr != nil {
					c.OnAddr(p.ID, p.PublicAddr)
				}
			}
		case proto.MsgError:
			var e proto.ErrorPayload
			proto.DecodePayload(msg, &e)
			log.Printf("server error: %s", e.Message)
		case proto.MsgInspectorEvent:
			var e proto.InspectorEventPayload
			if err := proto.DecodePayload(msg, &e); err == nil && c.inspector != nil {
				_ = c.inspector.Write(e)
			}
		case proto.MsgPing:
			proto.Write(c.ctrl, proto.Message{Type: proto.MsgPong})
		}
	}
}

func (c *Client) acceptLoop() {
	for {
		stream, err := c.mux.Accept()
		if err != nil {
			return
		}
		go c.handleStream(stream)
	}
}

func (c *Client) handleStream(stream net.Conn) {
	defer stream.Close()

	hdr, err := proto.ReadStreamHeader(stream)
	if err != nil {
		return
	}

	// Find the forward rule
	rule := c.lookupForward(hdr.ForwardID)
	if rule == nil {
		log.Printf("unknown forward %s", hdr.ForwardID)
		return
	}

	switch hdr.Protocol {
	case proto.ProtoHTTP, proto.ProtoTCP:
		c.handleTCPStream(stream, rule, hdr, false)
	case proto.ProtoHTTPS:
		c.handleTCPStream(stream, rule, hdr, true)
	case proto.ProtoUDP:
		c.handleUDPStream(stream, rule)
	case proto.ProtoStatic:
		c.handleStaticStream(stream, rule, hdr)
	}
}

// handleStaticStream serves files from rule.StaticRoot in response to a single
// HTTP request read off the stream. The stream is the request/response
// connection — read one request, write one response, then close. We deliberately
// do not loop: the server opens a fresh stream per request anyway.
func (c *Client) handleStaticStream(stream net.Conn, rule *ForwardRule, hdr proto.StreamHeader) {
	if rule.StaticRoot == "" {
		log.Printf("[%s] static forward has no static_root", rule.ID)
		return
	}
	br := bufio.NewReader(stream)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	defer req.Body.Close()

	cw := &countingResponseWriter{header: make(http.Header)}
	fs := http.FileServer(http.Dir(rule.StaticRoot))
	fs.ServeHTTP(cw, req)

	resp := http.Response{
		Status:        http.StatusText(cw.statusCode()),
		StatusCode:    cw.statusCode(),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        cw.header,
		Body:          io.NopCloser(&cw.body),
		ContentLength: int64(cw.body.Len()),
		Close:         true,
	}
	if resp.Header.Get("Content-Type") == "" {
		resp.Header.Set("Content-Type", "application/octet-stream")
	}
	if err := resp.Write(stream); err != nil {
		log.Printf("[%s] static write: %v", rule.ID, err)
	}
	c.logTraffic(rule, hdr.RemoteAddr, "STATIC", fmt.Sprintf("%s %s %d", req.Method, req.URL.Path, cw.statusCode()), cw.body.Len())
}

// countingResponseWriter buffers a complete HTTP response for static serving.
// FileServer expects a real ResponseWriter; we capture status/headers/body so
// the daemon can write a single self-contained response back over the stream.
type countingResponseWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *countingResponseWriter) Header() http.Header { return w.header }
func (w *countingResponseWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
}
func (w *countingResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}
func (w *countingResponseWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (c *Client) handleTCPStream(stream net.Conn, rule *ForwardRule, hdr proto.StreamHeader, useTLS bool) {
	var local net.Conn
	var err error
	if useTLS {
		insecureSkipVerify := rule.TLSSkipVerify && c.cfg.LocalDev
		local, err = tls.DialWithDialer(
			&net.Dialer{Timeout: 5 * time.Second},
			"tcp", rule.LocalAddr,
			&tls.Config{InsecureSkipVerify: insecureSkipVerify}, //nolint:gosec
		)
	} else {
		local, err = net.DialTimeout("tcp", rule.LocalAddr, 5*time.Second)
	}
	if err != nil {
		log.Printf("[%s] dial local %s: %v", rule.ID, rule.LocalAddr, err)
		return
	}
	defer local.Close()

	bytes := netx.Proxy(stream, local)
	// Log once at the end with the real byte total. We used to log "CONNECT"
	// at start with bytes=0, which left the dashboard's per-tunnel bandwidth
	// pinned at 0 because nothing ever fed it the actual transfer size.
	c.logTraffic(rule, hdr.RemoteAddr, string(hdr.Protocol), "CLOSE", int(bytes))
}

// handleUDPStream tunnels UDP traffic using a NAT-table pattern.
//
// The server labels every inbound datagram with the external client's address
// (frame.Addr). We maintain one local socket per distinct external client so
// that:
//   - packets always reach the local service from a stable source port, and
//   - echo replies carry the correct external-client address back to the server
//     (instead of the local service's address, which was the previous bug).
func (c *Client) handleUDPStream(stream net.Conn, rule *ForwardRule) {
	localAddr, err := net.ResolveUDPAddr("udp", rule.LocalAddr)
	if err != nil {
		return
	}

	var (
		sessionsMu sync.Mutex
		sessions   = make(map[string]net.PacketConn) // externalAddr → local socket
		encMu      sync.Mutex
		enc        = json.NewEncoder(stream)
	)

	sendToServer := func(extAddr string, data []byte) {
		encMu.Lock()
		defer encMu.Unlock()
		enc.Encode(proto.UDPFrame{Addr: extAddr, Data: data})
	}

	dec := json.NewDecoder(stream)
	for {
		var frame proto.UDPFrame
		if err := dec.Decode(&frame); err != nil {
			return
		}
		extAddr := frame.Addr

		sessionsMu.Lock()
		localConn, ok := sessions[extAddr]
		if !ok {
			// Open a dedicated local socket for this external client.
			lc, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				sessionsMu.Unlock()
				continue
			}
			sessions[extAddr] = lc
			localConn = lc

			// Reply goroutine: local service → server (with correct external addr).
			go func(lc net.PacketConn, extAddr string) {
				defer lc.Close()
				buf := make([]byte, 65535)
				for {
					n, _, err := lc.ReadFrom(buf)
					if err != nil {
						return
					}
					// Key fix: stamp frame with the EXTERNAL client addr, not the
					// local service addr, so the server routes the reply correctly.
					sendToServer(extAddr, buf[:n])
					c.logTraffic(rule, extAddr, "UDP", "OUT", n)
				}
			}(lc, extAddr)
		}
		sessionsMu.Unlock()

		if _, err := localConn.WriteTo(frame.Data, localAddr); err != nil {
			log.Printf("[%s] UDP write local: %v", rule.ID, err)
			continue
		}
		c.logTraffic(rule, extAddr, "UDP", "IN", len(frame.Data))
	}
}

func (c *Client) sendForwardAdd(rule ForwardRule) error {
	return proto.Write(c.ctrl, proto.Message{
		Type:    proto.MsgForwardAdd,
		Payload: rule.ToPayload(),
	})
}

// Close shuts down the client.
func (c *Client) Close() {
	if c.mux != nil {
		c.mux.Close()
	}
	if c.logFile != nil {
		c.logFile.Close()
	}
	if c.inspector != nil {
		c.inspector.Close()
	}
}

// ── Logging ────────────────────────────────────────────────────────────────────

func (c *Client) logTraffic(rule *ForwardRule, remoteAddr, protocol, action string, bytes int) {
	UpdateMetrics(rule.ID, bytes)
	entry := proto.TrafficLogEntry{
		Time:       time.Now().Format(time.RFC3339),
		ForwardID:  rule.ID,
		RemoteAddr: remoteAddr,
		Protocol:   protocol,
		Action:     action,
		Bytes:      bytes,
	}
	b, _ := json.Marshal(entry)
	c.logger.Println(string(b))
}
