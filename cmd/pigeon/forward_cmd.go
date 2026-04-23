package main

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/bthe0/pigeon/internal/client"
	"github.com/bthe0/pigeon/internal/proto"
	"github.com/spf13/cobra"
)

func forwardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "forward",
		Short: "Manage tunnel forwards",
	}

	var domain string
	var remotePort int
	var allowIPs []string
	var captureBodies bool

	addCmd := &cobra.Command{
		Use:   "add <http|https|tcp|udp|static> <local-addr|folder>",
		Short: "Add a forward rule",
		Example: `  pigeon forward add http localhost:80 --domain myapp.example.com
  pigeon forward add http localhost:80 --domain '*.preview.example.com'
  pigeon forward add tcp localhost:5432 --allow 10.0.0.0/8
  pigeon forward add udp localhost:7777 --port 7777
  pigeon forward add static ./public --domain docs.example.com`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			protocol := proto.Protocol(args[0])
			switch protocol {
			case proto.ProtoHTTP, proto.ProtoHTTPS, proto.ProtoTCP, proto.ProtoUDP, proto.ProtoStatic:
			default:
				return fmt.Errorf("protocol must be http, https, tcp, udp, or static")
			}

			cfg, err := client.LoadConfig()
			if err != nil {
				return err
			}

			rule := client.ForwardRule{
				ID:            proto.RandomID(8),
				Protocol:      protocol,
				Domain:        domain,
				RemotePort:    remotePort,
				AllowedIPs:    allowIPs,
				CaptureBodies: captureBodies,
			}
			if protocol == proto.ProtoStatic {
				abs, aerr := filepath.Abs(args[1])
				if aerr != nil {
					return fmt.Errorf("resolve static folder: %w", aerr)
				}
				info, serr := os.Stat(abs)
				if serr != nil || !info.IsDir() {
					return fmt.Errorf("static root %q is not a directory", args[1])
				}
				rule.StaticRoot = abs
			} else {
				rule.LocalAddr = args[1]
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
	addCmd.Flags().StringVar(&domain, "domain", "", "Custom domain (http/https/static; supports '*.x.example.com')")
	addCmd.Flags().IntVar(&remotePort, "port", 0, "Remote port (tcp/udp; 0 = auto-assign)")
	addCmd.Flags().StringSliceVar(&allowIPs, "allow", nil, "Restrict access to these IPs/CIDRs (repeatable)")
	addCmd.Flags().BoolVar(&captureBodies, "capture-bodies", false, "Capture request/response bodies in the inspector (http only)")

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
