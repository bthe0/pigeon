package proto

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

type MessageType string

const (
	MsgAuth          MessageType = "auth"
	MsgAuthAck       MessageType = "auth_ack"
	MsgForwardAdd    MessageType = "forward_add"
	MsgForwardAck    MessageType = "forward_ack"
	MsgForwardRemove MessageType = "forward_remove"
	MsgPing          MessageType = "ping"
	MsgPong          MessageType = "pong"
	MsgError         MessageType = "error"
)

type Protocol string

const (
	ProtoHTTP Protocol = "http"
	ProtoTCP  Protocol = "tcp"
	ProtoUDP  Protocol = "udp"
)

// Message is the control channel message envelope.
type Message struct {
	Type    MessageType `json:"type"`
	Payload any         `json:"payload,omitempty"`
}

type AuthPayload struct {
	Token string `json:"token"`
}

type AuthAckPayload struct {
	ClientID string `json:"client_id"`
}

type ForwardPayload struct {
	ID         string   `json:"id"`
	Protocol   Protocol `json:"protocol"`
	LocalAddr  string   `json:"local_addr"`           // e.g. "localhost:3000"
	Domain     string   `json:"domain,omitempty"`     // http only
	RemotePort int      `json:"remote_port,omitempty"` // tcp/udp; 0 = assign random
}

type ForwardAckPayload struct {
	ID         string `json:"id"`
	PublicAddr string `json:"public_addr"` // assigned domain or host:port
}

type ForwardRemovePayload struct {
	ID string `json:"id"`
}

type ErrorPayload struct {
	Message string `json:"message"`
}

// StreamHeader is written at the start of every server-opened data stream.
type StreamHeader struct {
	ForwardID  string   `json:"forward_id"`
	RemoteAddr string   `json:"remote_addr"`
	Protocol   Protocol `json:"protocol"`
}

const maxMessageSize = 10 * 1024 * 1024 // 10 MB

// Write sends a length-prefixed JSON message.
func Write(w io.Writer, msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	buf := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(buf[:4], uint32(len(data)))
	copy(buf[4:], data)
	_, err = w.Write(buf)
	return err
}

// Read receives a length-prefixed JSON message.
func Read(r io.Reader) (Message, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return Message{}, err
	}
	n := binary.BigEndian.Uint32(lenBuf[:])
	if n > maxMessageSize {
		return Message{}, fmt.Errorf("message too large: %d bytes", n)
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r, data); err != nil {
		return Message{}, err
	}
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return Message{}, fmt.Errorf("unmarshal: %w", err)
	}
	return msg, nil
}

// DecodePayload re-marshals msg.Payload into v.
func DecodePayload(msg Message, v any) error {
	b, err := json.Marshal(msg.Payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// WriteStreamHeader writes a length-prefixed stream header.
func WriteStreamHeader(w io.Writer, h StreamHeader) error {
	data, err := json.Marshal(h)
	if err != nil {
		return err
	}
	buf := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(buf[:4], uint32(len(data)))
	copy(buf[4:], data)
	_, err = w.Write(buf)
	return err
}

// ReadStreamHeader reads a length-prefixed stream header.
func ReadStreamHeader(r io.Reader) (StreamHeader, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return StreamHeader{}, err
	}
	n := binary.BigEndian.Uint32(lenBuf[:])
	if n > 64*1024 {
		return StreamHeader{}, fmt.Errorf("stream header too large: %d", n)
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r, data); err != nil {
		return StreamHeader{}, err
	}
	var h StreamHeader
	return h, json.Unmarshal(data, &h)
}
