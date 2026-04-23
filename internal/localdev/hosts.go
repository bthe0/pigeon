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
func AddHost(hostname string) error { return addHostAt(hostsFile, hostname) }

// RemoveHosts removes all pigeon-managed entries from /etc/hosts.
func RemoveHosts() error { return removeHostsAt(hostsFile) }

// addHostAt and removeHostsAt take an explicit path so tests can exercise
// the rewrite logic without touching the real /etc/hosts.
func addHostAt(path, hostname string) error {
	entries, err := readHosts(path)
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
	return writeHosts(path, entries)
}

func removeHostsAt(path string) error {
	entries, err := readHosts(path)
	if err != nil {
		return err
	}
	var kept []string
	for _, e := range entries {
		if !strings.Contains(e, pigeonMarker) {
			kept = append(kept, e)
		}
	}
	return writeHosts(path, kept)
}

func readHosts(path string) ([]string, error) {
	f, err := os.Open(path)
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

func writeHosts(path string, lines []string) error {
	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0644)
}
