package localdev

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

const hostsFile = "/etc/hosts"
const pigeonMarker = "# pigeon-local"

// AddHost adds "127.0.0.1 hostname" to /etc/hosts if not already present.
func AddHost(hostname string) error {
	entries, err := readHosts()
	if err != nil {
		return err
	}
	line := fmt.Sprintf("127.0.0.1 %s %s", hostname, pigeonMarker)
	for _, e := range entries {
		if strings.Contains(e, " "+hostname+" ") || strings.HasSuffix(e, " "+hostname) {
			return nil // already present
		}
	}
	entries = append(entries, line)
	return writeHosts(entries)
}

// RemoveHosts removes all pigeon-managed entries from /etc/hosts.
func RemoveHosts() error {
	entries, err := readHosts()
	if err != nil {
		return err
	}
	var kept []string
	for _, e := range entries {
		if !strings.Contains(e, pigeonMarker) {
			kept = append(kept, e)
		}
	}
	return writeHosts(kept)
}

func readHosts() ([]string, error) {
	f, err := os.Open(hostsFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, sc.Err()
}

func writeHosts(lines []string) error {
	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(hostsFile, []byte(content), 0644)
}
