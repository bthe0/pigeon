package localdev

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

const systemKeychain = "/Library/Keychains/System.keychain"

func DefaultCertDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pigeon", "dev-certs")
}

func CertPaths(certDir string) (certFile, keyFile string) {
	return filepath.Join(certDir, "cert.pem"), filepath.Join(certDir, "key.pem")
}

// GenerateCert creates a self-signed certificate for domain and *.domain,
// writing cert.pem and key.pem into certDir. Returns their paths.
func GenerateCert(domain, certDir string) (certFile, keyFile string, err error) {
	if err = os.MkdirAll(certDir, 0700); err != nil {
		return
	}
	certFile, keyFile = CertPaths(certDir)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: domain},
		DNSNames:     []string{domain, "*." + domain},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(2 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return
	}

	cf, err := os.OpenFile(certFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return
	}
	defer cf.Close()
	if err = pem.Encode(cf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		return
	}

	kf, err := os.OpenFile(keyFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return
	}
	defer kf.Close()
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return
	}
	err = pem.Encode(kf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})
	return
}

func TrustCert(certFile string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("trusting the dev certificate is only supported on macOS")
	}
	if _, err := os.Stat(certFile); err != nil {
		return fmt.Errorf("stat cert %s: %w", certFile, err)
	}
	cmd := exec.Command("security", "add-trusted-cert", "-d", "-r", "trustRoot", "-k", systemKeychain, certFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("security add-trusted-cert: %w", err)
	}
	return nil
}
