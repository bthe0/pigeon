package main

import (
	"fmt"
	"net"
	"strings"

	"github.com/bthe0/pigeon/internal/client"
	"github.com/spf13/cobra"
)

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
