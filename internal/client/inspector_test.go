package client_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bthe0/pigeon/internal/client"
)

// ── FetchRecentInspectorEntries ────────────────────────────────────────────────

func TestFetchRecentInspectorEntries_NoFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	entries, err := client.FetchRecentInspectorEntries(0, "")
	if err != nil {
		t.Fatalf("expected no error when file absent, got: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestFetchRecentInspectorEntries_BasicWrite(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	logDir := filepath.Join(dir, ".pigeon", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	iw, err := client.NewInspectorWriter()
	if err != nil {
		t.Fatalf("NewInspectorWriter: %v", err)
	}
	defer iw.Close()

	entry := client.InspectorEntry{
		Time:      time.Now().Format(time.RFC3339),
		ForwardID: "fwd1",
		Domain:    "app.example.com",
		Method:    "GET",
		Path:      "/hello",
		Status:    200,
	}
	if err := iw.Write(entry); err != nil {
		t.Fatalf("Write: %v", err)
	}
	iw.Close()

	got, err := client.FetchRecentInspectorEntries(0, "")
	if err != nil {
		t.Fatalf("FetchRecentInspectorEntries: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
	if got[0].ForwardID != "fwd1" {
		t.Errorf("ForwardID = %q, want fwd1", got[0].ForwardID)
	}
	if got[0].Path != "/hello" {
		t.Errorf("Path = %q, want /hello", got[0].Path)
	}
}

func TestFetchRecentInspectorEntries_FilterByForwardID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	logDir := filepath.Join(dir, ".pigeon", "logs")
	os.MkdirAll(logDir, 0755)

	iw, err := client.NewInspectorWriter()
	if err != nil {
		t.Fatalf("NewInspectorWriter: %v", err)
	}
	iw.Write(client.InspectorEntry{ForwardID: "wanted", Domain: "a.com", Time: time.Now().Format(time.RFC3339)})
	iw.Write(client.InspectorEntry{ForwardID: "other", Domain: "b.com", Time: time.Now().Format(time.RFC3339)})
	iw.Close()

	got, err := client.FetchRecentInspectorEntries(0, "wanted")
	if err != nil {
		t.Fatalf("FetchRecentInspectorEntries: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 entry after filter, got %d", len(got))
	}
	if got[0].ForwardID != "wanted" {
		t.Errorf("ForwardID = %q, want wanted", got[0].ForwardID)
	}
}

func TestFetchRecentInspectorEntries_FilterByDomain(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	logDir := filepath.Join(dir, ".pigeon", "logs")
	os.MkdirAll(logDir, 0755)

	iw, _ := client.NewInspectorWriter()
	iw.Write(client.InspectorEntry{ForwardID: "f1", Domain: "match.com", Time: time.Now().Format(time.RFC3339)})
	iw.Write(client.InspectorEntry{ForwardID: "f2", Domain: "other.com", Time: time.Now().Format(time.RFC3339)})
	iw.Close()

	got, err := client.FetchRecentInspectorEntries(0, "match.com")
	if err != nil {
		t.Fatalf("FetchRecentInspectorEntries: %v", err)
	}
	if len(got) != 1 || got[0].Domain != "match.com" {
		t.Errorf("expected 1 entry with domain=match.com, got %d entries", len(got))
	}
}

func TestFetchRecentInspectorEntries_LimitApplied(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	logDir := filepath.Join(dir, ".pigeon", "logs")
	os.MkdirAll(logDir, 0755)

	iw, _ := client.NewInspectorWriter()
	for i := 0; i < 10; i++ {
		iw.Write(client.InspectorEntry{
			ForwardID: "f1",
			Domain:    "app.com",
			Time:      time.Now().Format(time.RFC3339),
			Status:    200,
		})
	}
	iw.Close()

	got, err := client.FetchRecentInspectorEntries(3, "")
	if err != nil {
		t.Fatalf("FetchRecentInspectorEntries: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 entries (limit), got %d", len(got))
	}
}

func TestFetchRecentInspectorEntries_MalformedLinesSkipped(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	logDir := filepath.Join(dir, ".pigeon", "logs")
	os.MkdirAll(logDir, 0755)

	// Write one malformed line and one valid entry.
	logPath := filepath.Join(logDir, "inspector.ndjson")
	f, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY, 0600)
	f.WriteString("not-valid-json\n")
	f.Close()

	iw, _ := client.NewInspectorWriter()
	iw.Write(client.InspectorEntry{ForwardID: "ok", Domain: "x.com", Time: time.Now().Format(time.RFC3339)})
	iw.Close()

	got, err := client.FetchRecentInspectorEntries(0, "")
	if err != nil {
		t.Fatalf("FetchRecentInspectorEntries: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 valid entry (malformed skipped), got %d", len(got))
	}
}

func TestNewInspectorWriter_CreatesRestrictedFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	iw, err := client.NewInspectorWriter()
	if err != nil {
		t.Fatalf("NewInspectorWriter: %v", err)
	}
	defer iw.Close()

	// Inspector logs rotate daily — match today's dated file.
	logsDir := filepath.Join(dir, ".pigeon", "logs")
	matches, err := filepath.Glob(filepath.Join(logsDir, "inspector-*.ndjson"))
	if err != nil {
		t.Fatalf("glob inspector logs: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 inspector log file, got %d (%v)", len(matches), matches)
	}
	info, err := os.Stat(matches[0])
	if err != nil {
		t.Fatalf("stat inspector log: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Fatalf("mode = %o, want 0600", mode)
	}
}
