package server

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bthe0/pigeon/internal/proto"
	"golang.org/x/crypto/acme/autocert"
)

func (s *Server) serveHTTP() error {
	log.Printf("HTTP listening on %s", s.cfg.HTTPAddr)
	return http.ListenAndServe(s.cfg.HTTPAddr, s)
}

func (s *Server) serveHTTPS() error {
	srv := &http.Server{
		Addr:    s.cfg.HTTPSAddr,
		Handler: s,
		// Disable HTTP/2: the backend yamux transport only speaks HTTP/1.1,
		// and the Go stdlib auto-negotiates HTTP/2 on TLS which breaks the proxy.
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}
	if s.cfg.CertFile != "" && s.cfg.KeyFile != "" {
		log.Printf("HTTPS listening on %s (self-signed)", s.cfg.HTTPSAddr)
		return srv.ListenAndServeTLS(s.cfg.CertFile, s.cfg.KeyFile)
	}
	m := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(s.cfg.Domain, "*."+s.cfg.Domain),
		Cache:      autocert.DirCache(s.cfg.CertDir),
	}
	srv.TLSConfig = &tls.Config{GetCertificate: m.GetCertificate}
	log.Printf("HTTPS listening on %s", s.cfg.HTTPSAddr)
	return srv.ListenAndServeTLS("", "")
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	v, ok := s.sessions.Load("http:" + host)
	if !ok {
		writeStatusPage(w, http.StatusBadGateway, "default", "tunnel not found", "No active tunnel is currently registered for "+host+".")
		return
	}
	fwd := v.(*forward)

	isTLS := s.requestIsSecure(r)

	// Default to HTTPS if expose is empty or explicitly set to both
	expose := fwd.expose
	if expose == "" || expose == "both" {
		expose = "https"
	}

	switch expose {
	case "https":
		if !isTLS {
			http.Redirect(w, r, "https://"+host+r.RequestURI, http.StatusMovedPermanently)
			return
		}
	case "http":
		if isTLS {
			writeStatusPage(w, http.StatusNotFound, pageVariant(fwd.unavailablePage), "HTTPS disabled", "This tunnel is only available over plain HTTP.")
			return
		}
	}

	if fwd.httpPassword != "" {
		if !s.authorizeTunnelPassword(w, r, fwd) {
			return
		}
	}

	if !fwd.tryAcquire() {
		writeStatusPage(w, http.StatusTooManyRequests, pageVariant(fwd.unavailablePage), "Too many connections", "This tunnel reached its maximum number of active connections.")
		return
	}
	defer fwd.release()

	stream, err := fwd.session.mux.Open()
	if err != nil {
		writeStatusPage(w, http.StatusBadGateway, pageVariant(fwd.unavailablePage), "Tunnel unavailable", "The tunnel is online, but the upstream connection is currently unavailable.")
		return
	}
	defer stream.Close()

	if err := proto.WriteStreamHeader(stream, proto.StreamHeader{
		ForwardID:  fwd.id,
		RemoteAddr: r.RemoteAddr,
		Protocol:   fwd.protocol,
	}); err != nil {
		http.Error(w, "tunnel write error", http.StatusBadGateway)
		return
	}

	start := time.Now()
	requestHeaders := headerToMap(r.Header)
	clientAddr := clientIP(r.RemoteAddr, r.Header)
	browser, osName := browserAndOS(r.UserAgent())
	geo := s.lookupGeo(clientAddr)
	target, _ := url.Parse("http://" + host)
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.Transport = &connTransport{conn: stream}
	var responseHeaders map[string]string
	rp.ModifyResponse = func(resp *http.Response) error {
		responseHeaders = headerToMap(resp.Header)
		return nil
	}
	rp.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		writeStatusPage(rw, http.StatusBadGateway, pageVariant(fwd.unavailablePage), "Tunnel unavailable", "The tunnel could not reach the local service right now.")
	}
	cw := &captureWriter{ResponseWriter: w, status: http.StatusOK}
	rp.ServeHTTP(cw, r)

	fwd.session.writeMessage(proto.Message{
		Type: proto.MsgInspectorEvent,
		Payload: proto.InspectorEventPayload{
			// Nanosecond precision disambiguates rapid-fire requests on the
			// dashboard — the client derives a stable row ID from Time and
			// would otherwise collide when N requests land in the same second.
			Time:            time.Now().Format(time.RFC3339Nano),
			ForwardID:       fwd.id,
			Domain:          fwd.publicAddr,
			RemoteAddr:      clientAddr,
			Method:          r.Method,
			Path:            r.URL.RequestURI(),
			Status:          cw.status,
			DurationMs:      int(time.Since(start).Milliseconds()),
			Bytes:           cw.bytes,
			City:            geo.City,
			Country:         geo.Country,
			CountryCode:     geo.CountryCode,
			Latitude:        geo.Latitude,
			Longitude:       geo.Longitude,
			Browser:         browser,
			OS:              osName,
			RequestHeaders:  requestHeaders,
			ResponseHeaders: responseHeaders,
		},
	})

	s.logTraffic(fwd, r.RemoteAddr, "HTTP", fmt.Sprintf("%s %s %d %dms", r.Method, r.URL.Path, cw.status, time.Since(start).Milliseconds()), cw.bytes)
}

// connTransport implements http.RoundTripper using an existing net.Conn.
type connTransport struct {
	conn net.Conn
}

func (t *connTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Close = true
	clone.RequestURI = ""
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }
	go func() {
		select {
		case <-req.Context().Done():
			_ = t.conn.Close()
		case <-done:
		}
	}()
	if err := clone.Write(t.conn); err != nil {
		stop()
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(t.conn), clone)
	if err != nil {
		stop()
		return nil, err
	}
	resp.Body = &cancelAwareReadCloser{ReadCloser: resp.Body, stop: stop}
	return resp, nil
}

type cancelAwareReadCloser struct {
	io.ReadCloser
	stop func()
}

func (c *cancelAwareReadCloser) Close() error {
	c.stop()
	return c.ReadCloser.Close()
}

type captureWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *captureWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *captureWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func headerToMap(header http.Header) map[string]string {
	if len(header) == 0 {
		return nil
	}
	out := make(map[string]string, len(header))
	for k, v := range header {
		out[k] = strings.Join(v, ", ")
	}
	return out
}

// requestIsSecure reports whether the request came in over TLS, either
// natively or via a trusted reverse proxy that set X-Forwarded-Proto.
func (s *Server) requestIsSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if len(s.trustedProxies) == 0 {
		return false
	}
	if !strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range s.trustedProxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
