---
title: "Configuration Reference"
weight: 1
bookToC: true
---


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
| `HELIX_AUTH_JWT_SECRET` | (empty) | HS256 signing key for JWT auth. Minimum 32 bytes (`openssl rand -hex 32`); the server refuses to start on a shorter one. Leave empty to disable JWT auth. **When set, a credential is required even if `HELIX_AUTH_API_KEYS` is empty** — see [Security](../manual/security/). |
| `HELIX_AUTH_JWT_TTL_MINUTES` | `1440` | Lifetime of tokens issued by `POST /v1/auth/token`, in minutes. Ignored when the secret is unset. |
| `HELIX_AUTH_API_KEYS` | (empty) | Comma-separated list of valid API keys for Bearer token auth. Leave empty for open access. |

When `HELIX_AUTH_API_KEYS` **or** `HELIX_AUTH_JWT_SECRET` is set, all `/v1/*` endpoints require
`Authorization: Bearer <credential>` — a configured API key or a valid JWT. With both unset the
server is in open-access mode and says so at WARN on every startup.

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

---

## Concurrency Limits

Concurrency settings control how many simultaneous operations each subsystem allows. These are critical for preventing resource exhaustion under load and for tuning throughput on hardware with known constraints.

| Variable | Default | Description |
|----------|---------|-------------|
| `HELIX_LLM_MAX_CONCURRENT` | `10` | Maximum concurrent LLM inference requests across all providers. Requests exceeding this limit are queued. Set to `0` for unlimited. |
| `HELIX_EMBEDDING_MAX_CONCURRENT` | `20` | Maximum concurrent embedding operations. Higher than LLM concurrency because embeddings are cheaper and faster. Set to `0` for unlimited. |
| `HELIX_AGENT_MAX_CONCURRENT_TOOLS` | `5` | Maximum concurrent tool executions within a single agent ReAct loop. Limits parallel tool calls when the LLM requests multiple tools in one turn. |
| `HELIX_SSH_MAX_CONCURRENT` | `10` | Maximum concurrent SSH connections to remote hosts. Applies to the control plane prober, deployer, and monitor. |

All concurrency limits use semaphore-based flow control. When the limit is reached, new operations block until a slot becomes available. A value of `0` disables the limit (unlimited concurrency).

### Tuning Guidelines

**LLM concurrency (`HELIX_LLM_MAX_CONCURRENT`):**
- For local llama.cpp with a single GPU: set to `1`-`3` depending on VRAM
- For cloud providers (OpenAI, Anthropic): set to `10`-`50` depending on API tier
- For mixed local + cloud with `auto` routing: set to `10` (the default balances both)

**Embedding concurrency (`HELIX_EMBEDDING_MAX_CONCURRENT`):**
- For local embedding models: match to CPU core count
- For cloud embedding APIs: set to `20`-`100` (API rate limits are the real constraint)

**Tool concurrency (`HELIX_AGENT_MAX_CONCURRENT_TOOLS`):**
- Keep low (`3`-`5`) to avoid overwhelming downstream services
- Each tool execution may trigger network calls, database queries, or LLM requests

**SSH concurrency (`HELIX_SSH_MAX_CONCURRENT`):**
- Match to the number of cluster hosts (no benefit exceeding the host count)
- Lower values reduce load on target hosts during mass operations

## Lazy Infrastructure

| Variable | Default | Description |
|----------|---------|-------------|
| `HELIX_LAZY_INFRA` | `false` | Enable lazy initialization of infrastructure components. When true, containers and services start on first request rather than at boot time. |
| `HELIX_IDLE_SHUTDOWN_MINUTES` | `0` | Minutes of inactivity before idle services are shut down. Set to `0` to disable idle shutdown. Only effective when `HELIX_LAZY_INFRA=true`. |

When `HELIX_LAZY_INFRA=true`, the system defers starting infrastructure components (containers, database connections, vector store clients) until they receive their first request. Combined with `HELIX_IDLE_SHUTDOWN_MINUTES`, this enables a serverless-like lifecycle where resources are consumed only when needed.

Example for development environments:

```bash
HELIX_LAZY_INFRA=true
HELIX_IDLE_SHUTDOWN_MINUTES=15
```

This starts with minimal resource usage and shuts down idle components after 15 minutes of inactivity. The next request triggers a lazy restart.

## Analytics

| Variable | Default | Description |
|----------|---------|-------------|
| `HELIX_CLICKHOUSE_ADDR` | (empty) | ClickHouse server address (`host:port`). Leave empty to disable analytics collection. |
| `HELIX_CLICKHOUSE_DATABASE` | `helixllm` | ClickHouse database name for analytics events. |

When `HELIX_CLICKHOUSE_ADDR` is set, the analytics collector writes events (request metrics, provider latencies, error rates) to ClickHouse. If ClickHouse is unavailable at startup, the system falls back gracefully and logs a warning.

## Proxy Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `HELIX_HTTP_PROXY` | (empty) | HTTP proxy for outbound connections. |
| `HELIX_HTTPS_PROXY` | (empty) | HTTPS proxy for outbound connections (used by LLM providers). |
| `HELIX_NO_PROXY` | (empty) | Comma-separated list of hosts that bypass the proxy. |
| `HELIX_SOCKS_PROXY` | (empty) | SOCKS5 proxy for outbound connections. |

Proxy settings affect all outbound HTTP calls, including LLM provider API requests, embedding API calls, and vector database connections. Internal cluster communication (gRPC, SSH) is not affected by these settings.

## Configuration Loading Precedence

Configuration values are resolved from multiple sources. When the same setting is defined in multiple places, later sources override earlier ones:

```
1. Struct tag defaults (compiled into the binary)
   |
   v  overridden by
2. .env file (in the working directory)
   |
   v  overridden by
3. Environment variables (shell, systemd, container runtime)
   |
   v  overridden by
4. CLI flags (--mode, --port, etc.)
```

**Struct tag defaults** are defined in `internal/shared/config/config.go` using `default:"..."` tags on each field. These provide sensible values for single-host development and are always present.

**The `.env` file** is loaded from the working directory at startup. It is read once; changes to `.env` require a restart. The `.env` file is gitignored and should never be committed.

**Environment variables** from the shell or container runtime override `.env` values. This is the recommended mechanism for production deployments (e.g., via systemd `EnvironmentFile`, Kubernetes ConfigMaps, or container `--env` flags).

**CLI flags** have the highest precedence. Currently `--mode` overrides `HELIX_MODE`. Not all variables have CLI flag equivalents.

### Precedence Example

Given:

```
# Struct tag default: HELIX_PORT = 8443
# .env file:          HELIX_PORT=9443
# Shell:              export HELIX_PORT=7443
# CLI:                (no --port flag)
```

The effective value is `7443` (environment variable overrides `.env`, which overrides the default).

## Hot-Reload Behavior

HelixLLM does not support full hot-reload of configuration. The following summarizes what can and cannot be changed without a restart:

### Requires Restart

- `HELIX_MODE` -- Mode determines which layers are initialized at startup
- `HELIX_PORT`, `HELIX_HOST` -- Server bind address and port
- `HELIX_TLS_CERT`, `HELIX_TLS_KEY` -- TLS certificates
- `HELIX_DB_*` -- Database connection parameters
- `HELIX_REDIS_*` -- Redis connection parameters
- `HELIX_KAFKA_BROKERS` -- Kafka broker addresses
- `HELIX_OTEL_EXPORTER`, `HELIX_OTEL_ENDPOINT` -- Tracing configuration
- `HELIX_FEATURE_*` -- Feature flags
- All concurrency limits (`HELIX_LLM_MAX_CONCURRENT`, etc.)

### Changeable via API (No Restart)

- **API keys:** Keys in `HELIX_AUTH_API_KEYS` are validated per-request from the loaded config. To rotate keys, update the environment and restart.
- **Log level:** Can be changed at runtime by sending a signal or via the internal management API, if available in the deployment.

### Graceful Restart

For production deployments, use a graceful restart to apply configuration changes without dropping active connections:

```bash
# Send SIGTERM -- the server finishes in-flight requests, then exits
kill -TERM $(pidof helixllm)

# A process manager (systemd, container runtime) restarts with the new config
```

The server handles `SIGTERM` and `SIGINT` with graceful shutdown: it stops accepting new connections, waits for active requests to complete (with a timeout), and then exits cleanly.
