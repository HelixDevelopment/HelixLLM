# Modules Inventory

HelixLLM uses 43 Go submodules from the vasic-digital ecosystem. Each module is a separate Git repository vendored under `submodules/` and referenced via `replace` directives in `go.mod`.

## Gateway Layer

| Module | Go Path | Directory | Description |
|--------|---------|-----------|-------------|
| Streaming | `digital.vasic.streaming` | `submodules/Streaming` | SSE broker, WebSocket hub, gRPC streaming, and Gin adapters for real-time communication |
| Middleware | `digital.vasic.middleware` | `submodules/Middleware` | Standard HTTP middleware: CORS, request logging, panic recovery, request ID generation |
| Auth | `digital.vasic.auth` | `submodules/Auth` | JWT token lifecycle, API key validation, OAuth2 flows, Bearer/API-key middleware for Gin |
| RateLimiter | `digital.vasic.ratelimiter` | `submodules/RateLimiter` | Sliding-window rate limiting with in-memory and Redis-backed stores, Gin middleware |
| Security | `digital.vasic.security` | `submodules/Security` | Content guardrails, PII detection and redaction, HTTP security headers, AES-256-GCM encryption |
| I18n | `digital.vasic.i18n` | `submodules/I18n` | Multi-language API response localization, Accept-Language middleware |
| TOON | `digital.vasic.toon` | `submodules/TOON` | Token-Oriented Object Notation wrapper around toon-format/toon-go for LLM-efficient serialization |
| Formatters | `digital.vasic.formatters` | `submodules/Formatters` | Response formatting pipeline supporting JSON, TOON, and custom output formats |

## Brain Layer

| Module | Go Path | Directory | Description |
|--------|---------|-----------|-------------|
| LLMProvider | `digital.vasic.llmprovider` | `submodules/LLMProvider` | Unified LLM interface with 40+ provider adapters, circuit breakers, and health monitoring |
| Optimization | `digital.vasic.optimization` | `submodules/Optimization` | Semantic cache (gptcache-style cosine matching), prompt compression, streaming flush buffers |
| Cache | `digital.vasic.cache` | `submodules/Cache` | Response caching with Redis and in-memory (LRU, TTL) backends, distributed cache patterns |
| Recovery | `digital.vasic.recovery` | `submodules/Recovery` | Circuit breakers per LLM backend with state callbacks, health checker, resilience facade |

## Knowledge Layer

| Module | Go Path | Directory | Description |
|--------|---------|-----------|-------------|
| RAG | `digital.vasic.rag` | `submodules/RAG` | Full RAG pipeline: recursive/sentence/fixed-size chunking, hybrid retrieval (BM25 + semantic), MMR reranking, pipeline builder |
| VectorDB | `digital.vasic.vectordb` | `submodules/VectorDB` | Unified VectorStore interface for Qdrant, Pinecone, Milvus, and pgvector |
| Embeddings | `digital.vasic.embeddings` | `submodules/Embeddings` | Multi-provider embedding generation: OpenAI, Cohere, Voyage, Jina, Vertex, Bedrock |
| Document | `digital.vasic.document` | `submodules/Document` | Document model with 18-format detection, content extraction, and change tracking |
| Filesystem | `digital.vasic.filesystem` | `submodules/Filesystem` | Multi-protocol file access: SMB, FTP, WebDAV, NFS, and local filesystem |
| Database | `digital.vasic.database` | `submodules/Database` | PostgreSQL (pgx) and SQLite, schema migrations, generic Repository[T] pattern |
| BackgroundTasks | `digital.vasic.background` | `submodules/BackgroundTasks` | PostgreSQL-backed async task queue with progress reporting, resumable tasks |

## Agents Layer

| Module | Go Path | Directory | Description |
|--------|---------|-----------|-------------|
| Agentic | `digital.vasic.agentic` | `submodules/Agentic` | Graph-based workflow engine with 6 node types, checkpointing, and self-correction loops |
| Planning | `digital.vasic.planning` | `submodules/Planning` | HiPlan hierarchical planning, Monte Carlo Tree Search, Tree of Thoughts algorithms |
| ToolSchema | `digital.vasic.toolschema` | `submodules/ToolSchema` | Tool registry with 14+ built-in handlers, JSON Schema validation, safety guards, timeout enforcement |
| Conversation | `digital.vasic.conversation` | `submodules/conversation` | Infinite context management via Kafka event sourcing, LLM-based compression, LRU cache |
| SkillRegistry | `dev.helix.agent/skillregistry` | `submodules/SkillRegistry` | Skill loading from YAML/JSON/Markdown, semver validation, dependency resolution, execution hooks |
| MCP Module | `digital.vasic.mcp` | `submodules/MCP_Module` | Model Context Protocol client/server, JSON-RPC 2.0, stdio/HTTP/SSE transport |
| LLMOrchestrator | `digital.vasic.llmorchestrator` | `submodules/LLMOrchestrator` | Headless CLI agent management, hybrid pipe+file communication, capability-based agent pool |
| Memory | `digital.vasic.memory` | `submodules/Memory` | Mem0-style memory system: importance scoring, knowledge graph, entity extraction |

## Control Plane

| Module | Go Path | Directory | Description |
|--------|---------|-----------|-------------|
| Containers | `digital.vasic.containers` | `submodules/Containers` | Docker/Podman/K8s container management, SSH distribution, resource scheduling, TUI monitor |
| Discovery | `digital.vasic.discovery` | (transitive) | Network and service discovery with pluggable scanners |
| Benchmark | `digital.vasic.benchmark` | (transitive) | LLM benchmarking (SWE-bench, HumanEval, MMLU), host capability profiling |
| LLMOps | `digital.vasic.llmops` | (transitive) | Continuous evaluation pipelines, A/B experiments, prompt versioning, quality alerting |
| SelfImprove | `digital.vasic.selfimprove` | (transitive) | RLAIF pipeline, multi-dimension reward scoring, constitutional self-critique |

## Shared Foundation

| Module | Go Path | Directory | Description |
|--------|---------|-----------|-------------|
| Config | `digital.vasic.config` | `submodules/Config` | JSON and .env config loading, struct tag binding, composable validation rules |
| EventBus | `digital.vasic.eventbus` | `submodules/EventBus` | In-process pub/sub, typed topics, filter combinators, middleware hooks |
| Messaging | `digital.vasic.messaging` | (transitive) | Kafka/RabbitMQ broker abstraction, consumer groups, retry with dead letter queues |
| Observability | `digital.vasic.observability` | `submodules/Observability` | OpenTelemetry tracing, Prometheus metrics, structured logging, health aggregation |
| Concurrency | `digital.vasic.concurrency` | `submodules/Concurrency` | Worker pools, priority queues, semaphores, circuit breakers |
| Lazy | `digital.vasic.lazy` | `submodules/Lazy` | Thread-safe lazy initialization using sync.Once generics |
| Watcher | `digital.vasic.watcher` | `submodules/Watcher` | File and config change monitoring with debounced handler callbacks |
| Plugins | `digital.vasic.plugins` | (transitive) | Plugin lifecycle management, dependency ordering (Kahn's algorithm), shared object loading |
| Proxy | `digital.vasic.proxy` | (transitive) | HTTP and SOCKS5 proxy for external API access through restricted networks |

## Testing and QA

| Module | Go Path | Directory | Description |
|--------|---------|-----------|-------------|
| Challenges | `digital.vasic.challenges` | `submodules/Challenges` | Test challenge framework: assertion engine, multi-format reports (Markdown, HTML, JSON), shell adapter |
| HelixQA | `digital.vasic.helixqa` | `submodules/HelixQA` | QA orchestration: cross-platform validation, crash detection, evidence collection, YAML test banks |

## Dependency Graph

Key dependency relationships between modules:

```
HelixLLM (main binary)
  |
  +-- Config (env loading)
  +-- EventBus (in-process events)
  +-- Observability (tracing, metrics)
  |
  +-- Gateway Layer
  |     +-- Streaming (SSE, WebSocket)
  |     +-- Middleware (CORS, logging)
  |     +-- Auth (JWT, API keys)
  |     +-- RateLimiter
  |     +-- Security (PII, headers)
  |
  +-- Brain Layer
  |     +-- LLMProvider (unified interface)
  |     +-- Optimization (cache, compression)
  |     +-- Recovery (circuit breakers)
  |
  +-- Knowledge Layer
  |     +-- RAG (pipeline)
  |     +-- VectorDB (storage)
  |     +-- Embeddings (encoding)
  |     +-- Document (parsing)
  |
  +-- Agents Layer
  |     +-- Agentic (workflows)
  |     +-- ToolSchema (tools)
  |     +-- MCP Module (external tools)
  |     +-- Conversation (context)
  |     +-- Memory (persistence)
  |
  +-- Control Plane
        +-- Containers (deployment)
        +-- Benchmark (profiling)
```

## Updating Modules

Update a single module:

```bash
cd submodules/Config
git pull origin main
cd ../..
go mod tidy
```

Update all modules:

```bash
make deps
```

This runs `git submodule update --init --recursive` followed by `go mod tidy`.

## Adding a New Module

1. Add the Git submodule:
   ```bash
   git submodule add <repo-url> submodules/NewModule
   ```

2. Add a `replace` directive in `go.mod`:
   ```
   replace digital.vasic.newmodule => ./submodules/NewModule
   ```

3. Import and use in your code:
   ```go
   import "digital.vasic.newmodule"
   ```

4. Run `go mod tidy` to resolve dependencies.
