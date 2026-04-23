package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/bthe0/pigeon/internal/proto"
)

// InspectorEntry is the on-disk / over-the-wire format for inspector records.
// It intentionally aliases proto.InspectorEventPayload so the server, the
// client control loop, and the on-disk log share a single struct.
type InspectorEntry = proto.InspectorEventPayload

const inspectorDateLayout = "2006-01-02"

// InspectorWriter appends inspector events to a daily-rotated ndjson file.
type InspectorWriter struct {
	mu       sync.Mutex
	f        *os.File
	openDate string
}

func NewInspectorWriter() (*InspectorWriter, error) {
	iw := &InspectorWriter{}
	if err := iw.rotate(time.Now()); err != nil {
		return nil, err
	}
	return iw, nil
}

func (iw *InspectorWriter) Write(entry InspectorEntry) error {
	iw.mu.Lock()
	defer iw.mu.Unlock()
	if err := iw.rotateIfNeeded(time.Now()); err != nil {
		return err
	}
	return json.NewEncoder(iw.f).Encode(entry)
}

func (iw *InspectorWriter) Close() error {
	iw.mu.Lock()
	defer iw.mu.Unlock()
	if iw.f != nil {
		err := iw.f.Close()
		iw.f = nil
		return err
	}
	return nil
}

func (iw *InspectorWriter) rotateIfNeeded(now time.Time) error {
	today := now.Format(inspectorDateLayout)
	if iw.f != nil && iw.openDate == today {
		return nil
	}
	return iw.rotate(now)
}

func (iw *InspectorWriter) rotate(now time.Time) error {
	if iw.f != nil {
		iw.f.Close()
		iw.f = nil
	}
	path, err := inspectorLogPath(now)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	iw.f = f
	iw.openDate = now.Format(inspectorDateLayout)
	return nil
}

func inspectorLogPath(when time.Time) (string, error) {
	dir, err := LogDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("inspector-%s.ndjson", when.Format(inspectorDateLayout))), nil
}

// inspectorLogPaths returns all inspector ndjson files in ascending date order.
// Includes the legacy unrotated `inspector.ndjson` (if present) at the front
// so entries written before rotation was added still show up.
func inspectorLogPaths() ([]string, error) {
	dir, err := LogDir()
	if err != nil {
		return nil, err
	}
	matches, err := filepath.Glob(filepath.Join(dir, "inspector-*.ndjson"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	var paths []string
	legacy := filepath.Join(dir, "inspector.ndjson")
	if _, err := os.Stat(legacy); err == nil {
		paths = append(paths, legacy)
	}
	return append(paths, matches...), nil
}

func FetchRecentInspectorEntries(limit int, filter string) ([]InspectorEntry, error) {
	paths, err := inspectorLogPaths()
	if err != nil {
		return nil, err
	}
	var keep func(*InspectorEntry) bool
	if filter != "" {
		keep = func(e *InspectorEntry) bool {
			return e.ForwardID == filter || e.Domain == filter
		}
	}
	entries, err := tailNDJSON[InspectorEntry](paths, keep, limit)
	if err != nil {
		return nil, err
	}
	if entries == nil {
		entries = []InspectorEntry{}
	}
	return entries, nil
}
