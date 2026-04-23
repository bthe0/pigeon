package client

import (
	"fmt"
	"net"
	"time"

	"github.com/bthe0/pigeon/internal/proto"
	"github.com/hashicorp/yamux"
)

// ValidateToken performs just the auth handshake against the configured
// server and closes the connection, returning nil on success. Used by the
// dashboard's "Validate Token" button.
func ValidateToken(cfg *Config) error {
	if cfg == nil || cfg.Server == "" {
		return fmt.Errorf("server address not configured")
	}
	if cfg.Token == "" {
		return fmt.Errorf("token not configured")
	}

	conn, err := net.DialTimeout("tcp", cfg.Server, 10*time.Second)
	if err != nil {
		return fmt.Errorf("dial %s: %w", cfg.Server, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	mux, err := yamux.Client(conn, yamux.DefaultConfig())
	if err != nil {
		return fmt.Errorf("yamux: %w", err)
	}
	defer mux.Close()

	ctrl, err := mux.Open()
	if err != nil {
		return fmt.Errorf("open control stream: %w", err)
	}
	defer ctrl.Close()

	if err := proto.Write(ctrl, proto.Message{
		Type:    proto.MsgAuth,
		Payload: proto.AuthPayload{Token: cfg.Token},
	}); err != nil {
		return err
	}

	msg, err := proto.Read(ctrl)
	if err != nil {
		return err
	}
	if msg.Type == proto.MsgError {
		var e proto.ErrorPayload
		_ = proto.DecodePayload(msg, &e)
		return fmt.Errorf("%s", e.Message)
	}
	if msg.Type != proto.MsgAuthAck {
		return fmt.Errorf("unexpected response: %s", msg.Type)
	}
	return nil
}
