# Configuration Reference

HelixLLM is configured through environment variables. Copy `.env.example` to `.env` and set your values. All variables have sensible defaults for single-host development.

## Loading Precedence

Configuration is resolved in this order (later sources override earlier ones):

1. Struct tag defaults (compiled into the binary)
2. `.env` file in the working directory
3. Environment variables (from the shell or container runtime)
4. CLI flags (`--mode` overrides `HELIX_MODE`)

## Mode

| Variable | Default | Valid Values | Description |
|----------|---------|--------------|-------------|
| `HELIX_MODE` | `full` | `full`, `gateway`, `brain`, `knowledge`, `agents`, `control` | Operating mode. `full` runs all subsystems in a single process. Other modes activate only the named subsystem for distributed deployment. |

The mode can also be set with the `--mode` CLI flag, which takes precedence over the environment variable.

## Cluster

| Variable | Default | Description |
|----------|---------|-------------|
| `HELIX_HOSTS` | `nezha.local` | Comma-separated list of cluster hosts. The control plane probes these via SSH. |
| `HELIX_SSH_USER` | `milosvasic` | SSH username for connecting to remote hosts. |
| `HELIX_SSH_KEY` | `~/.ssh/id_ed25519` | Path to the SSH private key for host access. Must be key-based (no password). |

## Container Runtime

| Variable | Default | Valid Values | Description |
|----------|---------|--------------|-------------|
| `HELIX_CONTAINER_RUNTIME` | `auto` | `auto`, `podman`, `docker` | Container runtime. `auto` detects Podman first, then Docker. |

## Scheduling

| Variable | Default | Valid Values | Description |
|----------|---------|--------------|-------------|
| `HELIX_SCHEDULE_STRATEGY` | `auto` | `auto`, `binpack`, `spread`, `gpu-affinity`, `memory-first`, `latency` | Scheduling strategy for service placement. `auto` selects the best strategy per service based on resource requirements. |

Strategy details:
- **binpack** -- Pack services onto the fewest hosts (saves resources)
- **spread** -- Spread services across all hosts (high availability)
- **gpu-affinity** -- Place GPU workloads on hosts with the best GPU
- **memory-first** -- Place memory-intensive services on hosts with the most RAM
- **latency** -- Place latency-sensitive services near clients

## Server

| Variable | Default | Description |
|----------|---------|-------------|
| `HELIX_HOST` | `0.0.0.0` | Bind address for the HTTP server. |
| `HELIX_PORT` | `8443` | TCP/UDP port for HTTP/3 (QUIC) and HTTP/2 (TLS). |
| `HELIX_TLS_CERT` | `./certs/cert.pem` | Path to PEM-encoded TLS certificate. |
| `HELIX_TLS_KEY` | `./certs/key.pem` | Path to PEM-encoded TLS private key. |

TLS is required. For local development, `make certs` (or `make dev`) generates a self-signed certificate.

## LLM Providers

| Variable | Default | Description |
|----------|---------|-------------|
| `HELIX_LLM_LOCAL_MODEL` | `Llama-3.1-70B-Instruct-Q4_K_M` | Default local model for llama.cpp inference. |
| `HELIX_LLM_LOCAL_RPC_PORT` | `50052` | RPC port for the llama.cpp server. |
| `HELIX_LLM_OPENAI_KEY` | (empty) | OpenAI API key. Leave empty to disable. |
| `HELIX_LLM_ANTHROPIC_KEY` | (empty) | Anthropic API key. Leave empty to disable. |
| `HELIX_LLM_DEFAULT_PROVIDER` | `local` | Default LLM provider: `local`, `openai`, `anthropic`, or `auto`. |

When `HELIX_LLM_DEFAULT_PROVIDER=auto`, the router selects a provider based on the model name in the request:
- Models starting with `gpt` or `o1` route to OpenAI
- Models starting with `claude` route to Anthropic
- All others route to the local llama.cpp instance

## Knowledge / RAG

| Variable | Default | Valid Values | Description |
|----------|---------|--------------|-------------|
| `HELIX_VECTOR_DB` | `qdrant` | `qdrant`, `pgvector`, `milvus`, `pinecone` | Vector database backend. |
| `HELIX_EMBEDDING_PROVIDER` | `local` | `local`, `openai`, `cohere`, `voyage`, `jina` | Embedding model provider. |
| `HELIX_EMBEDDING_MODEL` | `all-mpnet-base-v2` | (model name) | Embedding model name. |
| `HELIX_RAG_CHUNK_SIZE` | `1000` | (integer) | Maximum characters per text chunk during ingestion. |
| `HELIX_RAG_CHUNK_OVERLAP` | `200` | (integer) | Character overlap between adjacent chunks. |
| `HELIX_RAG_TOP_K` | `5` | (integer) | Number of top results returned by vector search. |

## Database

| Variable | Default | Description |
|----------|---------|-------------|
| `HELIX_DB_HOST` | `localhost` | PostgreSQL host. |
| `HELIX_DB_PORT` | `5432` | PostgreSQL port. |
| `HELIX_DB_NAME` | `helixllm` | Database name. |
| `HELIX_DB_USER` | `helix` | Database user. |
| `HELIX_DB_PASSWORD` | (empty) | Database password. |

## Cache

| Variable | Default | Description |
|----------|---------|-------------|
| `HELIX_REDIS_HOST` | `localhost` | Redis host. |
| `HELIX_REDIS_PORT` | `6379` | Redis port. |
| `HELIX_REDIS_PASSWORD` | (empty) | Redis password. |

## Messaging

| Variable | Default | Description |
|----------|---------|-------------|
| `HELIX_KAFKA_BROKERS` | `localhost:9092` | Comma-separated Kafka broker addresses. |

## Observability

| Variable | Default | Valid Values | Description |
|----------|---------|--------------|-------------|
| `HELIX_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` | Logging verbosity. |
| `HELIX_LOG_FORMAT` | `text` | `text`, `json` | Log output format. Use `json` for structured logging in production. |
| `HELIX_OTEL_EXPORTER` | `none` | `none`, `stdout`, `otlp`, `jaeger`, `zipkin` | OpenTelemetry trace exporter. |
| `HELIX_OTEL_ENDPOINT` | `http://localhost:4317` | (URL) | OpenTelemetry collector endpoint (gRPC). |
| `HELIX_PROMETHEUS_PORT` | `9090` | (integer) | Prometheus server port. |
| `HELIX_GRAFANA_PORT` | `3001` | (integer) | Grafana dashboard port. |

## Authentication

| Variable | Default | Description |
|----------|---------|-------------|
| `HELIX_AUTH_JWT_SECRET` | (empty) | Secret key for JWT token signing. Leave empty to disable JWT auth. |
| `HELIX_AUTH_API_KEYS` | (empty) | Comma-separated list of valid API keys for Bearer token auth. Leave empty for open access. |

When `HELIX_AUTH_API_KEYS` is set, all `/v1/*` endpoints require `Authorization: Bearer <key>`.

## Feature Flags

| Variable | Default | Description |
|----------|---------|-------------|
| `HELIX_FEATURE_GRPC` | `true` | Enable gRPC streaming support. |
| `HELIX_FEATURE_TOON` | `true` | Enable TOON (Token-Oriented Object Notation) content negotiation. |
| `HELIX_FEATURE_SELFIMPROVE` | `false` | Enable RLAIF self-improvement pipeline. Experimental. |

## Example Minimal Configuration

For a single-host setup with local LLM only:

```bash
HELIX_MODE=full
HELIX_PORT=8443
HELIX_LLM_DEFAULT_PROVIDER=local
HELIX_LOG_LEVEL=info
```

## Example Multi-Host Configuration

```bash
HELIX_MODE=control
HELIX_HOSTS=nezha.local,thinker.local,amber.local
HELIX_SSH_USER=milosvasic
HELIX_SSH_KEY=~/.ssh/id_ed25519
HELIX_SCHEDULE_STRATEGY=auto
HELIX_LLM_DEFAULT_PROVIDER=auto
HELIX_LLM_OPENAI_KEY=sk-...
HELIX_LLM_ANTHROPIC_KEY=sk-ant-...
```
