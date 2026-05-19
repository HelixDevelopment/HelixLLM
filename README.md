# HelixLLM

Enterprise-grade distributed LLM system built in Go with Gin Gonic. A single binary with a mode system that enables flexible deployment from single-host development to multi-host production clusters.

HelixLLM provides fully compatible OpenAI and Anthropic APIs, local LLM inference via llama.cpp, a RAG knowledge pipeline, a ReAct agent system with tool calling, and a control plane for multi-host cluster management -- all served over HTTP/3 with automatic HTTP/2 fallback.

## Key Features

- **OpenAI and Anthropic compatible APIs** -- any existing SDK client works without modification
- **Local LLM inference** via llama.cpp with CUDA, Metal, and ROCm support
- **Multi-provider fallback chain** -- auto-discovers free models from 7+ cloud providers (Chutes, OpenRouter, HuggingFace, Nvidia, Cerebras, SambaNova, Together), scores them via LLMsVerifier, routes through the ranked chain with automatic 429/5xx failover, llama.cpp as guaranteed last resort
- **RAG knowledge pipeline** -- document ingestion, chunking, embedding, vector search
- **ReAct agent system** with tool calling, conversation sessions, and RAG integration
- **HTTP/3 (QUIC) server** with automatic HTTP/2 fallback and TLS 1.3
- **Multi-host distribution** -- SSH-based probing, scheduling, and container deployment
- **Mode system** -- run as `full` (all-in-one), `gateway`, `brain`, `knowledge`, `agents`, or `control`
- **Brotli and gzip compression** with automatic content negotiation
- **SSE streaming** matching OpenAI/Anthropic `text/event-stream` format
- **API key and JWT authentication** with rate limiting
- **Prometheus metrics and OpenTelemetry tracing**
- **43 Go submodules** providing production-grade infrastructure

## Quick Start

```bash
# Clone with submodules
git clone --recurse-submodules https://github.com/HelixDevelopment/HelixLLM.git
cd HelixLLM

# Copy and edit configuration
cp .env.example .env

# Generate TLS certificates and start in full mode
make dev
```

The server starts on `https://localhost:8443` with all subsystems active.

## API Endpoints

### OpenAI Compatible

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/v1/chat/completions` | Chat completions (SSE streaming with `stream: true`) |
| POST | `/v1/completions` | Text completions |
| GET | `/v1/models` | List available models |
| GET | `/v1/models/:id` | Get model details |
| POST | `/v1/embeddings` | Generate embeddings |

### Anthropic Compatible

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/v1/messages` | Messages API (SSE streaming with `stream: true`) |

### Agents

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/v1/agents/chat` | Run agent loop with optional session tracking |
| GET | `/v1/agents/tools` | List available tools |

### Knowledge (Internal)

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/internal/knowledge/ingest` | Ingest documents into vector store |
| POST | `/internal/knowledge/query` | Query knowledge base |
| GET | `/internal/knowledge/collections` | List collections |
| GET | `/internal/knowledge/stats` | Knowledge base statistics |

### Cluster Control (Internal)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/internal/cluster/status` | Cluster health and deployment status |
| POST | `/internal/cluster/probe` | Probe all configured hosts |
| POST | `/internal/cluster/deploy` | Schedule and deploy services |
| POST | `/internal/cluster/rebalance` | Rebalance service placement |

### Health

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/internal/health` | Aggregated health check |

## Architecture

HelixLLM compiles to a single binary that operates in one of six modes:

| Mode | Role |
|------|------|
| `full` | All-in-one, single process (development and single-host production) |
| `gateway` | API surface: HTTP/3, OpenAI/Anthropic compat, auth, streaming |
| `brain` | LLM coordination: routing, llama.cpp RPC, cloud providers |
| `knowledge` | RAG pipeline: retrieval, embeddings, vector store, ingestion |
| `agents` | Agent system: ReAct loop, tools, conversation context |
| `control` | Cluster management: host probing, scheduling, deployment, monitoring |

In `full` mode all layers communicate via direct Go function calls with zero network overhead. In distributed mode the same binary runs on multiple hosts in different modes, communicating via gRPC, SSE, and Kafka.

## Multi-Provider Fallback Chain

HelixLLM routes requests through a scored chain of free cloud providers with automatic failover:

1. **Auto-discovery** -- discovers available models from all configured providers (Chutes, OpenRouter, HuggingFace, Nvidia, Cerebras, SambaNova, Together)
2. **Scoring** -- ranks providers using LLMsVerifier scores (refreshed every 5 minutes)
3. **Fallback** -- on rate limit (429) or server error (5xx), automatically rotates to the next provider
4. **Local fallback** -- llama.cpp is always the last resort, guaranteed to be available
5. **Rate limit tracking** -- parses response headers to proactively skip providers approaching limits

Set API keys for any number of providers in `.env`:

```bash
HELIX_LLM_CHUTES_KEY=your-key
HELIX_LLM_OPENROUTER_KEY=your-key
HELIX_LLM_HUGGINGFACE_KEY=your-key
HELIX_LLM_NVIDIA_KEY=your-key
HELIX_LLM_CEREBRAS_KEY=your-key
HELIX_LLM_SAMBANOVA_KEY=your-key
HELIX_LLM_TOGETHER_KEY=your-key
```

The chain automatically discovers and ranks available models. No manual model configuration needed. OpenRouter models with the `:free` suffix are automatically filtered.

## Configuration

Configuration is loaded from environment variables with sensible defaults. Copy `.env.example` to `.env` and customize:

```bash
HELIX_MODE=full                          # Operating mode
HELIX_PORT=8443                          # Server port
HELIX_LLM_DEFAULT_PROVIDER=local         # local | openai | anthropic | auto
HELIX_LLM_OPENAI_KEY=sk-...             # OpenAI API key (optional)
HELIX_LLM_ANTHROPIC_KEY=sk-ant-...      # Anthropic API key (optional)
HELIX_LLM_CHUTES_KEY=...                # Free cloud providers (optional)
HELIX_LLM_OPENROUTER_KEY=...
HELIX_HOSTS=nezha.local                  # Comma-separated cluster hosts
```

See [docs/user-guide/configuration.md](docs/user-guide/configuration.md) for the full reference.

## Building and Testing

```bash
# Build the binary
make build

# Run unit tests with coverage
make test-unit

# Run integration tests
make test-integration

# Run all tests
make test-all

# Check coverage meets threshold (85%)
make coverage

# Lint
make lint

# Format code
make fmt

# Build container image (auto-detects Podman/Docker)
make container

# Update submodule dependencies
make deps
```

## Project Structure

```
helixllm/
  cmd/helixllm/           CLI entry point and mode routing
  internal/
    gateway/               API layer (OpenAI/Anthropic endpoints, auth, streaming)
    brain/                 LLM coordination (routing, llama.cpp, cloud providers)
    knowledge/             RAG pipeline (embeddings, vector store, chunking)
    agents/                Agent system (ReAct loop, tools, conversation context)
    control/               Cluster management (probing, scheduling, deployment)
    mode/                  Mode enum and parsing
    server/                HTTP/3 + HTTP/2 server with middleware
    shared/                Cross-cutting (config, events, health, logging, observability)
  pkg/
    api/                   Public request/response types
    types/                 Shared type definitions
  submodules/              43 Go modules (vasic-digital ecosystem)
  container/               Containerfiles for Podman/Docker
  deploy/                  Compose files for full stack
  tests/                   Integration and unit tests
  challenges/              Challenge banks for testing
  docs/
    user-guide/            End-user documentation
    manual/                Developer and operator documentation
```

## Documentation

- **User Guide:** [Getting Started](docs/user-guide/getting-started.md) | [Configuration](docs/user-guide/configuration.md) | [API Reference](docs/user-guide/api-reference.md) | [Models](docs/user-guide/models.md) | [RAG Knowledge](docs/user-guide/rag-knowledge.md) | [Agents](docs/user-guide/agents.md) | [Multi-Host Setup](docs/user-guide/multi-host-setup.md) | [Monitoring](docs/user-guide/monitoring.md) | [Troubleshooting](docs/user-guide/troubleshooting.md)
- **Manual:** [Architecture](docs/manual/architecture.md) | [Development](docs/manual/development.md) | [Testing](docs/manual/testing.md) | [Security](docs/manual/security.md) | [Operations](docs/manual/operations.md) | [Modules](docs/manual/modules.md)
- **Governance:** [`CONSTITUTION.md`](CONSTITUTION.md) | [`CLAUDE.md`](CLAUDE.md) | [`AGENTS.md`](AGENTS.md) — anti-bluff posture, cascade anchors, governance discipline. The constitution submodule at `<consuming-project>/constitution/` is the canonical root per CONST-059; HelixLLM's own files are consumer extensions and inherit every universal rule.
- **Test-coverage ledger:** [`docs/test-coverage.md`](docs/test-coverage.md) — CONST-050(B) accountability matrix mapping HelixLLM's seven primary surfaces against the fourteen test types §11.4.27 enumerates, with per-row evidence pointers.

## Test posture and anti-bluff guarantees

HelixLLM ships under the constitution submodule's Article XI §11.9
anti-bluff forensic anchor. The bar for shipping is **not** "tests
pass" but "users can use the feature." Every PASS in this codebase
carries positive runtime evidence captured during execution.

Five guard rails enforce that bar:

1. **No-fakes-beyond-unit-tests (CONST-050(A)).** Mocks live only in
   `*_test.go` files invoked without an integration build tag. The
   `fakeTranslator` in `cmd/helixllm/challenges_test.go` is the
   canonical example: satisfies `i18n.TranslatorAPI`, lives in the
   unit-test source, never imported from production code. Integration,
   E2E, security, chaos, stress, performance, benchmarking,
   Challenges, and helix_qa runs exercise the real, fully implemented
   HelixLLM against real backing services (llama.cpp endpoint, real
   Redis, real Postgres, real cloud-provider HTTP).

2. **No-hardcoded-content (CONST-046).** Every user-facing string is
   either LLM-generated at runtime, loaded from the i18n bundle
   (`internal/shared/i18n/`), or composed from verifier metadata.
   Round 95 migrated the two surviving CLI literals
   (`KeyHelixllmCLIFailedToLoadBanks`,
   `KeyHelixllmCLIErrorLoadingConfig`); round 215 wraps that work in
   `challenges/scripts/helixllm_cli_challenge.sh` with a
   paired-mutation gate that refuses any regression.

3. **No-secret-leak (CONST-042).** API keys live in `.env` files
   (mode 0600) listed in `.gitignore`. The repository's `.gitignore`
   forbids tracking any of the categories §11.4.30 enumerates:
   build artefacts, caches, tmp files, `.env*` (except
   `.env.example`), PEM/key/crt, logs, OS/IDE personal state.

4. **No-host-power-management (CONST-033).** HelixLLM never emits
   shell commands or systemd units that suspend, hibernate,
   hybrid-sleep, poweroff, halt, or reboot the host. See
   `docs/HOST_POWER_MANAGEMENT.md` for the verbatim ban and
   `challenges/scripts/host_no_auto_suspend_challenge.sh` +
   `challenges/scripts/no_suspend_calls_challenge.sh` for the
   runtime guards.

5. **Paired-mutation gates (§1.1).** Every governance gate ships with
   a paired-mutation self-test that plants a known violation and
   asserts the gate flips to FAIL. The new round-215
   `helixllm_cli_challenge.sh` demonstrates the pattern: invariant 6
   creates a sandbox copy with a bare-English literal restored, runs
   the same scan logic against the sandbox, and refuses to report
   overall PASS unless the planted violation is caught.

### Running the test suite

The full test-type catalog is documented in
[`docs/test-coverage.md`](docs/test-coverage.md). Quick reference:

```bash
make test-unit              # internal/... with -race, threshold 91%
make test-integration       # real backing services, no mocks
make test-e2e               # full user-flow exercise
make test-automation        # test-unit → test-integration → test-challenges
make test-security          # security bank + scan-quick + scan-fs
make test-stress            # request-flood profile (DDoS / stress)
make test-chaos             # failure-injection profile
make test-performance       # SLO baselines under tests/performance/baselines/
make test-benchmark         # historical p95 drift detection
make test-challenges        # every YAML bank under challenges/banks/
make coverage               # enforces 91% coverage floor

# Round-215 CLI-surface anti-bluff sweep (six invariants,
# 16 individual checks, paired-mutation self-test):
./challenges/scripts/helixllm_cli_challenge.sh
```

The release-gate sweep regenerates `docs/test-coverage.md` and
verifies every (surface × test-type) cell either has a documented
PASS evidence pointer or an explicit `PENDING`/`n/a` rationale. A row
that reads PASS without evidence is a CONST-035 violation of the
same severity as a green CI badge hiding a broken feature.

### Reproducing a Challenge result

Every Challenge under `challenges/scripts/` is self-contained and
executable from the repository root. Twelve cross-cutting scripts:

| Script | Surface |
|--------|---------|
| `helixllm_cli_challenge.sh` (round 215) | CLI surface, i18n, paired-mutation |
| `multi_provider_fallback_challenge.sh`  | Provider × fallback wiring, build, unit-test |
| `multi_model_fleet_challenge.sh`        | Multi-model coordination |
| `helixllm_memory_sync_challenge.sh`     | Conversation memory persistence |
| `chaos_failure_injection_challenge.sh`  | Recovery from forced failure |
| `ddos_health_flood_challenge.sh`        | Health-endpoint flood resilience |
| `scaling_horizontal_challenge.sh`       | Multi-process load-balancing |
| `stress_sustained_load_challenge.sh`    | Memory + goroutine leak detection |
| `ui_terminal_interaction_challenge.sh`  | TUI monitor interaction flow |
| `ux_end_to_end_flow_challenge.sh`       | Clone → dev → chat → stream |
| `host_no_auto_suspend_challenge.sh`     | CONST-033 host-power guard |
| `no_suspend_calls_challenge.sh`         | CONST-033 source-level guard |

Per-bank YAML challenges under `challenges/banks/` are driven by the
in-process runner via `make test-challenges` or by the binary directly:

```bash
./bin/helixllm --challenges --banks-dir=challenges/banks/api/ \
               --base-url=https://localhost:8443
```

### What "PASS" means here

A passing test in HelixLLM is a claim that the feature **works for
the end user** — Quality + Completion + Full Usability. Any test
that doesn't certify all three is a bluff and gets tightened (or
reopened per CONST-058 with `By: AI, Reason:
captured-evidence-contradicts`). The forensic anchor at Article XI
§11.9 of the constitution submodule is the operative authority; this
README's role is to make the discipline visible at the entry point.

## License

All rights reserved. See LICENSE for details.
