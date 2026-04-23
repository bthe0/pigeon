package server

import (
	"encoding/json"
	"time"

	"github.com/bthe0/pigeon/internal/proto"
)

func (s *Server) logTraffic(fwd *forward, remoteAddr, protocol, action string, bytes int) {
	entry := proto.TrafficLogEntry{
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
