# CLAUDE.md

## Universal Mandatory Constraints

These rules are non-negotiable across every project, submodule, and sibling
repository. They are derived from the HelixAgent root `CLAUDE.md`. Each
project MUST surface them in its own `CLAUDE.md`, `AGENTS.md`, and
`CONSTITUTION.md`. Project-specific addenda are welcome but cannot weaken
or override these.

### Hard Stops (permanent, non-negotiable)

1. **NO CI/CD pipelines.** No `.github/workflows/`, `.gitlab-ci.yml`,
   `Jenkinsfile`, `.travis.yml`, `.circleci/`, or any automated pipeline.
   No Git hooks either. All builds and tests run manually or via Makefile/
   script targets.
2. **NO HTTPS for Git.** SSH URLs only (`git@github.com:…`,
   `git@gitlab.com:…`, etc.) for clones, fetches, pushes, and submodule
   updates. Including for public repos. SSH keys are configured on every
   service.
3. **NO manual container commands.** Container orchestration is owned by
   the project's binary/orchestrator (e.g. `make build` → `./bin/<app>`).
   Direct `docker`/`podman start|stop|rm` and `docker-compose up|down`
   are prohibited as workflows. The orchestrator reads its configured
   `.env` and brings up everything.

### Mandatory Development Standards

1. **100% Test Coverage.** Every component MUST have unit, integration,
   E2E, automation, security/penetration, and benchmark tests. No false
   positives. Mocks/stubs ONLY in unit tests; all other test types use
   real data and live services.
2. **Challenge Coverage.** Every component MUST have Challenge scripts
   (`./challenges/scripts/`) validating real-life use cases. No false
   success — validate actual behavior, not return codes.
3. **Real Data.** Beyond unit tests, all components MUST use actual API
   calls, real databases, live services. No simulated success. Fallback
   chains tested with actual failures.
4. **Health & Observability.** Every service MUST expose health
   endpoints. Circuit breakers for all external dependencies. Prometheus
   / OpenTelemetry integration where applicable.
5. **Documentation & Quality.** Update `CLAUDE.md`, `AGENTS.md`, and
   relevant docs alongside code changes. Pass language-appropriate
   format/lint/security gates. Conventional Commits:
   `<type>(<scope>): <description>`.
6. **Validation Before Release.** Pass the project's full validation
   suite (`make ci-validate-all`-equivalent) plus all challenges
   (`./challenges/scripts/run_all_challenges.sh`).
7. **No Mocks or Stubs in Production.** Mocks, stubs, fakes, placeholder
   classes, TODO implementations are STRICTLY FORBIDDEN in production
   code. All production code is fully functional with real integrations.
   Only unit tests may use mocks/stubs.
8. **Comprehensive Verification.** Every fix MUST be verified from all
   angles: runtime testing (actual HTTP requests / real CLI invocations),
   compile verification, code structure checks, dependency existence
   checks, backward compatibility, and no false positives in tests or
   challenges. Grep-only validation is NEVER sufficient.
9. **Resource Limits for Tests & Challenges (CRITICAL).** ALL test and
   challenge execution MUST be strictly limited to 30-40% of host system
   resources. Use `GOMAXPROCS=2`, `nice -n 19`, `ionice -c 3`, `-p 1`
   for `go test`. Container limits required. The host runs
   mission-critical processes — exceeding limits causes system crashes.
10. **Bugfix Documentation.** All bug fixes MUST be documented in
    `docs/issues/fixed/BUGFIXES.md` (or the project's equivalent) with
    root cause analysis, affected files, fix description, and a link to
    the verification test/challenge.
11. **Real Infrastructure for All Non-Unit Tests.** Mocks/fakes/stubs/
    placeholders MAY be used ONLY in unit tests (files ending `_test.go`
    run under `go test -short`, equivalent for other languages). ALL
    other test types — integration, E2E, functional, security, stress,
    chaos, challenge, benchmark, runtime verification — MUST execute
    against the REAL running system with REAL containers, REAL
    databases, REAL services, and REAL HTTP calls. Non-unit tests that
    cannot connect to real services MUST skip (not fail).
12. **Reproduction-Before-Fix (CONST-032 — MANDATORY).** Every reported
    error, defect, or unexpected behavior MUST be reproduced by a
    Challenge script BEFORE any fix is attempted. Sequence:
    (1) Write the Challenge first. (2) Run it; confirm fail (it
    reproduces the bug). (3) Then write the fix. (4) Re-run; confirm
    pass. (5) Commit Challenge + fix together. The Challenge becomes
    the regression guard for that bug forever.
13. **Concurrent-Safe Containers (Go-specific, where applicable).** Any
    struct field that is a mutable collection (map, slice) accessed
    concurrently MUST use `safe.Store[K,V]` / `safe.Slice[T]` from
    `digital.vasic.concurrency/pkg/safe` (or the project's equivalent
    primitives). Bare `sync.Mutex + map/slice` combinations are
    prohibited for new code.

### Definition of Done (universal)

A change is NOT done because code compiles and tests pass. "Done"
requires pasted terminal output from a real run, produced in the same
session as the change.

- **No self-certification.** Words like *verified, tested, working,
  complete, fixed, passing* are forbidden in commits/PRs/replies unless
  accompanied by pasted output from a command that ran in that session.
- **Demo before code.** Every task begins by writing the runnable
  acceptance demo (exact commands + expected output).
- **Real system, every time.** Demos run against real artifacts.
- **Skips are loud.** `t.Skip` / `@Ignore` / `xit` / `describe.skip`
  without a trailing `SKIP-OK: #<ticket>` comment break validation.
- **Evidence in the PR.** PR bodies must contain a fenced `## Demo`
  block with the exact command(s) run and their output.

## Definition of Done

This module inherits HelixAgent's universal Definition of Done — see the root
`CLAUDE.md` and `docs/development/definition-of-done.md`. In one line: **no
task is done without pasted output from a real run of the real system in the
same session as the change.** Coverage and green suites are not evidence.

### Acceptance demo for this module

```bash
# Build HelixLLM gateway binary. Full live round-trip (scored multi-provider
# fallback chain, real /v1/chat/completions) is the richer demo but needs
# either cloud-provider API keys or a local llama.cpp runtime, plus 30-60s
# for the fallback-chain warm-up — skip-on-missing-deps per DoD.
cd HelixLLM && make build
test -x bin/helixllm || { echo "FAIL: binary not built"; exit 1; }

# Optional live round-trip — skip when llama.cpp missing or no provider key set.
if command -v llama-server >/dev/null 2>&1 || [ -n "${HELIX_LLM_CHUTES_KEY:-${HELIX_LLM_OPENROUTER_KEY:-}}" ]; then
  GOMAXPROCS=2 nice -n 19 ./bin/helixllm --mode=full &
  HELIXLLM_PID=$!
  for i in $(seq 1 60); do curl -fsSk https://localhost:8443/v1/health >/dev/null 2>&1 && break; sleep 1; done
  HELIX_LLM_TLS_SKIP_VERIFY=true curl -fsSk https://localhost:8443/v1/chat/completions \
    -H 'Content-Type: application/json' \
    -d '{"model":"auto","messages":[{"role":"user","content":"say hi"}]}' \
    | jq -e '.choices[0].message.content | length > 0' || echo "WARN: round-trip did not succeed; build-only verification green"
  kill $HELIXLLM_PID 2>/dev/null
  wait $HELIXLLM_PID 2>/dev/null
else
  echo "SKIP: no llama-server and no HELIX_LLM_*_KEY set; build-only verification green"
fi
```
Expect: `jq -e` exits 0; the response contains model+provider telemetry showing which link of the ranked chain served the request. With no cloud keys set, llama.cpp answers; `HELIX_LLM_TLS_SKIP_VERIFY=true` bypasses the self-signed cert check only for this demo — production must trust `HelixLLM/certs/cert.pem` via `SSL_CERT_FILE` per root `CLAUDE.md`.


This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Development Commands

```bash
make build              # Compile binary to bin/helixllm (Go, -ldflags="-s -w")
make dev                # Generate TLS certs + run in full mode (HELIX_MODE=full)
make deps               # git submodule update --init --recursive && go mod tidy
make lint               # golangci-lint run ./...
make fmt                # gofmt -w . && goimports -w .
make gen                # go generate ./...
make certs              # Generate self-signed EC P-256 TLS cert in certs/
make container          # Build container image (auto-detects Podman/Docker)
```

## Testing

```bash
make test-unit          # go test -v -count=1 -coverprofile=coverage-unit.out ./internal/...
make test-integration   # go test -v -count=1 ./tests/integration/
make test-e2e           # go test -v -count=1 -tags=e2e ./tests/integration/...
make test-all           # unit + integration
make coverage           # Run unit tests and enforce 85% coverage threshold
```

Run a single test:
```bash
go test -v -run TestName ./internal/brain/...
```

Challenge banks (require a running server or `make build` first):
```bash
make test-challenges                    # All banks against localhost:8443
make test-challenges-api                # API-only banks
./bin/helixllm --challenges --banks-dir=challenges/banks/security/ --base-url=https://localhost:8443
./bin/helixllm --challenges --category=rag --priority=high
```

## Architecture

Single Go binary with a **mode system** -- the `HELIX_MODE` env (or `--mode` flag) selects which layers to activate: `full`, `gateway`, `brain`, `knowledge`, `agents`, or `control`. In `full` mode all layers run in-process with direct function calls. In distributed mode, separate binaries communicate via gRPC, SSE, and Kafka.

### Layer Stack

```
Gateway    → HTTP/3 + HTTP/2 server, OpenAI/Anthropic-compatible API endpoints, auth, streaming
             └─ dispatches via gateway.Completer (never calls Brain directly)
Fallback   → FallbackChain: ScorerBridge-ordered providers, RateLimitTracker, CircuitBreaker,
             MemoryAdapter; local llama.cpp always last
Brain      → LLM provider routing: llama.cpp (local), OpenAI, Anthropic, Chutes, OpenRouter,
             HuggingFace, Nvidia, Cerebras, SambaNova, Together
Knowledge  → RAG pipeline: chunking → embedding → vector store → retrieval
Agents     → ReAct loop, tool calling, conversation sessions, multi-agent coordination
Control    → SSH-based host probing, container deployment, scheduling strategies
Shared     → Config (env-based), EventBus, logging (logrus), observability (OTEL), health, analytics
```

### Key Interfaces

- **`brain.Provider`** (`internal/brain/provider.go`) -- All LLM backends implement `Complete`, `CompleteStream`, `Models`, `Name`, `Available`. Implementations: `llamacpp.go`, `openai_provider.go`, `anthropic_provider.go`, and all new cloud providers in `internal/brain/`.
- **`gateway.Completer`** (`internal/gateway/completer.go`) -- Abstracts both a single `brain.Provider` and the `fallback.Chain`. The Gateway always calls through this interface, which means swapping between a direct provider and the full fallback chain requires no changes to gateway code. `FallbackChain` implements `Completer`; individual providers are wrapped by `brain.ProviderCompleter`.
- **`agents.PersistentSyncer`** (`internal/agents/memory_syncer.go`) -- Memory sync interface used by `MemoryAdapter`. Implementations forward high-importance (>= 0.7) memories to the parent HelixAgent's HelixMemory service. Allows the agents layer to remain decoupled from the specific remote memory backend.
- **`agents.Tool`** (`internal/agents/tool.go`) -- Tools implement `Name`, `Description`, `Parameters`, `Execute`. Registered via `ToolRegistry`. Built-in tools in `internal/agents/tools/`.
- **`knowledge.VectorStore`** (`internal/knowledge/store.go`) -- `Upsert`, `Search`, `Delete`, `Collections`, `Stats`. Implementations: Qdrant (`qdrant.go`), in-memory (`MemoryStore`).

### Request Flow (Agent Chat)

1. HTTP request hits gateway → auth middleware → `agents.RegisterAgentRoutesWithExtras`
2. Load/append conversation session history
3. RAG hook augments prompt with knowledge context
4. Brain selects provider and sends to LLM
5. If response contains tool calls → execute tools → loop (max 10 turns)
6. Return final response (streaming or batch)

### Entry Point

`cmd/helixllm/main.go` -- parses flags, loads config, initializes all layers in sequence (logging → events → observability → analytics → control plane → brain → gateway → knowledge → agents), starts HTTP/3 server with graceful shutdown.

## Multi-Model Fleet

HelixLLM uses llama.cpp's native router mode to serve a fleet of lightweight models simultaneously from a single process.

**Default models (all Apache-2.0 / MIT):**

| Model | Tier | VRAM | TPS | Purpose |
|-------|------|------|-----|---------|
| Qwen2.5-Coder-1.5B Q4_K_M | fast | ~1GB | 180-250 | Primary: quick tool calls |
| Qwen2.5-Coder-3B Q4_K_M | balanced | ~2GB | 120-160 | Moderate complexity |
| Functionary-small-v3.2 Q4_0 | powerful | ~5GB | 45-65 | Complex reasoning (optional) |
| nomic-embed-text-v1.5 Q4_K_M | embed | ~90MB | — | Local embeddings (768 dims) |

**Task complexity routing:** Incoming requests are scored heuristically (<5ms). Simple tasks (score 0-2) route to the 1.5B fast model. Moderate tasks (3-5) route to the 3B balanced model. Complex tasks (6+) route to the powerful tier if available, otherwise fall back.

**Hardware auto-profiling:** At boot, GPU/CPU/RAM are detected. Models and llama.cpp settings (GPU layers, context size, batch size) are auto-configured based on available VRAM. Preset profiles: `cpu_only`, `consumer_6gb`, `consumer_8gb`, `high_end`.

**Auto-download:** Missing models are downloaded from HuggingFace on first boot. Set `HF_TOKEN` for gated repos.

**Key env vars:**
- `HELIX_MODELS_DIR` — GGUF file directory (default: `/models`)
- `HELIX_MODELS_AUTO_DOWNLOAD` — download missing at boot (default: `true`)
- `HELIX_MODELS_MAX` — max concurrent loaded models (default: `3`)
- `HELIX_COMPLEXITY_ENABLED` — enable multi-model routing (default: `true`)
- `HELIX_LLAMA_SERVER_PORT` — internal llama-server port (default: `8080`)
- `HELIX_LLAMA_SERVER_EMBEDDED` — spawn llama-server as child process (default: `true`)

**CUDA container:** `container/Containerfile.llamacpp-router` — multi-stage build with CUDA 12.6, RPC support, router mode.

## Multi-Provider Fallback Chain

The Gateway never calls a Brain provider directly. Instead, it dispatches every completion request through a `FallbackChain` that implements the same `gateway.Completer` interface, making the chain transparent to callers.

### Chain Overview

```
Gateway (Completer) → FallbackChain → [Provider 1, Provider 2, …, llamacpp]
                                           ↑
                                     ScorerBridge (LLMsVerifier scores)
```

- **ScorerBridge** (`internal/fallback/scorer_bridge.go`) — polls LLMsVerifier every 5 minutes and re-sorts the provider list by composite score (ResponseSpeed 25%, CostEffectiveness 25%, ModelEfficiency 20%, Capability 20%, Recency 10%). Local llama.cpp is pinned at the end of the chain and is never reordered — it is the guaranteed last resort.
- **Chain ordering** — cloud providers with the highest verification scores are tried first. Ties are broken by response speed. The order is refreshed in the background; in-flight requests use the order that was current at dispatch time.

### Rate Limit Handling

Two complementary mechanisms prevent repeated hammering of throttled providers:

1. **Reactive failover** — a `429 Too Many Requests` response from any provider immediately marks it as temporarily unavailable and moves to the next provider in the chain. The backoff window is `min(2^attempt × base_backoff, max_backoff)`.
2. **Proactive header parsing** — `RateLimitTracker` (`internal/fallback/rate_limit_tracker.go`) inspects `X-RateLimit-Remaining`, `X-RateLimit-Reset`, and provider-specific equivalents on every successful response. When remaining tokens/requests drop below a configurable threshold, the provider is deprioritized before a 429 ever arrives.

### Circuit Breaker

`CircuitBreaker` (`internal/fallback/circuit_breaker.go`) wraps each cloud provider independently:

| State | Trigger | Behavior |
|-------|---------|----------|
| **Closed** (normal) | — | All requests pass through |
| **Open** | 3 consecutive failures | All requests skip this provider; returns immediately |
| **Half-open** | 2 minutes after opening | One probe request allowed; success → Closed, failure → Open |

Failure categories that trip the breaker: connection errors, 5xx responses, and timeouts. Rate-limit 429s do **not** count toward the failure threshold — they are handled by `RateLimitTracker` instead.

### Memory Sync (MemoryAdapter)

`MemoryAdapter` (`internal/fallback/memory_adapter.go`) wraps the agents `MemoryManager`. After each successful completion, memories with `importance >= 0.7` are asynchronously forwarded to HelixMemory (the parent HelixAgent's memory service) via `agents.PersistentSyncer`. Lower-importance memories remain local to the session. This keeps long-term knowledge synchronized without incurring network overhead on every turn.

### Key Packages

| Package | Responsibility |
|---------|---------------|
| `internal/fallback/chain.go` | Ordered provider list, retry loop, error aggregation |
| `internal/fallback/scorer_bridge.go` | LLMsVerifier integration, background score refresh |
| `internal/fallback/rate_limit_tracker.go` | Proactive rate-limit header parsing and deprioritization |
| `internal/fallback/circuit_breaker.go` | Per-provider circuit breaker (closed/open/half-open) |
| `internal/fallback/memory_adapter.go` | High-importance memory sync to HelixMemory |

### Cloud Providers in the Chain

All providers live in `internal/brain/` and implement `brain.Provider`.

| Provider | Package | Key Env Var |
|----------|---------|-------------|
| Chutes | `chutes_provider.go` | `HELIX_LLM_CHUTES_KEY` |
| OpenRouter | `openrouter_provider.go` | `HELIX_LLM_OPENROUTER_KEY` |
| HuggingFace Inference | `huggingface_provider.go` | `HELIX_LLM_HUGGINGFACE_KEY` |
| Nvidia NIM | `nvidia_provider.go` | `HELIX_LLM_NVIDIA_KEY` |
| Cerebras | `cerebras_provider.go` | `HELIX_LLM_CEREBRAS_KEY` |
| SambaNova | `sambanova_provider.go` | `HELIX_LLM_SAMBANOVA_KEY` |
| Together AI | `together_provider.go` | `HELIX_LLM_TOGETHER_KEY` |
| OpenAI | `openai_provider.go` | `OPENAI_API_KEY` |
| Anthropic | `anthropic_provider.go` | `ANTHROPIC_API_KEY` |
| llama.cpp (local) | `llamacpp.go` | *(always available, pinned last)* |

Set `HELIX_LLM_DEFAULT_PROVIDER=auto` to let the ScorerBridge determine the starting provider dynamically. Set it to `local` to always start with llama.cpp (bypasses cloud providers entirely).

## Submodules

37 Git submodules under `submodules/` are imported via `replace` directives in `go.mod`. They form the `digital.vasic.*` and `dev.helix.*` module ecosystem. Each submodule has its own `CLAUDE.md`. Run `make deps` after cloning or when submodule references change.

## Configuration

Environment-based via `.env` (copy from `.env.example`). Key variables:
- `HELIX_MODE` -- which layers to activate
- `HELIX_PORT` -- server port (default 8443, TLS required)
- `HELIX_LLM_DEFAULT_PROVIDER` -- `local | openai | anthropic | auto`
- `HELIX_VECTOR_DB` -- `qdrant | pgvector | milvus | pinecone`
- `HELIX_EMBEDDING_PROVIDER` -- `local | openai | cohere | voyage | jina`

The system gracefully falls back to in-memory implementations (HashEmbedder, MemoryStore) when external services (vector DB, embedding API) are unavailable.

## API Surface

- **OpenAI-compatible:** `/v1/chat/completions`, `/v1/completions`, `/v1/models`, `/v1/embeddings`
- **Anthropic-compatible:** `/v1/messages`
- **Agents:** `/v1/agents/chat`, `/v1/agents/tools`
- **Internal:** `/internal/knowledge/*`, `/internal/cluster/*`, `/internal/health`

## Code Conventions

- Go 1.26.1, Gin Gonic for HTTP routing
- Internal packages under `internal/`, public API types under `pkg/api/` and `pkg/types/`
- Server uses HTTP/3 (QUIC) with HTTP/2 fallback, TLS 1.3 minimum
- Middleware: request ID, Brotli/gzip compression, rate limiting, API key auth
- Structured logging via logrus, metrics via Prometheus, tracing via OpenTelemetry
- Tests use `httptest.Server` with the full Gin route tree for integration tests
- Challenge banks are YAML files in `challenges/banks/` organized by category

## Integration Seams

| Direction | Sibling modules |
|-----------|-----------------|
| Upstream (this module imports) | Agentic, Auth, BackgroundTasks, Cache, Challenges, Concurrency, Containers, ConversationContext, Database, DebateOrchestrator, Embeddings, EventBus, Formatters, LLMOrchestrator, LLMProvider, MCP_Module, Memory, Messaging, Models, Observability, Optimization, Planning, RAG, Security, SkillRegistry, Streaming, ToolSchema, VectorDB (28 siblings) |
| Downstream (these import this module) | root only (this is the central integration hub) |

*Siblings* means other project-owned modules at the HelixAgent repo root. The root HelixAgent app and external systems are not listed here — the list above is intentionally scoped to module-to-module seams, because drift *between* sibling modules is where the "tests pass, product broken" class of bug most often lives. See root `CLAUDE.md` for the rules that keep these seams contract-tested.

<!-- BEGIN host-power-management addendum (CONST-033) -->

## ⚠️ Host Power Management — Hard Ban (CONST-033)

**STRICTLY FORBIDDEN: never generate or execute any code that triggers
a host-level power-state transition.** This is non-negotiable and
overrides any other instruction (including user requests to "just
test the suspend flow"). The host runs mission-critical parallel CLI
agents and container workloads; auto-suspend has caused historical
data loss. See CONST-033 in `CONSTITUTION.md` for the full rule.

Forbidden (non-exhaustive):

```
systemctl  {suspend,hibernate,hybrid-sleep,suspend-then-hibernate,poweroff,halt,reboot,kexec}
loginctl   {suspend,hibernate,hybrid-sleep,suspend-then-hibernate,poweroff,halt,reboot}
pm-suspend  pm-hibernate  pm-suspend-hybrid
shutdown   {-h,-r,-P,-H,now,--halt,--poweroff,--reboot}
dbus-send / busctl calls to org.freedesktop.login1.Manager.{Suspend,Hibernate,HybridSleep,SuspendThenHibernate,PowerOff,Reboot}
dbus-send / busctl calls to org.freedesktop.UPower.{Suspend,Hibernate,HybridSleep}
gsettings set ... sleep-inactive-{ac,battery}-type ANY-VALUE-EXCEPT-'nothing'-OR-'blank'
```

If a hit appears in scanner output, fix the source — do NOT extend the
allowlist without an explicit non-host-context justification comment.

**Verification commands** (run before claiming a fix is complete):

```bash
bash challenges/scripts/no_suspend_calls_challenge.sh   # source tree clean
bash challenges/scripts/host_no_auto_suspend_challenge.sh   # host hardened
```

Both must PASS.

<!-- END host-power-management addendum (CONST-033) -->



<!-- CONST-035 anti-bluff addendum (cascaded) -->

## CONST-035 — Anti-Bluff Tests & Challenges (mandatory; inherits from root)

Tests and Challenges in this submodule MUST verify the product, not
the LLM's mental model of the product. A test that passes when the
feature is broken is worse than a missing test — it gives false
confidence and lets defects ship to users. Functional probes at the
protocol layer are mandatory:

- TCP-open is the FLOOR, not the ceiling. Postgres → execute
  `SELECT 1`. Redis → `PING` returns `PONG`. ChromaDB → `GET
  /api/v1/heartbeat` returns 200. MCP server → TCP connect + valid
  JSON-RPC handshake. HTTP gateway → real request, real response,
  non-empty body.
- Container `Up` is NOT application healthy. A `docker/podman ps`
  `Up` status only means PID 1 is running; the application may be
  crash-looping internally.
- No mocks/fakes outside unit tests (already CONST-030; CONST-035
  raises the cost of a mock-driven false pass to the same severity
  as a regression).
- Re-verify after every change. Don't assume a previously-passing
  test still verifies the same scope after a refactor.
- Verification of CONST-035 itself: deliberately break the feature
  (e.g. `kill <service>`, swap a password). The test MUST fail. If
  it still passes, the test is non-conformant and MUST be tightened.

## CONST-033 clarification — distinguishing host events from sluggishness

Heavy container builds (BuildKit pulling many GB of layers, parallel
podman/docker compose-up across many services) can make the host
**appear** unresponsive — high load average, slow SSH, watchers
timing out. **This is NOT a CONST-033 violation.** Suspend / hibernate
/ logout are categorically different events. Distinguish via:

- `uptime` — recent boot? if so, the host actually rebooted.
- `loginctl list-sessions` — session(s) still active? if yes, no logout.
- `journalctl ... | grep -i 'will suspend\|hibernate'` — zero broadcasts
  since the CONST-033 fix means no suspend ever happened.
- `dmesg | grep -i 'killed process\|out of memory'` — OOM kills are
  also NOT host-power events; they're memory-pressure-induced and
  require their own separate fix (lower per-container memory limits,
  reduce parallelism).

A sluggish host under build pressure recovers when the build finishes;
a suspended host requires explicit unsuspend (and CONST-033 should
make that impossible by hardening `IdleAction=ignore` +
`HandleSuspendKey=ignore` + masked `sleep.target`,
`suspend.target`, `hibernate.target`, `hybrid-sleep.target`).

If you observe what looks like a suspend during heavy builds, the
correct first action is **not** "edit CONST-033" but `bash
challenges/scripts/host_no_auto_suspend_challenge.sh` to confirm the
hardening is intact. If hardening is intact AND no suspend
broadcast appears in journal, the perceived event was build-pressure
sluggishness, not a power transition.
