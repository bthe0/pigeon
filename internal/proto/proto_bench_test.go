package proto

import (
	"bytes"
	"testing"
)

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func BenchmarkWrite(b *testing.B) {
	msg := Message{Type: MsgForwardAdd, Payload: ForwardPayload{
		ID:           "abc12345",
		Protocol:     ProtoHTTP,
		LocalAddr:    "127.0.0.1:3000",
		Domain:       "app.example.com",
		HTTPPassword: "secret",
	}}
	w := discardWriter{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := Write(w, msg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRead(b *testing.B) {
	buf := &bytes.Buffer{}
	msg := Message{Type: MsgInspectorEvent, Payload: InspectorEventPayload{
		Time:       "2026-04-22T03:00:00Z",
		ForwardID:  "abc12345",
		Domain:     "app.example.com",
		RemoteAddr: "203.0.113.10:4567",
		Method:     "GET",
		Path:       "/",
		Status:     200,
		DurationMs: 12,
		Bytes:      1234,
	}}
	if err := Write(buf, msg); err != nil {
		b.Fatal(err)
	}
	data := buf.Bytes()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Read(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteRead(b *testing.B) {
	msg := Message{Type: MsgAuthAck, Payload: AuthAckPayload{
		ClientID:   "abc12345",
		BaseDomain: "tun.example.com",
	}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf := &bytes.Buffer{}
		if err := Write(buf, msg); err != nil {
			b.Fatal(err)
		}
		if _, err := Read(bytes.NewReader(buf.Bytes())); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodePayload(b *testing.B) {
	msg := Message{Type: MsgForwardAdd, Payload: ForwardPayload{
		ID:             "abc12345",
		Protocol:       ProtoHTTP,
		LocalAddr:      "127.0.0.1:3000",
		Domain:         "app.example.com",
		HTTPPassword:   "secret",
		MaxConnections: 8,
	}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var p ForwardPayload
		if err := DecodePayload(msg, &p); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStreamHeaderWriteRead(b *testing.B) {
	hdr := StreamHeader{ForwardID: "abc12345", RemoteAddr: "203.0.113.10:4567", Protocol: ProtoTCP}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf := &bytes.Buffer{}
		if err := WriteStreamHeader(buf, hdr); err != nil {
			b.Fatal(err)
		}
		if _, err := ReadStreamHeader(bytes.NewReader(buf.Bytes())); err != nil {
			b.Fatal(err)
		}
	}
}
