# AGENTS.md

This file provides context for AI coding agents working on the Pigeon codebase.

## Project Overview

Pigeon is a self-hosted tunnelling tool written in Go. It exposes local services (HTTP, TCP, UDP) to the internet through a VPS relay server. Communication between client and server uses a single multiplexed TCP connection via [yamux](https://github.com/hashicorp/yamux) with a custom length-prefixed JSON wire protocol.

## Tech Stack

- **Language:** Go 1.26+
- **Dependencies:** `hashicorp/yamux`, `spf13/cobra`, `golang.org/x/crypto` (ACME)
- **Build:** `go build -o pigeon ./cmd/pigeon`
- **Tests:** `go test ./...`

## Repository Structure

```
cmd/pigeon/main.go          CLI entry point (cobra commands)
internal/
  proto/                     Wire protocol (length-prefixed JSON messages)
    proto.go                 Message types, Read/Write, StreamHeader
    proto_test.go            Round-trip, decode, stream header tests
  server/                    Tunnel server (runs on VPS)
    server.go                Control plane, HTTP/HTTPS reverse proxy, TCP/UDP listeners
    server_test.go           Server construction, ServeHTTP 502 tests
  client/                    Tunnel client (runs on user machine)
    client.go                yamux session, control loop, stream handlers (TCP/UDP)
    config.go                Config struct, ForwardRule, SaveConfig/LoadConfig, AddForward/RemoveForward
    config_test.go           Config CRUD, round-trip, duplicate detection tests
    daemon.go                Background daemon (fork, PID file, reconnect with backoff)
    daemon_test.go           IsDaemon, PIDFile, LogDir tests
    logs.go                  TailLogs (filter, since, limit, follow)
    logs_test.go             Log reading, filtering, limit, malformed line tests
testtools/                   Manual E2E test helpers (not part of the main binary)
  tcpecho/                   TCP echo server
  udpecho/                   UDP echo server
  tcpclient/                 TCP echo verification client
  udpclient/                 UDP echo verification client
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
                    └── UDP streams → NAT table (one local socket per external source)
```

## Key Design Decisions

1. **Single connection:** All data flows over one yamux-multiplexed TCP connection. The client only needs outbound access to the server's control port.

2. **Length-prefixed JSON protocol:** Control messages use a 4-byte big-endian length prefix followed by JSON. Max message size is 10 MB. Stream headers use the same framing (64 KB max).

3. **UDP NAT table:** The client maintains one local UDP socket per distinct external source address. This ensures echo replies carry the correct external address back to the server, not the local service's address.

4. **Config lives at `~/.pigeon/`:** `config.json` for credentials and forward rules, `logs/` for NDJSON traffic logs, `pigeon.pid` for daemon tracking.

5. **Daemon reconnect:** Exponential backoff capped at 30 seconds. The daemon forks via `os/exec` with `PIGEON_DAEMON=1` env var and `Setsid: true`.

## Coding Conventions

- **Package layout:** `internal/` for all library code, `cmd/` for the binary entry point.
- **Test style:** Black-box tests in `package_test` (e.g., `package proto_test`). Use `t.TempDir()` and `t.Setenv("HOME", ...)` to isolate filesystem tests.
- **Error handling:** Return `fmt.Errorf("context: %w", err)` for wrapping. Log-and-continue for non-fatal stream errors.
- **No global state:** Server uses `sync.Map` for session/forward registries. Client uses mutex-guarded maps for UDP NAT sessions.
- **Naming:** Lowercase short IDs (`randomID(8)`), camelCase methods, PascalCase exports.

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

## Common Tasks

### Adding a new message type
1. Add the `MessageType` constant in `internal/proto/proto.go`
2. Define the payload struct
3. Handle it in `server.go` control loop and/or `client.go` `controlLoop()`
4. Add a test case to `TestWriteRead` in `proto_test.go`

### Adding a new CLI command
1. Create a `func xxxCmd() *cobra.Command` in `cmd/pigeon/main.go`
2. Register it in `root.AddCommand(...)` inside `main()`

### Adding a new forward protocol
1. Add the `Protocol` constant in `proto.go`
2. Implement server-side listener in `server.go` (like `serveTCP`/`serveUDP`)
3. Implement client-side handler in `client.go` (like `handleTCPStream`/`handleUDPStream`)
4. Allow it in the `forward add` CLI validation switch
