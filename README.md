<h1 align="center">
  <img src="assets/logo.png" alt="Pigeon Logo" width="38" align="top" /> Pigeon
</h1>

<p align="center">
  <a href="https://goreportcard.com/report/github.com/bthe0/pigeon"><img src="https://goreportcard.com/badge/github.com/bthe0/pigeon" alt="Go Report Card"></a>
  <a href="https://opensource.org/licenses/MIT"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="License: MIT"></a>
  <a href="https://github.com/bthe0/pigeon"><img src="https://img.shields.io/github/go-mod/go-version/bthe0/pigeon" alt="Go Version"></a>
  <a href="https://awesome.re"><img src="https://awesome.re/badge.svg" alt="Awesome"></a>
</p>

<p align="center">
  A lightweight, self-hosted tunnelling tool that exposes local services to the internet over <strong>HTTP</strong>, <strong>TCP</strong>, <strong>UDP</strong>, and <strong>static files</strong> — no third-party services required.
</p>

<p align="center">
  <img src="assets/dashboard.png" alt="Pigeon Web Dashboard" />
</p>

```
                  ┌─────────────┐        ┌─────────────┐
internet ────────▶│  pigeon     │◀──────▶│  pigeon     │◀────── localhost:3000
                  │  server     │  yamux │  daemon     │
                  │  (your VPS) │  mux   │  (your mac) │
                  └─────────────┘        └─────────────┘
```

<p align="center">
  <a href="#quick-start">Quick Start</a> ·
  <a href="#the-web-dashboard">Dashboard</a> ·
  <a href="#command-reference">Commands</a> ·
  <a href="#http-api">HTTP API</a> ·
  <a href="#security">Security</a> ·
  <a href="#how-it-works">Architecture</a>
</p>

---

## What you can do

| | |
|---|---|
| **Expose a local HTTP app** | `pigeon forward add http localhost:3000` → public HTTPS URL with automatic Let's Encrypt certs. |
| **Tunnel a raw TCP port** | Postgres, SSH, Redis, a game server — anything that speaks TCP. |
| **Forward UDP** | NAT-table multiplexing per source, so replies come back to the right client. |
| **Serve a static folder** | `pigeon forward add static ./public` — no local web server needed. |
| **Inspect every request** | Per-request logs with geo, device, status, and optional body capture. |
| **Manage from a browser** | Full web dashboard with tunnel CRUD, live metrics, replay, and settings. |
| **Password-protect a tunnel** | HTTP Basic auth or a login page, with per-IP rate limiting. |
| **Restrict by IP / CIDR** | `--allow 10.0.0.0/8` on any forward. |
| **Run locally end-to-end** | `pigeon dev` spins up the relay, self-signed certs, and wildcard DNS on your machine. |

---

## Installation

The quickest path on macOS, Linux, or WSL:

```bash
go install github.com/bthe0/pigeon/cmd/pigeon@latest
```

> Make sure `$(go env GOPATH)/bin` is on your `$PATH`.

Or build from source:

```bash
git clone https://github.com/bthe0/pigeon.git
cd pigeon
go build -o pigeon ./cmd/pigeon
```

---

## Quick Start

### Option A — Guided setup (recommended)

```bash
pigeon setup
```

Interactive wizard: configures Nginx routing on your VPS, installs a systemd unit, provisions your client, and verifies the end-to-end connection.

### Option B — Manual in three steps

#### 1. Start the server (on your VPS)

```bash
pigeon server \
  --token mysecret \
  --domain tun.example.com \
  --http :80 \
  --https :443
```

<details>
<summary><b>Server flags</b></summary>

| Flag | Default | Description |
|------|---------|-------------|
| `--token` | *(required)* | Shared auth secret |
| `--domain` | *(required)* | Base domain, e.g. `tun.example.com` |
| `--control` | `:2222` | Control-plane port (yamux) |
| `--http` | `:80` | HTTP tunnel port |
| `--https` | `:443` | HTTPS port — enables ACME autocert |
| `--cert-dir` | `/var/lib/pigeon/certs` | Directory for ACME certificates |
| `--log` | stdout | Path to traffic log file |

</details>

#### 2. Initialise the client (on your machine)

```bash
pigeon init \
  --server tun.example.com:2222 \
  --token mysecret \
  --web 127.0.0.1:8080
```

Credentials and dashboard settings are written to `~/.pigeon/config.json`.

#### 3. Add tunnels and start the daemon

```bash
# HTTP — auto-assigned subdomain
pigeon forward add http localhost:3000

# HTTP — custom subdomain
pigeon forward add http localhost:3000 --domain myapp.tun.example.com

# HTTPS upstream (local service already speaks TLS)
pigeon forward add https localhost:8443 --domain secure.tun.example.com

# TCP
pigeon forward add tcp localhost:5432 --port 5432

# UDP
pigeon forward add udp localhost:7777 --port 7777

# Static files
pigeon forward add static ./public --domain docs.tun.example.com

# Start the background daemon
pigeon daemon start
```

The daemon connects to the server, registers every configured forward, hosts the web dashboard, and reconnects automatically with exponential backoff.

---

## The Web Dashboard

The dashboard is hosted by the daemon at `http://127.0.0.1:8080` by default.

### What you get

| View | What it does |
|---|---|
| **Overview** | Live request counts and bandwidth per tunnel, world-map of recent visitors. |
| **Tunnels** | Create, edit, enable/disable, or delete forwards visually. Click a tunnel for per-tunnel detail. |
| **Inspector** | Detailed request-by-request log with geo, browser, OS, headers, and body (when capture is enabled). Replay any captured request. |
| **System Logs** | Tail the daemon's own log alongside traffic events. |
| **Settings** | View / validate the auth token, export and import the full config, start/stop tunneling without killing the daemon, restart the daemon. |

### Authentication

The dashboard is protected by a password set via `dashboard_password` in `~/.pigeon/config.json`. On successful login you get a server-side session (random 32-byte ID, stored in memory). Logout invalidates it. Sessions idle-time out after 30 days.

Programmatic callers can skip the login form and use `Authorization: Bearer <token>` — the same token used to authenticate with the tunnel server.

> **Heads up:** The dashboard serves plain HTTP. If you bind it to anything other than a loopback address, the daemon prints a warning at startup — the session cookie and bearer token traverse the network in the clear. Front it with TLS (Nginx / Caddy / Cloudflare Tunnel) or SSH-tunnel to it.

### Tunneling pause / resume

The daemon can be put into a paused state from **Settings → Daemon**. The dashboard stays up; the reconnect loop stops until you resume. Useful when you want to cut off public traffic without killing the process.

---

## Command Reference

### `pigeon setup` — Guided install

```bash
pigeon setup
```

Walks through Nginx routing, systemd setup on the VPS, and client provisioning.

### `pigeon server` — Run the relay

```bash
pigeon server --token <tok> --domain <base-domain> [flags]
```

### `pigeon init` — Save client credentials

```bash
pigeon init --server <host:port> --token <tok> [--web 127.0.0.1:8080]
```

### `pigeon forward` — Manage tunnels

```bash
pigeon forward add <http|https|tcp|udp|static> <local-addr|folder> [flags]
pigeon forward remove <id|domain|port>
pigeon forward list
```

<details>
<summary><b>Forward-add flags</b></summary>

| Flag | Applies to | Description |
|---|---|---|
| `--domain` | http / https / static | Custom subdomain (supports `*.preview.example.com`) |
| `--port` | tcp / udp | Remote port (0 = auto-assign) |
| `--allow` | all | Restrict to IP or CIDR (repeatable) |
| `--capture-bodies` | http | Record request/response bodies in the inspector |

</details>

### `pigeon web` — Open the dashboard

```bash
pigeon web [--addr 127.0.0.1:8080]
```

Shortcut that opens your browser. Starts a standalone web server if the daemon isn't already running.

### `pigeon dev` — Full stack, locally

```bash
sudo pigeon dev --token secret
sudo pigeon dev --domain pigeon.local --token secret
```

Local-dev mode:
- generates a self-signed cert for `<domain>` and `*.<domain>`
- runs the relay locally on `127.0.0.1:2222`, `:80`, and `:443`
- configures wildcard DNS via `/etc/resolver/<domain>`
- provisions client config so the daemon and dashboard use the local relay

Add trust for the dev cert on macOS:

```bash
sudo pigeon dev trust [--domain pigeon.local]
```

### `pigeon daemon` — Background process

```bash
pigeon daemon start     # fork to background
pigeon daemon stop      # SIGTERM + remove pid file
pigeon daemon restart   # stop + start
pigeon daemon status    # running? what PID?
```

### `pigeon status` — Snapshot

```bash
pigeon status
# Daemon:   running (PID 12345)
# Server:   tun.example.com:2222
# Forwards: 3 configured
#   abc12345  http  localhost:3000 → myapp.tun.example.com
#   def67890  tcp   localhost:5432 → port 5432
```

### `pigeon logs` — Traffic log

```bash
pigeon logs                    # all entries
pigeon logs <forward-id>       # filter by forward
pigeon logs --since 1h
pigeon logs --limit 50
pigeon logs --follow           # tail -f style
```

---

## HTTP API

The same endpoints the dashboard uses are available to scripts. Two auth modes:

```bash
# 1. Bearer token (scripts / CLI)
curl -H "Authorization: Bearer $PIGEON_TOKEN" http://127.0.0.1:8080/api/config

# 2. Session cookie — state-changing calls also need X-Pigeon-CSRF
curl -b cookie.txt -H "X-Pigeon-CSRF: 1" \
     -X POST http://127.0.0.1:8080/api/daemon/stop
```

`X-Pigeon-CSRF` is required on any `POST`/`PUT`/`PATCH`/`DELETE` that authenticates via cookie; browsers can't set the header cross-origin without a preflight, which Pigeon doesn't answer — so it's a simple CSRF gate. Bearer-token callers are exempt.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/config` | Current config (server, forwards, dashboard settings). |
| `POST` | `/api/login` | Exchange password for a session cookie. |
| `POST` | `/api/logout` | Invalidate the current session. |
| `GET` | `/api/logs` | Recent traffic + daemon log entries. |
| `DELETE` | `/api/logs` | Truncate the system logs. |
| `GET` | `/api/inspector` | Recent inspector events. |
| `POST` | `/api/inspector/replay` | Re-fire a captured request through the tunnel. |
| `POST`/`PUT`/`DELETE` | `/api/forwards[/{id}]` | CRUD forwards. |
| `POST` | `/api/restart` | Bounce the whole daemon process. |
| `GET` | `/api/daemon/state` | `{ "paused": bool }`. |
| `POST` | `/api/daemon/stop` \| `/start` | Pause / resume tunneling without killing the daemon. |
| `POST` | `/api/token/validate` | Dial the configured server and test the token. |
| `GET` | `/api/config/export` | Download the full config as JSON. |
| `POST` | `/api/config/import` | Replace the active config with an uploaded JSON blob. |

---

## Security

Pigeon is designed for a single operator controlling both server and client, but it hardens against a few common attack shapes:

- **Random server-side dashboard sessions** — no password-derived cookies; logout and restart both invalidate.
- **Per-process HMAC on tunnel-password cookies** — captured cookies expire after 24 h and never survive a server restart.
- **CSRF header gate** — state-changing dashboard requests from cookies require `X-Pigeon-CSRF`.
- **CIDR allowlists** — `--allow` on any forward restricts who can reach it.
- **Per-IP brute-force limits** — both the control-plane auth and the per-tunnel password lock out sources after repeated failures (10 / 15 min for tunnel passwords, 20 / 15 min for control auth).
- **UDP peer filtering** — the server only returns UDP datagrams to peers it has recently heard from, so a compromised client can't use the tunnel as a UDP-spray relay.
- **Non-loopback dashboard warning** — the daemon logs a loud notice when the dashboard is bound somewhere reachable, because HTTP cookies travel in the clear.

> Known gap: the tunnel control connection is plaintext TCP. Authoritative deployments should front the control port with TLS (stunnel, Nginx stream, Cloudflare Access) or accept that anyone on the path can see the auth token.

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
  ├── HTTP    → dial localhost:PORT, proxy stream
  ├── TCP     → dial localhost:PORT, io.Copy both ways
  ├── UDP     → NAT table: one local socket per external peer
  ├── Static  → http.FileServer over configured root
  └── Web UI  → dashboard + HTTP API on WebAddr
```

Everything flows over a single multiplexed TCP connection ([yamux](https://github.com/hashicorp/yamux)), so the daemon only needs outbound access to the server's control port — no inbound firewall openings on the client side.

---

## Running Tests

```bash
go test ./...
```

All packages have unit tests. The `tools/` directory has standalone echo servers/clients for manual end-to-end checks:

```bash
# TCP
go run tools/tcpecho/main.go
go run tools/tcpclient/main.go localhost:<port> "hello"

# UDP
go run tools/udpecho/main.go :19201
go run tools/udpclient/main.go localhost:<port> "hello"
```

---

## File Layout

```
pigeon/
├── cmd/pigeon/                # CLI (cobra commands, one per file)
├── internal/
│   ├── proto/                 # Length-prefixed JSON wire protocol
│   ├── server/                # Relay: control + HTTP + TCP/UDP + password auth
│   ├── client/                # Daemon, config, inspector, dashboard, API
│   ├── localdev/              # `pigeon dev` DNS + cert automation
│   └── netx/                  # Shared network helpers
├── tools/                     # Manual E2E echo helpers
└── assets/                    # Branding / screenshots
```

---

## License

MIT
