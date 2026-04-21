package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bthe0/pigeon/internal/proto"
)

type ForwardRule struct {
	ID         string         `json:"id"`
	Protocol   proto.Protocol `json:"protocol"`
	LocalAddr  string         `json:"local_addr"`
	Domain     string         `json:"domain,omitempty"`
	RemotePort int            `json:"remote_port,omitempty"`
	Disabled   bool           `json:"disabled,omitempty"`
	PublicAddr string         `json:"public_addr,omitempty"`  // assigned by server after connect
	Expose     string         `json:"expose,omitempty"`       // "both" | "http" | "https"; default "both"
	HTTPPassword string       `json:"http_password,omitempty"`
	MaxConnections int        `json:"max_connections,omitempty"`
	UnavailablePage string    `json:"unavailable_page,omitempty"`
}

type Config struct {
	Server     string        `json:"server"`               // host:port
	Token      string        `json:"token"`
	LocalDev   bool          `json:"local_dev"`            // true when running in local dev mode (self-signed TLS)
	BaseDomain string        `json:"base_domain,omitempty"` // base domain for auto-assigned tunnel URLs
	Forwards   []ForwardRule `json:"forwards"`
}

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".pigeon")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

func ConfigPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func LogDir() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	logDir := filepath.Join(dir, "logs")
	return logDir, os.MkdirAll(logDir, 0755)
}

func PIDFile() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pigeon.pid"), nil
}

func LoadConfig() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("not initialised — run: pigeon init --server <host:port> --token <token>")
		}
		return nil, err
	}
	defer f.Close()
	var cfg Config
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}
	cfg.normalizeForwards()
	return &cfg, nil
}

func SaveConfig(cfg *Config) error {
	cfg.normalizeForwards()
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(cfg)
}

func (cfg *Config) AddForward(r ForwardRule) error {
	cfg.normalizeForward(&r)
	for _, f := range cfg.Forwards {
		if f.ID == r.ID || (f.Protocol == r.Protocol && f.LocalAddr == r.LocalAddr && f.Domain == r.Domain && f.RemotePort == r.RemotePort) {
			return fmt.Errorf("duplicate forward")
		}
	}
	cfg.Forwards = append(cfg.Forwards, r)
	return nil
}

func (cfg *Config) RemoveForward(id string) bool {
	for i, f := range cfg.Forwards {
		if f.ID == id || f.Domain == id || fmt.Sprintf("%d", f.RemotePort) == id {
			cfg.Forwards = append(cfg.Forwards[:i], cfg.Forwards[i+1:]...)
			return true
		}
	}
	return false
}

func (cfg *Config) SetPublicAddr(id, publicAddr string) {
	for i := range cfg.Forwards {
		if cfg.Forwards[i].ID == id {
			cfg.Forwards[i].PublicAddr = publicAddr
			return
		}
	}
}

func (cfg *Config) UpdateForward(id string, rule ForwardRule) error {
	cfg.normalizeForward(&rule)
	for i, f := range cfg.Forwards {
		if f.ID == id {
			cfg.Forwards[i] = rule
			return nil
		}
	}
	return fmt.Errorf("forward not found")
}

func (cfg *Config) normalizeForwards() {
	for i := range cfg.Forwards {
		cfg.normalizeForward(&cfg.Forwards[i])
	}
}

func (cfg *Config) normalizeForward(rule *ForwardRule) {
	if cfg.BaseDomain == "" {
		return
	}
	if rule.Protocol != proto.ProtoHTTP && rule.Protocol != proto.ProtoHTTPS {
		return
	}
	if rule.Domain != "" && !strings.Contains(rule.Domain, ".") {
		rule.Domain = rule.Domain + "." + cfg.BaseDomain
	}
	if rule.PublicAddr != "" && !strings.Contains(rule.PublicAddr, ".") {
		rule.PublicAddr = rule.PublicAddr + "." + cfg.BaseDomain
	}
}
