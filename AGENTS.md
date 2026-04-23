# AGENTS.md

Context for AI coding agents working on the Pigeon codebase. Optimised for fast orientation — skim the table of contents, read only the sections relevant to your task.

## Table of Contents

1. [Project Overview](#project-overview)
2. [Tech Stack](#tech-stack)
3. [Repository Structure](#repository-structure)
4. [Architecture](#architecture)
5. [Key Design Decisions](#key-design-decisions)
6. [Security Posture](#security-posture)
7. [Coding Conventions](#coding-conventions)
8. [Common Tasks](#common-tasks)
9. [Running Tests](#running-tests)

---

## Project Overview

Pigeon is a self-hosted tunnelling tool written in Go. It exposes local services (HTTP, TCP, UDP, static files) to the internet through a VPS relay server. Communication between client and server uses a single multiplexed TCP connection via [yamux](https://github.com/hashicorp/yamux) with a custom length-prefixed JSON wire protocol.

The code is intentionally small and opinionated: a single operator runs both the server (on a VPS) and the daemon (on their machine) and authenticates both ends with a shared token.

## Tech Stack

- **Language:** Go 1.26+
- **Server deps:** `hashicorp/yamux`, `spf13/cobra`, `golang.org/x/crypto/acme/autocert`
- **Dashboard:** React 18, bundled by [Farm](https://www.farmfe.org/) (`internal/client/web`). Bundled output is embedded into the Go binary via `//go:embed web/dist/*`.
- **Build:** `go build -o pigeon ./cmd/pigeon`
- **Frontend build:** `cd internal/client/web && npx farm build`
- **Tests:** `go test ./...`

## Repository Structure

```
cmd/pigeon/                   CLI entry point — one file per cobra sub-command
  main.go                     Root command; routes to daemon runloop when PIGEON_DAEMON=1
  server_cmd.go               `pigeon server`
  init_cmd.go                 `pigeon init`
  forward_cmd.go              `pigeon forward {add,remove,list}`
  daemon_cmd.go               `pigeon daemon {start,stop,restart,status,run}`
  web_cmd.go                  `pigeon web`
  logs_cmd.go                 `pigeon logs`
  status_cmd.go               `pigeon status`
  setup_cmd.go                `pigeon setup` — interactive wizard
  dev_cmd.go                  `pigeon dev` — local self-signed stack

internal/
  proto/                      Wire protocol (length-prefixed JSON messages)
    proto.go                  Message types, Read/Write, StreamHeader
    proto_test.go             Round-trip, decode, stream-header tests

  server/                     Tunnel server (runs on VPS)
    server.go                 Config, Server struct, Start/lifecycle
    control.go                yamux control plane: auth + forward registration
    http.go                   Public HTTP/HTTPS reverse proxy
    listeners.go              TCP/UDP listeners + UDP peer tracking
    password.go               Per-tunnel HTTP password + rate limits + signed cookies
    pages.go                  HTML templates for status / password pages
    log.go                    Traffic log (NDJSON)
    visitor_enrich.go         Request geo / browser enrichment

  client/                     Tunnel client (runs on user machine)
    client.go                 yamux session, control loop, stream handlers
    config.go                 Config struct, ForwardRule, Save/Load
    daemon.go                 Background daemon (fork, PID file, reconnect, pause/resume)
    validate.go               One-shot auth-handshake helper used by the dashboard
    logs.go                   Traffic log read/write/tail
    inspector.go              Inspector daily-rotated ndjson + reader
    web.go                    Dashboard HTTP server + API endpoints
    ndjson.go                 Shared ndjson tail helpers
    web/                      React SPA (Farm bundle)

  localdev/                   `pigeon dev` DNS + cert helpers
  netx/                       Shared network helpers (Proxy, etc.)

testtools/                    Manual E2E helpers, not part of the main binary
  tcpecho/, udpecho/, tcpclient/, udpclient/
```

## Architecture

```
External client → pigeon server (VPS)
                    ├── Control plane (:2222) — auth, forward registration, keepalive
                    ├── HTTP/HTTPS (:80/:443) — reverse proxy by Host header → yamux stream
                    └── TCP/UDP listeners — per-forward port → yamux stream
                              │
                    yamux multiplexed TCP connection
                              │
                  pigeon daemon (local machine)
                    ├── HTTP/TCP streams → net.Dial to local service, io.Copy
                    ├── UDP streams → NAT table (one local socket per external peer)
                    ├── Static streams → http.FileServer over rule.StaticRoot
                    └── Web Dashboard (:8080 or custom) — background goroutine
```

## Key Design Decisions

1. **Single connection.** All data flows over one yamux-multiplexed TCP connection. The client only needs outbound access to the server's control port.

2. **Length-prefixed JSON protocol.** Control messages use a 4-byte big-endian length prefix followed by JSON. Max message size is 10 MB. Stream headers use the same framing (64 KB max).

3. **UDP NAT table + peer allow-list.** The client maintains one local UDP socket per distinct external source address so echo replies carry the correct external address back to the server. The server, in turn, only forwards client-supplied frames to destinations whose address it has recently seen inbound (`udpPeerSet`, 2 min TTL in `listeners.go`) — a trusted client cannot abuse the tunnel as an outbound UDP spray relay.

4. **Config lives at `~/.pigeon/`.** `config.json` for credentials and forward rules (0600), `logs/` for NDJSON traffic and inspector logs (directory 0700, files 0600), `pigeon.pid` for daemon tracking.

5. **Daemon reconnect, pause, and Web hosting.** Exponential backoff capped at 30 seconds. The daemon forks via `os/exec` with `PIGEON_DAEMON=1`. It automatically hosts the Web UI on a background goroutine using the configured `web_addr`. A pause state (driven by `DaemonPause`/`DaemonResume`) stops the reconnect loop without killing the process, so the dashboard stays reachable.

6. **Base Domain Discovery.** On connection the server sends the base domain (e.g. `pigeon.btheo.com`) in the `AuthAck` message. The client persists it so subdomain-only inputs in the UI can be normalised.

7. **Proxy Support.** The server respects `X-Forwarded-Proto` only when `r.RemoteAddr` is in the configured trusted-proxy CIDR list — used for HTTPS detection behind Nginx/Cloudflare. Note: `clientIP()` in `visitor_enrich.go` currently trusts `X-Forwarded-For`/`X-Real-IP`/`CF-Connecting-IP` unconditionally for enrichment and for HTTP allow-list checks. If you touch that code, gate the header on the trusted-proxy list — unchecked trust lets attackers spoof their source for IP-allow-listed tunnels.

8. **Dashboard authentication.** The local web interface is protected by `DashboardPassword`. Login creates a random 32-byte session ID stored in an in-memory `sessionStore` (`web.go`); the cookie carries that ID, not a derivative of the password, and logout invalidates it server-side. Sessions idle-time out after 30 days. State-changing API calls from cookies must carry `X-Pigeon-CSRF` — Bearer-token callers are exempt.

9. **Tunnel-password cookies are HMAC-signed.** `password.go` issues cookies of the form `<unix-ts>.<hex(hmac-sha256(cookieSigningSecret, ts|token|id|pwd))>`. The secret is freshly generated on server start, so captured cookies never survive a restart and always expire after `passwordCookieMaxAge` (24 h).

10. **Template system.** `html/template` with split siblings: each page template (`status_*.html`, `password_*.html`) pulls its CSS from a matching `*.css.html` that `{{define}}`s a named template. The `template.ParseFS(*.html)` glob matches both and merges them into one namespace.

## Security Posture

Summarised so a reviewing agent can spot regressions fast:

| Layer | Protection | Where |
|---|---|---|
| Control plane auth | Per-IP rate limit (20 fails / 15 min) | `internal/server/control.go` + `password.go` |
| Control plane transport | **Plaintext TCP** — gap, mitigate via external TLS terminator | `internal/server/control.go:serveControl` |
| Tunnel HTTP password | Per-IP+forward rate limit, HMAC-signed expiring cookie, constant-time compare | `internal/server/password.go` |
| Tunnel IP allow-list | `fwd.allows()` checks client IP against CIDRs | `internal/server/control.go` + `http.go:63` |
| UDP arbitrary-peer send | Peer-seen allow-list with TTL | `internal/server/listeners.go` |
| Dashboard session | Random server-side IDs; revoked on logout / restart | `internal/client/web.go:sessionStore` |
| Dashboard CSRF | Required `X-Pigeon-CSRF` header on non-GET cookie-auth requests | `internal/client/web.go:auth` |
| Dashboard bind warning | Logged when `WebAddr` is not loopback | `internal/client/web.go:bindIsLoopback` |
| Config file perms | 0600; logs dir 0700 | `internal/client/config.go` |

If you're adding a new dashboard endpoint, the middleware stack is `auth(noCache(allowMethod(...)))` — keep that shape so CSRF + no-cache + 405 behaviour are all covered.

## Coding Conventions

- **Package layout.** `internal/` for library code, `cmd/` for binary entry points, `testtools/` for manual test helpers.
- **Test style.** Black-box tests in `package_test` where useful (e.g. `package proto_test`). Use `t.TempDir()` and `t.Setenv("HOME", ...)` to isolate filesystem tests.
- **Error handling.** Wrap with `fmt.Errorf("context: %w", err)`. Log-and-continue for non-fatal per-stream errors.
- **No global state.** Server uses `sync.Map` for session/forward registries. Client uses mutex-guarded maps for UDP NAT sessions and dashboard sessions.
- **Naming.** Lowercase short IDs (`proto.RandomID(8)`), camelCase methods, PascalCase exports.
- **Constant-time compares.** Use `crypto/subtle.ConstantTimeCompare` for any secret comparison.

## Common Tasks

### Adding a new message type

1. Add the `MessageType` constant in `internal/proto/proto.go`.
2. Define the payload struct.
3. Handle it in `server.go`/`control.go` and/or `client.go` `controlLoop()`.
4. Add a test case to `TestWriteRead` in `proto_test.go`.

### Adding a new CLI command

1. Create a `func xxxCmd() *cobra.Command` in its own `cmd/pigeon/xxx_cmd.go`.
2. Register it in `main()`'s `root.AddCommand(...)`.

### Adding a new forward protocol

1. Add the `Protocol` constant in `proto.go`.
2. Implement the server-side listener in `server/` (like `serveTCP`/`serveUDP`) or routing (for HTTP-like).
3. Implement the client-side handler in `client.go` (like `handleTCPStream`/`handleUDPStream`/`handleStaticStream`).
4. Allow it in the `forward add` CLI validation switch (`cmd/pigeon/forward_cmd.go`).

### Adding a new dashboard endpoint

1. Wrap with `auth(noCache(allowMethod(<METHOD>, ...)))` in `web.go` (or use Go 1.22 method-aware patterns for multi-method routes).
2. State-changing handlers inherit the CSRF requirement from `auth` automatically.
3. If the frontend should call it, use `dashFetch` in `web/src/` — the CSRF header is auto-added on non-GET requests.
4. Rebuild the bundle: `cd internal/client/web && npx farm build` so the embedded `dist/` is refreshed.

### Changing the dashboard UI

React code lives in `internal/client/web/src/`. The Go binary embeds the Farm-produced `web/dist/` at build time, but `StartWebInterface` prefers a local `./internal/client/web/dist` directory if one exists — handy for iterating without reinstalling the binary.

## Running Tests

```bash
# All tests
go test ./...

# Verbose
go test ./... -v

# Single package
go test ./internal/proto/...
go test ./internal/client/...
go test ./internal/server/...
```

Known pre-existing failure: `TestHandleStaticStream_ServesFile` in `internal/client` panics because the test constructs a `Client` without a logger. Unrelated to any recent changes on this branch.
