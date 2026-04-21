package localdev

import (
	"fmt"
	"os"
)

const resolverDir = "/etc/resolver"

// SetupResolver creates /etc/resolver/<domain> pointing to our local DNS server.
func SetupResolver(domain string) error {
	if err := os.MkdirAll(resolverDir, 0755); err != nil {
		return err
	}
	content := fmt.Sprintf("nameserver 127.0.0.1\nport %d\n", DNSPort)
	path := resolverDir + "/" + domain
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// RemoveResolver deletes /etc/resolver/<domain>.
func RemoveResolver(domain string) {
	os.Remove(resolverDir + "/" + domain)
}
