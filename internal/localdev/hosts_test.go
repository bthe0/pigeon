package localdev

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempHosts(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("seed hosts: %v", err)
	}
	return path
}

func TestAddHostAt_AppendsEntry(t *testing.T) {
	path := writeTempHosts(t, "127.0.0.1 localhost\n::1 localhost\n")
	if err := addHostAt(path, "foo.test"); err != nil {
		t.Fatalf("addHostAt: %v", err)
	}
	b, _ := os.ReadFile(path)
	got := string(b)
	if !strings.Contains(got, "127.0.0.1 foo.test "+pigeonMarker) {
		t.Errorf("expected pigeon entry in hosts, got:\n%s", got)
	}
	if !strings.Contains(got, "127.0.0.1 localhost") {
		t.Errorf("existing entries dropped:\n%s", got)
	}
}

func TestAddHostAt_Idempotent(t *testing.T) {
	path := writeTempHosts(t, "127.0.0.1 localhost\n")
	for i := 0; i < 3; i++ {
		if err := addHostAt(path, "foo.test"); err != nil {
			t.Fatalf("addHostAt %d: %v", i, err)
		}
	}
	b, _ := os.ReadFile(path)
	if n := strings.Count(string(b), "foo.test"); n != 1 {
		t.Errorf("expected 1 foo.test entry, got %d", n)
	}
}

func TestRemoveHostsAt_DropsOnlyPigeonEntries(t *testing.T) {
	path := writeTempHosts(t, "127.0.0.1 localhost\n")
	if err := addHostAt(path, "a.test"); err != nil {
		t.Fatal(err)
	}
	if err := addHostAt(path, "b.test"); err != nil {
		t.Fatal(err)
	}
	if err := removeHostsAt(path); err != nil {
		t.Fatalf("removeHostsAt: %v", err)
	}
	b, _ := os.ReadFile(path)
	got := string(b)
	if strings.Contains(got, pigeonMarker) {
		t.Errorf("pigeon entries not removed:\n%s", got)
	}
	if !strings.Contains(got, "127.0.0.1 localhost") {
		t.Errorf("non-pigeon entries removed:\n%s", got)
	}
}
