package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bthe0/pigeon/internal/client"
	"github.com/bthe0/pigeon/internal/proto"
	"github.com/hashicorp/yamux"
	"github.com/spf13/cobra"
)

func setupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Interactive setup wizard for Pigeon client/server",
		Run: func(cmd *cobra.Command, args []string) {
			reader := bufio.NewReader(os.Stdin)
			fmt.Println("🐦 Welcome to Pigeon Setup 🐦")
			fmt.Println()
			fmt.Println("Are you setting up a:")
			fmt.Println("  [1] Server (VPS/Relay)")
			fmt.Println("  [2] Client (Local Machine)")
			fmt.Print("\nEnter 1 or 2: ")

			ans, _ := reader.ReadString('\n')
			ans = strings.TrimSpace(ans)

			switch ans {
			case "1":
				setupServer(reader)
			case "2":
				setupClient(reader)
			default:
				fmt.Println("Invalid option chosen. Exiting.")
			}
		},
	}
	return cmd
}

func setupServer(reader *bufio.Reader) {
	fmt.Println("\n=== Pigeon Server Setup ===")
	fmt.Print("Enter your base domain (e.g. tun.example.com): ")
	domain, _ := reader.ReadString('\n')
	domain = strings.TrimSpace(domain)

	fmt.Print("Enter a strong secret token (or press enter to auto-generate): ")
	token, _ := reader.ReadString('\n')
	token = strings.TrimSpace(token)
	if token == "" {
		token = proto.RandomID(16)
		fmt.Println("Generated token:", token)
	}

	fmt.Println("\n✅ Steps to complete Server Setup:")
	fmt.Println()
	fmt.Println("1. Configure DNS records for your domain (in your registrar or Cloudflare):")
	fmt.Printf("   A   %s   <YOUR_SERVER_IP>\n", domain)
	fmt.Printf("   A   *.%s <YOUR_SERVER_IP>\n", domain)

	fmt.Println("\n2. Nginx Reverse Proxy (Optional, if Pigeon shares port 80/443 with other apps):")
	fmt.Printf(`   server {
       listen 80;
       server_name %s *.%s;
       location / {
           proxy_pass http://127.0.0.1:8080;
           proxy_set_header Host $host;
           proxy_set_header X-Real-IP $remote_addr;
           proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;

           # For WebSockets and Streams
           proxy_http_version 1.1;
           proxy_set_header Upgrade $http_upgrade;
           proxy_set_header Connection "upgrade";
       }
   }`+"\n", domain, domain)
	fmt.Print("\nDo you want to install and start Pigeon as a Systemd service? (y/N): ")
	installSvc, _ := reader.ReadString('\n')
	installSvc = strings.ToLower(strings.TrimSpace(installSvc))

	if installSvc != "y" && installSvc != "yes" {
		return
	}

	execPath, err := os.Executable()
	if err != nil {
		fmt.Println("❌ Could not determine executable path.")
		return
	}

	// Write token to a separate env file with restricted permissions.
	envContent := fmt.Sprintf("PIGEON_TOKEN=%s\n", token)
	envFile := "/etc/pigeon/token.env"
	if mkErr := os.MkdirAll("/etc/pigeon", 0700); mkErr != nil {
		fmt.Printf("❌ Failed to create /etc/pigeon: %v\n", mkErr)
	} else if envErr := os.WriteFile(envFile, []byte(envContent), 0600); envErr != nil {
		fmt.Printf("❌ Failed to write token env file: %v\n", envErr)
	}

	svcContent := fmt.Sprintf(`[Unit]
Description=Pigeon Tunnel Server
After=network.target

[Service]
EnvironmentFile=/etc/pigeon/token.env
ExecStart=%s server --domain %s --token ${PIGEON_TOKEN} --http :8080 --control :2222
Restart=always
User=root

[Install]
WantedBy=multi-user.target
`, execPath, domain)
	if err := os.WriteFile("/etc/systemd/system/pigeon-server.service", []byte(svcContent), 0644); err != nil {
		fmt.Printf("❌ Failed to write service file (try running setup as root / sudo): %v\n", err)
		return
	}
	fmt.Println("✅ Service written to /etc/systemd/system/pigeon-server.service")
	fmt.Printf("✅ Token stored in %s (readable only by root)\n", envFile)
	exec.Command("systemctl", "daemon-reload").Run()
	if err := exec.Command("systemctl", "enable", "--now", "pigeon-server").Run(); err != nil {
		fmt.Printf("❌ Failed to enable/start service: %v\n", err)
		return
	}
	fmt.Println("✅ Pigeon Server is now running and enabled on boot!")
}

func setupClient(reader *bufio.Reader) {
	fmt.Println("\n=== Pigeon Client Setup ===")
	fmt.Print("Enter your Pigeon Server Address (e.g. tun.example.com:2222): ")
	serverAddr, _ := reader.ReadString('\n')
	serverAddr = strings.TrimSpace(serverAddr)

	fmt.Print("Enter your Pigeon Auth Token: ")
	token, _ := reader.ReadString('\n')
	token = strings.TrimSpace(token)

	fmt.Print("Enter Web Dashboard Port (default :8080): ")
	webAddr, _ := reader.ReadString('\n')
	webAddr = strings.TrimSpace(webAddr)
	if webAddr == "" {
		webAddr = ":8080"
	}
	if !strings.Contains(webAddr, ":") {
		webAddr = ":" + webAddr
	}

	fmt.Print("Enter a Dashboard Login Password (min 4 chars): ")
	dashPass, _ := reader.ReadString('\n')
	dashPass = strings.TrimSpace(dashPass)
	if len(dashPass) < 4 {
		dashPass = proto.RandomID(12)
		fmt.Printf("⚠️ Password too short. Using auto-generated password: %s\n", dashPass)
	}

	// Check if port is available
	ln, err := net.Listen("tcp", webAddr)
	if err != nil {
		fmt.Printf("❌ Port %s is already in use by another application. Please choose a different one.\n", webAddr)
		return
	}
	ln.Close()

	fmt.Printf("\nTesting connection to server %s... ", serverAddr)
	if err := checkServerValidity(serverAddr, token); err != nil {
		fmt.Printf("\n❌ Failed to connect!\n   Error: %v\n", err)
		fmt.Println("   Please verify your server address, token, and firewalls, then try again.")
		return
	}
	fmt.Println("✅ Connection successful!")

	cfg := &client.Config{Server: serverAddr, Token: token, WebAddr: webAddr, DashboardPassword: dashPass}
	if err := client.SaveConfig(cfg); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
	} else {
		fmt.Println("\n✅ Client initialized successfully!")
	}

	fmt.Println("\nNext Steps:")
	fmt.Println("1. Add a forward rule (e.g. forward local port 3000):")
	fmt.Println("   pigeon forward add http localhost:3000")
	fmt.Println("\n2. Start the pigeon background daemon:")
	fmt.Println("   pigeon daemon start")
	fmt.Println("\n3. Open the Web UI to manage your tunnels visually!")
	fmt.Println("   pigeon web")
}

func checkServerValidity(addr, token string) error {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	mux, err := yamux.Client(conn, yamux.DefaultConfig())
	if err != nil {
		return err
	}
	defer mux.Close()

	ctrl, err := mux.Open()
	if err != nil {
		return err
	}
	defer ctrl.Close()

	if err := proto.Write(ctrl, proto.Message{
		Type:    proto.MsgAuth,
		Payload: proto.AuthPayload{Token: token},
	}); err != nil {
		return err
	}

	msg, err := proto.Read(ctrl)
	if err != nil {
		return err
	}
	if msg.Type == proto.MsgError {
		var e proto.ErrorPayload
		proto.DecodePayload(msg, &e)
		return fmt.Errorf("auth rejected: %s", e.Message)
	}
	if msg.Type != proto.MsgAuthAck {
		return fmt.Errorf("unexpected response: %v", msg.Type)
	}
	return nil
}
