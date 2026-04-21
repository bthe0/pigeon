package server

import (
	"net/http"
	"testing"
)

func TestBrowserAndOS(t *testing.T) {
	browser, osName := browserAndOS("Mozilla/5.0 (Macintosh; Intel Mac OS X 14_4) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	if browser != "Chrome" {
		t.Fatalf("browser = %q, want Chrome", browser)
	}
	if osName != "macOS" {
		t.Fatalf("os = %q, want macOS", osName)
	}
}

func TestClientIP(t *testing.T) {
	header := http.Header{}
	header.Set("X-Forwarded-For", "198.51.100.8, 10.0.0.4")
	got := clientIP("127.0.0.1:5555", header)
	if got != "198.51.100.8" {
		t.Fatalf("clientIP = %q, want 198.51.100.8", got)
	}
}

func TestLookupGeoLocalIP(t *testing.T) {
	s := New(Config{})
	info := s.lookupGeo("127.0.0.1")
	if info.City != "Local" {
		t.Fatalf("city = %q, want Local", info.City)
	}
	if info.CountryCode != "LO" {
		t.Fatalf("country code = %q, want LO", info.CountryCode)
	}
}
