package main

import (
	"fmt"

	"github.com/bthe0/pigeon/internal/client"
	"github.com/spf13/cobra"
)

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
