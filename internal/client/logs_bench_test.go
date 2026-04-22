package client

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/bthe0/pigeon/internal/proto"
)

func BenchmarkUpdateMetrics(b *testing.B) {
	metricsMu.Lock()
	metricsMap = make(map[string]*ForwardMetrics)
	metricsMu.Unlock()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			UpdateMetrics("abc12345", 128)
		}
	})
}

func BenchmarkFetchRecentLogs(b *testing.B) {
	home := b.TempDir()
	b.Setenv("HOME", home)
	logDir, err := LogDir()
	if err != nil {
		b.Fatal(err)
	}

	latest := filepath.Join(logDir, "2026-04-22.ndjson")
	f, err := os.Create(latest)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 2000; i++ {
		entry := proto.TrafficLogEntry{
			Time:       "2026-04-22T03:00:00Z",
			ForwardID:  fmt.Sprintf("fwd-%04d", i%25),
			RemoteAddr: "203.0.113.10:4567",
			Protocol:   "HTTP",
			Action:     "GET / 200 12ms",
			Bytes:      1234,
		}
		if _, err := fmt.Fprintf(f, "{\"time\":\"%s\",\"forward_id\":\"%s\",\"remote_addr\":\"%s\",\"protocol\":\"%s\",\"action\":\"%s\",\"bytes\":%d}\n",
			entry.Time, entry.ForwardID, entry.RemoteAddr, entry.Protocol, entry.Action, entry.Bytes); err != nil {
			b.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		entries, err := FetchRecentLogs("", 100)
		if err != nil {
			b.Fatal(err)
		}
		if len(entries) == 0 {
			b.Fatal("expected entries")
		}
	}
}
