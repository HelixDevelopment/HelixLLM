# HelixLLM Master Architecture Design

**Version:** 1.0
**Date:** 2026-04-04
**Status:** Approved
**Approach:** Layered Monolith with Modal Distribution

---

## 1. Overview

HelixLLM is an enterprise-grade distributed LLM system built in Go with Gin Gonic. It operates as a single binary with a mode system that enables flexible deployment from single-host development to multi-host production clusters.

### Core Principles

- **Go + Gin Gonic** as the sole implementation language/framework
- **Containers first** — Podman (preferred), Docker (fallback), auto-detected
- **HTTP/3** primary with HTTP/2 fallback; Brotli compression with gzip fallback
- **TOON** (Token-Oriented Object Notation) for LLM-facing serialization, JSON as default/fallback
- **OpenAI and Anthropic fully compatible APIs** — any existing client works without modification
- **WebSocket, SSE, gRPC streaming** — all major real-time standards
- **Dynamic multi-host distribution** — capability-based scheduling across hosts
- **SSH-based deployment** — no-password key auth to all hosts
- **100% test coverage** across 9+ test types — no mocks except unit tests
- **Comprehensive documentation** — user guide and manual covering every feature

### Hardware & Network

| Host | DNS | User | Access |
|------|-----|------|--------|
| Primary (local) | nezha.local | milosvasic | localhost |
| Worker 1 | thinker.local | milosvasic | SSH (key-based, no password) |
| Worker 2 | amber.local | milosvasic | SSH (key-based, no password) |

OS: Mixed Linux distributions and macOS. All platform-specific behavior abstracted by containers.

---

## 2. Modal Architecture

One Go binary, 6 modes:

| Mode | Flag | Role |
|------|------|------|
| **full** | `--mode=full` | All-in-one, single process (development, single-host) |
| **gateway** | `--mode=gateway` | API surface: HTTP/3, OpenAI/Anthropic compat, WebSocket, SSE |
| **brain** | `--mode=brain` | LLM coordination: routing, llama.cpp RPC, cloud providers |
| **knowledge** | `--mode=knowledge` | RAG pipeline: retrieval, embeddings, vector store, ingestion |
| **agents** | `--mode=agents` | Multi-agent: workflows, planning, tools, context, memory |
| **control** | `--mode=control` | Cluster: host probing, benchmarking, scheduling, deployment |

### Distribution Flow

1. `control` mode starts on the primary host (nezha.local)
2. SSH-probes all configured hosts via `digital.vasic.containers` Distributor
3. Benchmarks each host (CPU, RAM, GPU, disk, network)
4. ResourceScheduler assigns modes to hosts based on capabilities (5 strategies: BinPack, Spread, GPU-Affinity, Memory-First, Latency-Optimized)
5. Deploys containerized instances of the same binary in assigned modes
6. Continuous monitoring with auto-remediation (restart, reschedule, rebalance)

### Communication

- **Full mode (single host):** Direct Go function calls — zero network overhead
- **Distributed mode:** `digital.vasic.streaming` (gRPC/SSE/WebSocket) for real-time, `digital.vasic.messaging` (Kafka) for async, `digital.vasic.eventbus` for in-process pub/sub

---

## 3. Project Structure

```
helixllm/
  cmd/helixllm/              main.go — CLI entry, mode routing
  internal/
    gateway/                 API layer
      router.go              Gin router setup, HTTP/3, ALPN negotiation
      openai.go              OpenAI-compatible endpoint handlers
      anthropic.go           Anthropic-compatible endpoint handlers
      streaming.go           SSE + WebSocket streaming handlers
      auth.go                Authentication middleware (JWT, API key, OAuth2)
      negotiation.go         Content negotiation (TOON/JSON, Brotli/gzip)
    brain/                   LLM coordination
      router.go              Intelligent LLM routing (model, capability, cost, latency)
      llamacpp.go            llama.cpp RPC client adapter
      providers.go           Cloud provider configuration
      optimization.go        Semantic cache, prompt compression
    knowledge/               RAG pipeline
      pipeline.go            RAG query pipeline (retrieval → rerank → assemble)
      ingestion.go           Document processing and ingestion queue
      collections.go         Vector collection management
    agents/                  Multi-agent system
      loop.go                ReAct agent loop (reason → act → observe)
      coordinator.go         Phase-based multi-agent coordination
      tools.go               Tool registry and execution
      mcp.go                 MCP client integration
      skills.go              Skill discovery and invocation
      memory.go              Three-tier memory management
    control/                 Cluster management
      prober.go              SSH-based host probing and capability detection
      benchmarker.go         Host benchmarking runner
      scheduler.go           Container placement scheduling
      deployer.go            Container deployment via SSH
      monitor.go             Continuous health monitoring and auto-remediation
      llmops.go              Evaluation pipelines, A/B experiments
      selfimprove.go         RLAIF feedback collection and optimization
    shared/                  Cross-cutting concerns
      config.go              Configuration loading (.env + JSON + struct tags)
      events.go              EventBus setup and topic definitions
      messaging.go           Kafka/RabbitMQ broker setup
      observability.go       Tracing, metrics, logging setup
      health.go              Aggregated health checks
  pkg/                       Public interfaces (for external integration)
    api/                     Request/response types for OpenAI/Anthropic compat
    types/                   Shared type definitions
  container/                 Containerfiles
    Containerfile            Multi-stage build (Podman/Docker compatible)
    Containerfile.llamacpp   llama.cpp with CUDA/ROCm/Metal support
  deploy/                    Deployment configuration
    compose.yaml             Compose file for full stack
    compose.dev.yaml         Development overrides
  tests/                     All test types
    unit/                    Unit tests (mocks allowed here only)
    integration/             Integration tests (real services)
    e2e/                     End-to-end tests
    stress/                  Stress tests
    chaos/                   Chaos tests
    security/                Security tests
    benchmarks/              Benchmark tests
    automation/              Full automation pipeline tests
  challenges/                Challenge banks for Challenges + HelixQA
    banks/
      llm/                   Code generation, multi-turn, tool calling, streaming
      rag/                   Retrieval quality, ingestion, embedding accuracy
      api/                   OpenAI/Anthropic compat, error handling, auth
      cluster/               Deployment, failover, rebalancing, host probing
      chaos/                 Container kills, network partitions, resource exhaustion
      security/              Injection, auth bypass, PII, rate limiting
      benchmarks/            Latency, throughput, concurrent users
      workflows/             Real developer scenarios (coding, review, debugging, docs)
      regression/            Known-fixed bugs, edge cases
  docs/                      Documentation
    user-guide/
      getting-started.md
      configuration.md
      api-reference.md
      multi-host-setup.md
      models.md
      rag-knowledge.md
      agents.md
      monitoring.md
      troubleshooting.md
    manual/
      architecture.md
      development.md
      testing.md
      security.md
      operations.md
      modules.md
  Makefile                   Manual CI/CD targets
  go.mod
  go.sum
  .env.example
  .gitignore
  .gitmodules
  README.md
```

---

## 4. Submodule Inventory

41 Go modules from vasic-digital + 1 from HelixDevelopment (HelixQA) + 1 new module (TOON) = 43 total, organized by layer.

### 4.1 Gateway Layer

| Submodule | Go Module | Role |
|-----------|-----------|------|
| Streaming | digital.vasic.streaming | SSE broker, WebSocket hub, gRPC streaming, Gin adapters |
| Middleware | digital.vasic.middleware | CORS, logging, recovery, request ID |
| Auth | digital.vasic.auth | JWT, API key, OAuth2, Bearer/API-key middleware |
| RateLimiter | digital.vasic.ratelimiter | Sliding-window rate limiting, Redis-backed, HTTP middleware |
| Security | digital.vasic.security | Content guardrails, PII detection/redaction, HTTP security headers, AES-256-GCM |
| I18n | digital.vasic.i18n | Multi-language API responses, Accept-Language middleware |
| TOON (new) | digital.vasic.toon | Wrapper around toon-format/toon-go for our conventions |
| Formatters | digital.vasic.formatters | Response formatting pipeline (JSON, TOON, etc.) |

### 4.2 Brain Layer

| Submodule | Go Module | Role |
|-----------|-----------|------|
| LLMProvider | digital.vasic.llmprovider | Unified LLM interface, 40+ adapters, circuit breaker, health monitoring |
| Optimization | digital.vasic.optimization | Semantic cache (gptcache), prompt compression, streaming buffers |
| Cache | digital.vasic.cache | Response caching — Redis and in-memory (LRU, TTL), distributed patterns |
| Recovery | digital.vasic.recovery | Circuit breakers per LLM backend, health checker, resilience facade |

### 4.3 Knowledge Layer

| Submodule | Go Module | Role |
|-----------|-----------|------|
| RAG | digital.vasic.rag | Full RAG pipeline: chunking, hybrid retrieval, MMR reranking, pipeline builder |
| VectorDB | digital.vasic.vectordb | Unified VectorStore across Qdrant, Pinecone, Milvus, pgvector |
| Embeddings | digital.vasic.embeddings | Multi-provider embeddings (OpenAI, Cohere, Voyage, Jina, Vertex, Bedrock) |
| Document | digital.vasic.document | Document model, 18-format detection, change tracking |
| Filesystem | digital.vasic.filesystem | Multi-protocol file access (SMB, FTP, WebDAV, NFS, local) |
| Database | digital.vasic.database | PostgreSQL (pgx) + SQLite, migrations, generic Repository[T] |
| BackgroundTasks | digital.vasic.backgroundtasks | PostgreSQL-backed async task queue with progress reporting |

### 4.4 Agents Layer

| Submodule | Go Module | Role |
|-----------|-----------|------|
| Agentic | digital.vasic.agentic | Graph-based workflow engine, 6 node types, checkpointing, self-correction |
| Planning | digital.vasic.planning | HiPlan, MCTS, Tree of Thoughts planning algorithms |
| ToolSchema | digital.vasic.toolschema | Tool registry, 14+ built-in handlers, safety guards |
| conversation | digital.vasic.conversation | Infinite context via Kafka event sourcing, LLM compression, LRU cache |
| SkillRegistry | digital.vasic.skillregistry | Skill loading (YAML/JSON/MD), validation, execution with hooks |
| MCP_Module | digital.vasic.mcp | MCP client/server, JSON-RPC 2.0, stdio/HTTP/SSE transport |
| LLMOrchestrator | digital.vasic.llmorchestrator | Headless CLI agent management, capability-based pool |
| Memory | digital.vasic.memory | Mem0-style memory, importance scoring, knowledge graph, entity extraction |

### 4.5 Control Plane

| Submodule | Go Module | Role |
|-----------|-----------|------|
| Containers | digital.vasic.containers | Docker/Podman/K8s, SSH distribution, resource scheduling, TUI monitor |
| Discovery | digital.vasic.discovery | Network/service discovery, pluggable scanners |
| Benchmark | digital.vasic.benchmark | LLM benchmarking (SWE-bench, HumanEval, MMLU), host capability profiling |
| LLMOps | digital.vasic.llmops | Continuous evaluation, A/B experiments, prompt versioning, alerting |
| SelfImprove | digital.vasic.selfimprove | RLAIF pipeline, multi-dimension reward, constitutional self-critique |

### 4.6 Shared Foundation (all layers)

| Submodule | Go Module | Role |
|-----------|-----------|------|
| Config | digital.vasic.config | JSON + .env config, struct tag binding, validation |
| EventBus | digital.vasic.eventbus | In-process pub/sub, typed topics, filter combinators, middleware |
| Messaging | digital.vasic.messaging | Kafka/RabbitMQ broker, consumer groups, retry with DLQ |
| Observability | digital.vasic.observability | OpenTelemetry tracing, Prometheus metrics, structured logging, health |
| Concurrency | digital.vasic.concurrency | Worker pools, priority queues, semaphores, circuit breakers |
| Lazy | digital.vasic.lazy | Thread-safe lazy initialization (sync.Once generics) |
| Watcher | digital.vasic.watcher | File/config change monitoring, debounced handlers |
| Plugins | digital.vasic.plugins | Plugin lifecycle, dependency ordering (Kahn's), shared object loading |
| Proxy | digital.vasic.proxy | HTTP + SOCKS5 proxy for external access |

### 4.7 Testing & QA

| Submodule | Source Org | Role |
|-----------|-----------|------|
| Challenges | vasic-digital | Challenge framework, assertion engine, multi-format reports, shell adapter |
| HelixQA | HelixDevelopment | QA orchestration, crash detection, evidence collection, YAML test banks |

### 4.8 New Module

| Module | Go Module | Role |
|--------|-----------|------|
| TOON | digital.vasic.toon | Wrapper around toon-format/toon-go with our encoding conventions |

**Upstream submodule:** `toon-format/toon` added as a Git submodule for reference.

### 4.9 Dependency Submodules

Submodules that are dependencies of the above (pulled transitively):
- Challenges depends on Containers
- HelixQA depends on Challenges + Containers
- All modules with HTTP middleware depend on Middleware

These are added as Git submodules alongside their dependents.

---

## 5. Gateway Layer Design

### 5.1 HTTP/3 Server

- Primary: HTTP/3 (QUIC) via `quic-go` with Gin
- Automatic ALPN negotiation falls back to HTTP/2
- TLS required for HTTP/3; self-signed certs for local development, configurable for production

### 5.2 Content Negotiation

**Compression** (via `Accept-Encoding`):
- `br` → Brotli (primary)
- `gzip` → gzip (fallback)
- Automatic negotiation

**Serialization** (via `Accept` / `Content-Type`):
- `application/toon` → TOON format (token-efficient for LLM consumers)
- `application/json` → JSON (default)
- Server responds in ONE format based on the `Accept` header; JSON is the default if no preference is specified

### 5.3 OpenAI-Compatible API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/chat/completions` | POST | Chat completions (stream via SSE when `stream: true`) |
| `/v1/completions` | POST | Text completions |
| `/v1/models` | GET | List available models (local + cloud) |
| `/v1/models/{id}` | GET | Model details and capabilities |
| `/v1/embeddings` | POST | Generate embeddings |

Request/response schemas match OpenAI's API spec exactly. Any OpenAI SDK client works without changes.

### 5.4 Anthropic-Compatible API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/messages` | POST | Messages API (stream via SSE when `stream: true`) |
| `/v1/messages/{id}` | GET | Retrieve message |

Request/response schemas match Anthropic's Messages API spec. The `anthropic-version` header is respected.

### 5.5 Internal/Management API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/internal/health` | GET | Aggregated health check |
| `/internal/metrics` | GET | Prometheus metrics |
| `/internal/knowledge/query` | POST | RAG query |
| `/internal/knowledge/ingest` | POST | Document ingestion |
| `/internal/knowledge/collections` | GET | List vector collections |
| `/internal/knowledge/stats` | GET | Knowledge base statistics |
| `/internal/tools` | GET | List available tools |
| `/internal/tools/execute` | POST | Direct tool execution |
| `/internal/agents/status` | GET | Agent pool status |
| `/internal/cluster/status` | GET | Cluster health and placement |
| `/internal/cluster/rebalance` | POST | Trigger rebalancing |

### 5.6 Streaming

- **SSE:** Chat completion streaming matching OpenAI/Anthropic `text/event-stream` format exactly
- **WebSocket:** Bidirectional real-time for agent status, tool execution progress, live monitoring at `/ws`
- **gRPC streaming:** Inter-layer communication in distributed mode
- All via `digital.vasic.streaming` Gin adapters

### 5.7 Authentication

- **API Key:** `Authorization: Bearer sk-...` (OpenAI-compatible)
- **JWT:** Session-based access with configurable TTL
- **OAuth2:** External integrations with auto-refresh
- **Permission modes:** `readonly`, `standard`, `admin`
- All via `digital.vasic.auth`

### 5.8 Security

- PII detection/redaction on inputs and outputs (`digital.vasic.security`)
- Content guardrails (configurable rules engine)
- Rate limiting per API key (`digital.vasic.ratelimiter`, sliding window, Redis-backed in distributed mode)
- HTTP security headers middleware
- Request ID generation and correlation across all layers (`digital.vasic.middleware`)

### 5.9 Feature Gating

- **Compile-time:** Go build tags (e.g., `//go:build with_grpc`)
- **Runtime:** Feature flags via `digital.vasic.config` + `.env`
- **Beta features:** `X-HelixLLM-Beta: feature-name` header

---

## 6. Brain Layer Design

### 6.1 LLM Provider Routing

`digital.vasic.llmprovider` serves as the unified interface. Routing strategies:

| Strategy | Logic |
|----------|-------|
| By model name | `gpt-4o` → OpenAI, `claude-sonnet-4-20250514` → Anthropic, `llama-3.1-70b` → local |
| By capability | "needs vision" → provider with vision support |
| By cost | Prefer local when quality threshold is met |
| By latency | Route to fastest responding backend |
| Fallback chain | Primary fails → try next in chain |

Configured via `.env` and runtime config. The router evaluates all strategies and selects the best match.

### 6.2 llama.cpp Integration

- llama.cpp runs as an external container managed by Control plane
- RPC server on worker hosts for distributed GPU offloading (port 50052)
- Master connects with `--rpc` flag for multi-GPU inference
- llama.cpp's built-in OpenAI-compatible `/v1/chat/completions` consumed by LLMProvider's local adapter
- Model management: download, load, unload, switch via Control plane API
- Supports CUDA (Linux), Metal (macOS), ROCm (AMD Linux)

### 6.3 Optimization

- **Semantic cache** (`digital.vasic.optimization` gptcache): Cosine similarity matching — near-identical prompts hit cache
- **Prompt compression** (`digital.vasic.optimization` prompt): Reduce token count, TOON format for internal LLM communication
- **Streaming buffers** (`digital.vasic.optimization` streaming): Configurable flush strategies for smooth SSE output
- **Circuit breakers** (`digital.vasic.recovery`): Per-provider circuit breakers with state callbacks and health monitoring

---

## 7. Knowledge Layer Design

### 7.1 Document Ingestion Pipeline

```
Source → Document (format detection) → Filesystem (multi-protocol)
    → RAG Chunker (recursive/sentence/fixed-size, configurable)
    → Embeddings (multi-provider)
    → VectorDB (Qdrant/pgvector/Milvus/Pinecone)
    → Database (PostgreSQL metadata)
```

Ingestion runs asynchronously via `digital.vasic.backgroundtasks` — queued, resumable, with progress reporting. Supports 18 document formats via `digital.vasic.document`.

### 7.2 Retrieval Pipeline

```
Query → Embeddings (query embedding)
    → Hybrid Retrieval (BM25 + semantic via digital.vasic.rag)
    → Fusion (RRF or linear combination)
    → MMR Reranking (diversity + relevance)
    → Context Assembly (for LLM prompt, respecting token budget)
```

### 7.3 Vector Database

- **Default:** Qdrant (containerized, managed by Control plane)
- **Fallback:** pgvector (if PostgreSQL already available)
- All accessed through `digital.vasic.vectordb` unified `VectorStore` interface
- Collection management: create, delete, list, stats

### 7.4 Knowledge Base

- Enterprise software patterns (architecture, design patterns, best practices)
- User's codebase documentation (ingested on demand)
- Conversation history (searchable via vector similarity)
- Custom knowledge domains (user-configurable collections)

---

## 8. Agents Layer Design

### 8.1 Agent Loop (ReAct Pattern)

Core loop implemented via `digital.vasic.agentic` graph-based workflows:

```
User Query
    → Context Assembly (conversation + RAG + memory)
    → LLM Reasoning (via Brain)
    → Decision: respond | use_tool | delegate | plan
        → Tool Execution (ToolSchema + MCP)
        → Agent Delegation (Agentic workflow)
        → Planning (HiPlan / MCTS / ToT)
    → Response Assembly
    → Memory Update (conversation + Memory)
    → Stream to Gateway
```

### 8.2 Phase-based Multi-Agent Coordination

Adapted from Claude Code's Coordinator pattern:

1. **Investigation** — gather context, read files, search codebase
2. **Synthesis** — analyze findings, form a plan
3. **Implementation** — execute the plan, run tools
4. **Verification** — validate results, run tests

Each phase can run parallel workers via `digital.vasic.concurrency` worker pools. Workers communicate through EventBus (in-process) or Messaging (distributed).

Anti-lazy delegation rule: the orchestrator always reads worker results and explicitly specifies next actions.

### 8.3 Agent Types

| Type | Role | Backed By |
|------|------|-----------|
| Orchestrator | Routes tasks, coordinates phases | Agentic workflow engine |
| Coder | Code analysis, generation, refactoring | ToolSchema + LLM |
| Researcher | Information gathering, web search | MCP_Module + RAG |
| Reviewer | Code review, security analysis | ToolSchema + MCP (Semgrep) |
| Planner | Complex task decomposition | Planning (HiPlan/MCTS/ToT) |

### 8.4 Tool System

`digital.vasic.toolschema` registry + `digital.vasic.mcp` for external tools:

| Source | Module | Examples |
|--------|--------|----------|
| Built-in | ToolSchema | read_file, write_file, git ops, bash, lint, test, diff |
| MCP Servers | MCP_Module | Semgrep, web search, GitHub API, filesystem |
| LSP (bridge) | MCP_Module | Go to definition, references, diagnostics, completions |
| Custom | Plugins | User-defined tools loaded at runtime |

Execution includes: permission checking, JSON Schema validation, timeout enforcement, progress reporting, result caching, path traversal prevention, shell injection protection.

### 8.5 Three-Tier Memory

| Tier | Module | Purpose | Persistence |
|------|--------|---------|-------------|
| Working | conversation | Current session, sliding window | In-memory + Kafka event sourcing |
| Episodic | Memory (Mem0-style) | Important facts, decisions, preferences | Database + vector search |
| Semantic | RAG + VectorDB | Long-term knowledge base | Vector store |

Background consolidation during idle: compress history, extract entities/relations into knowledge graph, score memory importance.

### 8.6 Skill System

Via `digital.vasic.skillregistry`:
- Skills loaded from YAML/JSON/Markdown
- Validated (semver, dependencies, circular detection)
- Executed with timeout, hooks, concurrency control
- Dynamic discovery and invocation by agents

### 8.7 CLI Agent Management

Via `digital.vasic.llmorchestrator`:
- Manages headless CLI agents (Claude Code, Gemini CLI, OpenCode, etc.)
- Hybrid pipe+file communication
- Capability-based agent pool routing
- Circuit breaker health monitoring per agent

---

## 9. Control Plane Design

### 9.1 Host Probing

For each host in `HELIX_HOSTS`:
1. SSH connect (`milosvasic@{host}`, key-based, no password)
2. Detect OS (`uname -s` / `sw_vers` for macOS)
3. Detect container runtime (Podman → Docker → other, by priority)
4. Detect GPU (`nvidia-smi` / Metal / `rocm-smi`)
5. Detect CPU cores, RAM, disk space, network interfaces

Output: `HostCapabilityProfile` per host.

### 9.2 Benchmarking

`digital.vasic.benchmark` runs on each host via SSH:
- CPU benchmark (single + multi-thread)
- Memory bandwidth
- Disk I/O (sequential + random)
- GPU compute (CUDA/Metal/ROCm if available)
- Network latency + throughput to other hosts
- Container runtime performance

### 9.3 Scheduling

`digital.vasic.containers` ResourceScheduler with 5 strategies:

| Strategy | Use Case |
|----------|----------|
| BinPack | Low-resource services (monitoring) — pack onto fewest hosts |
| Spread | High-availability — spread across all hosts |
| GPU-Affinity | GPU workloads (llama.cpp) — place on best GPU host |
| Memory-First | Memory-intensive (vector DB, RAG) — place on most RAM |
| Latency-Optimized | Latency-sensitive (Redis, gateway) — place near clients |

`auto` strategy: Control plane selects best strategy per service based on its resource profile.

### 9.4 External Services

| Service | Image | Scheduling | Purpose |
|---------|-------|-----------|---------|
| llama.cpp RPC | `ghcr.io/ggml-org/llama.cpp:server-cuda` | GPU-Affinity | Local LLM inference |
| Qdrant | `qdrant/qdrant` | Memory-First | Primary vector database |
| PostgreSQL | `postgres:16-alpine` | Disk I/O priority | Metadata, migrations, task queue |
| Redis | `redis:7-alpine` | Latency-Optimized | Cache, rate limiting, sessions |
| Kafka | `bitnami/kafka` | Spread | Event sourcing, async messaging |
| Prometheus | `prom/prometheus` | BinPack | Metrics collection |
| Grafana | `grafana/grafana` | BinPack | Dashboards |
| Loki | `grafana/loki` | BinPack | Log aggregation |

### 9.5 Monitoring & Auto-Remediation

Via `digital.vasic.observability`:
- OpenTelemetry traces across all services with correlation IDs
- Prometheus metrics (auto-registered per service)
- Structured logging aggregated in Loki
- Health check aggregation with status dashboard

Auto-remediation:
- Container died → restart on same host
- Host unreachable → reschedule services to surviving hosts
- Performance degraded → trigger rebalancing

TUI monitor via `digital.vasic.containers` — ctop-style live dashboard for container stats, logs, health across all hosts.

### 9.6 LLMOps

Via `digital.vasic.llmops`:
- Continuous evaluation pipelines with regression detection
- A/B experiments between models (local vs cloud, model versions)
- Prompt versioning with variable rendering
- Alerting on quality drops

### 9.7 SelfImprove

Via `digital.vasic.selfimprove`:
- RLAIF pipeline collecting user feedback (thumbs up/down, response edits)
- Multi-dimension reward scoring (correctness, helpfulness, safety, code quality, etc.)
- Background optimization of prompt templates, routing rules, RAG parameters
- Constitutional self-critique before applying any changes

---

## 10. Configuration

### 10.1 .env.example

```bash
# ── Cluster ──────────────────────────────────────────────
HELIX_MODE=full
HELIX_HOSTS=nezha.local,thinker.local,amber.local
HELIX_SSH_USER=milosvasic
HELIX_SSH_KEY=~/.ssh/id_ed25519

# ── Container Runtime ────────────────────────────────────
HELIX_CONTAINER_RUNTIME=auto
# auto | podman | docker

# ── Scheduling ───────────────────────────────────────────
HELIX_SCHEDULE_STRATEGY=auto
# auto | binpack | spread | gpu-affinity | memory-first | latency

# ── Server ───────────────────────────────────────────────
HELIX_HOST=0.0.0.0
HELIX_PORT=8443
HELIX_TLS_CERT=./certs/server.crt
HELIX_TLS_KEY=./certs/server.key

# ── LLM ──────────────────────────────────────────────────
HELIX_LLM_LOCAL_MODEL=Llama-3.1-70B-Instruct-Q4_K_M
HELIX_LLM_LOCAL_RPC_PORT=50052
HELIX_LLM_OPENAI_KEY=
HELIX_LLM_ANTHROPIC_KEY=
HELIX_LLM_DEFAULT_PROVIDER=local
# local | openai | anthropic | auto

# ── Knowledge ────────────────────────────────────────────
HELIX_VECTOR_DB=qdrant
# qdrant | pgvector | milvus | pinecone
HELIX_EMBEDDING_PROVIDER=local
# local | openai | cohere | voyage | jina
HELIX_EMBEDDING_MODEL=all-mpnet-base-v2
HELIX_RAG_CHUNK_SIZE=1000
HELIX_RAG_CHUNK_OVERLAP=200
HELIX_RAG_TOP_K=5

# ── Database ─────────────────────────────────────────────
HELIX_DB_HOST=localhost
HELIX_DB_PORT=5432
HELIX_DB_NAME=helixllm
HELIX_DB_USER=helix
HELIX_DB_PASSWORD=

# ── Cache ────────────────────────────────────────────────
HELIX_REDIS_HOST=localhost
HELIX_REDIS_PORT=6379
HELIX_REDIS_PASSWORD=

# ── Messaging ────────────────────────────────────────────
HELIX_KAFKA_BROKERS=localhost:9092

# ── Observability ────────────────────────────────────────
HELIX_OTEL_ENDPOINT=http://localhost:4317
HELIX_PROMETHEUS_PORT=9090
HELIX_GRAFANA_PORT=3001
HELIX_LOG_LEVEL=info
# debug | info | warn | error

# ── Auth ─────────────────────────────────────────────────
HELIX_AUTH_JWT_SECRET=
HELIX_AUTH_API_KEYS=
# Comma-separated API keys

# ── Feature Flags ────────────────────────────────────────
HELIX_FEATURE_GRPC=true
HELIX_FEATURE_TOON=true
HELIX_FEATURE_SELFIMPROVE=false
```

### 10.2 Configuration Loading

Via `digital.vasic.config`:
1. Load defaults from embedded struct tags
2. Load JSON config file (if present)
3. Override with `.env` file
4. Override with environment variables
5. Override with CLI flags
6. Validate with composable rules

Hot-reload supported via `digital.vasic.watcher` — config file changes detected and applied without restart.

---

## 11. Testing Strategy

### 11.1 Coverage Requirement

100% test coverage is mandatory across all test types. No exceptions.

### 11.2 Mock Policy

- **Unit tests:** Mocks, stubs, and test doubles are allowed
- **All other test types:** No mocks, no stubs, no placeholders. Real services, real containers, real LLM inference.

### 11.3 Test Types

| Type | Framework | Scope |
|------|-----------|-------|
| Unit | Go `testing` + Challenges | Individual functions, methods, type behavior |
| Integration | Challenges + real containers | Layer-to-layer, service-to-service with real backends |
| E2E | HelixQA + Challenges | Full request flow through real cluster |
| Stress | Challenges + custom runners | Sustained high load, connection limits, memory pressure |
| Chaos | Challenges + Containers | Container kills, network partitions, host failures |
| Security | Challenges + Semgrep MCP + Security module | OWASP top 10, injection, PII leaks, auth bypass |
| Benchmarking | Benchmark module + Challenges | Latency, throughput, tokens/s, TTFT, P50/P95/P99 |
| Full Automation | HelixQA orchestrator | Unattended: deploy → test → report pipeline |
| Real-World Use Cases | Challenges test banks | Developer workflows with real LLM and real codebases |

### 11.4 Challenge Banks

YAML test definitions organized by domain:

```
challenges/banks/
  llm/          Multi-turn conversation, code generation, tool calling,
                streaming correctness, model switching, context management
  rag/          Retrieval relevance, ingestion correctness, embedding quality,
                collection management, hybrid search accuracy
  api/          OpenAI SDK compatibility, Anthropic SDK compatibility,
                error codes, auth flows, rate limiting, streaming format
  cluster/      Multi-host deployment, host failover, container rescheduling,
                rebalancing under load, rolling updates
  chaos/        Container kills during requests, network partitions between hosts,
                disk full scenarios, OOM conditions, clock skew
  security/     SQL injection, prompt injection, path traversal, auth bypass,
                PII in responses, rate limit bypass, SSRF attempts
  benchmarks/   Single-user latency, concurrent throughput, sustained load,
                RAG retrieval speed, embedding generation speed
  workflows/    Developer coding tasks, code review, debugging sessions,
                documentation generation, project analysis, refactoring
  regression/   Previously fixed bugs, edge cases from production
```

### 11.5 HelixQA Integration

HelixQA orchestrates the full QA pipeline:
- Cross-platform validation (API clients on different OSes)
- Real-time crash detection during tests
- Step-by-step evidence collection (request/response logs, screenshots for TUI)
- Automated ticket generation for failures
- Multi-format reports (Markdown, HTML, JSON)

---

## 12. Build & CI/CD

### 12.1 Makefile Targets

```makefile
# ── Build ────────────────────────────────────────────────
build                    Build helixllm binary
container                Build container image (auto-detects Podman/Docker)
container-push           Push to container registry

# ── Test ─────────────────────────────────────────────────
test-unit                Unit tests with coverage report
test-integration         Integration tests (starts real services)
test-e2e                 End-to-end tests (full cluster)
test-stress              Stress tests
test-chaos               Chaos tests (injects failures)
test-security            Security tests (Semgrep + custom)
test-benchmark           Benchmark tests with comparison
test-automation          Full automation pipeline
test-usecases            Real-world use case validation
test-all                 Run all test types sequentially
coverage                 Aggregate coverage report (enforces 100%)

# ── Cluster ──────────────────────────────────────────────
probe                    Probe all configured hosts
deploy                   Deploy to cluster based on scheduling
status                   Show cluster status and service placement
logs                     Aggregated logs from all hosts
monitor                  Launch TUI monitor
rebalance                Trigger cluster rebalancing

# ── Knowledge ────────────────────────────────────────────
ingest                   Ingest documents (DIR=./path)
collections              List vector collections
stats                    Knowledge base statistics

# ── Development ──────────────────────────────────────────
dev                      Run in full mode locally (hot-reload)
lint                     golangci-lint + vet + staticcheck
fmt                      gofmt + goimports
docs                     Generate documentation
gen                      Run go generate
deps                     Update all submodule dependencies
```

### 12.2 Container Build

Multi-stage Containerfile:
1. **Builder stage:** Go build with all modules
2. **Runtime stage:** Minimal Alpine/distroless with just the binary
3. Build tags for optional features (`with_grpc`, `with_cuda`)
4. Compatible with both Podman and Docker (no Docker-specific features)

### 12.3 No CI/CD Pipelines

No GitHub Actions. No GitLab CI. All CI/CD is manual via Makefile targets. The developer runs `make test-all` locally or on any host before merging.

---

## 13. Documentation

### 13.1 User Guide

| Document | Content |
|----------|---------|
| getting-started.md | Installation, prerequisites, first run, single-host quick start |
| configuration.md | All .env variables, config files, hot-reload, config precedence |
| api-reference.md | OpenAI/Anthropic endpoints, request/response examples, error codes |
| multi-host-setup.md | SSH setup, host configuration, scheduling strategies, deployment |
| models.md | Model management, local vs cloud, switching, downloading, capabilities |
| rag-knowledge.md | Document ingestion, collection management, retrieval tuning |
| agents.md | Multi-agent system, skills, tools, workflows, planning |
| monitoring.md | Grafana dashboards, Prometheus metrics, alerts, logs, TUI monitor |
| troubleshooting.md | Common issues, debugging, log analysis, health checks |

### 13.2 Manual

| Document | Content |
|----------|---------|
| architecture.md | System architecture, layer design, mode system, data flow |
| development.md | Building from source, adding modules, contribution guide |
| testing.md | Running all test types, writing challenges, coverage enforcement |
| security.md | Security model, permissions, PII handling, hardening guide |
| operations.md | Backup, restore, upgrades, scaling, disaster recovery |
| modules.md | All 40 submodule interfaces, usage examples, configuration |

---

## 14. Implementation Phases

The master spec covers the full system. Implementation proceeds in phases, each with its own plan:

| Phase | Scope | Dependencies |
|-------|-------|-------------|
| 1 | **Foundation** — Project scaffold, shared layer, Config, EventBus, Observability, Containerfiles, .env, Makefile skeleton | None |
| 2 | **Gateway** — Gin server, HTTP/3, OpenAI/Anthropic compat APIs, auth, streaming, TOON negotiation | Phase 1 |
| 3 | **Brain** — LLMProvider integration, llama.cpp adapter, cloud provider routing, optimization, circuit breakers | Phase 1 |
| 4 | **Knowledge** — RAG pipeline, VectorDB, Embeddings, document ingestion, background tasks | Phase 1 |
| 5 | **Agents** — Agent loop, Agentic workflows, Planning, ToolSchema, MCP, conversation, Memory | Phases 2-4 |
| 6 | **Control Plane** — Host probing, benchmarking, scheduling, deployment, monitoring, LLMOps, SelfImprove | Phase 1, partially 2-5 |
| 7 | **Testing & Validation** — All 9 test types, challenge banks, HelixQA integration, 100% coverage | Phases 1-6 |
| 8 | **Documentation** — User guide, manual, module docs | Phases 1-7 |

Each phase gets its own implementation plan via the writing-plans skill.

---

## 15. Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| Single binary with modes | One build, one test suite, zero overhead in full mode, distributable when needed |
| Go + Gin (not Python FastAPI) | Type safety, single binary deployment, native concurrency, matches existing module ecosystem |
| Podman priority over Docker | Rootless by default, daemonless, better security posture |
| TOON alongside JSON | Token efficiency for LLM payloads without breaking standard API compatibility |
| HTTP/3 primary | Lower latency for streaming, multiplexed connections, better mobile/unreliable network performance |
| EventBus (in-process) + Messaging (distributed) | Zero overhead locally, Kafka durability when distributed — same code paths |
| Dynamic scheduling over static config | Hosts may change capabilities (GPU load, available RAM) — adapt continuously |
| Challenges + HelixQA over custom test framework | Existing, proven modules with rich features (assertions, reports, evidence, banks) |
| No CI/CD pipelines | Manual control via Makefile — developer decides when to test and deploy |
| 100% coverage mandate | Critical system handling LLM inference — untested code is unacceptable |
