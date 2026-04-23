package main

import (
	"fmt"
	"strings"

	"github.com/bthe0/pigeon/internal/client"
	"github.com/spf13/cobra"
)

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
