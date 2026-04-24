# CLAUDE.md


## Definition of Done

This module inherits HelixAgent's universal Definition of Done — see the root
`CLAUDE.md` and `docs/development/definition-of-done.md`. In one line: **no
task is done without pasted output from a real run of the real system in the
same session as the change.** Coverage and green suites are not evidence.

### Acceptance demo for this module

<!-- TODO: replace this block with the exact command(s) that exercise this
     module end-to-end against real dependencies, and the expected output.
     The commands must run the real artifact (built binary, deployed
     container, real service) — no in-process fakes, no mocks, no
     `httptest.NewServer`, no Robolectric, no JSDOM as proof of done. -->

```bash
# TODO
```

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
