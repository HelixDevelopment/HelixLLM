#!/usr/bin/env bash
# =============================================================================
# HelixLLM Phase-3 VISION (VLM) end-to-end proof harness — RTX 5090.
#
# Boots a small llama.cpp VLM (Qwen2.5-VL-3B, GGUF + mmproj) via the ALREADY
# BUILT router image, on the GPU, ALONGSIDE the live coder at :18434 — WITHOUT
# touching the coder — feeds a REAL known-content image, asserts the real
# description is grounded in that content (§11.4.108), proves the checker is
# honest with a golden-BAD negative (§11.4.107(10)), snapshots VRAM showing
# BOTH fit (no OOM, §11.4.119/.133), tears the VLM down (single-owner cleanup),
# and confirms the coder is still Up + answering.
#
# SAFETY: checks free VRAM BEFORE boot and refuses if the model would not fit
# with >= HEADROOM_GB headroom. NEVER stops/restarts the coder.
#
# Reproducible: re-run from a clean host after `podman pull`/image build. The
# GGUF weights are cached (gitignored) at CACHE_DIR; first run downloads them
# over HTTPS via the router image's own curl+openssl stack (the same stack
# `-hf` uses, proven 2026-07-06).
# =============================================================================
set -euo pipefail

# ---- config ----------------------------------------------------------------
IMAGE="localhost/helixllm/llamacpp-router:cuda12.8-sm120"
HF_REPO="ggml-org/Qwen2.5-VL-3B-Instruct-GGUF"
MODEL_FILE="Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf"
MMPROJ_FILE="mmproj-Qwen2.5-VL-3B-Instruct-Q8_0.gguf"
CACHE_DIR="${HOME}/models/vlm_cache"           # host, gitignored (outside repo)
VLM_NAME="helixllm-vlm-phase3"
VLM_PORT="18500"                                # != 18434 (coder, untouched)
CODER_URL="http://localhost:18434"
HEADROOM_GB="2"
EST_VLM_GB="6"                                  # weights+mmproj+kv+compute est.

HARNESS_DIR="$(cd "$(dirname "$0")" && pwd)"
PHASE_DIR="$(cd "${HARNESS_DIR}/.." && pwd)"
EV="${PHASE_DIR}/evidence"
mkdir -p "${EV}" "${CACHE_DIR}"

log(){ printf '\n=== %s ===\n' "$*"; }
free_vram_mb(){ nvidia-smi --query-gpu=memory.free --format=csv,noheader,nounits | head -1; }

cleanup_vlm(){ podman rm -f "${VLM_NAME}" >/dev/null 2>&1 || true; }
trap 'cleanup_vlm' EXIT

# ---- 0. baseline + VRAM safety gate ---------------------------------------
log "0. baseline VRAM + coder health"
nvidia-smi --query-gpu=memory.total,memory.used,memory.free --format=csv,noheader,nounits | tee "${EV}/01_vram_before.txt"
curl -fsS --max-time 10 "${CODER_URL}/health" | tee -a "${EV}/01_vram_before.txt"; echo
FREE_MB="$(free_vram_mb)"
NEED_MB=$(( (EST_VLM_GB + HEADROOM_GB) * 1024 ))
echo "free=${FREE_MB} MiB ; need>=${NEED_MB} MiB (est ${EST_VLM_GB}G + headroom ${HEADROOM_GB}G)" | tee -a "${EV}/01_vram_before.txt"
if [ "${FREE_MB}" -lt "${NEED_MB}" ]; then
  echo "SKIP: insufficient free VRAM (${FREE_MB} < ${NEED_MB}) — honest §11.4.3 SKIP, NOT an OOM." | tee "${EV}/SKIP.txt"
  exit 3
fi

# ---- 1. download GGUF + mmproj over HTTPS (router image curl) --------------
log "1. HTTPS pull of GGUF + mmproj (router image curl+openssl)"
pull_one(){
  local f="$1"
  if [ -s "${CACHE_DIR}/${f}" ]; then echo "cached: ${f} ($(du -h "${CACHE_DIR}/${f}" | cut -f1))"; return; fi
  podman run --rm -v "${CACHE_DIR}:/cache:z" --entrypoint curl "${IMAGE}" \
    -fL --retry 3 -o "/cache/${f}.tmp" \
    "https://huggingface.co/${HF_REPO}/resolve/main/${f}"
  mv "${CACHE_DIR}/${f}.tmp" "${CACHE_DIR}/${f}"
  echo "downloaded: ${f} ($(du -h "${CACHE_DIR}/${f}" | cut -f1))"
}
{ pull_one "${MODEL_FILE}"; pull_one "${MMPROJ_FILE}"; ls -l "${CACHE_DIR}"; } 2>&1 | tee "${EV}/02_https_download.txt"

# ---- 2. boot the VLM on the GPU (coder untouched) -------------------------
log "2. boot VLM via router image (GPU, mmproj, port ${VLM_PORT})"
cleanup_vlm
podman run -d --name "${VLM_NAME}" \
  --device nvidia.com/gpu=all --security-opt=label=disable --network=host \
  -v "${CACHE_DIR}:/cache:ro" \
  "${IMAGE}" \
  -m "/cache/${MODEL_FILE}" \
  --mmproj "/cache/${MMPROJ_FILE}" \
  -ngl 99 -c 4096 --host 0.0.0.0 --port "${VLM_PORT}" --jinja 2>&1 | tee "${EV}/03_vlm_boot.log"

# ---- 3. poll health --------------------------------------------------------
log "3. poll VLM health"
ok=0
for i in $(seq 1 120); do
  if curl -fsS --max-time 5 "http://localhost:${VLM_PORT}/health" 2>/dev/null | grep -q '"status":"ok"'; then ok=1; break; fi
  sleep 2
done
{ echo "health polls: $((i*2))s"; curl -fsS "http://localhost:${VLM_PORT}/health"; echo;
  curl -fsS "http://localhost:${VLM_PORT}/v1/models"; echo; } | tee "${EV}/04_health.txt"
[ "${ok}" -eq 1 ] || { echo "BOOT-FAIL: VLM health never ok"; podman logs "${VLM_NAME}" 2>&1 | tail -60 | tee -a "${EV}/03_vlm_boot.log"; exit 4; }

# ---- 4. nvidia-smi: coder + VLM BOTH fit, no OOM --------------------------
log "4. nvidia-smi snapshot — coder + VLM both resident"
nvidia-smi --query-gpu=memory.total,memory.used,memory.free --format=csv,noheader,nounits | tee "${EV}/05_nvidia_smi_both_fit.txt"
nvidia-smi --query-compute-apps=pid,used_memory,process_name --format=csv,noheader | tee -a "${EV}/05_nvidia_smi_both_fit.txt"

# ---- 5. grounded vision probe + golden-BAD --------------------------------
log "5. grounded vision probe + golden-BAD (§11.4.108 / §11.4.107(10))"
VLM_MODEL_ID="$(curl -fsS "http://localhost:${VLM_PORT}/v1/models" | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"][0]["id"])')"
PROBE_RC=0
python3 "${HARNESS_DIR}/vision_probe.py" "http://localhost:${VLM_PORT}" "${EV}/test_image.png" "${EV}" "${VLM_MODEL_ID}" || PROBE_RC=$?

# ---- 6. teardown VLM (single-owner cleanup) -------------------------------
log "6. teardown VLM container"
podman rm -f "${VLM_NAME}" 2>&1 | tee "${EV}/10_teardown.txt"
trap - EXIT
{ echo "--- podman ps after teardown ---"; podman ps --format '{{.Names}} {{.Status}}';
  echo "--- VRAM after teardown ---"; nvidia-smi --query-gpu=memory.free --format=csv,noheader,nounits; } | tee -a "${EV}/10_teardown.txt"

# ---- 7. coder untouched: still Up + real answer ---------------------------
log "7. coder untouched — real /v1/chat/completions"
{ echo "--- coder container ---"; podman ps --filter name=helixllm-coder --format '{{.Names}} {{.Status}} {{.Ports}}';
  echo "--- coder health ---"; curl -fsS "${CODER_URL}/health"; echo; } | tee "${EV}/11_coder_untouched.txt"
curl -fsS --max-time 60 "${CODER_URL}/v1/chat/completions" \
  -H 'Content-Type: application/json' \
  -d '{"model":"coder","messages":[{"role":"user","content":"Reply with exactly: CODER_OK"}],"temperature":0,"max_tokens":16}' \
  | tee "${EV}/11_coder_chat_response.json"; echo

log "DONE — vision probe rc=${PROBE_RC}"
exit "${PROBE_RC}"
