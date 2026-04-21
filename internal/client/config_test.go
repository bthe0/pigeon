package client_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bthe0/pigeon/internal/client"
	"github.com/bthe0/pigeon/internal/proto"
)

// ── helpers ────────────────────────────────────────────────────────────────────

func tempConfigPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "config.json")
}

func writeConfig(t *testing.T, path string, cfg *client.Config) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatalf("writeConfig open: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		t.Fatalf("writeConfig encode: %v", err)
	}
}

// ── Config.AddForward ──────────────────────────────────────────────────────────

func TestAddForward_Basic(t *testing.T) {
	cfg := &client.Config{}
	rule := client.ForwardRule{
		ID:        "abc",
		Protocol:  proto.ProtoHTTP,
		LocalAddr: "localhost:3000",
		Domain:    "myapp.example.com",
	}
	if err := cfg.AddForward(rule); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Forwards) != 1 {
		t.Fatalf("expected 1 forward, got %d", len(cfg.Forwards))
	}
	if cfg.Forwards[0].ID != "abc" {
		t.Errorf("ID: got %q want %q", cfg.Forwards[0].ID, "abc")
	}
}

func TestAddForward_DuplicateID(t *testing.T) {
	cfg := &client.Config{}
	rule := client.ForwardRule{ID: "dup", Protocol: proto.ProtoTCP, LocalAddr: "localhost:5432"}
	if err := cfg.AddForward(rule); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := cfg.AddForward(rule); err == nil {
		t.Fatal("expected error for duplicate rule, got nil")
	}
}

func TestAddForward_DuplicateContent(t *testing.T) {
	cfg := &client.Config{}
	r1 := client.ForwardRule{ID: "id1", Protocol: proto.ProtoHTTP, LocalAddr: "localhost:80", Domain: "x.example.com"}
	r2 := client.ForwardRule{ID: "id2", Protocol: proto.ProtoHTTP, LocalAddr: "localhost:80", Domain: "x.example.com"}
	cfg.AddForward(r1)
	if err := cfg.AddForward(r2); err == nil {
		t.Fatal("expected duplicate content error, got nil")
	}
}

func TestAddForward_MultipleDistinct(t *testing.T) {
	cfg := &client.Config{}
	rules := []client.ForwardRule{
		{ID: "a", Protocol: proto.ProtoHTTP, LocalAddr: "localhost:3000"},
		{ID: "b", Protocol: proto.ProtoTCP, LocalAddr: "localhost:5432"},
		{ID: "c", Protocol: proto.ProtoUDP, LocalAddr: "localhost:7777", RemotePort: 7777},
	}
	for _, r := range rules {
		if err := cfg.AddForward(r); err != nil {
			t.Fatalf("add %q: %v", r.ID, err)
		}
	}
	if len(cfg.Forwards) != 3 {
		t.Fatalf("expected 3 forwards, got %d", len(cfg.Forwards))
	}
}

func TestAddForward_NormalizesShortHTTPDomainWithBaseDomain(t *testing.T) {
	cfg := &client.Config{BaseDomain: "pigeon.local"}
	rule := client.ForwardRule{
		ID:         "abc",
		Protocol:   proto.ProtoHTTP,
		LocalAddr:  "localhost:3000",
		Domain:     "asd",
		PublicAddr: "asd",
	}
	if err := cfg.AddForward(rule); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.Forwards[0].Domain; got != "asd.pigeon.local" {
		t.Fatalf("Domain: got %q want %q", got, "asd.pigeon.local")
	}
	if got := cfg.Forwards[0].PublicAddr; got != "asd.pigeon.local" {
		t.Fatalf("PublicAddr: got %q want %q", got, "asd.pigeon.local")
	}
}

// ── Config.RemoveForward ───────────────────────────────────────────────────────

func TestRemoveForward_ByID(t *testing.T) {
	cfg := &client.Config{
		Forwards: []client.ForwardRule{
			{ID: "del", Protocol: proto.ProtoHTTP, LocalAddr: "localhost:80"},
			{ID: "keep", Protocol: proto.ProtoTCP, LocalAddr: "localhost:5432"},
		},
	}
	if !cfg.RemoveForward("del") {
		t.Fatal("expected true, got false")
	}
	if len(cfg.Forwards) != 1 {
		t.Fatalf("expected 1 forward after remove, got %d", len(cfg.Forwards))
	}
	if cfg.Forwards[0].ID != "keep" {
		t.Errorf("remaining ID: got %q want %q", cfg.Forwards[0].ID, "keep")
	}
}

func TestRemoveForward_ByDomain(t *testing.T) {
	cfg := &client.Config{
		Forwards: []client.ForwardRule{
			{ID: "x1", Protocol: proto.ProtoHTTP, LocalAddr: "localhost:80", Domain: "my.example.com"},
		},
	}
	if !cfg.RemoveForward("my.example.com") {
		t.Fatal("expected removal by domain, got false")
	}
	if len(cfg.Forwards) != 0 {
		t.Fatalf("expected 0 forwards, got %d", len(cfg.Forwards))
	}
}

func TestRemoveForward_ByPort(t *testing.T) {
	cfg := &client.Config{
		Forwards: []client.ForwardRule{
			{ID: "p1", Protocol: proto.ProtoTCP, LocalAddr: "localhost:5432", RemotePort: 5432},
		},
	}
	if !cfg.RemoveForward("5432") {
		t.Fatal("expected removal by port, got false")
	}
}

func TestRemoveForward_NotFound(t *testing.T) {
	cfg := &client.Config{
		Forwards: []client.ForwardRule{
			{ID: "existing", Protocol: proto.ProtoTCP, LocalAddr: "localhost:8080"},
		},
	}
	if cfg.RemoveForward("ghost") {
		t.Fatal("expected false for non-existent forward, got true")
	}
	if len(cfg.Forwards) != 1 {
		t.Fatalf("expected forwards unchanged, got %d", len(cfg.Forwards))
	}
}

func TestRemoveForward_Empty(t *testing.T) {
	cfg := &client.Config{}
	if cfg.RemoveForward("anything") {
		t.Fatal("expected false on empty forwards")
	}
}

// ── SaveConfig / LoadConfig round-trip ─────────────────────────────────────────
// These tests swap out the config path via environment so they don't pollute ~/.pigeon.

func TestSaveLoadConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cfg := &client.Config{
		Server: "tun.example.com:2222",
		Token:  "secret-token",
		Forwards: []client.ForwardRule{
			{ID: "f1", Protocol: proto.ProtoHTTP, LocalAddr: "localhost:3000", Domain: "app.tun.example.com"},
			{ID: "f2", Protocol: proto.ProtoTCP, LocalAddr: "localhost:5432", RemotePort: 5432},
		},
	}

	if err := client.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	loaded, err := client.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if loaded.Server != cfg.Server {
		t.Errorf("Server: got %q want %q", loaded.Server, cfg.Server)
	}
	if loaded.Token != cfg.Token {
		t.Errorf("Token: got %q want %q", loaded.Token, cfg.Token)
	}
	if len(loaded.Forwards) != 2 {
		t.Fatalf("Forwards len: got %d want 2", len(loaded.Forwards))
	}
	if loaded.Forwards[0].ID != "f1" {
		t.Errorf("Forwards[0].ID: got %q want %q", loaded.Forwards[0].ID, "f1")
	}
	if loaded.Forwards[1].RemotePort != 5432 {
		t.Errorf("Forwards[1].RemotePort: got %d want 5432", loaded.Forwards[1].RemotePort)
	}
}

func TestLoadConfig_NormalizesShortHTTPDomainsWithBaseDomain(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cfg := &client.Config{
		Server:     "127.0.0.1:2222",
		Token:      "secret-token",
		BaseDomain: "pigeon.local",
		Forwards: []client.ForwardRule{
			{ID: "f1", Protocol: proto.ProtoHTTP, LocalAddr: "localhost:3000", Domain: "asd", PublicAddr: "asd"},
		},
	}

	if err := client.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	loaded, err := client.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := loaded.Forwards[0].Domain; got != "asd.pigeon.local" {
		t.Fatalf("Domain: got %q want %q", got, "asd.pigeon.local")
	}
	if got := loaded.Forwards[0].PublicAddr; got != "asd.pigeon.local" {
		t.Fatalf("PublicAddr: got %q want %q", got, "asd.pigeon.local")
	}
}

func TestLoadConfig_NotInitialised(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	_, err := client.LoadConfig()
	if err == nil {
		t.Fatal("expected error when config file does not exist, got nil")
	}
}

func TestSaveConfig_Overwrites(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cfg1 := &client.Config{Server: "old.example.com:2222", Token: "tok1"}
	client.SaveConfig(cfg1)

	cfg2 := &client.Config{Server: "new.example.com:2222", Token: "tok2"}
	if err := client.SaveConfig(cfg2); err != nil {
		t.Fatalf("SaveConfig overwrite: %v", err)
	}

	loaded, err := client.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if loaded.Server != "new.example.com:2222" {
		t.Errorf("Server: got %q want %q", loaded.Server, "new.example.com:2222")
	}
}
