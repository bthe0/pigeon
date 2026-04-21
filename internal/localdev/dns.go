package localdev

import (
	"fmt"
	"log"
	"net"
	"strings"

	"golang.org/x/net/dns/dnsmessage"
)

const DNSPort = 5454

// StartDNS runs a UDP DNS server on 127.0.0.1:5353 that resolves all queries
// for domain and *.domain to 127.0.0.1. Blocks until the connection fails.
func StartDNS(domain string) error {
	addr := fmt.Sprintf("127.0.0.1:%d", DNSPort)
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return fmt.Errorf("dns listen %s: %w", addr, err)
	}
	defer conn.Close()
	log.Printf("DNS server listening on %s (resolving *.%s → 127.0.0.1)", addr, domain)

	buf := make([]byte, 512)
	for {
		n, src, err := conn.ReadFrom(buf)
		if err != nil {
			return err
		}
		go handleDNS(conn, src, buf[:n], domain)
	}
}

func handleDNS(conn net.PacketConn, src net.Addr, req []byte, domain string) {
	var msg dnsmessage.Message
	if err := msg.Unpack(req); err != nil {
		return
	}

	resp := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID:                 msg.Header.ID,
			Response:           true,
			Authoritative:      true,
			RecursionDesired:   msg.Header.RecursionDesired,
			RecursionAvailable: false,
		},
		Questions: msg.Questions,
	}

	for _, q := range msg.Questions {
		name := strings.TrimSuffix(strings.ToLower(q.Name.String()), ".")
		if q.Type == dnsmessage.TypeA && (name == domain || strings.HasSuffix(name, "."+domain)) {
			resp.Answers = append(resp.Answers, dnsmessage.Resource{
				Header: dnsmessage.ResourceHeader{
					Name:  q.Name,
					Type:  dnsmessage.TypeA,
					Class: dnsmessage.ClassINET,
					TTL:   60,
				},
				Body: &dnsmessage.AResource{A: [4]byte{127, 0, 0, 1}},
			})
		}
	}

	out, err := resp.Pack()
	if err != nil {
		return
	}
	conn.WriteTo(out, src)
}
