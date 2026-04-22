package client

import (
	"testing"

	"github.com/bthe0/pigeon/internal/proto"
)

func benchmarkConfig(b *testing.B, forwards int) *Config {
	cfg := &Config{Server: "example.com:2222", Token: "secret", BaseDomain: "tun.example.com"}
	cfg.Forwards = make([]ForwardRule, forwards)
	for i := range cfg.Forwards {
		cfg.Forwards[i] = ForwardRule{
			ID:         proto.RandomID(8),
			Protocol:   proto.ProtoHTTP,
			LocalAddr:  "127.0.0.1:3000",
			Domain:     "app" + proto.RandomID(4),
			PublicAddr: "app" + proto.RandomID(4),
		}
	}
	return cfg
}

func BenchmarkSaveLoadConfig(b *testing.B) {
	home := b.TempDir()
	b.Setenv("HOME", home)
	cfg := benchmarkConfig(b, 200)
	if err := SaveConfig(cfg); err != nil {
		b.Fatal(err)
	}
	b.Run("save", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if err := SaveConfig(cfg); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("load", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := LoadConfig(); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkAddForward(b *testing.B) {
	cfg := benchmarkConfig(b, 200)
	newRule := ForwardRule{ID: "new-forward", Protocol: proto.ProtoTCP, LocalAddr: "127.0.0.1:5432", RemotePort: 5432}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		clone := *cfg
		clone.Forwards = append([]ForwardRule(nil), cfg.Forwards...)
		if err := clone.AddForward(newRule); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRemoveForward(b *testing.B) {
	cfg := benchmarkConfig(b, 200)
	removeID := cfg.Forwards[len(cfg.Forwards)/2].ID
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		clone := *cfg
		clone.Forwards = append([]ForwardRule(nil), cfg.Forwards...)
		if !clone.RemoveForward(removeID) {
			b.Fatal("forward not removed")
		}
	}
}

func BenchmarkConfigPath(b *testing.B) {
	// Ensure the home-dependent path resolution doesn't regress.
	b.Setenv("HOME", b.TempDir())
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := ConfigPath(); err != nil {
			b.Fatal(err)
		}
	}
}
