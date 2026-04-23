package main

import (
	"fmt"
	"os"
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
