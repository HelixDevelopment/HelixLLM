#!/usr/bin/env bash
# Phase-1 HelixMemory bring-up END-TO-END proof
# (§11.4.108 runtime signature / §11.4.6 / §11.4.107(10) / §11.4.115).
#
# Composes THREE local building blocks:
#   - Postgres + pgvector: booted via the containers submodule orchestrator
#     (§11.4.76), rootless podman (§11.4.161), on its OWN port 18450.
#   - CPU embeddings: HF Text Embeddings Inference (TEI), BAAI/bge-small-en-v1.5,
#     dim 384, on its OWN port 18451 (reuses the already-populated
#     helixllm-tei-cache volume — no re-download, §11.4.82).
#   - the live coder LLM (helixllm-coder, already running at :18434) — READ
#     ONLY, never restarted/stopped (§11.4.122 / §11.4.119).
#
# Unfakeable proof: two "remember this" facts invented for this proof (plus
# two distractors). RED baseline asks the coder the bare recall question (no
# memory stored/retrieved) -> must NOT know the answer. GREEN stores all four
# facts (real TEI embeddings -> real Postgres/pgvector persistence), then per
# query embeds -> real pgvector cosine-distance top-1 recall -> grounds a
# prompt with ONLY the recalled memory -> generates on the SAME live coder ->
# the answer MUST contain the invented token. A self-validation pass proves
# the analyzer genuinely discriminates (golden-good PASS; 3 golden-bad
# variants MUST FAIL).
#
# HONEST SCOPE NOTE: this proof exercises a minimal reference implementation
# of the mem0-style memory mechanism using Postgres+pgvector — it does NOT
# install/invoke the upstream mem0 Python package or the Graphiti library.
# See HELIXMEMORY_PROVIDER.md.
#
# Reproducible: re-run against a clean host to regenerate every evidence file.
# All model/port/limit/credential values are config-injected here
# (§CONST-045/046) — the compose file carries no literal; the Postgres
# password is freshly generated per run, never hardcoded (§11.4.10).
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"              # .../helix_llm/docs/qa/phase1_helixmemory_<ts>/harness
EVID="$(cd "$HERE/../../../../../../docs/qa/phase1_helixmemory_20260708T061824Z" && pwd)"  # outer repo root evidence dir
cd "$HERE"

STATUS_DIR="$(mktemp -d)"
trap 'rm -rf "$STATUS_DIR"' EXIT

# ---- config injection (no hardcoded host/port/model/credential literal
# downstream) ----
export PGHM_HOST_PORT="${PGHM_HOST_PORT:-18450}"   # OWN port — coder=18434, Phase-3 lanes=18435-18441, vision=18439
export TEI_HOST_PORT="${TEI_HOST_PORT:-18451}"     # OWN port, distinct from all of the above
export PGHM_USER="${PGHM_USER:-helixmemory}"
export PGHM_DB="${PGHM_DB:-helixmemory}"
export PGHM_PASSWORD="${PGHM_PASSWORD:-$(openssl rand -hex 16)}"
export PGHM_MEM_LIMIT="${PGHM_MEM_LIMIT:-2g}"
export PGHM_CPUS="${PGHM_CPUS:-2}"
export TEI_MEM_LIMIT="${TEI_MEM_LIMIT:-8g}"
export TEI_CPUS="${TEI_CPUS:-8}"
export TEI_MODEL_ID="${TEI_MODEL_ID:-BAAI/bge-small-en-v1.5}"  # proven CPU lane (embeddings proof, 2026-07-06)
PROJECT="phase1helixmemory"
COMPOSE="compose.phase1helixmemory.yml"
PG_CONNINFO="postgresql://${PGHM_USER}:${PGHM_PASSWORD}@localhost:${PGHM_HOST_PORT}/${PGHM_DB}?sslmode=disable"
TEI_BASE="http://localhost:${TEI_HOST_PORT}"
CODER_BASE="${CODER_BASE:-http://localhost:18434}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-300}"

BIN="$HERE/phase1helixmemory.bin"

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

PG_CONTAINER="${PROJECT}_pg-helixmemory_1"
TEI_CONTAINER="${PROJECT}_tei-helixmemory_1"

poll_health() {
  local deadline=$(( $(date +%s) + HEALTH_TIMEOUT ))
  local n=0
  while [ "$(date +%s)" -lt "$deadline" ]; do
    n=$((n+1))
    tei_code=$(curl -s -o /dev/null -w '%{http_code}' "$TEI_BASE/health" 2>/dev/null || echo 000)
    pg_ready=0
    pg_isready -h localhost -p "$PGHM_HOST_PORT" -U "$PGHM_USER" -d "$PGHM_DB" >/dev/null 2>&1 && pg_ready=1
    if [ "$tei_code" = "200" ] && [ "$pg_ready" = "1" ]; then
      log "health OK after $n polls (tei=200, pg_isready=1)"
      return 0
    fi
    tei_st=$(podman inspect "$TEI_CONTAINER" --format '{{.State.Status}}' 2>/dev/null || echo starting)
    pg_st=$(podman inspect "$PG_CONTAINER" --format '{{.State.Status}}' 2>/dev/null || echo starting)
    if [ "$tei_st" = "exited" ] || [ "$pg_st" = "exited" ]; then
      log "a container state=exited before healthy — abort poll (n=$n, tei=$tei_st, pg=$pg_st)"
      return 1
    fi
    sleep 3
  done
  log "health poll TIMED OUT after ${HEALTH_TIMEOUT}s (tei=$tei_code pg_ready=$pg_ready)"
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
  echo "uname_m=$(uname -m)  podman=$(podman --version)  psql=$(psql --version)"
  echo "coder container (MUST remain running, untouched):"
  podman ps --filter name=helixllm-coder --format '{{.Names}} {{.Image}} {{.Status}}'
  echo "coder live model + health:"
  curl -s -o /dev/null -w 'coder /v1/models http_code=%{http_code}\n' "$CODER_BASE/v1/models" || true
  echo "target host ports ${PGHM_HOST_PORT} (postgres) / ${TEI_HOST_PORT} (tei) listeners (expect none):"
  ss -ltn 2>/dev/null | grep -E ":${PGHM_HOST_PORT} |:${TEI_HOST_PORT} " || echo "  (both free)"
  echo "other host containers observed (read-only, never touched — §11.4.174):"
  podman ps --format '{{.Names}} {{.Image}} {{.Status}}'
} | tee "$EVID/00_preflight.txt"

# ---------- §11.4.150 research note ----------
{
  echo "### deep-research note (§11.4.150) — HelixMemory local stack, access-date 2026-07-08"
  echo "Zep Community Edition is DEPRECATED (April 2025, Apache-2.0 code frozen, no updates) —"
  echo "https://blog.getzep.com/announcing-a-new-direction-for-zeps-open-source-strategy/ ."
  echo "Self-hostable durable spine today = the raw Graphiti library/MCP-server (Apache-2.0,"
  echo "https://github.com/getzep/graphiti), backed by Neo4j 5.26 / FalkorDB 1.1.2 (SSPLv1,"
  echo "https://docs.falkordb.com/license.html) / Amazon Neptune / Kuzu 0.11.2 (embedded, MIT)."
  echo "Graphiti supports OpenAI-compatible local endpoints (Ollama/vLLM/llama.cpp/LM Studio) via"
  echo "OpenAIGenericClient with a custom base_url — the live coder (:18434) qualifies."
  echo "mem0 (Apache-2.0, https://github.com/mem0ai/mem0) self-hosted OSS server = FastAPI +"
  echo "Postgres/pgvector (ankane/pgvector image) + optional Neo4j; 'swap both [LLM+embed] for"
  echo "Ollama models to go fully offline' (https://mem0.ai/blog/self-host-mem0-docker)."
  echo "This harness implements the mem0-style embed/persist/recall/ground mechanism directly"
  echo "against Postgres+pgvector (pgvector/pgvector:pg16) — see HELIXMEMORY_PROVIDER.md for the"
  echo "full citation set + the honest scope note (no upstream mem0/Graphiti package installed"
  echo "this session)."
} | tee "$EVID/01_research_note.txt"

# ---------- RED-FIRST BASELINE (§11.4.115) — BOTH invented facts, live coder, no memory ----------
{
  echo "### RED baseline (§11.4.115): coder asked the SAME recall questions with NOTHING stored/retrieved"
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

# ---------- BOOT Postgres+pgvector + TEI (reused bge-small lane + cache, §11.4.82) ----------
log "boot pg-helixmemory(port $PGHM_HOST_PORT) + tei-helixmemory(model=$TEI_MODEL_ID port=$TEI_HOST_PORT) project=$PROJECT via containers submodule orchestrator"
"$BIN" boot-up "$COMPOSE" "$PROJECT" 2>&1 | tee "$EVID/20_boot.txt"
if ! poll_health 2>&1 | tee "$EVID/21_health.txt"; then
  log "a lane did not become healthy; capturing logs + tearing down"
  { podman logs "$PG_CONTAINER" 2>&1 | tail -60; echo "--- tei ---"; podman logs "$TEI_CONTAINER" 2>&1 | tail -60; } | tee "$EVID/22_logs.txt" || true
  teardown_project "$EVID/28_teardown_failed.txt"
  echo "BLOCKED: a lane never became healthy — see 22_logs.txt" | tee "$EVID/90_blocked.txt"
  exit 4
fi

podman ps --format '{{.Names}} {{.Image}} {{.Status}} {{.Ports}}' | tee "$EVID/24_container_state.txt"

# ---------- DB INIT (pgvector extension + memory_facts table) ----------
"$BIN" db-init "$PG_CONNINFO" 2>&1 | tee "$EVID/25_db_init.txt"

# ---------- REMEMBER (embed + persist the whole fixture fact set — real TEI + real Postgres) ----------
"$BIN" remember-all "$TEI_BASE" "$PG_CONNINFO" "$EVID/remembered_facts.json" 2>&1 | tee "$EVID/30_remember_all.txt"

# ---------- PER-QUERY: embed -> recall -> checkretrieval -> green -> analyze ----------
for qk in q1 q2; do
  {
    echo "### qkey=$qk pipeline"
    "$BIN" embed-query "$TEI_BASE" "$qk" "$EVID/query_embedding_${qk}.json"
    "$BIN" recall "$PG_CONNINFO" "$EVID/query_embedding_${qk}.json" "$qk" "$EVID/retrieval_${qk}.json"
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
      echo "qkey=${qk}: GREEN-OK (memory runtime signature PASS)"
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
  echo "pg-helixmemory + tei-helixmemory containers (expect none):"
  podman ps -a --format '{{.Names}}' | grep "${PROJECT}_" || echo "  (none — removed)"
  echo "coder still running (untouched):"
  podman ps --filter name=helixllm-coder --format '{{.Names}} {{.Status}}'
  echo "ports ${PGHM_HOST_PORT}/${TEI_HOST_PORT} freed:"
  ss -ltn 2>/dev/null | grep -E ":${PGHM_HOST_PORT} |:${TEI_HOST_PORT} " && echo "  still listening!" || echo "  both free"
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
