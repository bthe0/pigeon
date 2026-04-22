package client

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type InspectorWriter struct {
	f  *os.File
	mu sync.Mutex
}

func NewInspectorWriter() (*InspectorWriter, error) {
	path, err := inspectorLogPath()
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &InspectorWriter{f: f}, nil
}

func (iw *InspectorWriter) Write(entry InspectorEntry) error {
	iw.mu.Lock()
	defer iw.mu.Unlock()
	return json.NewEncoder(iw.f).Encode(entry)
}

func (iw *InspectorWriter) Close() error {
	if iw.f != nil {
		return iw.f.Close()
	}
	return nil
}

type InspectorEntry struct {
	Time            string            `json:"time"`
	ForwardID       string            `json:"forward_id"`
	Domain          string            `json:"domain"`
	RemoteAddr      string            `json:"remote_addr"`
	Method          string            `json:"method"`
	Path            string            `json:"path"`
	Status          int               `json:"status"`
	DurationMs      int               `json:"duration_ms"`
	Bytes           int               `json:"bytes,omitempty"`
	City            string            `json:"city,omitempty"`
	Country         string            `json:"country,omitempty"`
	CountryCode     string            `json:"country_code,omitempty"`
	Latitude        float64           `json:"latitude,omitempty"`
	Longitude       float64           `json:"longitude,omitempty"`
	Browser         string            `json:"browser,omitempty"`
	OS              string            `json:"os,omitempty"`
	RequestHeaders  map[string]string `json:"request_headers,omitempty"`
	ResponseHeaders map[string]string `json:"response_headers,omitempty"`
}

func inspectorLogPath() (string, error) {
	dir, err := LogDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "inspector.ndjson"), nil
}


func FetchRecentInspectorEntries(limit int, filter string) ([]InspectorEntry, error) {
	path, err := inspectorLogPath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []InspectorEntry{}, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []InspectorEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry InspectorEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if filter != "" && entry.ForwardID != filter && entry.Domain != filter {
			continue
		}
		entries = append(entries, entry)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	return entries, nil
}
