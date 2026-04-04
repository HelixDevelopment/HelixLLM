# Architecture

HelixLLM is a layered monolith with modal distribution. A single Go binary compiles all layers together and uses a mode system to activate the appropriate subsystems at runtime.

## Design Principles

- **Single binary, multiple modes:** One build, one test suite, deployable as a monolith or distributed across hosts
- **Go + Gin Gonic:** Type safety, native concurrency, single binary deployment
- **Containers first:** Podman preferred, Docker as fallback, auto-detected
- **HTTP/3 primary:** QUIC for lower latency, HTTP/2 fallback via ALPN negotiation
- **Zero overhead in full mode:** Direct function calls between layers, no serialization or network hops

## Layers

```
+-----------------------------------------------------------+
|                     Gateway Layer                          |
|  HTTP/3 Server, OpenAI/Anthropic APIs, Auth, Streaming    |
+-----------------------------------------------------------+
|                      Brain Layer                           |
|  LLM Routing, llama.cpp, OpenAI, Anthropic, Optimization  |
+-----------------------------------------------------------+
|                    Knowledge Layer                         |
|  RAG Pipeline, Embeddings, Vector Store, Chunking          |
+-----------------------------------------------------------+
|                     Agents Layer                           |
|  ReAct Loop, Tools, Conversation Context, RAG Hook         |
+-----------------------------------------------------------+
|                   Control Plane Layer                      |
|  Host Probing, Scheduling, Deployment, Monitoring          |
+-----------------------------------------------------------+
|                    Shared Foundation                       |
|  Config, Events, Logging, Observability, Health            |
+-----------------------------------------------------------+
```

## Mode System

The mode is set via `HELIX_MODE` environment variable or `--mode` CLI flag:

| Mode | Layers Active | Use Case |
|------|---------------|----------|
| `full` | All | Single-host development and production |
| `gateway` | Gateway + Shared | Dedicated API frontend |
| `brain` | Brain + Shared | Dedicated LLM inference node |
| `knowledge` | Knowledge + Shared | Dedicated RAG pipeline |
| `agents` | Agents + Shared | Dedicated agent workers |
| `control` | Control + Shared | Cluster management node |

Mode parsing is in `internal/mode/mode.go`. The main function in `cmd/helixllm/main.go` always initializes all layers in the current implementation (full mode). In distributed deployment, each host runs the binary in a specific mode.

## Communication

### Full Mode (Single Host)

All layers are in the same process. They communicate through direct Go function calls:

```
Gateway Handler -> brain.Brain.ChatCompletion() -> direct return
```

No serialization, no network, no latency overhead.

### Distributed Mode (Multi-Host)

When layers run on separate hosts, communication uses:

- **gRPC streaming** (`digital.vasic.streaming`): Real-time inter-layer calls
- **Kafka messaging** (`digital.vasic.messaging`): Async events with durability
- **EventBus** (`digital.vasic.eventbus`): In-process pub/sub within each host

## Request Flow

### Chat Completion

```
Client
  |
  v
Gateway (router.go)
  | Auth middleware (API key validation)
  | Rate limiting middleware
  | Security headers middleware
  | Content negotiation middleware
  |
  v
HandleChatCompletions (openai.go)
  | Parse OpenAI-format request
  | Convert to internal types
  |
  v
Brain (brain.go)
  | Route to provider (by model name, capability, cost)
  |
  v
Provider (llamacpp.go | openai_provider.go | anthropic_provider.go)
  | Send to LLM backend
  | Receive response
  |
  v
Gateway
  | Convert to OpenAI-format response
  | Compress (Brotli/gzip)
  | Stream via SSE if requested
  |
  v
Client
```

### Agent Chat

```
Client
  |
  v
Agent API (api.go)
  | Load session history (if session_id provided)
  | Append new messages
  |
  v
Agent (agent.go) -- ReAct Loop
  | 1. RAG hook: augment with knowledge context
  | 2. Send to Brain for reasoning
  | 3. Check response for tool calls
  |    If tool call: execute tool, append observation, loop
  |    If final answer: break
  | 4. Repeat (max 10 turns)
  |
  v
Agent API
  | Save to session context
  | Return response
  |
  v
Client
```

### Document Ingestion

```
Client
  |
  v
Knowledge API (api.go)
  | Validate request (content, collection not empty)
  |
  v
Pipeline (pipeline.go)
  | 1. Chunk document (FixedSizeChunker)
  | 2. Embed each chunk (Embedder)
  | 3. Store vectors (VectorStore)
  |
  v
Client <- IngestResult (id, collection, chunk count)
```

## Server Architecture

The server (`internal/server/server.go`) runs two transports simultaneously:

- **HTTP/3:** QUIC over UDP via `quic-go/http3`
- **HTTP/2:** TLS over TCP via standard `net/http`

Both serve the same Gin engine. The `Alt-Svc` header advertises HTTP/3 on every response so clients can upgrade.

TLS 1.3 is the minimum version. ALPN negotiation supports `h3`, `h2`, and `http/1.1`.

Standard middleware applied to all requests:
- `gin.Recovery()` -- panic recovery
- `middleware.RequestID()` -- unique request ID generation
- `middleware.Compression()` -- Brotli/gzip response compression

## Gateway Layer

Defined in `internal/gateway/`:

- **router.go** -- Registers `/v1/*` routes with auth, rate limiting, security headers, and content negotiation middleware
- **openai.go** -- OpenAI-compatible handlers (chat completions, completions, models, embeddings)
- **anthropic.go** -- Anthropic-compatible handler (messages)
- **streaming.go** -- SSE streaming utilities

All `/v1/*` routes go through:
1. `APIKeyAuth` -- validates `Authorization: Bearer` tokens
2. `RateLimit` -- sliding-window rate limiting per IP
3. `SecurityHeaders` -- sets security-related HTTP headers
4. `ContentNegotiation` -- handles Accept/Content-Type for TOON/JSON

## Brain Layer

Defined in `internal/brain/`:

- **brain.go** -- Orchestrator that holds providers and routes requests
- **router.go** -- Intelligent routing by model name, capability, cost, latency
- **provider.go** -- Common provider interface
- **llamacpp.go** -- llama.cpp HTTP client adapter
- **openai_provider.go** -- OpenAI API client
- **anthropic_provider.go** -- Anthropic API client

The Brain creates provider instances at startup based on configuration. When a key is provided, that provider is registered. The router uses the model name in each request to select the provider.

## Knowledge Layer

Defined in `internal/knowledge/`:

- **pipeline.go** -- Orchestrates ingestion and query flows
- **api.go** -- HTTP handlers for `/internal/knowledge/*`
- **embedder.go** -- Embedding interface and hash-based implementation
- **store.go** -- VectorStore interface and in-memory implementation
- **chunker.go** -- Document chunking (fixed-size with overlap)

## Agents Layer

Defined in `internal/agents/`:

- **agent.go** -- ReAct loop implementation
- **api.go** -- HTTP handlers for `/v1/agents/*`
- **tool.go** -- Tool interface and registry
- **context.go** -- Conversation session management
- **tools/** -- Built-in tool implementations (echo, time, knowledge_query)

## Control Plane

Defined in `internal/control/`:

- **api.go** -- HTTP handlers for `/internal/cluster/*` and ControlPlane orchestrator
- **prober.go** -- SSH-based host capability detection
- **scheduler.go** -- Service placement with 5 strategies
- **deployer.go** -- Container deployment via SSH
- **monitor.go** -- Continuous cluster health monitoring
- **types.go** -- HostProfile, ServiceRequirement, PlacementResult, etc.

## Shared Foundation

Defined in `internal/shared/`:

- **config/** -- Configuration loading from environment variables with struct tag defaults
- **events/** -- EventBus setup with typed topics (ServerStarted, ServerStopped)
- **health/** -- Health checker with aggregated status reporting
- **logging/** -- Structured logger with level and format configuration
- **observability/** -- OpenTelemetry tracer setup with multiple exporters

## Package Structure

Public types live in `pkg/`:

- **pkg/api/** -- OpenAI and Anthropic request/response types
- **pkg/types/** -- Internal message and response types shared across layers

## Submodule Architecture

43 Git submodules in `submodules/` provide the production infrastructure. They are referenced via `replace` directives in `go.mod`:

```
replace digital.vasic.config => ./submodules/Config
replace digital.vasic.eventbus => ./submodules/EventBus
...
```

This allows local development against all modules while maintaining them as independent repositories.
