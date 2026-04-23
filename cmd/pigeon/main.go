// Pigeon command-line entry point. This file wires up the cobra root command
// and dispatches to either the daemon runtime (when PIGEON_DAEMON=1) or the
// CLI sub-commands. Each sub-command lives in its own *_cmd.go file.
package main

import (
	"log"
	"os"

	"github.com/bthe0/pigeon/internal/client"
	"github.com/spf13/cobra"
)

// version is set via -ldflags "-X main.version=x.y.z" at build time.
var version = "dev"

func main() {
	client.AgentVersion = version

	// If running as daemon worker, skip CLI and enter the daemon runloop.
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
