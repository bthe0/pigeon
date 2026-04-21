package client

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bthe0/pigeon/internal/proto"
	"github.com/hashicorp/yamux"
)

// Client manages a single connection to the pigeon server.
type Client struct {
	cfg     *Config
	mux     *yamux.Session
	ctrl    net.Conn
	logger  *log.Logger
	logFile io.WriteCloser
	OnAddr  func(id, publicAddr string) // called when a forward is acknowledged
}

// New creates a new Client.
func New(cfg *Config) (*Client, error) {
	dir, err := LogDir()
	if err != nil {
		return nil, err
	}
	logPath := filepath.Join(dir, time.Now().Format("2006-01-02")+".ndjson")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	c := &Client{cfg: cfg, logFile: f}
	c.logger = log.New(io.MultiWriter(os.Stdout, f), "", 0)
	return c, nil
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

	c.mux = mux
	c.ctrl = ctrl

	// Register all configured forwards
	for _, rule := range c.cfg.Forwards {
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
	var rule *ForwardRule
	for i := range c.cfg.Forwards {
		if c.cfg.Forwards[i].ID == hdr.ForwardID {
			rule = &c.cfg.Forwards[i]
			break
		}
	}
	if rule == nil {
		log.Printf("unknown forward %s", hdr.ForwardID)
		return
	}

	switch hdr.Protocol {
	case proto.ProtoHTTP, proto.ProtoTCP:
		c.handleTCPStream(stream, rule, hdr)
	case proto.ProtoUDP:
		c.handleUDPStream(stream, rule)
	}
}

func (c *Client) handleTCPStream(stream net.Conn, rule *ForwardRule, hdr proto.StreamHeader) {
	local, err := net.DialTimeout("tcp", rule.LocalAddr, 5*time.Second)
	if err != nil {
		log.Printf("[%s] dial local %s: %v", rule.ID, rule.LocalAddr, err)
		return
	}
	defer local.Close()

	c.logTraffic(rule, hdr.RemoteAddr, string(hdr.Protocol), "CONNECT", 0)

	done := make(chan struct{}, 2)
	cp := func(dst io.Writer, src io.Reader) {
		io.Copy(dst, src)
		done <- struct{}{}
	}
	go cp(stream, local)
	go cp(local, stream)
	<-done
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
		enc.Encode(udpFrame{Addr: extAddr, Data: data})
	}

	dec := json.NewDecoder(stream)
	for {
		var frame udpFrame
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
				log.Printf("[%s] UDP session open: %v", rule.ID, err)
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

type udpFrame struct {
	Addr string `json:"addr"`
	Data []byte `json:"data"`
}

// SendForwardAdd registers a new forward with the server.
func (c *Client) SendForwardAdd(rule ForwardRule) error {
	return c.sendForwardAdd(rule)
}

func (c *Client) sendForwardAdd(rule ForwardRule) error {
	return proto.Write(c.ctrl, proto.Message{
		Type: proto.MsgForwardAdd,
		Payload: proto.ForwardPayload{
			ID:         rule.ID,
			Protocol:   rule.Protocol,
			LocalAddr:  rule.LocalAddr,
			Domain:     rule.Domain,
			RemotePort: rule.RemotePort,
		},
	})
}

// SendForwardRemove deregisters a forward.
func (c *Client) SendForwardRemove(id string) error {
	return proto.Write(c.ctrl, proto.Message{
		Type:    proto.MsgForwardRemove,
		Payload: proto.ForwardRemovePayload{ID: id},
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
}

// ── Logging ────────────────────────────────────────────────────────────────────

type LogEntry struct {
	Time       string `json:"time"`
	ForwardID  string `json:"forward_id"`
	RemoteAddr string `json:"remote_addr"`
	Protocol   string `json:"protocol"`
	Action     string `json:"action"`
	Bytes      int    `json:"bytes,omitempty"`
}

func (c *Client) logTraffic(rule *ForwardRule, remoteAddr, protocol, action string, bytes int) {
	entry := LogEntry{
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
