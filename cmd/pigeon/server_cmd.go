package main

import (
	"fmt"
	"os"

	"github.com/bthe0/pigeon/internal/server"
	"github.com/spf13/cobra"
)

func serverCmd() *cobra.Command {
	var controlAddr, httpAddr, httpsAddr, token, domain, certDir, logFile string
	var trustedProxies []string

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
				ControlAddr:       controlAddr,
				HTTPAddr:          httpAddr,
				HTTPSAddr:         httpsAddr,
				Token:             token,
				Domain:            domain,
				CertDir:           certDir,
				LogFile:           logFile,
				TrustedProxyCIDRs: trustedProxies,
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
	cmd.Flags().StringSliceVar(&trustedProxies, "trusted-proxy", nil, "CIDR(s) whose X-Forwarded-Proto is trusted (repeatable or comma-separated)")
	return cmd
}
