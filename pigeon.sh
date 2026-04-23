#!/usr/bin/env bash
# Helper script for common Pigeon dev tasks.
#
# Usage:
#   ./pigeon.sh build            # compile the binary
#   ./pigeon.sh dev [args...]    # run local dev stack (sudo)
#   ./pigeon.sh stop             # stop any running pigeon processes (sudo)
#   ./pigeon.sh status           # show running pigeon processes + port bindings
#   ./pigeon.sh test             # run the full test suite
#   ./pigeon.sh race             # run tests with the race detector
#   ./pigeon.sh cover            # open HTML coverage report
#   ./pigeon.sh vet              # go vet + go build sanity check
#   ./pigeon.sh fmt              # go fmt
#   ./pigeon.sh clean            # remove built binaries and coverage files
#   ./pigeon.sh install          # install globally to /usr/local/bin (sudo)
#   ./pigeon.sh uninstall        # remove the global install
#   ./pigeon.sh help
#
# Env vars:
#   PIGEON_TOKEN     dev/server auth token (default: "secret")
#   PIGEON_DOMAIN    local dev domain     (default: "pigeon.local")
#   INSTALL_DIR      global install prefix (default: /usr/local/bin)

set -euo pipefail

# Resolve the directory this script lives in so it works from anywhere.
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

BIN="./pigeon"
PKG="./cmd/pigeon"

: "${PIGEON_TOKEN:=secret}"
: "${PIGEON_DOMAIN:=pigeon.local}"
: "${INSTALL_DIR:=/usr/local/bin}"

cmd_build() {
  echo "==> Building $BIN"
  go build -o "$BIN" "$PKG"
  ls -lh "$BIN"
}

cmd_dev() {
  # dev needs root to bind :80/:443 and write /etc/resolver/<domain>.
  # Rebuild first so `sudo` runs a fresh binary.
  cmd_build
  # Clean up any leftover daemon/server from a previous run. Failure is fine
  # (nothing to kill) — the real binding errors will still surface below.
  cmd_stop --quiet || true
  echo "==> Starting dev stack (sudo)"
  exec sudo "$BIN" dev --token "$PIGEON_TOKEN" --domain "$PIGEON_DOMAIN" "$@"
}

cmd_stop() {
  local quiet=0
  if [ "${1:-}" = "--quiet" ]; then quiet=1; shift; fi
  [ "$quiet" = 1 ] || echo "==> Stopping pigeon processes"

  # pkill -x matches on the exact process name, so this won't hit the helper
  # script itself or unrelated binaries that happen to contain "pigeon".
  if pgrep -x pigeon >/dev/null 2>&1; then
    sudo pkill -x pigeon || true
    # Give the kernel a moment to release :80/:443/:2222.
    sleep 0.5
  fi

  # Stale PID files can be root-owned after a `sudo dev` session.
  sudo rm -f "$HOME"/.pigeon/pigeon.pid "$HOME"/.pigeon/pigeon-dev.pid 2>/dev/null || true

  [ "$quiet" = 1 ] || echo "Stopped."
}

cmd_status() {
  echo "==> Running pigeon processes:"
  # Filter: processes whose command starts with "pigeon" or ends with "/pigeon"
  # (so `./pigeon.sh` and `pigeon.sh` editors don't count).
  local procs
  procs="$(ps -eo pid,user,command | awk '
    $3 ~ /(^|\/)pigeon$/ { print }
    $3 ~ /(^|\/)pigeon$/ && $4 != "" { next }
  ' || true)"
  if [ -z "$procs" ]; then
    echo "  (none)"
  else
    echo "$procs"
  fi

  echo
  echo "==> Port bindings (80, 443, 2222, 8080, 5454):"
  if ! lsof -nP -iTCP:80 -iTCP:443 -iTCP:2222 -iTCP:8080 -iUDP:5454 2>/dev/null | awk 'NR==1 || /LISTEN|UDP/'; then
    echo "  (lsof unavailable)"
  fi
}

cmd_test() {
  echo "==> go test ./..."
  go test ./...
}

cmd_race() {
  echo "==> go test -race ./..."
  go test -race ./...
}

cmd_cover() {
  echo "==> go test -coverprofile=coverage.out ./..."
  go test -coverprofile=coverage.out ./...
  go tool cover -func=coverage.out | tail -20
  echo "==> Opening HTML report"
  go tool cover -html=coverage.out
}

cmd_vet() {
  echo "==> go vet ./..."
  go vet ./...
  echo "==> go build ./..."
  go build ./...
}

cmd_fmt() {
  echo "==> go fmt ./..."
  go fmt ./...
}

cmd_clean() {
  echo "==> Removing build + coverage artifacts"
  rm -f "$BIN" pigeon-linux main coverage.out
}

cmd_install() {
  cmd_build
  local dest="$INSTALL_DIR/pigeon"
  echo "==> Installing to $dest"
  if [ -w "$INSTALL_DIR" ]; then
    install -m 0755 "$BIN" "$dest"
  else
    sudo install -m 0755 "$BIN" "$dest"
  fi
  echo "Installed: $(command -v pigeon || echo "$dest (not on PATH)")"
  pigeon --help >/dev/null 2>&1 && echo "Run 'pigeon --help' to get started." || true
}

cmd_uninstall() {
  local dest="$INSTALL_DIR/pigeon"
  if [ ! -e "$dest" ]; then
    echo "Nothing to uninstall at $dest"
    return 0
  fi
  echo "==> Removing $dest"
  if [ -w "$INSTALL_DIR" ]; then
    rm -f "$dest"
  else
    sudo rm -f "$dest"
  fi
  echo "Removed."
}

cmd_help() {
  sed -n '2,23p' "$0"
}

case "${1:-help}" in
  build)    shift; cmd_build    "$@" ;;
  dev)      shift; cmd_dev      "$@" ;;
  stop)     shift; cmd_stop     "$@" ;;
  status)   shift; cmd_status   "$@" ;;
  test)     shift; cmd_test     "$@" ;;
  race)     shift; cmd_race     "$@" ;;
  cover)    shift; cmd_cover    "$@" ;;
  vet)      shift; cmd_vet      "$@" ;;
  fmt)      shift; cmd_fmt      "$@" ;;
  clean)    shift; cmd_clean    "$@" ;;
  install)   shift; cmd_install   "$@" ;;
  uninstall) shift; cmd_uninstall "$@" ;;
  help|-h|--help) cmd_help ;;
  *)
    echo "unknown command: $1" >&2
    echo >&2
    cmd_help >&2
    exit 2
    ;;
esac
