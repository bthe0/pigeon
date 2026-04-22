package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/bthe0/pigeon/internal/client"
	"github.com/bthe0/pigeon/internal/localdev"
	"github.com/bthe0/pigeon/internal/proto"
	"github.com/bthe0/pigeon/internal/server"
	"github.com/hashicorp/yamux"
	"github.com/spf13/cobra"
)

// version is set via -ldflags "-X main.version=x.y.z" at build time.
var version = "dev"

func main() {
	client.AgentVersion = version
	// If running as daemon worker, skip CLI
	if client.IsDaemon() {
		cfg, err := client.LoadConfig()
		if err != nil {
			log.Fatal(err)
		}
		client.DaemonRun(cfg)
		return
	}

	root := &cobra.Command{
		Use:   "pigeon",
		Short: "Pigeon — simple self-hosted tunnels",
	}

	root.AddCommand(
		serverCmd(),
		devCmd(),
		initCmd(),
		setupCmd(),
		daemonCmd(),
		forwardCmd(),
		logsCmd(),
		statusCmd(),
		webCmd(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// ── pigeon server ──────────────────────────────────────────────────────────────

func serverCmd() *cobra.Command {
	var controlAddr, httpAddr, httpsAddr, token, domain, certDir, logFile string

	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run the pigeon server (on your VPS)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "" {
				return fmt.Errorf("--token is required")
			}
			if domain == "" {
				return fmt.Errorf("--domain is required")
			}
			if certDir == "" {
				certDir = "/var/lib/pigeon/certs"
			}
			os.MkdirAll(certDir, 0700)

			s := server.New(server.Config{
				ControlAddr: controlAddr,
				HTTPAddr:    httpAddr,
				HTTPSAddr:   httpsAddr,
				Token:       token,
				Domain:      domain,
				CertDir:     certDir,
				LogFile:     logFile,
			})
			return s.Start()
		},
	}

	cmd.Flags().StringVar(&controlAddr, "control", ":2222", "Control plane listen address")
	cmd.Flags().StringVar(&httpAddr, "http", ":80", "HTTP listen address")
	cmd.Flags().StringVar(&httpsAddr, "https", ":443", "HTTPS listen address (empty to disable)")
	cmd.Flags().StringVar(&token, "token", "", "Shared auth token (required)")
	cmd.Flags().StringVar(&domain, "domain", "", "Base domain, e.g. tun.example.com (required)")
	cmd.Flags().StringVar(&certDir, "cert-dir", "", "Directory for ACME certs")
	cmd.Flags().StringVar(&logFile, "log", "", "Traffic log file (default: stdout)")
	return cmd
}

// ── pigeon init ────────────────────────────────────────────────────────────────

func initCmd() *cobra.Command {
	var serverAddr, token, webAddr string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialise client with server credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			if serverAddr == "" {
				return fmt.Errorf("--server is required")
			}
			if token == "" {
				return fmt.Errorf("--token is required")
			}
			if webAddr == "" {
				webAddr = "127.0.0.1:8080"
			}
			if !strings.Contains(webAddr, ":") {
				webAddr = ":" + webAddr
			}

			// Check if port is available
			ln, err := net.Listen("tcp", webAddr)
			if err != nil {
				return fmt.Errorf("port %s is already in use", webAddr)
			}
			ln.Close()

			cfg := &client.Config{Server: serverAddr, Token: token, WebAddr: webAddr}
			if err := client.SaveConfig(cfg); err != nil {
				return err
			}
			fmt.Printf("Saved config. Run `pigeon forward add` to add tunnels.\n")
			return nil
		},
	}
	cmd.Flags().StringVar(&serverAddr, "server", "", "Server address, e.g. tun.example.com:2222")
	cmd.Flags().StringVar(&token, "token", "", "Shared auth token")
	cmd.Flags().StringVar(&webAddr, "web", "127.0.0.1:8080", "Dashboard listen address")
	return cmd
}

// ── pigeon daemon ──────────────────────────────────────────────────────────────

func daemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the background daemon",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use: "start", Short: "Start daemon",
			RunE: func(cmd *cobra.Command, args []string) error { return client.DaemonStart() },
		},
		&cobra.Command{
			Use: "stop", Short: "Stop daemon",
			RunE: func(cmd *cobra.Command, args []string) error { return client.DaemonStop() },
		},
		&cobra.Command{
			Use: "status", Short: "Show daemon status",
			Run: func(cmd *cobra.Command, args []string) { client.DaemonStatus() },
		},
		&cobra.Command{
			Use: "restart", Short: "Restart daemon",
			RunE: func(cmd *cobra.Command, args []string) error {
				_ = client.DaemonStop()
				time.Sleep(500 * time.Millisecond)
				return client.DaemonStart()
			},
		},
		&cobra.Command{
			Use:    "run",
			Hidden: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, err := client.LoadConfig()
				if err != nil {
					return err
				}
				client.DaemonRun(cfg)
				return nil
			},
		},
	)
	return cmd
}

// ── pigeon forward ─────────────────────────────────────────────────────────────

func forwardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "forward",
		Short: "Manage tunnel forwards",
	}

	// forward add
	var domain string
	var remotePort int

	addCmd := &cobra.Command{
		Use:   "add <http|tcp|udp> <local-addr>",
		Short: "Add a forward rule",
		Example: `  pigeon forward add http localhost:80 --domain myapp.example.com
  pigeon forward add tcp localhost:5432
  pigeon forward add udp localhost:7777 --port 7777`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			protocol := proto.Protocol(args[0])
			switch protocol {
			case proto.ProtoHTTP, proto.ProtoHTTPS, proto.ProtoTCP, proto.ProtoUDP:
			default:
				return fmt.Errorf("protocol must be http, https, tcp, or udp")
			}

			cfg, err := client.LoadConfig()
			if err != nil {
				return err
			}

			rule := client.ForwardRule{
				ID:         proto.RandomID(8),
				Protocol:   protocol,
				LocalAddr:  args[1],
				Domain:     domain,
				RemotePort: remotePort,
			}
			if err := cfg.AddForward(rule); err != nil {
				return err
			}
			if err := client.SaveConfig(cfg); err != nil {
				return err
			}
			fmt.Printf("Forward added (id: %s). Restart daemon to apply: pigeon daemon restart\n", rule.ID)
			return nil
		},
	}
	addCmd.Flags().StringVar(&domain, "domain", "", "Custom domain (http only)")
	addCmd.Flags().IntVar(&remotePort, "port", 0, "Remote port (tcp/udp; 0 = auto-assign)")

	// forward remove
	removeCmd := &cobra.Command{
		Use:   "remove <id|domain|port>",
		Short: "Remove a forward rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := client.LoadConfig()
			if err != nil {
				return err
			}
			if !cfg.RemoveForward(args[0]) {
				return fmt.Errorf("forward %q not found", args[0])
			}
			if err := client.SaveConfig(cfg); err != nil {
				return err
			}
			fmt.Printf("Removed. Restart daemon to apply: pigeon daemon restart\n")
			return nil
		},
	}

	// forward list
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List configured forwards",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := client.LoadConfig()
			if err != nil {
				return err
			}
			if len(cfg.Forwards) == 0 {
				fmt.Println("No forwards configured.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tPROTOCOL\tLOCAL\tDOMAIN/PORT")
			for _, f := range cfg.Forwards {
				remote := f.Domain
				if remote == "" && f.RemotePort > 0 {
					remote = fmt.Sprintf(":%d", f.RemotePort)
				}
				if remote == "" {
					remote = "(auto)"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", f.ID, f.Protocol, f.LocalAddr, remote)
			}
			return w.Flush()
		},
	}

	cmd.AddCommand(addCmd, removeCmd, listCmd)
	return cmd
}

// ── pigeon logs ────────────────────────────────────────────────────────────────

func logsCmd() *cobra.Command {
	var since string
	var limit int
	var follow bool

	cmd := &cobra.Command{
		Use:   "logs [forward-id]",
		Short: "Show tunnel traffic logs",
		Example: `  pigeon logs
  pigeon logs abc12345
  pigeon logs --since 1h --follow`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filter := ""
			if len(args) > 0 {
				filter = args[0]
			}
			var sinceDur time.Duration
			if since != "" {
				var err error
				sinceDur, err = time.ParseDuration(since)
				if err != nil {
					return fmt.Errorf("invalid --since: %s (use e.g. 1h, 30m, 24h)", since)
				}
			}
			return client.TailLogs(filter, sinceDur, limit, follow)
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "Show logs since duration, e.g. 1h, 30m")
	cmd.Flags().IntVar(&limit, "limit", 0, "Max number of entries (0 = all)")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	return cmd
}

// ── pigeon status ──────────────────────────────────────────────────────────────

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon and forward status",
		RunE: func(cmd *cobra.Command, args []string) error {
			client.DaemonStatus()

			cfg, err := client.LoadConfig()
			if err != nil {
				return nil
			}
			fmt.Printf("Server:   %s\n", cfg.Server)
			fmt.Printf("Forwards: %d configured\n", len(cfg.Forwards))
			for _, f := range cfg.Forwards {
				remote := f.Domain
				if remote == "" && f.RemotePort > 0 {
					remote = fmt.Sprintf("port %d", f.RemotePort)
				}
				if remote == "" {
					remote = "(auto-assign on connect)"
				}
				fmt.Printf("  %s  %s  %s → %s\n", f.ID, f.Protocol, f.LocalAddr, remote)
			}
			return nil
		},
	}
}

// ── pigeon web ─────────────────────────────────────────────────────────────────

func webCmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "web",
		Short: "Open or start the web configuration interface",
		RunE: func(cmd *cobra.Command, args []string) error {
			url := "http://" + addr
			if strings.HasPrefix(addr, ":") {
				url = "http://127.0.0.1" + addr
			}
			fmt.Printf("Opening dashboard at %s\n", url)
			client.OpenBrowser(url)

			err := client.StartWebInterface(addr, true)
			if err != nil && strings.Contains(err.Error(), "address already in use") {
				// Dashboard is likely already running in the background daemon
				fmt.Println("Dashboard is already running (likely via the background daemon).")
				return nil
			}
			return err
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8080", "Address to run the web interface on")
	return cmd
}

// ── pigeon dev ─────────────────────────────────────────────────────────────────

func devCmd() *cobra.Command {
	var controlAddr, httpAddr, httpsAddr, token, domain, certDir, logFile string
	var watch bool

	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Run server + client locally with self-signed certs and /etc/hosts entries",
		Long: `Starts pigeon in local-dev mode:
  • Generates a self-signed certificate for <domain> and *.<domain>
  • Adds 127.0.0.1 <domain> to /etc/hosts (requires write access, run with sudo)
  • Adds an /etc/hosts entry for each tunnel as it registers
  • Starts the server with TLS using the self-signed cert

Add --watch to rebuild and restart automatically when any .go file changes.
For frontend HMR run: cd internal/client/web && farm start

Example:
  sudo pigeon dev --token secret
  sudo pigeon dev --token secret --watch
  sudo pigeon dev --domain myapp.local --token secret`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if watch {
				return runDevWatch(devWatchArgs{
					controlAddr: controlAddr,
					httpAddr:    httpAddr,
					httpsAddr:   httpsAddr,
					token:       token,
					domain:      domain,
					certDir:     certDir,
					logFile:     logFile,
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

			// Set up DNS resolver so *.domain resolves to 127.0.0.1 without /etc/hosts wildcards.
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

			// Write client config so the daemon and web UI know we're in local dev mode.
			devCfg := &client.Config{
				Server:     controlAddr,
				Token:      token,
				LocalDev:   true,
				BaseDomain: domain,
			}
			if existing, err := client.LoadConfig(); err == nil {
				devCfg.Forwards = existing.Forwards
			}
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
	return cmd
}

// ── dev watch supervisor ───────────────────────────────────────────────────────

type devWatchArgs struct {
	controlAddr, httpAddr, httpsAddr string
	token, domain, certDir, logFile  string
}

func runDevWatch(a devWatchArgs) error {
	root, err := findGoModDir()
	if err != nil {
		return fmt.Errorf("could not find project root (go.mod): %w\nrun pigeon dev --watch from the project directory", err)
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
	}
	if a.certDir != "" {
		childArgs = append(childArgs, "--cert-dir", a.certDir)
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

// ── pigeon setup ───────────────────────────────────────────────────────────────

func setupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Interactive setup wizard for Pigeon client/server",
		Run: func(cmd *cobra.Command, args []string) {
			reader := bufio.NewReader(os.Stdin)
			fmt.Println("🐦 Welcome to Pigeon Setup 🐦")
			fmt.Println()
			fmt.Println("Are you setting up a:")
			fmt.Println("  [1] Server (VPS/Relay)")
			fmt.Println("  [2] Client (Local Machine)")
			fmt.Print("\nEnter 1 or 2: ")

			ans, _ := reader.ReadString('\n')
			ans = strings.TrimSpace(ans)

			if ans == "1" {
				fmt.Println("\n=== Pigeon Server Setup ===")
				fmt.Print("Enter your base domain (e.g. tun.example.com): ")
				domain, _ := reader.ReadString('\n')
				domain = strings.TrimSpace(domain)

				fmt.Print("Enter a strong secret token (or press enter to auto-generate): ")
				token, _ := reader.ReadString('\n')
				token = strings.TrimSpace(token)
				if token == "" {
					token = proto.RandomID(16)
					fmt.Println("Generated token:", token)
				}

				fmt.Println("\n✅ Steps to complete Server Setup:")
				fmt.Println()
				fmt.Println("1. Configure DNS records for your domain (in your registrar or Cloudflare):")
				fmt.Printf("   A   %s   <YOUR_SERVER_IP>\n", domain)
				fmt.Printf("   A   *.%s <YOUR_SERVER_IP>\n", domain)

				fmt.Println("\n2. Nginx Reverse Proxy (Optional, if Pigeon shares port 80/443 with other apps):")
				fmt.Printf(`   server {
       listen 80;
       server_name %s *.%s;
       location / {
           proxy_pass http://127.0.0.1:8080;
           proxy_set_header Host $host;
           proxy_set_header X-Real-IP $remote_addr;
           proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
           
           # For WebSockets and Streams
           proxy_http_version 1.1;
           proxy_set_header Upgrade $http_upgrade;
           proxy_set_header Connection "upgrade";
       }
   }`+"\n", domain, domain)
				fmt.Print("\nDo you want to install and start Pigeon as a Systemd service? (y/N): ")
				installSvc, _ := reader.ReadString('\n')
				installSvc = strings.ToLower(strings.TrimSpace(installSvc))

				if installSvc == "y" || installSvc == "yes" {
					execPath, err := os.Executable()
					if err != nil {
						fmt.Println("❌ Could not determine executable path.")
					} else {
						// Write token to a separate env file with restricted permissions.
						envContent := fmt.Sprintf("PIGEON_TOKEN=%s\n", token)
						envFile := "/etc/pigeon/token.env"
						if mkErr := os.MkdirAll("/etc/pigeon", 0700); mkErr != nil {
							fmt.Printf("❌ Failed to create /etc/pigeon: %v\n", mkErr)
						} else if envErr := os.WriteFile(envFile, []byte(envContent), 0600); envErr != nil {
							fmt.Printf("❌ Failed to write token env file: %v\n", envErr)
						}

						svcContent := fmt.Sprintf(`[Unit]
Description=Pigeon Tunnel Server
After=network.target

[Service]
EnvironmentFile=/etc/pigeon/token.env
ExecStart=%s server --domain %s --token ${PIGEON_TOKEN} --http :8080 --control :2222
Restart=always
User=root

[Install]
WantedBy=multi-user.target
`, execPath, domain)
						err = os.WriteFile("/etc/systemd/system/pigeon-server.service", []byte(svcContent), 0644)
						if err != nil {
							fmt.Printf("❌ Failed to write service file (try running setup as root / sudo): %v\n", err)
						} else {
							fmt.Println("✅ Service written to /etc/systemd/system/pigeon-server.service")
							fmt.Printf("✅ Token stored in %s (readable only by root)\n", envFile)
							exec.Command("systemctl", "daemon-reload").Run()
							if err := exec.Command("systemctl", "enable", "--now", "pigeon-server").Run(); err != nil {
								fmt.Printf("❌ Failed to enable/start service: %v\n", err)
							} else {
								fmt.Println("✅ Pigeon Server is now running and enabled on boot!")
							}
						}
					}
				}

			} else if ans == "2" {
				fmt.Println("\n=== Pigeon Client Setup ===")
				fmt.Print("Enter your Pigeon Server Address (e.g. tun.example.com:2222): ")
				serverAddr, _ := reader.ReadString('\n')
				serverAddr = strings.TrimSpace(serverAddr)

				fmt.Print("Enter your Pigeon Auth Token: ")
				token, _ := reader.ReadString('\n')
				token = strings.TrimSpace(token)

				fmt.Print("Enter Web Dashboard Port (default :8080): ")
				webAddr, _ := reader.ReadString('\n')
				webAddr = strings.TrimSpace(webAddr)
				if webAddr == "" {
					webAddr = ":8080"
				}
				if !strings.Contains(webAddr, ":") {
					webAddr = ":" + webAddr
				}

				fmt.Print("Enter a Dashboard Login Password (min 4 chars): ")
				dashPass, _ := reader.ReadString('\n')
				dashPass = strings.TrimSpace(dashPass)
				if len(dashPass) < 4 {
					dashPass = proto.RandomID(12)
					fmt.Printf("⚠️ Password too short. Using auto-generated password: %s\n", dashPass)
				}

				// Check if port is available
				ln, err := net.Listen("tcp", webAddr)
				if err != nil {
					fmt.Printf("❌ Port %s is already in use by another application. Please choose a different one.\n", webAddr)
					return
				}
				ln.Close()

				fmt.Printf("\nTesting connection to server %s... ", serverAddr)
				if err := checkServerValidity(serverAddr, token); err != nil {
					fmt.Printf("\n❌ Failed to connect!\n   Error: %v\n", err)
					fmt.Println("   Please verify your server address, token, and firewalls, then try again.")
					return
				}
				fmt.Println("✅ Connection successful!")

				cfg := &client.Config{Server: serverAddr, Token: token, WebAddr: webAddr, DashboardPassword: dashPass}
				if err := client.SaveConfig(cfg); err != nil {
					fmt.Printf("Error saving config: %v\n", err)
				} else {
					fmt.Println("\n✅ Client initialized successfully!")
				}

				fmt.Println("\nNext Steps:")
				fmt.Println("1. Add a forward rule (e.g. forward local port 3000):")
				fmt.Println("   pigeon forward add http localhost:3000")
				fmt.Println("\n2. Start the pigeon background daemon:")
				fmt.Println("   pigeon daemon start")
				fmt.Println("\n3. Open the Web UI to manage your tunnels visually!")
				fmt.Println("   pigeon web")

			} else {
				fmt.Println("Invalid option chosen. Exiting.")
			}
		},
	}
	return cmd
}

// ── helpers ────────────────────────────────────────────────────────────────────

func checkServerValidity(addr, token string) error {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	mux, err := yamux.Client(conn, yamux.DefaultConfig())
	if err != nil {
		return err
	}
	defer mux.Close()

	ctrl, err := mux.Open()
	if err != nil {
		return err
	}
	defer ctrl.Close()

	if err := proto.Write(ctrl, proto.Message{
		Type:    proto.MsgAuth,
		Payload: proto.AuthPayload{Token: token},
	}); err != nil {
		return err
	}

	msg, err := proto.Read(ctrl)
	if err != nil {
		return err
	}
	if msg.Type == proto.MsgError {
		var e proto.ErrorPayload
		proto.DecodePayload(msg, &e)
		return fmt.Errorf("auth rejected: %s", e.Message)
	}
	if msg.Type != proto.MsgAuthAck {
		return fmt.Errorf("unexpected response: %v", msg.Type)
	}
	return nil
}
