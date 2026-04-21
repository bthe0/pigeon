package main

import (
	"fmt"
	"log"
	"os"
	"text/tabwriter"
	"time"

	"github.com/bthe0/pigeon/internal/client"
	"github.com/bthe0/pigeon/internal/proto"
	"github.com/bthe0/pigeon/internal/server"
	"github.com/spf13/cobra"
)

func main() {
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
		initCmd(),
		daemonCmd(),
		forwardCmd(),
		logsCmd(),
		statusCmd(),
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
	var serverAddr, token string

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
			cfg := &client.Config{Server: serverAddr, Token: token}
			if err := client.SaveConfig(cfg); err != nil {
				return err
			}
			fmt.Printf("Saved config. Run `pigeon forward add` to add tunnels.\n")
			return nil
		},
	}
	cmd.Flags().StringVar(&serverAddr, "server", "", "Server address, e.g. tun.example.com:2222")
	cmd.Flags().StringVar(&token, "token", "", "Shared auth token")
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
			case proto.ProtoHTTP, proto.ProtoTCP, proto.ProtoUDP:
			default:
				return fmt.Errorf("protocol must be http, tcp, or udp")
			}

			cfg, err := client.LoadConfig()
			if err != nil {
				return err
			}

			rule := client.ForwardRule{
				ID:         randomID(8),
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

// ── helpers ────────────────────────────────────────────────────────────────────

const idChars = "abcdefghijklmnopqrstuvwxyz0123456789"

func randomID(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = idChars[time.Now().UnixNano()%int64(len(idChars))]
		time.Sleep(1)
	}
	return string(b)
}
