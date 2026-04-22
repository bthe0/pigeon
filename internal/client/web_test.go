package client_test

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bthe0/pigeon/internal/client"
	"github.com/bthe0/pigeon/internal/proto"
)

// startWeb spins up a StartWebInterface server in a background goroutine and
// returns its address once it is ready to accept connections.
func startWeb(t *testing.T, cfg *client.Config) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".pigeon", "logs"), 0755)
	if err := client.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// Find a free port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	go func() { client.StartWebInterface(addr, false) }() //nolint

	// Poll until the server is up (max 2 s).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/api/logs?token=" + cfg.Token)
		if err == nil {
			resp.Body.Close()
			return addr
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("web server did not start in time")
	return ""
}

func authHeader(token string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

func doRequest(t *testing.T, method, url string, body []byte, mods ...func(*http.Request)) *http.Response {
	t.Helper()
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequest(method, url, bytes.NewReader(body))
	} else {
		req, err = http.NewRequest(method, url, nil)
	}
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, m := range mods {
		m(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

// ── Auth middleware ────────────────────────────────────────────────────────────

func TestWebAPI_Auth_NoToken_Returns401(t *testing.T) {
	addr := startWeb(t, &client.Config{Server: "s:2222", Token: "tok"})
	resp, err := http.Get("http://" + addr + "/api/config")
	if err != nil {
		t.Fatalf("GET /api/config: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestWebAPI_Auth_BearerToken_Returns200(t *testing.T) {
	addr := startWeb(t, &client.Config{Server: "s:2222", Token: "tok"})
	resp := doRequest(t, "GET", "http://"+addr+"/api/config", nil, authHeader("tok"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestWebAPI_Auth_QueryToken_Returns200(t *testing.T) {
	addr := startWeb(t, &client.Config{Server: "s:2222", Token: "tok"})
	resp, err := http.Get("http://" + addr + "/api/config?token=tok")
	if err != nil {
		t.Fatalf("GET /api/config?token: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestWebAPI_Auth_WrongToken_Returns401(t *testing.T) {
	addr := startWeb(t, &client.Config{Server: "s:2222", Token: "tok"})
	resp := doRequest(t, "GET", "http://"+addr+"/api/config", nil, authHeader("wrong"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

// ── /api/login ─────────────────────────────────────────────────────────────────

func TestWebAPI_Login_WrongPassword_Returns401(t *testing.T) {
	addr := startWeb(t, &client.Config{Server: "s:2222", Token: "tok", DashboardPassword: "correct"})
	body, _ := json.Marshal(map[string]string{"password": "wrong"})
	resp := doRequest(t, "POST", "http://"+addr+"/api/login", body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestWebAPI_Login_CorrectPassword_SetsCookie(t *testing.T) {
	addr := startWeb(t, &client.Config{Server: "s:2222", Token: "tok", DashboardPassword: "correct"})
	body, _ := json.Marshal(map[string]string{"password": "correct"})
	resp := doRequest(t, "POST", "http://"+addr+"/api/login", body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	found := false
	for _, c := range resp.Cookies() {
		if c.Name == "pigeon_session" && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Error("expected pigeon_session cookie after successful login")
	}
}

func TestWebAPI_Login_MethodNotAllowed(t *testing.T) {
	addr := startWeb(t, &client.Config{Server: "s:2222", Token: "tok"})
	resp, err := http.Get("http://" + addr + "/api/login")
	if err != nil {
		t.Fatalf("GET /api/login: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

// ── /api/logout ────────────────────────────────────────────────────────────────

func TestWebAPI_Logout_ClearsCookie(t *testing.T) {
	addr := startWeb(t, &client.Config{Server: "s:2222", Token: "tok"})
	resp := doRequest(t, "POST", "http://"+addr+"/api/logout", nil)
	resp.Body.Close()
	for _, c := range resp.Cookies() {
		if c.Name == "pigeon_session" && c.MaxAge == -1 {
			return
		}
	}
	t.Error("expected pigeon_session cookie with MaxAge=-1 after logout")
}

// ── /api/config ────────────────────────────────────────────────────────────────

func TestWebAPI_Config_ReturnsForwards(t *testing.T) {
	cfg := &client.Config{
		Server: "s:2222",
		Token:  "tok",
		Forwards: []client.ForwardRule{
			{ID: "f1", Protocol: proto.ProtoHTTP, LocalAddr: "localhost:3000"},
		},
	}
	addr := startWeb(t, cfg)
	resp := doRequest(t, "GET", "http://"+addr+"/api/config", nil, authHeader("tok"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	list, _ := result["forwards"].([]interface{})
	if len(list) == 0 {
		t.Error("expected non-empty forwards in /api/config response")
	}
}

// ── /api/forwards POST ────────────────────────────────────────────────────────

func TestWebAPI_AddForward_Returns201(t *testing.T) {
	addr := startWeb(t, &client.Config{Server: "s:2222", Token: "tok"})
	rule := client.ForwardRule{ID: "new1", Protocol: proto.ProtoHTTP, LocalAddr: "localhost:9000"}
	body, _ := json.Marshal(rule)
	resp := doRequest(t, "POST", "http://"+addr+"/api/forwards", body, authHeader("tok"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want 201", resp.StatusCode)
	}
}

func TestWebAPI_AddForward_Duplicate_Returns409(t *testing.T) {
	cfg := &client.Config{
		Server: "s:2222",
		Token:  "tok",
		Forwards: []client.ForwardRule{
			{ID: "dup", Protocol: proto.ProtoHTTP, LocalAddr: "localhost:3000"},
		},
	}
	addr := startWeb(t, cfg)
	rule := client.ForwardRule{ID: "dup", Protocol: proto.ProtoHTTP, LocalAddr: "localhost:3000"}
	body, _ := json.Marshal(rule)
	resp := doRequest(t, "POST", "http://"+addr+"/api/forwards", body, authHeader("tok"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

// ── /api/forwards/{id} DELETE ─────────────────────────────────────────────────

func TestWebAPI_DeleteForward_Returns200(t *testing.T) {
	cfg := &client.Config{
		Server: "s:2222",
		Token:  "tok",
		Forwards: []client.ForwardRule{
			{ID: "del1", Protocol: proto.ProtoHTTP, LocalAddr: "localhost:3000"},
		},
	}
	addr := startWeb(t, cfg)
	resp := doRequest(t, "DELETE", "http://"+addr+"/api/forwards/del1", nil, authHeader("tok"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestWebAPI_DeleteForward_NotFound_Returns404(t *testing.T) {
	addr := startWeb(t, &client.Config{Server: "s:2222", Token: "tok"})
	resp := doRequest(t, "DELETE", "http://"+addr+"/api/forwards/ghost", nil, authHeader("tok"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// ── /api/forwards/{id} PATCH ──────────────────────────────────────────────────

func TestWebAPI_PatchForward_Disable(t *testing.T) {
	cfg := &client.Config{
		Server: "s:2222",
		Token:  "tok",
		Forwards: []client.ForwardRule{
			{ID: "p1", Protocol: proto.ProtoHTTP, LocalAddr: "localhost:3000"},
		},
	}
	addr := startWeb(t, cfg)
	patch := map[string]interface{}{"disabled": true}
	body, _ := json.Marshal(patch)
	resp := doRequest(t, "PATCH", "http://"+addr+"/api/forwards/p1", body, authHeader("tok"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestWebAPI_PatchForward_NotFound_Returns404(t *testing.T) {
	addr := startWeb(t, &client.Config{Server: "s:2222", Token: "tok"})
	patch := map[string]interface{}{"disabled": true}
	body, _ := json.Marshal(patch)
	resp := doRequest(t, "PATCH", "http://"+addr+"/api/forwards/ghost", body, authHeader("tok"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// ── /api/logs ─────────────────────────────────────────────────────────────────

func TestWebAPI_Logs_Returns200(t *testing.T) {
	addr := startWeb(t, &client.Config{Server: "s:2222", Token: "tok"})
	resp := doRequest(t, "GET", "http://"+addr+"/api/logs", nil, authHeader("tok"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

// ── /api/inspector ────────────────────────────────────────────────────────────

func TestWebAPI_Inspector_Returns200(t *testing.T) {
	addr := startWeb(t, &client.Config{Server: "s:2222", Token: "tok"})
	resp := doRequest(t, "GET", "http://"+addr+"/api/inspector", nil, authHeader("tok"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}
