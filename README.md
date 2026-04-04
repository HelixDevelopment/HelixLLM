# HelixLLM

Enterprise-grade distributed LLM system built in Go with Gin Gonic. A single binary with a mode system that enables flexible deployment from single-host development to multi-host production clusters.

HelixLLM provides fully compatible OpenAI and Anthropic APIs, local LLM inference via llama.cpp, a RAG knowledge pipeline, a ReAct agent system with tool calling, and a control plane for multi-host cluster management -- all served over HTTP/3 with automatic HTTP/2 fallback.

## Key Features

- **OpenAI and Anthropic compatible APIs** -- any existing SDK client works without modification
- **Local LLM inference** via llama.cpp with CUDA, Metal, and ROCm support
- **Cloud provider routing** to OpenAI and Anthropic with intelligent fallback chains
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

## Configuration

Configuration is loaded from environment variables with sensible defaults. Copy `.env.example` to `.env` and customize:

```bash
HELIX_MODE=full                          # Operating mode
HELIX_PORT=8443                          # Server port
HELIX_LLM_DEFAULT_PROVIDER=local         # local | openai | anthropic | auto
HELIX_LLM_OPENAI_KEY=sk-...             # OpenAI API key (optional)
HELIX_LLM_ANTHROPIC_KEY=sk-ant-...      # Anthropic API key (optional)
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

## License

All rights reserved. See LICENSE for details.
