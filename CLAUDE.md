# CLAUDE.md

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
Brain      → LLM provider routing: llama.cpp (local), OpenAI, Anthropic
Knowledge  → RAG pipeline: chunking → embedding → vector store → retrieval
Agents     → ReAct loop, tool calling, conversation sessions, multi-agent coordination
Control    → SSH-based host probing, container deployment, scheduling strategies
Shared     → Config (env-based), EventBus, logging (logrus), observability (OTEL), health, analytics
```

### Key Interfaces

- **`brain.Provider`** (`internal/brain/provider.go`) -- All LLM backends implement `Complete`, `CompleteStream`, `Models`, `Name`, `Available`. Implementations: `llamacpp.go`, `openai_provider.go`, `anthropic_provider.go`.
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
