package client_test

import (
	"os"
	"testing"

	"github.com/bthe0/pigeon/internal/client"
)

// ── IsDaemon ───────────────────────────────────────────────────────────────────

func TestIsDaemon_NotSet(t *testing.T) {
	os.Unsetenv("PIGEON_DAEMON")
	if client.IsDaemon() {
		t.Fatal("expected IsDaemon()=false when env not set")
	}
}

func TestIsDaemon_Set(t *testing.T) {
	t.Setenv("PIGEON_DAEMON", "1")
	if !client.IsDaemon() {
		t.Fatal("expected IsDaemon()=true when PIGEON_DAEMON=1")
	}
}

func TestIsDaemon_WrongValue(t *testing.T) {
	t.Setenv("PIGEON_DAEMON", "true")
	if client.IsDaemon() {
		t.Fatal("expected IsDaemon()=false for value 'true' (only '1' is valid)")
	}
}

// ── DaemonStatus (smoke test — checks it doesn't panic) ────────────────────────

func TestDaemonStatus_Smoke(t *testing.T) {
	// Use a fresh HOME so there's no stale PID file.
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// Should not panic even with no PID file.
	client.DaemonStatus()
}

// ── PIDFile path ───────────────────────────────────────────────────────────────

func TestPIDFile_ReturnsPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	path, err := client.PIDFile()
	if err != nil {
		t.Fatalf("PIDFile: %v", err)
	}
	if path == "" {
		t.Fatal("PIDFile returned empty string")
	}
}

// ── LogDir ─────────────────────────────────────────────────────────────────────

func TestLogDir_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	logDir, err := client.LogDir()
	if err != nil {
		t.Fatalf("LogDir: %v", err)
	}
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		t.Fatalf("LogDir did not create directory: %s", logDir)
	}
}
