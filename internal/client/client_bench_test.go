package client

import (
	"bytes"
	"io"
	"testing"

	"github.com/bthe0/pigeon/internal/netx"
	"github.com/bthe0/pigeon/internal/proto"
)

type benchReader struct {
	data []byte
	off  int
}

func (r *benchReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}

type benchWriter struct{ n int }

func (w *benchWriter) Write(p []byte) (int, error) {
	w.n += len(p)
	return len(p), nil
}

func BenchmarkLookupForward(b *testing.B) {
	cfg := &Config{Forwards: make([]ForwardRule, 1000)}
	for i := range cfg.Forwards {
		cfg.Forwards[i] = ForwardRule{ID: proto.RandomID(8), LocalAddr: "127.0.0.1:3000", Protocol: proto.ProtoTCP}
	}
	c := &Client{cfg: cfg}
	c.rebuildForwardIndex()
	target := cfg.Forwards[len(cfg.Forwards)/2].ID
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if c.lookupForward(target) == nil {
			b.Fatal("missing forward")
		}
	}
}

func BenchmarkCopyStream(b *testing.B) {
	data := bytes.Repeat([]byte("x"), 256*1024)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r := &benchReader{data: data}
		w := &benchWriter{}
		netx.Copy(w, r)
		if w.n != len(data) {
			b.Fatal(w.n)
		}
	}
}
