package main

import (
	"time"

	"github.com/bthe0/pigeon/internal/client"
	"github.com/spf13/cobra"
)

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
