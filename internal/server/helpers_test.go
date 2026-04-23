package server

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"testing"

	"github.com/bthe0/pigeon/internal/proto"
)

func TestHeaderToMap_Empty(t *testing.T) {
	if got := headerToMap(http.Header{}); got != nil {
		t.Errorf("got %v, want nil for empty header", got)
	}
}

func TestHeaderToMap_SingleValue(t *testing.T) {
	h := http.Header{"Content-Type": []string{"text/html"}}
	got := headerToMap(h)
	if got["Content-Type"] != "text/html" {
		t.Errorf("got %q", got["Content-Type"])
	}
}

// Multi-value headers (Set-Cookie, Accept, etc.) are common and must be
// joined with ", " so the full value survives into the inspector.
func TestHeaderToMap_MultipleValuesJoined(t *testing.T) {
	h := http.Header{"Set-Cookie": []string{"a=1", "b=2"}}
	got := headerToMap(h)
	if got["Set-Cookie"] != "a=1, b=2" {
		t.Errorf("got %q, want %q", got["Set-Cookie"], "a=1, b=2")
	}
}

// ── pageVariant ──────────────────────────────────────────────────────────────

func TestPageVariant(t *testing.T) {
	cases := map[string]string{
		"terminal": "terminal",
		"minimal":  "minimal",
		"default":  "default",
		"":         "default",
		"bogus":    "default",
		"TERMINAL": "default", // case-sensitive on purpose
	}
	for in, want := range cases {
		if got := pageVariant(in); got != want {
			t.Errorf("pageVariant(%q) = %q, want %q", in, got, want)
		}
	}
}

// ── logTraffic ───────────────────────────────────────────────────────────────

func TestLogTraffic_EmitsValidNDJSON(t *testing.T) {
	var buf bytes.Buffer
	s := &Server{logger: log.New(&buf, "", 0)}
	fwd := &forward{id: "fwd1", publicAddr: "app.example.com"}

	s.logTraffic(fwd, "10.0.0.1:443", "HTTP", "GET / 200 5ms", 123)

	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("logTraffic wrote nothing")
	}
	var e proto.TrafficLogEntry
	if err := json.Unmarshal([]byte(line), &e); err != nil {
		t.Fatalf("not valid JSON: %v\nline: %s", err, line)
	}
	if e.ForwardID != "fwd1" {
		t.Errorf("ForwardID = %q", e.ForwardID)
	}
	if e.Domain != "app.example.com" {
		t.Errorf("Domain = %q", e.Domain)
	}
	if e.Protocol != "HTTP" {
		t.Errorf("Protocol = %q", e.Protocol)
	}
	if e.Bytes != 123 {
		t.Errorf("Bytes = %d", e.Bytes)
	}
	if e.Time == "" {
		t.Errorf("Time missing")
	}
}

// ── captureWriter ────────────────────────────────────────────────────────────

type fakeRW struct {
	status int
	body   bytes.Buffer
	header http.Header
}

func (f *fakeRW) Header() http.Header {
	if f.header == nil {
		f.header = http.Header{}
	}
	return f.header
}
func (f *fakeRW) Write(b []byte) (int, error) { return f.body.Write(b) }
func (f *fakeRW) WriteHeader(s int)            { f.status = s }

func TestCaptureWriter_RecordsStatusAndBytes(t *testing.T) {
	rw := &fakeRW{}
	cw := &captureWriter{ResponseWriter: rw, status: http.StatusOK}
	cw.WriteHeader(http.StatusTeapot)
	n, err := cw.Write([]byte("hello"))
	if err != nil || n != 5 {
		t.Fatalf("write: n=%d err=%v", n, err)
	}
	if cw.status != http.StatusTeapot {
		t.Errorf("status = %d, want 418", cw.status)
	}
	if cw.bytes != 5 {
		t.Errorf("bytes = %d, want 5", cw.bytes)
	}
}

// Default-200 path: Write before WriteHeader should leave status at 200.
func TestCaptureWriter_DefaultsTo200_WhenWriteBeforeHeader(t *testing.T) {
	rw := &fakeRW{}
	// Initialize with zero to exercise the fallback branch.
	cw := &captureWriter{ResponseWriter: rw}
	cw.Write([]byte("abc"))
	if cw.status != http.StatusOK {
		t.Errorf("status = %d, want 200", cw.status)
	}
}
