package proto_test

import (
	"bytes"
	"testing"

	"github.com/bthe0/pigeon/internal/proto"
)

func TestWriteRead(t *testing.T) {
	tests := []struct {
		name string
		msg  proto.Message
	}{
		{
			name: "auth",
			msg:  proto.Message{Type: proto.MsgAuth, Payload: proto.AuthPayload{Token: "secret"}},
		},
		{
			name: "auth_ack",
			msg:  proto.Message{Type: proto.MsgAuthAck, Payload: proto.AuthAckPayload{ClientID: "abc123"}},
		},
		{
			name: "forward_add",
			msg: proto.Message{Type: proto.MsgForwardAdd, Payload: proto.ForwardPayload{
				ID:        "fwd1",
				Protocol:  proto.ProtoHTTP,
				LocalAddr: "localhost:3000",
				Domain:    "myapp.example.com",
			}},
		},
		{
			name: "ping",
			msg:  proto.Message{Type: proto.MsgPing},
		},
		{
			name: "error",
			msg:  proto.Message{Type: proto.MsgError, Payload: proto.ErrorPayload{Message: "invalid token"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := proto.Write(&buf, tt.msg); err != nil {
				t.Fatalf("Write: %v", err)
			}
			got, err := proto.Read(&buf)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if got.Type != tt.msg.Type {
				t.Errorf("type: got %q want %q", got.Type, tt.msg.Type)
			}
		})
	}
}

func TestDecodePayload(t *testing.T) {
	msg := proto.Message{
		Type:    proto.MsgForwardAck,
		Payload: proto.ForwardAckPayload{ID: "fwd1", PublicAddr: "abc.tun.example.com"},
	}

	var buf bytes.Buffer
	proto.Write(&buf, msg)
	got, _ := proto.Read(&buf)

	var ack proto.ForwardAckPayload
	if err := proto.DecodePayload(got, &ack); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if ack.ID != "fwd1" {
		t.Errorf("ID: got %q want %q", ack.ID, "fwd1")
	}
	if ack.PublicAddr != "abc.tun.example.com" {
		t.Errorf("PublicAddr: got %q", ack.PublicAddr)
	}
}

func TestStreamHeader(t *testing.T) {
	h := proto.StreamHeader{
		ForwardID:  "fwd-tcp-1",
		RemoteAddr: "1.2.3.4:54321",
		Protocol:   proto.ProtoTCP,
	}

	var buf bytes.Buffer
	if err := proto.WriteStreamHeader(&buf, h); err != nil {
		t.Fatalf("WriteStreamHeader: %v", err)
	}
	got, err := proto.ReadStreamHeader(&buf)
	if err != nil {
		t.Fatalf("ReadStreamHeader: %v", err)
	}
	if got.ForwardID != h.ForwardID {
		t.Errorf("ForwardID: got %q want %q", got.ForwardID, h.ForwardID)
	}
	if got.RemoteAddr != h.RemoteAddr {
		t.Errorf("RemoteAddr: got %q want %q", got.RemoteAddr, h.RemoteAddr)
	}
	if got.Protocol != h.Protocol {
		t.Errorf("Protocol: got %q want %q", got.Protocol, h.Protocol)
	}
}

func TestReadTooLarge(t *testing.T) {
	// Craft a message claiming to be 20MB
	var buf bytes.Buffer
	buf.Write([]byte{0x01, 0x40, 0x00, 0x00}) // 20MB
	_, err := proto.Read(&buf)
	if err == nil {
		t.Fatal("expected error for oversized message")
	}
}

func TestMultipleMessages(t *testing.T) {
	var buf bytes.Buffer
	msgs := []proto.Message{
		{Type: proto.MsgPing},
		{Type: proto.MsgPong},
		{Type: proto.MsgAuth, Payload: proto.AuthPayload{Token: "tok"}},
	}
	for _, m := range msgs {
		proto.Write(&buf, m)
	}
	for _, want := range msgs {
		got, err := proto.Read(&buf)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if got.Type != want.Type {
			t.Errorf("type: got %q want %q", got.Type, want.Type)
		}
	}
}
