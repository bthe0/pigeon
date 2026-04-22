package client_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bthe0/pigeon/internal/client"
	"github.com/bthe0/pigeon/internal/proto"
)

// writeNDJSON writes NDJSON log entries to a temp file inside the given log dir.
func writeNDJSON(t *testing.T, logDir string, entries []proto.TrafficLogEntry) string {
	t.Helper()
	path := filepath.Join(logDir, time.Now().Format("2006-01-02")+".ndjson")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		t.Fatalf("writeNDJSON open: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			t.Fatalf("writeNDJSON encode: %v", err)
		}
	}
	return path
}

func pigeonHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	// Pre-create ~/.pigeon/logs so LogDir() succeeds.
	logDir := filepath.Join(dir, ".pigeon", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return logDir
}

func TestTailLogs_EmptyDir(t *testing.T) {
	pigeonHome(t)
	// Should return nil and not panic.
	if err := client.TailLogs("", 0, 0, false); err != nil {
		t.Fatalf("TailLogs on empty dir: %v", err)
	}
}

func TestTailLogs_AllEntries(t *testing.T) {
	logDir := pigeonHome(t)

	entries := []proto.TrafficLogEntry{
		{Time: time.Now().Format(time.RFC3339), ForwardID: "f1", Protocol: "HTTP", Action: "GET /", RemoteAddr: "1.2.3.4:1234"},
		{Time: time.Now().Format(time.RFC3339), ForwardID: "f2", Protocol: "TCP", Action: "CONNECT", RemoteAddr: "5.6.7.8:5678"},
	}
	writeNDJSON(t, logDir, entries)

	// Should not error.
	if err := client.TailLogs("", 0, 0, false); err != nil {
		t.Fatalf("TailLogs all: %v", err)
	}
}

func TestTailLogs_FilterByForwardID(t *testing.T) {
	logDir := pigeonHome(t)

	entries := []proto.TrafficLogEntry{
		{Time: time.Now().Format(time.RFC3339), ForwardID: "wanted", Protocol: "HTTP", Action: "GET /"},
		{Time: time.Now().Format(time.RFC3339), ForwardID: "other", Protocol: "TCP", Action: "CONNECT"},
	}
	writeNDJSON(t, logDir, entries)

	// Should not error (filtering is done internally; we verify no crash).
	if err := client.TailLogs("wanted", 0, 0, false); err != nil {
		t.Fatalf("TailLogs filter: %v", err)
	}
}

func TestTailLogs_Limit(t *testing.T) {
	logDir := pigeonHome(t)

	var entries []proto.TrafficLogEntry
	for i := 0; i < 10; i++ {
		entries = append(entries, proto.TrafficLogEntry{
			Time:      time.Now().Format(time.RFC3339),
			ForwardID: "f1",
			Protocol:  "HTTP",
			Action:    "GET /",
		})
	}
	writeNDJSON(t, logDir, entries)

	if err := client.TailLogs("", 0, 3, false); err != nil {
		t.Fatalf("TailLogs limit: %v", err)
	}
}

func TestTailLogs_Since(t *testing.T) {
	logDir := pigeonHome(t)

	old := proto.TrafficLogEntry{
		Time:      time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
		ForwardID: "f1",
		Protocol:  "HTTP",
		Action:    "GET /old",
	}
	recent := proto.TrafficLogEntry{
		Time:      time.Now().Format(time.RFC3339),
		ForwardID: "f1",
		Protocol:  "HTTP",
		Action:    "GET /recent",
	}
	writeNDJSON(t, logDir, []proto.TrafficLogEntry{old, recent})

	// --since 1h should filter out the 2-hour-old entry.
	if err := client.TailLogs("", time.Hour, 0, false); err != nil {
		t.Fatalf("TailLogs since: %v", err)
	}
}

func TestTailLogs_MalformedLines(t *testing.T) {
	logDir := pigeonHome(t)

	path := filepath.Join(logDir, time.Now().Format("2006-01-02")+".ndjson")
	os.WriteFile(path, []byte("not-json\nalso-not-json\n"), 0644)

	// Should not error — malformed lines for empty filter should be printed as raw.
	if err := client.TailLogs("", 0, 0, false); err != nil {
		t.Fatalf("TailLogs malformed: %v", err)
	}
}

// ── FetchRecentLogs ────────────────────────────────────────────────────────────

func TestFetchRecentLogs_EmptyDir(t *testing.T) {
	pigeonHome(t)
	_, err := client.FetchRecentLogs("", 0)
	if err != nil {
		t.Fatalf("FetchRecentLogs on empty dir: %v", err)
	}
}

func TestFetchRecentLogs_ReturnsEntries(t *testing.T) {
	logDir := pigeonHome(t)
	now := time.Now()
	writeNDJSON(t, logDir, []proto.TrafficLogEntry{
		{Time: now.Format(time.RFC3339), ForwardID: "f1", Protocol: "HTTP", Action: "GET /"},
		{Time: now.Format(time.RFC3339), ForwardID: "f2", Protocol: "TCP", Action: "CONNECT"},
	})

	entries, err := client.FetchRecentLogs("", 0)
	if err != nil {
		t.Fatalf("FetchRecentLogs: %v", err)
	}
	found := 0
	for _, e := range entries {
		if e.ForwardID == "f1" || e.ForwardID == "f2" {
			found++
		}
	}
	if found != 2 {
		t.Errorf("expected 2 matching entries, got %d (total %d)", found, len(entries))
	}
}

func TestFetchRecentLogs_FilterByForwardID(t *testing.T) {
	logDir := pigeonHome(t)
	now := time.Now()
	writeNDJSON(t, logDir, []proto.TrafficLogEntry{
		{Time: now.Format(time.RFC3339), ForwardID: "wanted", Protocol: "HTTP", Action: "GET /"},
		{Time: now.Format(time.RFC3339), ForwardID: "other", Protocol: "TCP", Action: "CONNECT"},
	})

	entries, err := client.FetchRecentLogs("wanted", 0)
	if err != nil {
		t.Fatalf("FetchRecentLogs filter: %v", err)
	}
	for _, e := range entries {
		if e.Protocol != "DAEMON" && e.ForwardID != "wanted" {
			t.Errorf("unexpected forward ID %q in filtered results", e.ForwardID)
		}
	}
}

func TestFetchRecentLogs_LimitApplied(t *testing.T) {
	logDir := pigeonHome(t)
	now := time.Now()
	var bulk []proto.TrafficLogEntry
	for i := 0; i < 20; i++ {
		bulk = append(bulk, proto.TrafficLogEntry{
			Time: now.Format(time.RFC3339), ForwardID: "f1", Protocol: "HTTP", Action: "GET /",
		})
	}
	writeNDJSON(t, logDir, bulk)

	entries, err := client.FetchRecentLogs("", 5)
	if err != nil {
		t.Fatalf("FetchRecentLogs limit: %v", err)
	}
	if len(entries) > 5 {
		t.Errorf("expected at most 5 entries, got %d", len(entries))
	}
}

// ── UpdateMetrics / GetMetrics ─────────────────────────────────────────────────

func TestUpdateMetrics_Basic(t *testing.T) {
	client.UpdateMetrics("metrics-test-fwd", 100)
	client.UpdateMetrics("metrics-test-fwd", 200)

	m, err := client.GetMetrics()
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}
	got, ok := m["metrics-test-fwd"]
	if !ok {
		t.Fatal("expected metrics entry for 'metrics-test-fwd'")
	}
	if got.Requests < 2 {
		t.Errorf("Requests = %d, want >= 2", got.Requests)
	}
	if got.Bytes < 300 {
		t.Errorf("Bytes = %d, want >= 300", got.Bytes)
	}
}
