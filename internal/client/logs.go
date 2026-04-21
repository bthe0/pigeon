package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TailLogs prints log entries, optionally filtered by forwardID or domain.
func TailLogs(filter string, since time.Duration, limit int, follow bool) error {
	logDir, err := LogDir()
	if err != nil {
		return err
	}

	entries, err := filepath.Glob(filepath.Join(logDir, "*.ndjson"))
	if err != nil {
		return err
	}
	// Also include daemon log
	daemonLog := filepath.Join(logDir, "daemon.log")
	if _, err := os.Stat(daemonLog); err == nil {
		entries = append(entries, daemonLog)
	}

	sort.Strings(entries)

	var cutoff time.Time
	if since > 0 {
		cutoff = time.Now().Add(-since)
	}

	count := 0
	for _, path := range entries {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			var entry LogEntry
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				// Raw log line (daemon.log)
				if filter == "" {
					fmt.Println(line)
					count++
				}
				continue
			}

			if filter != "" && entry.ForwardID != filter {
				continue
			}
			if !cutoff.IsZero() {
				t, err := time.Parse(time.RFC3339, entry.Time)
				if err == nil && t.Before(cutoff) {
					continue
				}
			}

			printEntry(entry)
			count++
			if limit > 0 && count >= limit {
				f.Close()
				return nil
			}
		}
		f.Close()
	}

	if !follow {
		return nil
	}

	// Follow the latest log file
	latest := latestNDJSON(logDir)
	if latest == "" {
		fmt.Println("No log file to follow.")
		return nil
	}
	return tailFollow(latest, filter)
}

func tailFollow(path, filter string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	f.Seek(0, io.SeekEnd)

	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			fmt.Println(line)
			continue
		}
		if filter == "" || entry.ForwardID == filter {
			printEntry(entry)
		}
	}
}

func printEntry(e LogEntry) {
	t, _ := time.Parse(time.RFC3339, e.Time)
	fmt.Printf("%s  %-4s  %-8s  %-6s  %s  %s",
		t.Format("2006-01-02 15:04:05"),
		e.Protocol,
		e.ForwardID,
		e.Action,
		e.RemoteAddr,
		ifBytes(e.Bytes),
	)
	fmt.Println()
}

func ifBytes(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("(%d bytes)", n)
}

func latestNDJSON(dir string) string {
	entries, _ := filepath.Glob(filepath.Join(dir, "*.ndjson"))
	if len(entries) == 0 {
		return ""
	}
	sort.Strings(entries)
	return entries[len(entries)-1]
}
