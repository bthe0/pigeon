package server

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type geoInfo struct {
	City        string
	Country     string
	CountryCode string
	Latitude    float64
	Longitude   float64
}

// geoLookupInFlight tracks IPs with an in-flight background lookup to
// dedupe concurrent requests for the same IP.
var geoLookupInFlight sync.Map // ip -> struct{}

type ipAPIResponse struct {
	Status      string  `json:"status"`
	Message     string  `json:"message"`
	Country     string  `json:"country"`
	CountryCode string  `json:"countryCode"`
	City        string  `json:"city"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
}

func browserAndOS(userAgent string) (browser, os string) {
	ua := strings.ToLower(userAgent)
	switch {
	case strings.Contains(ua, "edg/"):
		browser = "Edge"
	case strings.Contains(ua, "opr/"), strings.Contains(ua, "opera"):
		browser = "Opera"
	case strings.Contains(ua, "firefox/"):
		browser = "Firefox"
	case strings.Contains(ua, "safari/") && strings.Contains(ua, "chrome/"):
		browser = "Chrome"
	case strings.Contains(ua, "safari/"):
		browser = "Safari"
	case strings.Contains(ua, "curl/"):
		browser = "curl"
	default:
		browser = "Unknown"
	}

	switch {
	case strings.Contains(ua, "android"):
		os = "Android"
	case strings.Contains(ua, "iphone"), strings.Contains(ua, "ipad"), strings.Contains(ua, "ios"):
		os = "iOS"
	case strings.Contains(ua, "mac os x"), strings.Contains(ua, "macintosh"):
		os = "macOS"
	case strings.Contains(ua, "windows"):
		os = "Windows"
	case strings.Contains(ua, "linux"):
		os = "Linux"
	default:
		os = "Unknown"
	}

	return browser, os
}

func clientIP(remoteAddr string, header http.Header) string {
	for _, key := range []string{"CF-Connecting-IP", "X-Real-IP", "X-Forwarded-For"} {
		if v := strings.TrimSpace(header.Get(key)); v != "" {
			if key == "X-Forwarded-For" {
				v = strings.TrimSpace(strings.Split(v, ",")[0])
			}
			return strings.TrimSpace(v)
		}
	}
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

// lookupGeo returns cached geo data for ip immediately. If the IP is not
// cached, an empty geoInfo is returned and a background lookup is scheduled
// so subsequent requests for the same IP can be answered from cache. This
// keeps the request path off the external HTTP dependency.
func (s *Server) lookupGeo(ip string) geoInfo {
	if ip == "" {
		return geoInfo{}
	}
	if v, ok := s.geoCache.Load(ip); ok {
		return v.(geoInfo)
	}

	// Refuse to touch the outbound lookup URL with anything that isn't a
	// literal IP. Callers receive ip from X-Forwarded-For et al., so a hostile
	// value could otherwise inject ?query or /path into ip-api.com/json/<ip>.
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return geoInfo{}
	}
	if parsed.IsLoopback() || parsed.IsPrivate() {
		info := geoInfo{City: "Local", Country: "Local", CountryCode: "LO"}
		s.geoCache.Store(ip, info)
		return info
	}

	if _, loaded := geoLookupInFlight.LoadOrStore(ip, struct{}{}); !loaded {
		go s.fetchGeoAsync(ip)
	}
	return geoInfo{}
}

func (s *Server) fetchGeoAsync(ip string) {
	defer geoLookupInFlight.Delete(ip)

	if pauseUntil := time.Unix(s.geoPauseUntil.Load(), 0); time.Now().Before(pauseUntil) {
		return
	}

	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://ip-api.com/json/%s?fields=status,message,country,countryCode,city,lat,lon", ip), nil)
	if err != nil {
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		if ttl := resp.Header.Get("X-Ttl"); ttl != "" {
			if secs, err := strconv.Atoi(ttl); err == nil && secs > 0 {
				s.geoPauseUntil.Store(time.Now().Add(time.Duration(secs) * time.Second).Unix())
			}
		}
		return
	}

	var body ipAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return
	}
	if body.Status != "success" {
		return
	}

	s.geoCache.Store(ip, geoInfo{
		City:        body.City,
		Country:     body.Country,
		CountryCode: body.CountryCode,
		Latitude:    body.Lat,
		Longitude:   body.Lon,
	})
}
