package main

import (
	"fmt"
	"time"

	"github.com/bthe0/pigeon/internal/client"
	"github.com/spf13/cobra"
)

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
