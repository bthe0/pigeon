package localdev

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateCert_CreatesValidCertAndKey(t *testing.T) {
	dir := t.TempDir()

	certFile, keyFile, err := GenerateCert("example.test", dir)
	if err != nil {
		t.Fatalf("GenerateCert: %v", err)
	}
	if certFile != filepath.Join(dir, "cert.pem") {
		t.Errorf("cert path = %q", certFile)
	}
	if keyFile != filepath.Join(dir, "key.pem") {
		t.Errorf("key path = %q", keyFile)
	}

	// Key file must be 0600.
	info, err := os.Stat(keyFile)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("key mode = %o, want 0600", mode)
	}

	// Cert must parse and cover both the base domain and the wildcard.
	b, err := os.ReadFile(certFile)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	block, _ := pem.Decode(b)
	if block == nil {
		t.Fatalf("cert is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	want := map[string]bool{"example.test": false, "*.example.test": false}
	for _, name := range cert.DNSNames {
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("cert missing DNS SAN %q (have %v)", name, cert.DNSNames)
		}
	}
}

func TestGenerateCert_Reuses_WhenBothFilesExist(t *testing.T) {
	dir := t.TempDir()

	c1, k1, err := GenerateCert("example.test", dir)
	if err != nil {
		t.Fatalf("first GenerateCert: %v", err)
	}
	b1, err := os.ReadFile(c1)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}

	c2, k2, err := GenerateCert("example.test", dir)
	if err != nil {
		t.Fatalf("second GenerateCert: %v", err)
	}
	if c1 != c2 || k1 != k2 {
		t.Errorf("paths changed on re-gen")
	}
	b2, _ := os.ReadFile(c2)
	if string(b1) != string(b2) {
		t.Errorf("cert contents regenerated when they should have been reused")
	}
}
