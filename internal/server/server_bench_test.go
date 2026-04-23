package server

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bthe0/pigeon/internal/netx"
	"github.com/bthe0/pigeon/internal/proto"
	"github.com/hashicorp/yamux"
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

func BenchmarkServeHTTPMissingForward(b *testing.B) {
	s := New(Config{HTTPAddr: ":0"})
	req := httptest.NewRequest(http.MethodGet, "http://missing.example.com/", nil)
	req.Host = "missing.example.com"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		s.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadGateway {
			b.Fatal(rr.Code)
		}
	}
}

func BenchmarkConnTransportRoundTrip(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		client, server := net.Pipe()
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer server.Close()
			req, err := http.ReadRequest(bufio.NewReader(server))
			if err != nil {
				return
			}
			_ = req.Body.Close()
			resp := &http.Response{
				StatusCode:    http.StatusOK,
				Proto:         "HTTP/1.1",
				ProtoMajor:    1,
				ProtoMinor:    1,
				Body:          io.NopCloser(bytes.NewBufferString("ok")),
				ContentLength: 2,
			}
			_ = resp.Write(server)
		}()
		tr := &connTransport{conn: client}
		req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
		resp, err := tr.RoundTrip(req)
		if err != nil {
			b.Fatal(err)
		}
		_, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		client.Close()
		<-done
	}
}

func BenchmarkServeHTTPSuccess(b *testing.B) {
	serverConn, clientConn := net.Pipe()
	serverMux, err := yamux.Server(serverConn, yamux.DefaultConfig())
	if err != nil {
		b.Fatal(err)
	}
	clientMux, err := yamux.Client(clientConn, yamux.DefaultConfig())
	if err != nil {
		b.Fatal(err)
	}

	ctrlServer, ctrlClient := net.Pipe()
	go func() {
		defer ctrlServer.Close()
		for {
			if _, err := proto.Read(ctrlServer); err != nil {
				return
			}
		}
	}()

	go func() {
		for {
			stream, err := clientMux.Accept()
			if err != nil {
				return
			}
			go func(s net.Conn) {
				defer s.Close()
				hdr, err := proto.ReadStreamHeader(s)
				if err != nil {
					return
				}
				if hdr.Protocol != proto.ProtoHTTPS {
					return
				}
				req, err := http.ReadRequest(bufio.NewReader(s))
				if err != nil {
					return
				}
				_ = req.Body.Close()
				resp := &http.Response{
					StatusCode:    http.StatusOK,
					Proto:         "HTTP/1.1",
					ProtoMajor:    1,
					ProtoMinor:    1,
					Body:          io.NopCloser(bytes.NewBufferString("ok")),
					ContentLength: 2,
				}
				_ = resp.Write(s)
			}(stream)
		}
	}()

	s := New(Config{HTTPAddr: ":0"})
	s.logger = log.New(io.Discard, "", 0)
	fwd := &forward{
		id:         "abc12345",
		protocol:   proto.ProtoHTTPS,
		publicAddr: "app.example.com",
		expose:     "https",
		session:    &session{id: "sess", mux: serverMux, ctrl: ctrlClient, forwards: map[string]*forward{}},
	}
	s.sessions.Store("http:app.example.com", fwd)

	req := httptest.NewRequest(http.MethodGet, "https://app.example.com/", nil)
	req.Host = "app.example.com"
	req.TLS = &tls.ConnectionState{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		s.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			b.Fatal(rr.Code)
		}
	}
	b.StopTimer()
	_ = serverMux.Close()
	_ = clientMux.Close()
	_ = serverConn.Close()
	_ = clientConn.Close()
	_ = ctrlServer.Close()
	_ = ctrlClient.Close()
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
