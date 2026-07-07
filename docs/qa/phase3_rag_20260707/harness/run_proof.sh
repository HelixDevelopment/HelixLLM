#!/usr/bin/env bash
# Phase-3 RAG (Retrieval-Augmented Generation) END-TO-END proof
# (§11.4.108 runtime signature / §11.4.6 / §11.4.107(10) / §11.4.115).
#
# Composes the two ALREADY-PROVEN Phase-3 capabilities:
#   - CPU embeddings: HF Text Embeddings Inference (TEI), BAAI/bge-small-en-v1.5,
#     dim 384, booted via the containers submodule orchestrator (§11.4.76),
#     rootless podman (§11.4.161), on its OWN port 18440.
#   - the live coder LLM (helixllm-coder, already running at :18434) — READ
#     ONLY, never restarted/stopped (§11.4.122 / §11.4.119).
#
# Unfakeable proof: two INVENTED facts in a small corpus. RED baseline asks
# the coder the bare question (no context) -> must NOT know the answer.
# GREEN runs the full pipeline (embed -> real cosine retrieve top-1 ->
# grounded prompt -> generate) -> the answer MUST contain the invented token.
# A self-validation pass proves the analyzer genuinely discriminates
# (golden-good PASS; 3 golden-bad variants MUST FAIL).
#
# Reproducible: re-run against a clean host to regenerate every evidence file.
# All model/port/limit values are config-injected here (§CONST-045/046) — the
# compose file carries no literal.
#
# NOTE: status is tracked via files in a scratch STATUS_DIR rather than shell
# variables set inside `{ ... } | tee` blocks, because each element of a bash
# pipeline runs in its own subshell — a variable assignment inside such a
# block is silently lost to the parent shell (a real bug class this script
# deliberately avoids).
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"              # .../helix_llm/docs/qa/phase3_rag_20260707/harness
EVID="$(cd "$HERE/../../../../../../docs/qa/phase3_rag_20260707" && pwd)"  # outer repo root evidence dir
cd "$HERE"

STATUS_DIR="$(mktemp -d)"
trap 'rm -rf "$STATUS_DIR"' EXIT

# ---- config injection (no hardcoded host/port/model literal downstream) ----
export TEI_HOST_PORT="${TEI_HOST_PORT:-18440}"    # OWN port — coder=18434, siblings=18435-18439
export TEI_MEM_LIMIT="${TEI_MEM_LIMIT:-8g}"
export TEI_CPUS="${TEI_CPUS:-8}"
export TEI_MODEL_ID="${TEI_MODEL_ID:-BAAI/bge-small-en-v1.5}"  # proven CPU lane (embeddings proof, 2026-07-06)
PROJECT="phase3rag"
COMPOSE="compose.phase3rag.yml"
TEI_BASE="http://localhost:${TEI_HOST_PORT}"
CODER_BASE="${CODER_BASE:-http://localhost:18434}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-300}"

BIN="$HERE/phase3rag.bin"

log() { echo "[$(date -u +%H:%M:%S)] $*"; }

build_harness() {
  log "building harness (containers-submodule replace) ..."
  GOFLAGS=-mod=mod go build -o "$BIN" . || { echo "BUILD FAILED"; exit 3; }
}

teardown_project() {
  local out="${1:-/dev/stdout}"
  log "teardown project=$PROJECT (single-owner cleanup, coder untouched) ..."
  "$BIN" boot-down "$COMPOSE" "$PROJECT" 2>&1 | tee "$out"
}

CURRENT_CONTAINER="${PROJECT}_tei-rag_1"

poll_health() {
  local deadline=$(( $(date +%s) + HEALTH_TIMEOUT ))
  local n=0
  while [ "$(date +%s)" -lt "$deadline" ]; do
    n=$((n+1))
    code=$(curl -s -o /dev/null -w '%{http_code}' "$TEI_BASE/health" 2>/dev/null || echo 000)
    if [ "$code" = "200" ]; then
      log "health OK after $n polls"
      return 0
    fi
    st=$(podman inspect "$CURRENT_CONTAINER" --format '{{.State.Status}}' 2>/dev/null || echo starting)
    if [ "$st" = "exited" ]; then
      log "container state=exited before healthy — abort poll (n=$n)"
      return 1
    fi
    sleep 3
  done
  log "health poll TIMED OUT after ${HEALTH_TIMEOUT}s"
  return 1
}

# ---------- BUILD + PRE-CLEAN (clean-target integrity §11.4.108/§11.4.139) ----------
build_harness
"$BIN" boot-down "$COMPOSE" "$PROJECT" >/dev/null 2>&1 || true
podman volume create helixllm-tei-cache >/dev/null 2>&1 || true

# ---------- PRE-FLIGHT ----------
{
  echo "### pre-flight (§11.4.119 single-owner, coder untouched)"
  echo "date_utc=$(date -u +%FT%TZ)"
  echo "uname_m=$(uname -m)  podman=$(podman --version)"
  echo "coder container (MUST remain running, untouched):"
  podman ps --filter name=helixllm-coder --format '{{.Names}} {{.Image}} {{.Status}}'
  echo "coder live model + health:"
  curl -s -o /dev/null -w 'coder /v1/models http_code=%{http_code}\n' "$CODER_BASE/v1/models" || true
  echo "target host port ${TEI_HOST_PORT} listeners (expect none):"
  ss -ltn 2>/dev/null | grep ":${TEI_HOST_PORT} " || echo "  (free)"
  echo "sibling Phase-3 ports 18435-18439 (MUST remain untouched by this run):"
  for p in 18435 18436 18437 18438 18439; do
    ss -ltn 2>/dev/null | grep ":${p} " >/dev/null 2>&1 && echo "  :$p LISTENING (untouched by this harness)" || echo "  :$p free"
  done
} | tee "$EVID/00_preflight.txt"

# ---------- §11.4.150 research note ----------
{
  echo "### deep-research note (§11.4.150) — minimal RAG pipeline pattern"
  echo "Pattern: embed corpus -> cosine top-k retrieve -> stuff retrieved context into the"
  echo "generation prompt -> call the LLM (\"stuff\"/\"grounded QA\" pattern). Sources cited in RESULTS.md."
} | tee "$EVID/01_research_note.txt"

# ---------- RED-FIRST BASELINE (§11.4.115) — BOTH invented facts, live coder, no context ----------
{
  echo "### RED baseline (§11.4.115): coder asked the SAME questions with NO retrieved context"
  echo "RED_MODE=1 — expect the coder to NOT know either invented fact (defect reproduced)"
  for qk in q1 q2; do
    echo "--- qkey=$qk ---"
    if "$BIN" red "$CODER_BASE" "$qk" "$EVID/red_response_${qk}.json"; then
      echo "red_exit_${qk}=0"
    else
      rc=$?
      echo "red_exit_${qk}=$rc"
      echo "$qk" >> "$STATUS_DIR/red_fail"
    fi
  done
} 2>&1 | tee "$EVID/10_red_baseline.txt"

# ---------- BOOT TEI (reused bge-small lane + cache, §11.4.82) ----------
log "boot TEI lane model=$TEI_MODEL_ID port=$TEI_HOST_PORT project=$PROJECT via containers submodule orchestrator"
"$BIN" boot-up "$COMPOSE" "$PROJECT" 2>&1 | tee "$EVID/20_boot.txt"
if ! poll_health 2>&1 | tee "$EVID/21_health.txt"; then
  log "TEI lane did not become healthy; capturing logs + tearing down"
  podman logs "$CURRENT_CONTAINER" 2>&1 | tail -60 | tee "$EVID/22_teilogs.txt" || true
  teardown_project "$EVID/28_teardown_failed.txt"
  echo "BLOCKED: TEI lane never became healthy — see 22_teilogs.txt" | tee "$EVID/90_blocked.txt"
  exit 4
fi

podman ps --format '{{.Names}} {{.Image}} {{.Status}} {{.Ports}}' | tee "$EVID/24_container_state.txt"

# ---------- EMBED CORPUS (real TEI vectors) ----------
"$BIN" embed-corpus "$TEI_BASE" "$EVID/corpus_embeddings.json" 2>&1 | tee "$EVID/30_embed_corpus.txt"

# ---------- PER-QUERY: embed -> retrieve -> checkretrieval -> green -> analyze ----------
for qk in q1 q2; do
  {
    echo "### qkey=$qk pipeline"
    "$BIN" embed-query "$TEI_BASE" "$qk" "$EVID/query_embedding_${qk}.json"
    "$BIN" retrieve "$EVID/corpus_embeddings.json" "$EVID/query_embedding_${qk}.json" "$qk" "$EVID/retrieval_${qk}.json"
    "$BIN" checkretrieval "$EVID/retrieval_${qk}.json" "$qk"; rc_retr=$?
    echo "checkretrieval_exit_${qk}=$rc_retr"
    "$BIN" green "$CODER_BASE" "$EVID/retrieval_${qk}.json" "$qk" "$EVID/green_response_${qk}.json"; rc_green=$?
    echo "green_exit_${qk}=$rc_green"
    "$BIN" analyze "$EVID/retrieval_${qk}.json" "$EVID/green_response_${qk}.json" "$qk"; rc_an=$?
    echo "analyze_exit_${qk}=$rc_an"
    if [ "$rc_retr" -ne 0 ] || [ "$rc_green" -ne 0 ] || [ "$rc_an" -ne 0 ]; then
      echo "qkey=${qk}: GREEN-FAIL"
      echo "$qk" >> "$STATUS_DIR/overall_fail"
    else
      echo "qkey=${qk}: GREEN-OK (RAG runtime signature PASS)"
    fi
  } 2>&1 | tee "$EVID/11_green_proof_${qk}.txt"
done

# ---------- SELF-VALIDATION (§11.4.107(10)) — both queries ----------
for qk in q1 q2; do
  {
    echo "### analyzer self-validation (§11.4.107(10)) qkey=$qk"
    "$BIN" selfvalidate "$EVID/retrieval_${qk}.json" "$EVID/green_response_${qk}.json" "$qk"; sv=$?
    echo "selfvalidate_exit_${qk}=$sv"
    if [ "$sv" -ne 0 ]; then
      echo "${qk}-selfvalidate" >> "$STATUS_DIR/overall_fail"
    fi
  } 2>&1 | tee "$EVID/12_self_validation_${qk}.txt"
done

# ---------- TEARDOWN + coder-untouched proof (§11.4.119) ----------
teardown_project "$EVID/29_teardown.txt"
{
  echo "### post-teardown state"
  echo "tei-rag containers (expect none):"
  podman ps -a --format '{{.Names}}' | grep "${PROJECT}_" || echo "  (none — removed)"
  echo "coder still running (untouched):"
  podman ps --filter name=helixllm-coder --format '{{.Names}} {{.Status}}'
  echo "sibling ports 18435-18439 unaffected:"
  for p in 18435 18436 18437 18438 18439; do
    ss -ltn 2>/dev/null | grep ":${p} " >/dev/null 2>&1 && echo "  :$p still LISTENING" || echo "  :$p free"
  done
} | tee "$EVID/29b_post_teardown.txt"

RED_RC=0
[ -f "$STATUS_DIR/red_fail" ] && RED_RC=1
OVERALL_RC=0
[ -f "$STATUS_DIR/overall_fail" ] && OVERALL_RC=1

if [ "$RED_RC" -ne 0 ]; then
  log "RED-VIOLATION detected for: $(cat "$STATUS_DIR/red_fail" | tr '\n' ' ') — inspect 10_red_baseline.txt"
  OVERALL_RC=1
fi

if [ "$OVERALL_RC" -eq 0 ]; then
  log "DONE. ALL GREEN. Evidence in $EVID"
else
  log "DONE WITH FAILURES. Evidence in $EVID — inspect per-qkey logs above."
fi
exit "$OVERALL_RC"
