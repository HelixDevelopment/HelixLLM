#!/usr/bin/env bash
# install.sh — Build and install HelixLLM binaries to ~/.local/bin.
#
# Builds:
#   - helixllm        (main gateway binary, cmd/helixllm)
#   - mcp-gateway     (MCP gateway, cmd/mcp-gateway)
#   - a2a-server      (Agent-to-Agent server, cmd/a2a-server)
#
# Idempotent: rebuilds in place, re-creates symlinks.
#
# Usage:
#   bash scripts/install.sh
#   bash scripts/install.sh --bin-dir /usr/local/bin
#   bash scripts/install.sh --check-only
#
# Env:
#   BIN_DIR   target for symlinks (default ~/.local/bin)
#   GOFLAGS   extra go build flags

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
CHECK_ONLY=0
for arg in "$@"; do
  case "$arg" in
    --check-only) CHECK_ONLY=1 ;;
    --bin-dir) shift; BIN_DIR="$1" ;;
  esac
done

GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

log() { printf '%b[helixllm]%b %s\n' "$GREEN" "$NC" "$*"; }
die() { printf '%b[helixllm]%b %s\n' "$RED" "$NC" "$*" >&2; exit 1; }

# ─── Preflight ──────────────────────────────────────────────────────

if ! command -v go >/dev/null 2>&1; then
  if [ -x "$BIN_DIR/helixllm" ]; then
    log "Go not found but helixllm already installed at $BIN_DIR/helixllm"
    exit 0
  fi
  die "Go toolchain not found. Install Go: https://go.dev/dl/"
fi

# ─── Build ───────────────────────────────────────────────────────────

cd "$REPO_ROOT"
GOFLAGS="${GOFLAGS:--ldflags=-s -w}"

BINARIES=(
  "cmd/helixllm:helixllm"
  "cmd/mcp-gateway:mcp-gateway"
  "cmd/a2a-server:a2a-server"
)

log "Building HelixLLM binaries ..."
mkdir -p "$REPO_ROOT/bin"

for entry in "${BINARIES[@]}"; do
  src="${entry%%:*}"
  name="${entry##*:}"

  if [ "$CHECK_ONLY" -eq 1 ]; then
    if [ -x "$BIN_DIR/$name" ]; then
      log "  $name: OK ($BIN_DIR/$name)"
    else
      log "  $name: MISSING"
    fi
    continue
  fi

  echo "  building $name from $src ..."
  # shellcheck disable=SC2086
  go build $GOFLAGS -o "bin/$name" "./$src"
  log "  $name built -> bin/$name"
done

# ─── Install ─────────────────────────────────────────────────────────

if [ "$CHECK_ONLY" -eq 1 ]; then
  exit 0
fi

mkdir -p "$BIN_DIR"

for entry in "${BINARIES[@]}"; do
  name="${entry##*:}"
  ln -sf "$REPO_ROOT/bin/$name" "$BIN_DIR/$name"
  log "  $name -> $BIN_DIR/$name"
done

# ─── Verify ──────────────────────────────────────────────────────────

if [ -x "$BIN_DIR/helixllm" ]; then
  log "helixllm installed successfully."
  "$BIN_DIR/helixllm" version 2>/dev/null || true
else
  die "helixllm install failed"
fi
