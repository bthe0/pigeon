package server

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/bthe0/pigeon/internal/netx"
	"github.com/bthe0/pigeon/internal/proto"
)

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

	// Server → client: read datagrams, frame them
	go func() {
		buf := make([]byte, 65535)
		enc := json.NewEncoder(stream)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			enc.Encode(proto.UDPFrame{Addr: addr.String(), Data: buf[:n]})
			s.logTraffic(fwd, addr.String(), "UDP", "IN", n)
		}
	}()

	// Client → server: read framed datagrams, send them
	dec := json.NewDecoder(stream)
	for {
		var frame proto.UDPFrame
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
