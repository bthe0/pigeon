package main

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bthe0/pigeon/internal/client"
	"github.com/bthe0/pigeon/internal/localdev"
	"github.com/bthe0/pigeon/internal/server"
	"github.com/spf13/cobra"
)

// useDevConfig points PIGEON_CONFIG at dev.json so dev-mode config is fully
// isolated from the user's real ~/.pigeon/config.json. Respects an already-set
// PIGEON_CONFIG so watch-mode child processes inherit the same value.
func useDevConfig() {
	if os.Getenv("PIGEON_CONFIG") == "" {
		os.Setenv("PIGEON_CONFIG", "dev.json")
	}
}

func devCmd() *cobra.Command {
	var controlAddr, httpAddr, httpsAddr, token, domain, certDir, logFile string
	var watch, ui, skipChildren bool

	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Run server + daemon locally with self-signed certs and /etc/hosts entries",
		Long: `Starts pigeon in local-dev mode:
  • Generates a self-signed certificate for <domain> and *.<domain>
  • Configures a DNS resolver so *.<domain> resolves to 127.0.0.1
  • Starts the tunnel server (TLS via self-signed cert)
  • Starts the daemon (connects client to server, serves web UI on :8080)

Flags:
  --watch   Rebuild and restart the server automatically on .go file changes
  --ui      Also run 'farm start' for frontend HMR (browser at localhost:9000)

Example:
  sudo pigeon dev --token secret
  sudo pigeon dev --token secret --watch --ui`,
		RunE: func(cmd *cobra.Command, args []string) error {
			useDevConfig()
			if watch {
				return runDevWatch(devWatchArgs{
					controlAddr: controlAddr,
					httpAddr:    httpAddr,
					httpsAddr:   httpsAddr,
					token:       token,
					domain:      domain,
					certDir:     certDir,
					logFile:     logFile,
					ui:          ui,
				})
			}
			if token == "" {
				return fmt.Errorf("--token is required")
			}
			if certDir == "" {
				certDir = localdev.DefaultCertDir()
			}

			certFile, keyFile, err := localdev.GenerateCert(domain, certDir)
			if err != nil {
				return fmt.Errorf("generate cert: %w", err)
			}
			log.Printf("Self-signed cert written to %s", certDir)

			if err := localdev.SetupResolver(domain); err != nil {
				log.Printf("Warning: could not write /etc/resolver/%s (%v) — run with sudo", domain, err)
			} else {
				log.Printf("DNS resolver configured for *.%s", domain)
			}
			go func() {
				if err := localdev.StartDNS(domain); err != nil {
					log.Printf("DNS server error: %v", err)
				}
			}()

			devCfg, err := client.LoadConfig()
			if err != nil {
				devCfg = &client.Config{}
			}
			devCfg.Server = controlAddr
			devCfg.Token = token
			devCfg.LocalDev = true
			devCfg.BaseDomain = domain
			if err := client.SaveConfig(devCfg); err != nil {
				log.Printf("Warning: could not save client config: %v", err)
			}

			s := server.New(server.Config{
				ControlAddr: controlAddr,
				HTTPAddr:    httpAddr,
				HTTPSAddr:   httpsAddr,
				Token:       token,
				Domain:      domain,
				CertDir:     certDir,
				CertFile:    certFile,
				KeyFile:     keyFile,
				LogFile:     logFile,
				OnForwardRegistered: func(subdomain string) {
					log.Printf("Tunnel ready: https://%s", subdomain)
				},
			})

			// Start daemon and optional farm dev server as side processes.
			// In watch mode the parent owns these so they survive server restarts.
			if !skipChildren {
				children := startDevChildren(ui)
				defer stopChildren(children)
			}

			return s.Start()
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "trust",
		Short: "Trust the local dev self-signed certificate on macOS",
		RunE: func(cmd *cobra.Command, args []string) error {
			if certDir == "" {
				certDir = localdev.DefaultCertDir()
			}
			certFile, _ := localdev.CertPaths(certDir)
			if _, err := os.Stat(certFile); os.IsNotExist(err) {
				var genErr error
				certFile, _, genErr = localdev.GenerateCert(domain, certDir)
				if genErr != nil {
					return fmt.Errorf("generate cert: %w", genErr)
				}
			} else if err != nil {
				return fmt.Errorf("stat cert: %w", err)
			}
			if err := localdev.TrustCert(certFile); err != nil {
				return err
			}
			fmt.Printf("Trusted dev certificate: %s\n", certFile)
			return nil
		},
	})

	cmd.PersistentFlags().StringVar(&controlAddr, "control", "127.0.0.1:2222", "Control plane listen address")
	cmd.PersistentFlags().StringVar(&httpAddr, "http", "127.0.0.1:80", "HTTP listen address")
	cmd.PersistentFlags().StringVar(&httpsAddr, "https", "127.0.0.1:443", "HTTPS listen address")
	cmd.PersistentFlags().StringVar(&token, "token", "", "Shared auth token (required for `pigeon dev`)")
	cmd.PersistentFlags().StringVar(&domain, "domain", "pigeon.local", "Local base domain")
	cmd.PersistentFlags().StringVar(&certDir, "cert-dir", "", "Directory for dev certs (default ~/.pigeon/dev-certs)")
	cmd.PersistentFlags().StringVar(&logFile, "log", "", "Traffic log file (default: stdout)")
	cmd.PersistentFlags().BoolVar(&watch, "watch", false, "Watch .go files and rebuild/restart on changes")
	cmd.PersistentFlags().BoolVar(&ui, "ui", false, "Also run 'farm start' for frontend HMR")
	cmd.PersistentFlags().BoolVar(&skipChildren, "skip-children", false, "Internal: skip starting daemon/farm (used by --watch supervisor)")
	_ = cmd.PersistentFlags().MarkHidden("skip-children")
	return cmd
}

// ── dev watch supervisor ───────────────────────────────────────────────────────

type devWatchArgs struct {
	controlAddr, httpAddr, httpsAddr string
	token, domain, certDir, logFile  string
	ui                               bool
}

func runDevWatch(a devWatchArgs) error {
	if a.token == "" {
		return fmt.Errorf("--token is required")
	}

	root, err := findGoModDir()
	if err != nil {
		return fmt.Errorf("could not find project root (go.mod): %w\nrun pigeon dev --watch from the project directory", err)
	}

	certDir := a.certDir
	if certDir == "" {
		certDir = localdev.DefaultCertDir()
	}
	if _, _, err := localdev.GenerateCert(a.domain, certDir); err != nil {
		return fmt.Errorf("generate cert: %w", err)
	}
	log.Printf("Self-signed cert written to %s", certDir)

	if err := localdev.SetupResolver(a.domain); err != nil {
		log.Printf("Warning: could not write /etc/resolver/%s (%v) — run with sudo", a.domain, err)
	} else {
		log.Printf("DNS resolver configured for *.%s", a.domain)
	}
	go func() {
		if err := localdev.StartDNS(a.domain); err != nil {
			log.Printf("DNS server error: %v", err)
		}
	}()

	devCfg, err := client.LoadConfig()
	if err != nil {
		devCfg = &client.Config{}
	}
	devCfg.Server = a.controlAddr
	devCfg.Token = a.token
	devCfg.LocalDev = true
	devCfg.BaseDomain = a.domain
	if err := client.SaveConfig(devCfg); err != nil {
		log.Printf("Warning: could not save client config: %v", err)
	}

	tmpBin := filepath.Join(os.TempDir(), fmt.Sprintf("pigeon-dev-%d", os.Getpid()))
	defer os.Remove(tmpBin)

	build := func() error {
		log.Println("Building...")
		c := exec.Command("go", "build", "-o", tmpBin, "./cmd/pigeon")
		c.Dir = root
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()
	}

	childArgs := []string{"dev",
		"--token", a.token,
		"--domain", a.domain,
		"--control", a.controlAddr,
		"--http", a.httpAddr,
		"--https", a.httpsAddr,
		"--cert-dir", certDir,
		"--skip-children",
	}
	if a.logFile != "" {
		childArgs = append(childArgs, "--log", a.logFile)
	}

	var (
		mu    sync.Mutex
		child *exec.Cmd
	)

	stopChild := func() {
		mu.Lock()
		defer mu.Unlock()
		if child != nil && child.Process != nil {
			child.Process.Signal(syscall.SIGTERM)
			child.Wait()
			child = nil
		}
	}

	startChild := func() {
		if err := build(); err != nil {
			log.Printf("Build failed: %v", err)
			return
		}
		mu.Lock()
		child = exec.Command(tmpBin, childArgs...)
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			log.Printf("Start failed: %v", err)
			mu.Unlock()
			return
		}
		log.Printf("Dev server started (PID %d)", child.Process.Pid)
		mu.Unlock()
	}

	startChild()

	// Start daemon and optional farm dev server once; they stay up across server restarts.
	children := startDevChildren(a.ui)
	defer stopChildren(children)

	// Poll for changed .go files; debounce rapid saves.
	go func() {
		mtimes := map[string]time.Time{}
		initialized := false
		var debounce *time.Timer

		for {
			time.Sleep(400 * time.Millisecond)
			changed := false
			filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				// Skip vendor, node_modules, hidden dirs.
				for _, part := range strings.Split(path, string(filepath.Separator)) {
					if part == "vendor" || part == "node_modules" || (len(part) > 1 && part[0] == '.') {
						return fs.SkipDir
					}
				}
				if !strings.HasSuffix(path, ".go") {
					return nil
				}
				info, err := d.Info()
				if err != nil {
					return nil
				}
				if prev, ok := mtimes[path]; ok && info.ModTime().After(prev) {
					changed = true
				}
				mtimes[path] = info.ModTime()
				return nil
			})
			if !initialized {
				initialized = true
				log.Printf("Watching %s for .go changes (tip: run 'farm start' in internal/client/web for frontend HMR)", root)
				continue
			}
			if changed {
				if debounce != nil {
					debounce.Stop()
				}
				debounce = time.AfterFunc(300*time.Millisecond, func() {
					log.Println("Change detected — restarting...")
					stopChild()
					startChild()
				})
			}
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	stopChild()
	return nil
}

// startDevChildren launches the daemon and, optionally, the Farm dev server.
// Both are kept alive independently of server restarts — the daemon reconnects
// automatically when the server comes back up after a rebuild.
func startDevChildren(withUI bool) []*exec.Cmd {
	var children []*exec.Cmd

	// Daemon — connects client to the local dev server and serves the web UI.
	exe, err := os.Executable()
	if err == nil {
		d := exec.Command(exe, "daemon", "run")
		d.Stdout = os.Stdout
		d.Stderr = os.Stderr
		if err := d.Start(); err != nil {
			log.Printf("Warning: could not start daemon: %v", err)
		} else {
			log.Printf("Daemon started (PID %d) — web UI on http://127.0.0.1:8080", d.Process.Pid)
			children = append(children, d)
		}
	}

	// Farm dev server — optional frontend HMR.
	if withUI {
		webDir, err := findWebDir()
		if err != nil {
			log.Printf("Warning: could not find web dir for --ui: %v", err)
		} else {
			f := exec.Command("farm", "start")
			f.Dir = webDir
			f.Stdout = os.Stdout
			f.Stderr = os.Stderr
			if err := f.Start(); err != nil {
				log.Printf("Warning: could not start farm (is it installed? npm i -g @farmfe/cli): %v", err)
			} else {
				log.Printf("Farm dev server started (PID %d) — frontend HMR on http://localhost:9000", f.Process.Pid)
				children = append(children, f)
			}
		}
	}

	return children
}

func stopChildren(children []*exec.Cmd) {
	for _, c := range children {
		if c != nil && c.Process != nil {
			c.Process.Signal(syscall.SIGTERM)
			c.Wait()
		}
	}
}

// findWebDir returns the path to internal/client/web relative to go.mod root.
func findWebDir() (string, error) {
	root, err := findGoModDir()
	if err != nil {
		return "", err
	}
	webDir := filepath.Join(root, "internal", "client", "web")
	if _, err := os.Stat(webDir); err != nil {
		return "", fmt.Errorf("web dir not found at %s", webDir)
	}
	return webDir, nil
}

func findGoModDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}
