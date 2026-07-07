#!/usr/bin/env bash
# run_proof.sh — the ONE operator step that turns the Phase-4 video-gen SCAFFOLD
# into a captured runtime proof, once a coder-pause window is authorized.
#
# It is SAFE to run the `admit-check` phase at ANY time (read-only nvidia-smi
# admission probe — no boot, no GPU workload, coder untouched). The `boot` +
# `generate` phases require the operator to have freed enough VRAM (typically a
# coder-pause, §11.4.122) AND the WAN 2.2 / LTX weights present in the HF cache
# volume. Nothing here pauses the coder autonomously (§11.4.122/§11.4.101).
#
# This script drives the containers-submodule orchestrator via the `videogen-boot`
# Go binary (built from ../../../cmd/videogen-boot) and the videogen HTTP shim —
# it does NOT hand-run podman/docker (§11.4.76 / §11.4.161 / Rule 4).
#
# Usage:
#   ./run_proof.sh admit-check           # SAFE now: broker VRAM admission verdict only
#   ./run_proof.sh boot                  # AUTHORIZED window: admit -> compose up -> /health
#   ./run_proof.sh generate "<prompt>"   # AUTHORIZED window: real clip + analyzer verdict
#   ./run_proof.sh selfcheck             # SAFE now: analyzer self-validation (no GPU)
#   ./run_proof.sh down                  # single-owner teardown (coder untouched)
#
# Every value below is CONFIG-INJECTED (§CONST-045/046); edit via env or .env,
# NEVER hardcode a model/precision/host/port literal into source.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${HERE}/../../../.." && pwd)"          # submodules/helix_llm root
BOOT_DIR="${REPO_ROOT}/cmd/videogen-boot"
COMPOSE_FILE="${BOOT_DIR}/compose.videogen.yml"
PROJECT_NAME="${VIDEOGEN_PROJECT:-helixllm_videogen}"
ANALYZER_DIR="${HERE}/vidanalyzer"

# ---- config-injected service parameters (Blackwell no-pause WAN-5B-FP8@480p) ----
export VIDEOGEN_BACKEND="${VIDEOGEN_BACKEND:-wan}"          # wan | ltx
export VIDEOGEN_MODEL="${VIDEOGEN_MODEL:-Wan-AI/Wan2.2-TI2V-5B-Diffusers}"
export VIDEOGEN_PRECISION="${VIDEOGEN_PRECISION:-fp8}"      # RTX 5090 no-pause default: WAN-5B FP8 @480p
export VIDEOGEN_CPU_OFFLOAD="${VIDEOGEN_CPU_OFFLOAD:-1}"
export VIDEOGEN_MAX_STEPS="${VIDEOGEN_MAX_STEPS:-30}"
export VIDEOGEN_NUM_FRAMES="${VIDEOGEN_NUM_FRAMES:-49}"     # WAN uses 4n+1; 49@16fps ~= 3 s
export VIDEOGEN_FPS="${VIDEOGEN_FPS:-16}"
export VIDEOGEN_SIZE="${VIDEOGEN_SIZE:-832x480}"           # 480p short-clip no-pause default
export VIDEOGEN_HOST_PORT="${VIDEOGEN_HOST_PORT:-18443}"   # OWN port — coder :18434, image-gen :18442
export VIDEOGEN_MEM_LIMIT="${VIDEOGEN_MEM_LIMIT:-24g}"
export VIDEOGEN_SHM_SIZE="${VIDEOGEN_SHM_SIZE:-2g}"
# HF_TOKEN comes from the environment / .env (§11.4.10) — never committed. Only
# needed if a gated WAN/LTX variant is selected; the default repos are open.
export HF_TOKEN="${HF_TOKEN:-}"

log() { printf '[run_proof] %s\n' "$*"; }

build_boot() {
  ( cd "${REPO_ROOT}" && go build -o "${BOOT_DIR}/videogen-boot" ./cmd/videogen-boot )
}

cmd_admit_check() {
  log "Building videogen-boot (compile-only, no GPU) ..."
  build_boot
  log "Broker VRAM admission verdict (read-only nvidia-smi, coder untouched):"
  "${BOOT_DIR}/videogen-boot" admit-check
}

cmd_boot() {
  build_boot
  log "Booting videogen on :${VIDEOGEN_HOST_PORT} via containers-submodule orchestrator ..."
  "${BOOT_DIR}/videogen-boot" boot "${COMPOSE_FILE}" "${PROJECT_NAME}"
}

cmd_down() {
  build_boot
  "${BOOT_DIR}/videogen-boot" down "${COMPOSE_FILE}" "${PROJECT_NAME}"
}

cmd_selfcheck() {
  log "Analyzer self-validation (golden-good LIVE / golden-bad DEGENERATE, no GPU) ..."
  ( cd "${ANALYZER_DIR}" && go build -o vidanalyzer . && ./vidanalyzer selfvalidate )
}

cmd_generate() {
  local prompt="${1:?usage: run_proof.sh generate \"<prompt>\"}"
  local out_dir="${HERE}/out"
  mkdir -p "${out_dir}"
  local mp4="${out_dir}/helix_llm---videogen---$(date -u +%Y%m%dT%H%M%SZ).mp4"

  log "POST /v1/videos/generations (real WAN/LTX inference) ..."
  # base64 JSON -> MP4. Requires the service booted (cmd_boot) + weights present.
  curl -fsS -X POST "http://localhost:${VIDEOGEN_HOST_PORT}/v1/videos/generations" \
    -H 'Content-Type: application/json' \
    -d "{\"prompt\":$(printf '%s' "${prompt}" | python3 -c 'import json,sys;print(json.dumps(sys.stdin.read()))')}" \
    | python3 -c 'import base64,json,sys;d=json.load(sys.stdin);open(sys.argv[1],"wb").write(base64.b64decode(d["data"][0]["b64_mp4"]))' "${mp4}"
  log "Wrote ${mp4}"

  log "Analyzer GREEN-guard verdict (RED_MODE=0: PASS iff a REAL live generated video) ..."
  ( cd "${ANALYZER_DIR}" && go build -o vidanalyzer . && RED_MODE=0 ./vidanalyzer analyze "${mp4}" )
}

case "${1:-}" in
  admit-check) cmd_admit_check ;;
  boot)        cmd_boot ;;
  down)        cmd_down ;;
  selfcheck)   cmd_selfcheck ;;
  generate)    shift; cmd_generate "${1:-}" ;;
  *) echo "usage: $0 <admit-check|boot|generate \"<prompt>\"|selfcheck|down>" >&2; exit 2 ;;
esac
