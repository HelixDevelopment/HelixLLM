#!/usr/bin/env bash
# RAG-Qdrant + cross-encoder-reranker fusion — end-to-end LIVE proof.
# (§11.4.108 runtime signature / §11.4.6 / §11.4.107(10) / §11.4.115 /
#  §11.4.169 comprehensive test-type coverage / §11.4.119 single-owner)
#
# UPGRADES the already-proven RAG core (in-memory embeddings + cosine
# retrieval, docs/qa/phase3_rag_20260707/) with a REAL Qdrant vector DB
# (real HTTP upsert + real ANN cosine search) + a REAL cross-encoder
# reranker (TEI /rerank, BAAI/bge-reranker-base). Everything CPU-only —
# the GPU stays owned by the concurrent video-analysis stream (§11.4.119).
# The live coder (:18434) is READ-ONLY, never restarted (§11.4.122).
#
# All model/port/limit values are config-injected here (§CONST-045/046) —
# the compose file carries no literal.
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"   # .../helix_llm/docs/qa/phase3_rag_qdrant_rerank_<ts>/harness
cd "$HERE"

RUN_ID="rag_qdrant_liveproof_20260711T142237Z"
# HERE = <repo-root>/submodules/helix_llm/docs/qa/<this-run>/harness — 6 levels
# up reaches <repo-root> (verified: submodules/helix_llm/docs/qa/<run>/harness).
REPO_ROOT="$(cd "$HERE/../../../../../.." && pwd)"
EVID="$REPO_ROOT/docs/qa/${RUN_ID}"
mkdir -p "$EVID"

STATUS_DIR="$(mktemp -d)"
trap 'rm -rf "$STATUS_DIR"' EXIT

# ---- config injection (no hardcoded host/port/model literal downstream) ----
export QDRANT_HTTP_PORT="${QDRANT_HTTP_PORT:-18460}"
export QDRANT_GRPC_PORT="${QDRANT_GRPC_PORT:-18461}"
export QDRANT_MEM_LIMIT="${QDRANT_MEM_LIMIT:-2g}"
export QDRANT_CPUS="${QDRANT_CPUS:-2}"

export TEI_EMBED_HOST_PORT="${TEI_EMBED_HOST_PORT:-18462}"
export TEI_EMBED_MEM_LIMIT="${TEI_EMBED_MEM_LIMIT:-4g}"
export TEI_EMBED_CPUS="${TEI_EMBED_CPUS:-4}"
export TEI_EMBED_MODEL_ID="${TEI_EMBED_MODEL_ID:-BAAI/bge-small-en-v1.5}"

export TEI_RERANK_HOST_PORT="${TEI_RERANK_HOST_PORT:-18463}"
export TEI_RERANK_MEM_LIMIT="${TEI_RERANK_MEM_LIMIT:-4g}"
export TEI_RERANK_CPUS="${TEI_RERANK_CPUS:-4}"
export TEI_RERANK_MODEL_ID="${TEI_RERANK_MODEL_ID:-BAAI/bge-reranker-base}"

PROJECT="phase3ragqdrant"
COMPOSE="compose.qdrant_rerank.yml"
QDRANT_BASE="http://localhost:${QDRANT_HTTP_PORT}"
TEI_EMBED_BASE="http://localhost:${TEI_EMBED_HOST_PORT}"
TEI_RERANK_BASE="http://localhost:${TEI_RERANK_HOST_PORT}"
CODER_BASE="${CODER_BASE:-http://localhost:18434}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-360}"
COLLECTION="helixrag_qr_$(date -u +%Y%m%dT%H%M%SZ)"
# Retrieve the WHOLE fixture corpus (12 docs) so recall is guaranteed and the
# reranker-improvement test isolates ORDERING (not recall). The adversarial
# q3/q4 probes need the fact doc present in the candidate set for the
# cross-encoder to be able to promote it.
TOPN=12
QUERIES="q1 q2 q3 q4"

BIN="$HERE/phase3ragqdrant.bin"

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

poll_health() {
  local deadline=$(( $(date +%s) + HEALTH_TIMEOUT ))
  local n=0
  while [ "$(date +%s)" -lt "$deadline" ]; do
    n=$((n+1))
    q=$(curl -s -o /dev/null -w '%{http_code}' "$QDRANT_BASE/collections" 2>/dev/null || echo 000)
    e=$(curl -s -o /dev/null -w '%{http_code}' "$TEI_EMBED_BASE/health" 2>/dev/null || echo 000)
    r=$(curl -s -o /dev/null -w '%{http_code}' "$TEI_RERANK_BASE/health" 2>/dev/null || echo 000)
    if [ "$q" = "200" ] && [ "$e" = "200" ] && [ "$r" = "200" ]; then
      log "health OK after $n polls (qdrant=$q tei-embed=$e tei-rerank=$r)"
      return 0
    fi
    sleep 3
  done
  log "health poll TIMED OUT after ${HEALTH_TIMEOUT}s (last: qdrant=$q tei-embed=$e tei-rerank=$r)"
  return 1
}

# ---------- BUILD + PRE-CLEAN (clean-target integrity §11.4.108/§11.4.139) ----------
build_harness
"$BIN" boot-down "$COMPOSE" "$PROJECT" >/dev/null 2>&1 || true
podman volume create helixllm-tei-cache >/dev/null 2>&1 || true
podman volume create helixllm-tei-rerank-cache >/dev/null 2>&1 || true

# ---------- PRE-FLIGHT (§11.4.119 single-owner, coder + GPU untouched) ----------
{
  echo "### pre-flight"
  echo "date_utc=$(date -u +%FT%TZ)"
  echo "uname_m=$(uname -m)  podman=$(podman --version)"
  echo "coder container (MUST remain running, untouched):"
  podman ps --filter name=helixllm-coder --format '{{.Names}} {{.Image}} {{.Status}}'
  curl -s -o /dev/null -w 'coder /v1/models http_code=%{http_code}\n' "$CODER_BASE/v1/models" || true
  echo "GPU state (MUST be untouched by this CPU-only lane):"
  nvidia-smi --query-gpu=memory.used,memory.total --format=csv,noheader 2>/dev/null || echo "nvidia-smi unavailable"
  echo "target host ports ${QDRANT_HTTP_PORT},${QDRANT_GRPC_PORT},${TEI_EMBED_HOST_PORT},${TEI_RERANK_HOST_PORT} (expect all free):"
  for p in "$QDRANT_HTTP_PORT" "$QDRANT_GRPC_PORT" "$TEI_EMBED_HOST_PORT" "$TEI_RERANK_HOST_PORT"; do
    ss -ltn 2>/dev/null | grep ":${p} " >/dev/null && echo "  :$p BUSY" || echo "  :$p free"
  done
  echo "sibling lanes 18435-18443 (MUST remain untouched by this run):"
  for p in 18435 18436 18437 18438 18439 18440 18441 18442 18443; do
    ss -ltn 2>/dev/null | grep ":${p} " >/dev/null 2>&1 && echo "  :$p LISTENING (untouched by this harness)" || echo "  :$p free"
  done
} | tee "$EVID/00_preflight.txt"

# ---------- §11.4.150 research note ----------
{
  echo "### deep-research note (§11.4.150) — Qdrant hybrid RAG + cross-encoder rerank pattern"
  echo "Ref design: docs/research/07.2026/04_embeddings_rag/04_embeddings_rag.md"
  echo "  - Vector DB: Qdrant (REST /collections + /points/search, real ANN cosine)"
  echo "  - Reranker: bge-reranker-v2-m3-class cross-encoder via HF TEI /rerank endpoint"
  echo "  - Pattern: embed -> Qdrant ANN top-N -> cross-encoder rerank -> top-k -> grounded generation"
  echo "TEI /rerank confirmed via https://github.com/huggingface/text-embeddings-inference README"
  echo "  (curl 127.0.0.1:8080/rerank -d '{\"query\":...,\"texts\":[...]}, sequence-classification reranker models)."
} | tee "$EVID/01_research_note.txt"

# ---------- RED-FIRST BASELINE (§11.4.115) — coder-only, no context, no Qdrant/rerank booted yet ----------
{
  echo "### RED baseline (§11.4.115): coder asked the SAME questions with NO retrieved context"
  echo "RED_MODE=1 — expect the coder to NOT know any FRESH invented fact (defect reproduced)"
  for qk in $QUERIES; do
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

# ---------- BOOT Qdrant + tei-embed + tei-rerank ----------
log "boot qdrant+tei-embed(${TEI_EMBED_MODEL_ID})+tei-rerank(${TEI_RERANK_MODEL_ID}) project=$PROJECT via containers submodule orchestrator"
"$BIN" boot-up "$COMPOSE" "$PROJECT" 2>&1 | tee "$EVID/20_boot.txt"
if ! poll_health 2>&1 | tee "$EVID/21_health.txt"; then
  log "one or more services did not become healthy; capturing logs + tearing down"
  for c in "${PROJECT}_qdrant_1" "${PROJECT}_tei-embed_1" "${PROJECT}_tei-rerank_1"; do
    echo "### $c" >> "$EVID/22_logs.txt"
    podman logs "$c" 2>&1 | tail -60 >> "$EVID/22_logs.txt" || true
  done
  teardown_project "$EVID/28_teardown_failed.txt"
  echo "BLOCKED: qdrant/tei-embed/tei-rerank did not become healthy — see 22_logs.txt" | tee "$EVID/90_blocked.txt"
  exit 4
fi

podman ps --format '{{.Names}} {{.Image}} {{.Status}} {{.Ports}}' | grep "$PROJECT" | tee "$EVID/24_container_state.txt"

# ---------- EMBED CORPUS (real TEI vectors) + REAL QDRANT UPSERT ----------
"$BIN" embed-corpus "$TEI_EMBED_BASE" "$EVID/corpus_embeddings.json" 2>&1 | tee "$EVID/30_embed_corpus.txt"
"$BIN" qdrant-upsert "$QDRANT_BASE" "$COLLECTION" "$EVID/corpus_embeddings.json" "$EVID/qdrant_upsert.json" 2>&1 | tee "$EVID/31_qdrant_upsert.txt"

# ---------- PER-QUERY: embed -> Qdrant ANN search -> cross-encoder rerank -> checkrerankimproves -> green -> analyze ----------
for qk in $QUERIES; do
  {
    echo "### qkey=$qk pipeline (collection=$COLLECTION topN=$TOPN)"
    "$BIN" embed-query "$TEI_EMBED_BASE" "$qk" "$EVID/query_embedding_${qk}.json"
    "$BIN" qdrant-search "$QDRANT_BASE" "$COLLECTION" "$EVID/query_embedding_${qk}.json" "$qk" "$TOPN" "$EVID/ann_${qk}.json"
    "$BIN" rerank "$TEI_RERANK_BASE" "$EVID/ann_${qk}.json" "$qk" "$EVID/reranked_${qk}.json"
    "$BIN" checkrerankimproves "$EVID/reranked_${qk}.json" "$qk"; rc_improves=$?
    echo "checkrerankimproves_exit_${qk}=$rc_improves"
    "$BIN" green "$CODER_BASE" "$EVID/reranked_${qk}.json" "$qk" "$EVID/green_response_${qk}.json"; rc_green=$?
    echo "green_exit_${qk}=$rc_green"
    "$BIN" analyze "$EVID/ann_${qk}.json" "$EVID/reranked_${qk}.json" "$EVID/green_response_${qk}.json" "$qk"; rc_an=$?
    echo "analyze_exit_${qk}=$rc_an"
    if [ "$rc_green" -ne 0 ] || [ "$rc_an" -ne 0 ]; then
      echo "qkey=${qk}: GREEN-FAIL"
      echo "$qk" >> "$STATUS_DIR/overall_fail"
    else
      echo "qkey=${qk}: GREEN-OK (RAG+Qdrant+rerank runtime signature PASS)"
    fi
    if [ "$rc_improves" -eq 0 ]; then
      echo "qkey=${qk}: RERANK-IMPROVES-ORDERING DEMONSTRATED (real ANN top-1 was a distractor; cross-encoder promoted the fact doc to top-1)"
      echo "$qk" >> "$STATUS_DIR/rerank_improved"
    else
      echo "qkey=${qk}: rerank-did-not-demonstrably-improve-ordering (real ANN already ranked the fact doc top-1; see 11_pipeline_${qk}.txt) — honest, not a pipeline failure"
      echo "$qk" >> "$STATUS_DIR/rerank_no_improve"
    fi
  } 2>&1 | tee "$EVID/11_pipeline_${qk}.txt"
done

# ---------- SELF-VALIDATION (§11.4.107(10)) — all queries ----------
for qk in $QUERIES; do
  {
    echo "### analyzer self-validation (§11.4.107(10)) qkey=$qk"
    "$BIN" selfvalidate "$EVID/ann_${qk}.json" "$EVID/reranked_${qk}.json" "$EVID/green_response_${qk}.json" "$qk"; sv=$?
    echo "selfvalidate_exit_${qk}=$sv"
    if [ "$sv" -ne 0 ]; then
      echo "${qk}-selfvalidate" >> "$STATUS_DIR/overall_fail"
    fi
  } 2>&1 | tee "$EVID/12_self_validation_${qk}.txt"
done

# ---------- TEARDOWN + coder/GPU-untouched proof (§11.4.119) ----------
teardown_project "$EVID/29_teardown.txt"
{
  echo "### post-teardown state"
  echo "qdrant/tei-embed/tei-rerank containers (expect none):"
  podman ps -a --format '{{.Names}}' | grep "${PROJECT}_" || echo "  (none — removed)"
  echo "coder still running (untouched):"
  podman ps --filter name=helixllm-coder --format '{{.Names}} {{.Status}}'
  echo "GPU state (unchanged by this CPU-only lane):"
  nvidia-smi --query-gpu=memory.used,memory.total --format=csv,noheader 2>/dev/null || echo "nvidia-smi unavailable"
  echo "sibling ports 18435-18443 unaffected:"
  for p in 18435 18436 18437 18438 18439 18440 18441 18442 18443; do
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

{
  echo "### rerank-improves-ordering tally (the reranker's causal contribution)"
  if [ -f "$STATUS_DIR/rerank_improved" ]; then
    echo "DEMONSTRATED for: $(cat "$STATUS_DIR/rerank_improved" | tr '\n' ' ')"
    echo "  (real Qdrant ANN top-1 was a distractor; real cross-encoder rerank promoted the fact doc to top-1)"
  else
    echo "DEMONSTRATED for: (none) — on this corpus the bge-small bi-encoder already ranked every fact doc top-1"
  fi
  if [ -f "$STATUS_DIR/rerank_no_improve" ]; then
    echo "no-top-1-correction-needed for: $(cat "$STATUS_DIR/rerank_no_improve" | tr '\n' ' ') (ANN already correct — honest)"
  fi
} | tee "$EVID/40_rerank_improves_tally.txt"

RERANK_IMPROVED_RC=0
if [ ! -f "$STATUS_DIR/rerank_improved" ]; then
  RERANK_IMPROVED_RC=1
  log "RERANK-IMPROVES-ORDERING was NOT demonstrated by any query this run — the concrete correction case did not reproduce (inspect 11_pipeline_*.txt real scores)"
fi

if [ "$OVERALL_RC" -eq 0 ] && [ "$RERANK_IMPROVED_RC" -eq 0 ]; then
  log "DONE. ALL GREEN + rerank-improves-ordering DEMONSTRATED. Evidence in $EVID"
elif [ "$OVERALL_RC" -eq 0 ]; then
  log "DONE. Core pipeline ALL GREEN but rerank-improves-ordering NOT demonstrated. Evidence in $EVID"
else
  log "DONE WITH FAILURES. Evidence in $EVID — inspect per-qkey logs above."
fi
exit "$OVERALL_RC"
