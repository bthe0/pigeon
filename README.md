# 🐦 Pigeon

A lightweight, self-hosted tunnelling tool that exposes local services to the internet over **HTTP**, **TCP**, and **UDP** — no third-party services required.

```
                  ┌─────────────┐        ┌─────────────┐
internet ────────▶│  pigeon     │◀──────▶│  pigeon     │◀────── localhost:3000
                  │  server     │  yamux │  daemon     │
                  │  (your VPS) │  mux   │  (your mac) │
                  └─────────────┘        └─────────────┘
```

---

## Features

- **HTTP tunnels** — expose any local HTTP server under a public subdomain
- **TCP tunnels** — forward raw TCP (Postgres, SSH, Redis, …)
- **UDP tunnels** — forward UDP traffic with per-source NAT-table multiplexing
- **TLS / Let's Encrypt** — automatic ACME certs on the server side
- **Background daemon** — persistent connection with exponential-backoff reconnect
- **Traffic logs** — structured NDJSON logs with `--since` / `--follow` / filter support
- **Zero dependencies on the client** — single static binary

---

## Installation

```bash
go install github.com/bthe0/pigeon/cmd/pigeon@latest
```

Or build from source:

```bash
git clone https://github.com/bthe0/pigeon.git
cd pigeon
go build -o pigeon ./cmd/pigeon
```

---

## Quick Start

### 1 — Run the server (on your VPS)

```bash
pigeon server \
  --token mysecret \
  --domain tun.example.com \
  --control :2222 \
  --http :80 \
  --https :443
```

| Flag | Default | Description |
|------|---------|-------------|
| `--token` | *(required)* | Shared auth secret |
| `--domain` | *(required)* | Base domain, e.g. `tun.example.com` |
| `--control` | `:2222` | Control-plane port |
| `--http` | `:80` | HTTP tunnel port |
| `--https` | *(disabled)* | HTTPS port — enables ACME autocert |
| `--cert-dir` | `/var/lib/pigeon/certs` | Directory for ACME certificates |
| `--log` | stdout | Path to traffic log file |

### 2 — Init the client (on your machine)

```bash
pigeon init --server tun.example.com:2222 --token mysecret
```

This saves credentials to `~/.pigeon/config.json`.

### 3 — Add tunnel rules

```bash
# HTTP — auto-assigned subdomain
pigeon forward add http localhost:3000

# HTTP — custom subdomain
pigeon forward add http localhost:3000 --domain myapp.tun.example.com

# TCP — auto-assigned port
pigeon forward add tcp localhost:5432

# TCP — fixed remote port
pigeon forward add tcp localhost:5432 --port 5432

# UDP
pigeon forward add udp localhost:7777 --port 7777
```

### 4 — Start the daemon

```bash
pigeon daemon start
```

The daemon connects to the server, registers all configured forwards, and automatically reconnects on disconnect with exponential backoff.

---

## Commands

### `pigeon server` — Run the tunnel server

```bash
pigeon server --token <tok> --domain <base-domain> [flags]
```

### `pigeon init` — Save server credentials

```bash
pigeon init --server <host:port> --token <tok>
```

### `pigeon forward` — Manage tunnel rules

```bash
pigeon forward add <http|tcp|udp> <local-addr> [--domain <d>] [--port <p>]
pigeon forward remove <id|domain|port>
pigeon forward list
```

### `pigeon web` — Start the configuration web interface

```bash
pigeon web --addr 127.0.0.1:8080
```
This opens a beautiful single-page dashboard where you can view, add, and delete configuration forwards, and easily restart the background daemon.

### `pigeon daemon` — Manage the background process

```bash
pigeon daemon start    # fork daemon to background
pigeon daemon stop     # send SIGTERM
pigeon daemon restart  # stop + start
pigeon daemon status   # print PID / stopped
```

### `pigeon status` — Show overall status

```bash
pigeon status
# Daemon: running (PID 12345)
# Server:   tun.example.com:2222
# Forwards: 3 configured
#   abc12345  http  localhost:3000 → myapp.tun.example.com
#   def67890  tcp   localhost:5432 → port 5432
```

### `pigeon logs` — Inspect traffic

```bash
pigeon logs                    # all entries
pigeon logs <forward-id>       # filter by forward
pigeon logs --since 1h         # last hour only
pigeon logs --limit 50         # cap output
pigeon logs --follow           # tail -f style
```

---

## How It Works

```
External request
      │
      ▼
pigeon server  (public VPS)
  ├── Control plane (:2222) — yamux-multiplexed TCP
  │     auth → forward registration → ping/pong keepalive
  ├── HTTP plane  (:80/:443) — reverse proxy per-subdomain
  └── TCP/UDP listeners — one port per registered forward
      │
      │  yamux stream (data plane)
      ▼
pigeon daemon  (your machine)
  ├── HTTP  → dial localhost:PORT, proxy stream
  ├── TCP   → dial localhost:PORT, io.Copy both ways
  └── UDP   → NAT table: one local socket per external client
              replies stamped with original source addr
```

All data flows over a single multiplexed TCP connection ([yamux](https://github.com/hashicorp/yamux)) so only outbound port 2222 needs to be open on the client.

---

## Running Tests

```bash
go test ./...
```

All packages have unit tests. The `testtools/` directory contains standalone echo servers and clients for manual end-to-end testing:

```bash
# TCP echo server / client
go run testtools/tcpecho/main.go
go run testtools/tcpclient/main.go localhost:<port> "hello"

# UDP echo server / client
go run testtools/udpecho/main.go :19201
go run testtools/udpclient/main.go localhost:<port> "hello"
```

---

## File Layout

```
pigeon/
├── cmd/pigeon/main.go          # CLI (cobra commands)
├── internal/
│   ├── proto/                  # Length-prefixed JSON wire protocol
│   ├── server/                 # Tunnel server (control + HTTP + TCP/UDP)
│   └── client/                 # Daemon, config, logs, tunnel client
└── testtools/                  # Manual E2E test helpers
```

---

## License

MIT
