#!/usr/bin/env bash
# run_proof.sh — the ONE operator step that turns the Phase-4 image-gen SCAFFOLD
# into a captured runtime proof, once a coder-pause window is authorized.
#
# It is SAFE to run the `admit-check` phase at ANY time (read-only nvidia-smi
# admission probe — no boot, no GPU workload, coder untouched). The `boot` +
# `generate` phases require the operator to have freed enough VRAM (typically a
# coder-pause, §11.4.122) AND the FLUX + Nunchaku NVFP4 weights present in the
# HF cache volume. Nothing here pauses the coder autonomously (§11.4.122/§11.4.101).
#
# This script drives the containers-submodule orchestrator via the `imagegen-boot`
# Go binary (built from ../../../cmd/imagegen-boot) and the imagegen HTTP shim —
# it does NOT hand-run podman/docker (§11.4.76 / §11.4.161 / Rule 4).
#
# Usage:
#   ./run_proof.sh admit-check           # SAFE now: broker VRAM admission verdict only
#   ./run_proof.sh boot                  # AUTHORIZED window: admit -> compose up -> /health
#   ./run_proof.sh generate "<prompt>"   # AUTHORIZED window: real image + analyzer verdict
#   ./run_proof.sh down                  # single-owner teardown (coder untouched)
#
# Every value below is CONFIG-INJECTED (§CONST-045/046); edit via env or .env,
# NEVER hardcode a model/precision/host/port literal into source.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${HERE}/../../../.." && pwd)"          # submodules/helix_llm root
BOOT_DIR="${REPO_ROOT}/cmd/imagegen-boot"
COMPOSE_FILE="${BOOT_DIR}/compose.imagegen.yml"
PROJECT_NAME="${IMAGEGEN_PROJECT:-helixllm_imagegen}"
ANALYZER_DIR="${HERE}/imganalyzer"

# ---- config-injected service parameters (Blackwell NVFP4 min-mem path) ----
# Default model: FLUX.1-schnell (Apache-2.0, §11.4.150 research 2026-07-08 —
# HF API confirms license: apache-2.0). HONEST NOTE (§11.4.6): schnell is
# STILL an HF "auto"-gated repo (same as dev) — HF_TOKEN is still required
# for the base pipeline download; the win is the permissive licence + the
# instant/no-review gate class, not token-freedom. Set
# IMAGEGEN_MODEL=black-forest-labs/FLUX.1-dev to opt back into dev.
export IMAGEGEN_MODEL="${IMAGEGEN_MODEL:-black-forest-labs/FLUX.1-schnell}"
export IMAGEGEN_PRECISION="${IMAGEGEN_PRECISION:-nvfp4}"     # RTX 5090 cc10.x -> NVFP4
export IMAGEGEN_QUANTIZE_T5="${IMAGEGEN_QUANTIZE_T5:-1}"
export IMAGEGEN_CPU_OFFLOAD="${IMAGEGEN_CPU_OFFLOAD:-1}"
# schnell model card (verified 2026-07-08): num_inference_steps=4,
# guidance_scale=0.0. Override both for a dev opt-in (e.g. steps=28, guidance=3.5).
export IMAGEGEN_MAX_STEPS="${IMAGEGEN_MAX_STEPS:-4}"
export IMAGEGEN_GUIDANCE_SCALE="${IMAGEGEN_GUIDANCE_SCALE:-0.0}"
export IMAGEGEN_HOST_PORT="${IMAGEGEN_HOST_PORT:-18442}"     # OWN port — coder is :18434
export IMAGEGEN_MEM_LIMIT="${IMAGEGEN_MEM_LIMIT:-24g}"
export IMAGEGEN_SHM_SIZE="${IMAGEGEN_SHM_SIZE:-2g}"
# NUNCHAKU_WHEEL + HF_TOKEN come from the environment / .env (§11.4.10) — never
# committed. The wheel URL is verified LATEST per §11.4.99 (see README).
export NUNCHAKU_WHEEL="${NUNCHAKU_WHEEL:-}"

log() { printf '[run_proof] %s\n' "$*"; }

build_boot() {
  ( cd "${REPO_ROOT}" && go build -o "${BOOT_DIR}/imagegen-boot" ./cmd/imagegen-boot )
}

cmd_admit_check() {
  log "Building imagegen-boot (compile-only, no GPU) ..."
  build_boot
  log "Broker VRAM admission verdict (read-only nvidia-smi, coder untouched):"
  "${BOOT_DIR}/imagegen-boot" admit-check
}

cmd_boot() {
  build_boot
  log "Booting imagegen on :${IMAGEGEN_HOST_PORT} via containers-submodule orchestrator ..."
  "${BOOT_DIR}/imagegen-boot" boot "${COMPOSE_FILE}" "${PROJECT_NAME}"
}

cmd_down() {
  build_boot
  "${BOOT_DIR}/imagegen-boot" down "${COMPOSE_FILE}" "${PROJECT_NAME}"
}

cmd_generate() {
  local prompt="${1:?usage: run_proof.sh generate \"<prompt>\"}"
  local out_dir="${HERE}/out"
  mkdir -p "${out_dir}"
  local png="${out_dir}/helix_llm---imagegen---$(date -u +%Y%m%dT%H%M%SZ).png"

  log "POST /v1/images/generations (real FLUX+NVFP4 inference) ..."
  # base64 JSON -> PNG. Requires the service booted (cmd_boot) + weights present.
  curl -fsS -X POST "http://localhost:${IMAGEGEN_HOST_PORT}/v1/images/generations" \
    -H 'Content-Type: application/json' \
    -d "{\"prompt\":$(printf '%s' "${prompt}" | python3 -c 'import json,sys;print(json.dumps(sys.stdin.read()))'),\"response_format\":\"b64_json\"}" \
    | python3 -c 'import base64,json,sys;d=json.load(sys.stdin);open(sys.argv[1],"wb").write(base64.b64decode(d["data"][0]["b64_json"]))' "${png}"
  log "Wrote ${png}"

  log "Analyzer GREEN-guard verdict (RED_MODE=0: PASS iff a REAL generated image) ..."
  ( cd "${ANALYZER_DIR}" && go build -o imganalyzer . && RED_MODE=0 ./imganalyzer analyze "${png}" )
}

case "${1:-}" in
  admit-check) cmd_admit_check ;;
  boot)        cmd_boot ;;
  down)        cmd_down ;;
  generate)    shift; cmd_generate "${1:-}" ;;
  *) echo "usage: $0 <admit-check|boot|generate \"<prompt>\"|down>" >&2; exit 2 ;;
esac
