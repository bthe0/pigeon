package client

import (
	"bufio"
	"encoding/json"
	"os"
)

// tailNDJSON reads paths in the given order and returns the last `limit`
// entries that decode cleanly and pass `keep`. Lines that fail to decode or
// fail the filter are dropped silently. If limit <= 0 no cap is applied.
// Missing files are skipped rather than reported as errors — callers typically
// point this at a globbed list where some entries may not exist yet.
func tailNDJSON[T any](paths []string, keep func(*T) bool, limit int) ([]T, error) {
	var out []T
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Bytes()
			if len(line) == 0 {
				continue
			}
			var entry T
			if err := json.Unmarshal(line, &entry); err != nil {
				continue
			}
			if keep != nil && !keep(&entry) {
				continue
			}
			out = append(out, entry)
		}
		f.Close()
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}
