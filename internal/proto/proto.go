package proto

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"crypto/rand"
)

type MessageType string

const (
	MsgAuth          MessageType = "auth"
	MsgAuthAck       MessageType = "auth_ack"
	MsgForwardAdd    MessageType = "forward_add"
	MsgForwardAck    MessageType = "forward_ack"
	MsgForwardRemove MessageType = "forward_remove"
	MsgInspectorEvent MessageType = "inspector_event"
	MsgPing          MessageType = "ping"
	MsgPong          MessageType = "pong"
	MsgError         MessageType = "error"
)

type Protocol string

const (
	ProtoHTTP  Protocol = "http"
	ProtoHTTPS Protocol = "https" // forward to a local service that speaks TLS
	ProtoTCP   Protocol = "tcp"
	ProtoUDP   Protocol = "udp"
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
	ClientID   string `json:"client_id"`
	BaseDomain string `json:"base_domain,omitempty"`
}

type ForwardPayload struct {
	ID         string   `json:"id"`
	Protocol   Protocol `json:"protocol"`
	LocalAddr  string   `json:"local_addr"`            // e.g. "localhost:3000"
	Domain     string   `json:"domain,omitempty"`      // http only
	RemotePort int      `json:"remote_port,omitempty"` // tcp/udp; 0 = assign random
	Expose     string   `json:"expose,omitempty"`      // "http" | "https"; default "https"
	HTTPPassword string `json:"http_password,omitempty"`
	MaxConnections int  `json:"max_connections,omitempty"`
	UnavailablePage string `json:"unavailable_page,omitempty"`
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

type InspectorEventPayload struct {
	Time            string            `json:"time"`
	ForwardID       string            `json:"forward_id"`
	Domain          string            `json:"domain"`
	RemoteAddr      string            `json:"remote_addr"`
	Method          string            `json:"method"`
	Path            string            `json:"path"`
	Status          int               `json:"status"`
	DurationMs      int               `json:"duration_ms"`
	Bytes           int               `json:"bytes,omitempty"`
	City            string            `json:"city,omitempty"`
	Country         string            `json:"country,omitempty"`
	CountryCode     string            `json:"country_code,omitempty"`
	Latitude        float64           `json:"latitude,omitempty"`
	Longitude       float64           `json:"longitude,omitempty"`
	Browser         string            `json:"browser,omitempty"`
	OS              string            `json:"os,omitempty"`
	RequestHeaders  map[string]string `json:"request_headers,omitempty"`
	ResponseHeaders map[string]string `json:"response_headers,omitempty"`
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

// UDPFrame is a framed UDP datagram.
type UDPFrame struct {
	Addr string `json:"addr"`
	Data []byte `json:"data"`
}

// TrafficLogEntry represents a single traffic log record.
type TrafficLogEntry struct {
	Time       string `json:"time"`
	ForwardID  string `json:"forward_id"`
	Domain     string `json:"domain,omitempty"`
	RemoteAddr string `json:"remote_addr"`
	Protocol   string `json:"protocol"`
	Action     string `json:"action"`
	Bytes      int    `json:"bytes,omitempty"`
}

const idChars = "abcdefghijklmnopqrstuvwxyz0123456789"

// RandomID generates a secure random string of length n.
func RandomID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = idChars[int(b[i])%len(idChars)]
	}
	return string(b)
}
