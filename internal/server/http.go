package server

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/base64"
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
	"unicode/utf8"

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

	fwd := s.lookupHTTPForward(host)
	if fwd == nil {
		writeStatusPage(w, http.StatusBadGateway, "default", "tunnel not found", "No active tunnel is currently registered for "+host+".")
		return
	}

	clientAddr := clientIP(r.RemoteAddr, r.Header)
	if !fwd.allows(clientAddr) {
		writeStatusPage(w, http.StatusForbidden, pageVariant(fwd.unavailablePage), "Forbidden", "Your IP is not permitted to access this tunnel.")
		return
	}

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
	browser, osName := browserAndOS(r.UserAgent())
	geo := s.lookupGeo(clientAddr)

	// Capture request body up to the cap. We always read into the buffer first
	// then re-attach a reader that replays the captured bytes followed by the
	// remaining stream — preserves streaming for bodies larger than the cap.
	var reqBody []byte
	var reqTruncated bool
	if fwd.captureBodies && r.Body != nil {
		reqBody, reqTruncated, r.Body = captureBody(r.Body, proto.MaxCapturedBodyBytes)
	}

	// The host the upstream reverse-proxy URL points at is irrelevant — the
	// connTransport sends the request directly down the yamux stream — but
	// httputil insists on a non-empty host, so reuse the request's host.
	target, _ := url.Parse("http://" + host)
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.Transport = &connTransport{conn: stream}
	var responseHeaders map[string]string
	var respCapture *bodyCapture
	rp.ModifyResponse = func(resp *http.Response) error {
		responseHeaders = headerToMap(resp.Header)
		if fwd.captureBodies && resp.Body != nil {
			respCapture = &bodyCapture{}
			resp.Body = newCappedTeeReader(resp.Body, respCapture, proto.MaxCapturedBodyBytes)
		}
		return nil
	}
	rp.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		writeStatusPage(rw, http.StatusBadGateway, pageVariant(fwd.unavailablePage), "Tunnel unavailable", "The tunnel could not reach the local service right now.")
	}
	cw := &captureWriter{ResponseWriter: w, status: http.StatusOK}
	rp.ServeHTTP(cw, r)

	event := proto.InspectorEventPayload{
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
	}
	if fwd.captureBodies {
		event.RequestBody, event.RequestBodyEncoding = encodeBody(reqBody)
		event.RequestBodyTruncated = reqTruncated
		if respCapture != nil {
			event.ResponseBody, event.ResponseBodyEncoding = encodeBody(respCapture.buf.Bytes())
			event.ResponseBodyTruncated = respCapture.truncated
		}
	}
	fwd.session.writeMessage(proto.Message{Type: proto.MsgInspectorEvent, Payload: event})

	s.logTraffic(fwd, r.RemoteAddr, "HTTP", fmt.Sprintf("%s %s %d %dms", r.Method, r.URL.Path, cw.status, time.Since(start).Milliseconds()), cw.bytes)
}

// lookupHTTPForward resolves host to a forward. Exact matches win; otherwise
// we fall back to one-level wildcard matches (`*.suffix`), which only fire
// when host has the form `<one-label>.<suffix>` (no nested subdomains).
func (s *Server) lookupHTTPForward(host string) *forward {
	if v, ok := s.sessions.Load("http:" + host); ok {
		return v.(*forward)
	}
	idx := strings.IndexByte(host, '.')
	if idx <= 0 {
		return nil
	}
	suffix := host[idx+1:]
	if v, ok := s.wildcards.Load(suffix); ok {
		return v.(*forward)
	}
	return nil
}

// captureBody reads up to limit bytes from rc into a buffer and returns a
// replacement ReadCloser that replays the captured bytes followed by the
// remaining stream, so the downstream reader sees the full body.
func captureBody(rc io.ReadCloser, limit int) (captured []byte, truncated bool, replacement io.ReadCloser) {
	buf := make([]byte, limit)
	n, err := io.ReadFull(rc, buf)
	switch err {
	case nil:
		// We filled the buffer exactly — there might still be more on the wire.
		truncated = true
		captured = buf[:n]
		// Drain the rest into a separate buffer so the proxy still sees the
		// full body. Bounded by the request as a whole, not by us.
		rest, _ := io.ReadAll(rc)
		_ = rc.Close()
		replacement = io.NopCloser(io.MultiReader(bytes.NewReader(captured), bytes.NewReader(rest)))
	case io.ErrUnexpectedEOF, io.EOF:
		captured = buf[:n]
		_ = rc.Close()
		replacement = io.NopCloser(bytes.NewReader(captured))
	default:
		captured = buf[:n]
		_ = rc.Close()
		replacement = io.NopCloser(bytes.NewReader(captured))
	}
	return
}

// bodyCapture accumulates the first N bytes of a stream while passing the
// rest through unchanged. Used on the response side via newCappedTeeReader.
type bodyCapture struct {
	buf       bytes.Buffer
	truncated bool
}

type cappedTeeReader struct {
	src io.ReadCloser
	cap *bodyCapture
	max int
}

func newCappedTeeReader(src io.ReadCloser, cap *bodyCapture, max int) io.ReadCloser {
	return &cappedTeeReader{src: src, cap: cap, max: max}
}

func (t *cappedTeeReader) Read(p []byte) (int, error) {
	n, err := t.src.Read(p)
	if n > 0 && t.cap.buf.Len() < t.max {
		room := t.max - t.cap.buf.Len()
		if n <= room {
			t.cap.buf.Write(p[:n])
		} else {
			t.cap.buf.Write(p[:room])
			t.cap.truncated = true
		}
	}
	return n, err
}

func (t *cappedTeeReader) Close() error { return t.src.Close() }

// encodeBody returns the captured body as either a UTF-8 string (encoding "")
// or a base64-encoded blob (encoding "base64") when the bytes don't form
// valid UTF-8 or contain NULs typical of binary content.
func encodeBody(b []byte) (string, string) {
	if len(b) == 0 {
		return "", ""
	}
	if utf8.Valid(b) && !bytes.ContainsRune(b, 0) {
		return string(b), ""
	}
	return base64.StdEncoding.EncodeToString(b), "base64"
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
