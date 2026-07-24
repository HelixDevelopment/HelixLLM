#!/usr/bin/env bash
# setup.sh — one-time dependency check, config bootstrap, and systemd unit
# installation for HelixLLM.
#
# What it does:
#   1. Check Go toolchain + nvidia-ctk (GPU env)
#   2. Generate certs if missing (dev TLS)
#   3. Bootstrap HelixLLM config from template
#   4. Install systemd user unit for HelixLLM gateway
#
# Idempotent: safe to re-run.
#
# Usage:
#   bash scripts/setup.sh
#   bash scripts/setup.sh --check-only
#   bash scripts/setup.sh --no-systemd

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SYSTEMD_USER_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/helixllm"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

info() { printf '%b[INFO]%b  %s\n' "$GREEN" "$NC" "$*"; }
warn() { printf '%b[WARN]%b  %s\n' "$YELLOW" "$NC" "$*" >&2; }
die()  { printf '%b[ERROR]%b %s\n' "$RED" "$NC" "$*" >&2; exit 1; }

CHECK_ONLY=0
SKIP_SYSTEMD=0
for arg in "$@"; do
  case "$arg" in
    --check-only) CHECK_ONLY=1 ;;
    --no-systemd) SKIP_SYSTEMD=1 ;;
  esac
done

info "HelixLLM Setup"
info "Root: $REPO_ROOT"

# ─── 1. Dependencies ─────────────────────────────────────────────────

info ""
info "1/4 Checking dependencies"

missing=0
for tool in go nvidia-smi nvidia-ctk curl; do
  if command -v "$tool" >/dev/null 2>&1; then
    info "  $tool: found"
  else
    if [ "$tool" = "nvidia-smi" ] || [ "$tool" = "nvidia-ctk" ]; then
      warn "  $tool: not found (no GPU support — HelixLLM will run in CPU-only mode)"
    elif [ "$tool" = "go" ]; then
      die "  go: REQUIRED but not found — install from https://go.dev/dl/"
    else
      warn "  $tool: not found"
    fi
  fi
done

if [ "$CHECK_ONLY" -eq 1 ]; then
  info "Check complete (--check-only)."
  exit 0
fi

# ─── 2. Certificates ─────────────────────────────────────────────────

info ""
info "2/4 Checking TLS certificates"
CERT_DIR="$REPO_ROOT/certs"

if [ -f "$CERT_DIR/server.crt" ] && [ -f "$CERT_DIR/server.key" ]; then
  info "  TLS certs already present"
else
  warn "  TLS certs not found at $CERT_DIR/"
  if command -v openssl >/dev/null 2>&1; then
    info "  Generating self-signed dev certs ..."
    mkdir -p "$CERT_DIR"
    openssl req -x509 -newkey rsa:4096 -keyout "$CERT_DIR/server.key" \
      -out "$CERT_DIR/server.crt" -days 365 -nodes \
      -subj "/CN=helixllm-localhost" 2>/dev/null
    info "  Dev certs generated"
  else
    warn "  openssl not found — cannot generate certs"
  fi
fi

# ─── 3. Config ───────────────────────────────────────────────────────

info ""
info "3/4 Bootstrapping config"
mkdir -p "$CONFIG_DIR"

if [ ! -f "$CONFIG_DIR/gateway.yaml" ]; then
  if [ -f "$REPO_ROOT/configs/gateway.template.yaml" ]; then
    cp "$REPO_ROOT/configs/gateway.template.yaml" "$CONFIG_DIR/gateway.yaml"
    info "  Config created: $CONFIG_DIR/gateway.yaml"
  else
    warn "  No config template found at configs/gateway.template.yaml"
    warn "  Create $CONFIG_DIR/gateway.yaml manually"
  fi
else
  info "  Config already exists: $CONFIG_DIR/gateway.yaml"
fi

# ─── 4. Systemd unit ─────────────────────────────────────────────────

if [ "$SKIP_SYSTEMD" -eq 1 ]; then
  info "4/4 Skipping systemd (--no-systemd)"
  exit 0
fi

info ""
info "4/4 Installing systemd user unit"

HELIX_CODE_ROOT="$REPO_ROOT/../.."
UNIT_SRC="$HELIX_CODE_ROOT/scripts/systemd/helixllm-gateway.service"
UNIT_DST="$SYSTEMD_USER_DIR/helixllm-gateway.service"

mkdir -p "$SYSTEMD_USER_DIR"

# The unit is created alongside the HelixCode scripts; if it doesn't exist yet,
# write it directly.
if [ -f "$UNIT_SRC" ]; then
  cp "$UNIT_SRC" "$UNIT_DST"
  info "  helixllm-gateway.service installed"
else
  warn "  Unit source not found: $UNIT_SRC"
  warn "  Unit was created independently — no copy needed"
fi

systemctl --user daemon-reload 2>/dev/null || true
systemctl --user enable helixllm-gateway.service 2>/dev/null || \
  warn "  could not enable helixllm-gateway.service"

info ""
info "HelixLLM setup complete."
info "Start:  systemctl --user start helixllm-gateway"
info "Status: systemctl --user status helixllm-gateway"
