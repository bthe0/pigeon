package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/bthe0/pigeon/internal/netx"
	"github.com/bthe0/pigeon/internal/proto"
)

// udpPeerTTL bounds how long a UDP peer address stays on the allow-list after
// the server last heard from it. The pigeon client may only send a datagram
// back to a peer we've recently heard from — otherwise an authenticated
// client could abuse the server's UDP socket to spray traffic to arbitrary
// destinations from the server's IP.
const udpPeerTTL = 2 * time.Minute

// udpPeerSet tracks recently-seen UDP source addresses for a single forward.
// Access is guarded by sync.Map so the reader (inbound datagrams) and writer
// (client-supplied frames) goroutines don't race.
type udpPeerSet struct {
	m sync.Map // canonical addr string → time.Time (lastSeen)
}

func (s *udpPeerSet) remember(addr string) {
	s.m.Store(addr, time.Now())
}

// allowed reports whether addr is currently trusted. Stale entries are purged
// on lookup so the map can't grow without bound even under steady traffic.
func (s *udpPeerSet) allowed(addr string) bool {
	v, ok := s.m.Load(addr)
	if !ok {
		return false
	}
	seen := v.(time.Time)
	if time.Since(seen) > udpPeerTTL {
		s.m.Delete(addr)
		return false
	}
	return true
}

// openPort binds a TCP listener or UDP packet conn for fwd and starts serving
// it. The returned port is the resolved numeric port (useful when the caller
// passed 0 to request an auto-assigned port).
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
		fwd.listener = ln
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
		fwd.listener = pc
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
			if host, _, err := net.SplitHostPort(conn.RemoteAddr().String()); err == nil {
				if !fwd.allows(host) {
					return
				}
			}
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
			netx.Proxy(conn, stream)
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

	peers := &udpPeerSet{}

	// Server → client: read datagrams, frame them
	go func() {
		buf := make([]byte, 65535)
		enc := json.NewEncoder(stream)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if host, _, err := net.SplitHostPort(addr.String()); err == nil {
				if !fwd.allows(host) {
					continue
				}
			}
			peers.remember(addr.String())
			enc.Encode(proto.UDPFrame{Addr: addr.String(), Data: buf[:n]})
			s.logTraffic(fwd, addr.String(), "UDP", "IN", n)
		}
	}()

	// Client → server: read framed datagrams, send them. Drop frames addressed
	// to peers we haven't recently heard from — the client is trusted to
	// respond to inbound senders, not to originate traffic to arbitrary
	// destinations through the server.
	dec := json.NewDecoder(stream)
	for {
		var frame proto.UDPFrame
		if err := dec.Decode(&frame); err != nil {
			return
		}
		if !peers.allowed(frame.Addr) {
			log.Printf("[%s] dropped UDP frame to unseen peer %s", fwd.id, frame.Addr)
			continue
		}
		addr, err := net.ResolveUDPAddr("udp", frame.Addr)
		if err != nil {
			continue
		}
		pc.WriteTo(frame.Data, addr)
		s.logTraffic(fwd, frame.Addr, "UDP", "OUT", len(frame.Data))
	}
}
