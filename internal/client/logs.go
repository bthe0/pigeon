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
	"sync"
	"time"

	"github.com/bthe0/pigeon/internal/proto"
)

var (
	metricsMu  sync.RWMutex
	metricsMap = make(map[string]*ForwardMetrics)
)

func UpdateMetrics(id string, bytes int) {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	m, ok := metricsMap[id]
	if !ok {
		m = &ForwardMetrics{}
		metricsMap[id] = m
	}
	m.Requests++
	m.Bytes += int64(bytes)
}

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

			var entry proto.TrafficLogEntry
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
		var entry proto.TrafficLogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			fmt.Println(line)
			continue
		}
		if filter == "" || entry.ForwardID == filter {
			printEntry(entry)
		}
	}
}

func printEntry(e proto.TrafficLogEntry) {
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

// FetchRecentLogs returns recent JSON logs as structs.
func FetchRecentLogs(filter string, limit int) ([]proto.TrafficLogEntry, error) {
	logDir, err := LogDir()
	if err != nil {
		return nil, err
	}

	entries := readDaemonLogTail(filepath.Join(logDir, "daemon.log"), 50)

	latest := latestNDJSON(logDir)
	if latest != "" {
		var keep func(*proto.TrafficLogEntry) bool
		if filter != "" {
			keep = func(e *proto.TrafficLogEntry) bool { return e.ForwardID == filter }
		}
		more, err := tailNDJSON[proto.TrafficLogEntry]([]string{latest}, keep, 0)
		if err != nil {
			return nil, err
		}
		entries = append(entries, more...)
	}

	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	return entries, nil
}

// readDaemonLogTail reads the tail of a plain daemon.log (not ndjson) and
// returns the last `limit` lines wrapped as TrafficLogEntry records so they
// surface in the dashboard alongside traffic events.
func readDaemonLogTail(path string, limit int) []proto.TrafficLogEntry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	// Simple tail: read near the end so we don't scan multi-MB logs.
	if info, err := f.Stat(); err == nil && info.Size() > 10000 {
		f.Seek(-10000, io.SeekEnd)
	}

	var entries []proto.TrafficLogEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		// Daemon log uses standard `log` package format: "2026/04/22 04:03:23 msg"
		timestamp := time.Now().Format(time.RFC3339)
		msg := line
		if len(line) > 19 && line[4] == '/' && line[7] == '/' {
			if t, err := time.Parse("2006/01/02 15:04:05", line[:19]); err == nil {
				timestamp = t.Format(time.RFC3339)
				msg = line[20:]
			}
		}
		entries = append(entries, proto.TrafficLogEntry{
			Time:      timestamp,
			Protocol:  "DAEMON",
			ForwardID: "system",
			Action:    msg,
		})
	}
	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	return entries
}

type ForwardMetrics struct {
	Requests int64 `json:"requests"`
	Bytes    int64 `json:"bytes"`
}

// GetMetrics aggregates total requests and bytes from all log files.
func GetMetrics() (map[string]*ForwardMetrics, error) {
	metricsMu.RLock()
	defer metricsMu.RUnlock()

	// Copy to avoid race on return
	res := make(map[string]*ForwardMetrics)
	for k, v := range metricsMap {
		res[k] = &ForwardMetrics{
			Requests: v.Requests,
			Bytes:    v.Bytes,
		}
	}
	return res, nil
}
