package client

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// When the calendar date advances, rotateIfNeeded must switch the writer to a
// new file — entries written before and after the rollover must land in
// different daily files. This is the invariant that keeps the log bounded.
func TestInspectorWriter_RotatesOnDateChange(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	iw := &InspectorWriter{}
	day1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	if err := iw.rotate(day1); err != nil {
		t.Fatalf("rotate day1: %v", err)
	}
	if err := iw.writeAt(day1, InspectorEntry{ForwardID: "a", Time: day1.Format(time.RFC3339)}); err != nil {
		t.Fatalf("write day1: %v", err)
	}

	day2 := time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)
	// rotateIfNeeded is the public-inside-package hook Write uses on every call.
	if err := iw.rotateIfNeeded(day2); err != nil {
		t.Fatalf("rotateIfNeeded day2: %v", err)
	}
	if err := iw.writeAt(day2, InspectorEntry{ForwardID: "b", Time: day2.Format(time.RFC3339)}); err != nil {
		t.Fatalf("write day2: %v", err)
	}
	iw.Close()

	logsDir := filepath.Join(dir, ".pigeon", "logs")
	matches, err := filepath.Glob(filepath.Join(logsDir, "inspector-*.ndjson"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 dated files, got %d: %v", len(matches), matches)
	}

	seen := map[string]string{} // forward_id → filename
	for _, path := range matches {
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			var e InspectorEntry
			if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
				continue
			}
			seen[e.ForwardID] = filepath.Base(path)
		}
		f.Close()
	}
	if seen["a"] == seen["b"] {
		t.Errorf("entries a and b in same file %q — rotation didn't happen", seen["a"])
	}
	if !strings.Contains(seen["a"], "2026-01-01") {
		t.Errorf("entry a in %q, want 2026-01-01 file", seen["a"])
	}
	if !strings.Contains(seen["b"], "2026-01-02") {
		t.Errorf("entry b in %q, want 2026-01-02 file", seen["b"])
	}
}

// Same-day writes must not churn the file descriptor.
func TestInspectorWriter_DoesNotRotateWithinSameDay(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	iw := &InspectorWriter{}
	day := time.Date(2026, 5, 10, 1, 0, 0, 0, time.UTC)
	if err := iw.rotate(day); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	firstFD := iw.f
	// Advance by 10 hours but same day.
	if err := iw.rotateIfNeeded(day.Add(10 * time.Hour)); err != nil {
		t.Fatalf("rotateIfNeeded same day: %v", err)
	}
	if iw.f != firstFD {
		t.Error("file handle changed within the same day")
	}
	iw.Close()
}

// writeAt is a test-only helper mirroring Write but pinning the "current
// time" so we can exercise the date boundary deterministically.
func (iw *InspectorWriter) writeAt(now time.Time, entry InspectorEntry) error {
	iw.mu.Lock()
	defer iw.mu.Unlock()
	if err := iw.rotateIfNeeded(now); err != nil {
		return err
	}
	return json.NewEncoder(iw.f).Encode(entry)
}
