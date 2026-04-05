# Lesson 4: Configuration

**Duration:** 20 minutes
**Prerequisites:** Lesson 2 (Installation)
**Learning Objectives:**
- Understand the configuration loading precedence
- Configure the mode system for different deployment scenarios
- Set up LLM providers and vector database backends
- Use feature flags to enable or disable subsystems

---

## Scene 1: Configuration Precedence (3 min)

**Narration:** "HelixLLM is configured entirely through environment variables. There are four levels of configuration, and later sources override earlier ones."

**Screen:** Show the precedence diagram.

**Key points:**
1. **Struct tag defaults** -- compiled into the binary, always present
2. **`.env` file** -- in the working directory, loaded at startup
3. **Environment variables** -- from the shell or container runtime
4. **CLI flags** -- highest priority, `--mode` overrides `HELIX_MODE`

**Demo steps:**

```bash
# Copy the example configuration
cp .env.example .env

# View the file
cat .env
```

**Narration:** "The .env file is the primary way to configure HelixLLM. It is gitignored so you never accidentally commit secrets. Let us walk through the major sections."

---

## Scene 2: Mode Configuration (4 min)

**Narration:** "The most important variable is HELIX_MODE. It controls which layers are active in the running process."

**Screen:** Show the mode table.

| Mode | Layers Active | Use Case |
|------|---------------|----------|
| `full` | All | Single-host development and production |
| `gateway` | Gateway + Shared | Dedicated API frontend |
| `brain` | Brain + Shared | Dedicated LLM inference node |
| `knowledge` | Knowledge + Shared | Dedicated RAG pipeline |
| `agents` | Agents + Shared | Dedicated agent workers |
| `control` | Control + Shared | Cluster management node |

**Demo steps:**

```bash
# Default: full mode (all layers active)
HELIX_MODE=full

# Run as a gateway-only node
HELIX_MODE=gateway ./bin/helixllm

# Override with CLI flag
./bin/helixllm --mode=brain
```

**Key points:**
- `full` is the default and recommended for getting started
- In distributed deployments, each host runs a different mode
- CLI flag `--mode` takes precedence over the environment variable

---

## Scene 3: Server and TLS Settings (3 min)

**Narration:** "The server section controls where HelixLLM listens and how TLS is configured."

**Screen:** Show the server variables in the .env file.

```bash
# Server binding
HELIX_HOST=0.0.0.0        # Bind address
HELIX_PORT=8443            # TLS port (HTTP/3 + HTTP/2)

# TLS certificates
HELIX_TLS_CERT=./certs/cert.pem
HELIX_TLS_KEY=./certs/key.pem
```

**Narration:** "TLS is mandatory. The server runs HTTP/3 over QUIC and HTTP/2 over TCP simultaneously on the same port. There is no plaintext HTTP option -- this is a deliberate security decision."

**Key points:**
- Port 8443 is the default for both HTTP/3 (UDP) and HTTP/2 (TCP)
- TLS 1.3 is the minimum version
- `make certs` generates self-signed certificates for development
- Production deployments should use certificates from a trusted CA

---

## Scene 4: LLM Provider Configuration (4 min)

**Narration:** "The brain layer supports three LLM backends. You configure them by setting API keys and choosing a default provider."

**Demo steps:**

```bash
# Provider configuration
HELIX_LLM_DEFAULT_PROVIDER=local       # local | openai | anthropic | auto
HELIX_LLM_LOCAL_MODEL=Llama-3.1-70B-Instruct-Q4_K_M
HELIX_LLM_LOCAL_RPC_PORT=50052

# Cloud provider keys (leave empty to disable)
HELIX_LLM_OPENAI_KEY=sk-your-openai-key
HELIX_LLM_ANTHROPIC_KEY=sk-ant-your-anthropic-key
```

**Narration:** "When the default provider is set to auto, HelixLLM routes requests based on the model name. Models starting with gpt or o1 go to OpenAI. Models starting with claude go to Anthropic. Everything else goes to the local llama.cpp instance."

**Screen:** Show the routing logic.

| Model Name Pattern | Routed To |
|--------------------|-----------|
| `gpt-*`, `o1-*` | OpenAI |
| `claude-*` | Anthropic |
| Everything else | Local llama.cpp |

**Key points:**
- Only providers with keys configured are registered
- `auto` mode enables intelligent routing by model name
- The local provider works without any API keys

---

## Scene 5: Knowledge and Embedding Configuration (3 min)

**Narration:** "The knowledge layer has its own set of configuration variables for the vector database and embedding provider."

**Demo steps:**

```bash
# Vector database backend
HELIX_VECTOR_DB=qdrant                  # qdrant | pgvector | milvus | pinecone

# Embedding configuration
HELIX_EMBEDDING_PROVIDER=local          # local | openai | cohere | voyage | jina
HELIX_EMBEDDING_MODEL=all-mpnet-base-v2

# RAG tuning
HELIX_RAG_CHUNK_SIZE=1000               # Characters per chunk
HELIX_RAG_CHUNK_OVERLAP=200             # Overlap between chunks
HELIX_RAG_TOP_K=5                       # Results per query
```

**Narration:** "If the configured vector database is not available, the system gracefully falls back to an in-memory store. The same applies to embeddings -- if the provider is unreachable, a hash-based embedder is used. This means HelixLLM always starts, even without external services."

**Key points:**
- Four vector database backends: Qdrant, pgvector, Milvus, Pinecone
- Five embedding providers: local, OpenAI, Cohere, Voyage, Jina
- Graceful fallback to in-memory implementations when services are unavailable
- RAG tuning parameters control chunk size, overlap, and retrieval depth

---

## Scene 6: Authentication and Observability (3 min)

**Narration:** "Let me cover two more important sections: authentication and observability."

**Demo steps:**

```bash
# Authentication
HELIX_AUTH_API_KEYS=sk-key1,sk-key2     # Comma-separated API keys
HELIX_AUTH_JWT_SECRET=your-secret        # JWT signing key

# Observability
HELIX_LOG_LEVEL=info                     # debug | info | warn | error
HELIX_LOG_FORMAT=text                    # text | json
HELIX_OTEL_EXPORTER=none                # none | stdout | otlp | jaeger | zipkin
HELIX_OTEL_ENDPOINT=http://localhost:4317
HELIX_PROMETHEUS_PORT=9090

# Feature flags
HELIX_FEATURE_GRPC=true
HELIX_FEATURE_TOON=true
HELIX_FEATURE_SELFIMPROVE=false
```

**Narration:** "When API keys are set, all /v1/ endpoints require a Bearer token. Leave the keys empty for open access during development. For logging, use text format locally and json format in production for structured log aggregation."

**Key points:**
- API key auth protects all `/v1/*` endpoints
- Internal endpoints (`/internal/*`) do not require authentication
- JSON log format is recommended for production (Loki, ELK)
- OpenTelemetry supports multiple exporters for distributed tracing
- Feature flags control optional subsystems like gRPC and TOON

---

## Exercises

1. Copy `.env.example` to `.env`, change the port to 9443, and verify the server starts on the new port
2. Set `HELIX_LLM_DEFAULT_PROVIDER=auto` and send requests with model names `gpt-4o` and `claude-sonnet-4-20250514` to observe the routing behavior
3. Enable debug logging with `HELIX_LOG_LEVEL=debug` and make an API call -- observe the additional log output showing internal request processing
