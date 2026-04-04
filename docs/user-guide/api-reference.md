# API Reference

HelixLLM exposes three groups of endpoints: public API (OpenAI/Anthropic compatible), agent API, and internal management API. All endpoints are served over HTTPS on the configured port (default 8443).

## Authentication

When `HELIX_AUTH_API_KEYS` is configured, all `/v1/*` endpoints require a Bearer token:

```
Authorization: Bearer sk-your-api-key
```

Internal endpoints (`/internal/*`) do not require authentication.

## OpenAI Compatible Endpoints

These endpoints match the OpenAI API specification. Any OpenAI SDK client works without modification -- just point the base URL to your HelixLLM instance.

### POST /v1/chat/completions

Create a chat completion. Supports SSE streaming when `stream: true`.

**Request:**

```json
{
  "model": "Llama-3.1-70B-Instruct-Q4_K_M",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Explain the ReAct pattern."}
  ],
  "temperature": 0.7,
  "max_tokens": 1024,
  "stream": false
}
```

**Response (non-streaming):**

```json
{
  "id": "chatcmpl-abc123",
  "object": "chat.completion",
  "created": 1712188800,
  "model": "Llama-3.1-70B-Instruct-Q4_K_M",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "The ReAct pattern combines reasoning and acting..."
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 25,
    "completion_tokens": 150,
    "total_tokens": 175
  }
}
```

**Streaming response:** When `stream: true`, the server responds with `Content-Type: text/event-stream`. Each SSE event contains a delta:

```
data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"The "},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"ReAct "},"finish_reason":null}]}

data: [DONE]
```

### POST /v1/completions

Create a text completion (legacy format).

**Request:**

```json
{
  "model": "Llama-3.1-70B-Instruct-Q4_K_M",
  "prompt": "Once upon a time",
  "max_tokens": 100,
  "temperature": 0.9
}
```

**Response:**

```json
{
  "id": "cmpl-abc123",
  "object": "text_completion",
  "created": 1712188800,
  "model": "Llama-3.1-70B-Instruct-Q4_K_M",
  "choices": [
    {
      "text": " in a land far away...",
      "index": 0,
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 5,
    "completion_tokens": 50,
    "total_tokens": 55
  }
}
```

### GET /v1/models

List all available models across all configured providers.

**Response:**

```json
{
  "object": "list",
  "data": [
    {
      "id": "Llama-3.1-70B-Instruct-Q4_K_M",
      "object": "model",
      "created": 1712188800,
      "owned_by": "local"
    },
    {
      "id": "gpt-4o",
      "object": "model",
      "created": 1712188800,
      "owned_by": "openai"
    }
  ]
}
```

### GET /v1/models/:id

Get details for a specific model.

**Response:**

```json
{
  "id": "Llama-3.1-70B-Instruct-Q4_K_M",
  "object": "model",
  "created": 1712188800,
  "owned_by": "local"
}
```

### POST /v1/embeddings

Generate embeddings for input text.

**Request:**

```json
{
  "model": "all-mpnet-base-v2",
  "input": "The quick brown fox jumps over the lazy dog"
}
```

**Response:**

```json
{
  "object": "list",
  "data": [
    {
      "object": "embedding",
      "index": 0,
      "embedding": [0.0123, -0.0456, 0.0789, ...]
    }
  ],
  "model": "all-mpnet-base-v2",
  "usage": {
    "prompt_tokens": 9,
    "total_tokens": 9
  }
}
```

## Anthropic Compatible Endpoints

These endpoints match the Anthropic Messages API specification. The `anthropic-version` header is respected.

### POST /v1/messages

Create a message using the Anthropic Messages format.

**Request:**

```json
{
  "model": "claude-sonnet-4-20250514",
  "max_tokens": 1024,
  "messages": [
    {"role": "user", "content": "What is HelixLLM?"}
  ]
}
```

**Headers:**

```
Content-Type: application/json
anthropic-version: 2023-06-01
```

**Response (non-streaming):**

```json
{
  "id": "msg_abc123",
  "type": "message",
  "role": "assistant",
  "content": [
    {
      "type": "text",
      "text": "HelixLLM is an enterprise-grade distributed LLM system..."
    }
  ],
  "model": "claude-sonnet-4-20250514",
  "stop_reason": "end_turn",
  "usage": {
    "input_tokens": 12,
    "output_tokens": 150
  }
}
```

**Streaming:** Set `"stream": true` in the request body. The response uses SSE with Anthropic's event format:

```
event: message_start
data: {"type":"message_start","message":{"id":"msg_abc123","type":"message","role":"assistant","model":"claude-sonnet-4-20250514"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"HelixLLM "}}

event: message_stop
data: {"type":"message_stop"}
```

## Agent Endpoints

### POST /v1/agents/chat

Run the ReAct agent loop. The agent reasons about the query, optionally calls tools, and returns a final response. Supports multi-turn sessions via `session_id`.

**Request:**

```json
{
  "session_id": "sess_abc123",
  "messages": [
    {"role": "user", "content": "What time is it?"}
  ],
  "model": "Llama-3.1-70B-Instruct-Q4_K_M"
}
```

- `session_id` (optional): When provided, conversation history is preserved across calls.
- `messages` (required): New messages for this turn.
- `model` (optional): Model hint for provider selection.

**Response:**

```json
{
  "session_id": "sess_abc123",
  "response": {
    "message": {
      "role": "assistant",
      "content": "The current time is 2026-04-04T14:30:00Z."
    },
    "usage": {
      "prompt_tokens": 50,
      "completion_tokens": 20,
      "total_tokens": 70
    }
  }
}
```

### GET /v1/agents/tools

List all registered tools available to the agent.

**Response:**

```json
{
  "tools": [
    {
      "name": "echo",
      "description": "Echoes back the input message",
      "parameters": {
        "message": {"type": "string", "description": "Message to echo"}
      }
    },
    {
      "name": "time",
      "description": "Returns the current UTC time",
      "parameters": {}
    },
    {
      "name": "knowledge_query",
      "description": "Query the knowledge base for relevant documents",
      "parameters": {
        "query": {"type": "string", "description": "Search query"}
      }
    }
  ]
}
```

## Knowledge Endpoints

Internal endpoints for the RAG knowledge pipeline. These are under `/internal/` and do not require API key authentication.

### POST /internal/knowledge/ingest

Ingest a document into the knowledge base. The document is chunked, embedded, and stored in the vector database.

**Request:**

```json
{
  "content": "HelixLLM is an enterprise-grade distributed LLM system built in Go...",
  "collection": "docs",
  "metadata": {
    "source": "readme.md",
    "title": "HelixLLM Overview"
  }
}
```

**Response:**

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "collection": "docs",
  "chunks": 3,
  "status": "completed"
}
```

### POST /internal/knowledge/query

Query the knowledge base using semantic search.

**Request:**

```json
{
  "query": "How does the mode system work?",
  "collection": "docs",
  "top_k": 5
}
```

**Response:**

```json
{
  "results": [
    {
      "content": "HelixLLM compiles to a single binary that operates in one of six modes...",
      "score": 0.92,
      "metadata": {
        "source": "architecture.md",
        "chunk_index": 0
      }
    }
  ]
}
```

### GET /internal/knowledge/collections

List all vector collections.

**Response:**

```json
[
  {
    "name": "default",
    "document_count": 42,
    "chunk_count": 156
  }
]
```

### GET /internal/knowledge/stats

Get knowledge base statistics.

**Response:**

```json
{
  "total_documents": 42,
  "total_chunks": 156,
  "collections": 1,
  "embedding_dimensions": 768
}
```

## Cluster Control Endpoints

Internal endpoints for multi-host cluster management.

### GET /internal/cluster/status

Get the current cluster status including host health and service placements.

**Response:**

```json
{
  "checked_at": "2026-04-04T14:30:00Z",
  "healthy": true,
  "hosts": [],
  "deployments": []
}
```

### POST /internal/cluster/probe

Probe all configured hosts via SSH to detect capabilities (OS, CPU, RAM, GPU, container runtime).

**Response:**

```json
{
  "hosts": [
    {
      "hostname": "nezha.local",
      "reachable": true,
      "os": "linux",
      "cpu_cores": 16,
      "memory_mb": 65536,
      "gpu": "NVIDIA RTX 4090",
      "container_runtime": "podman"
    }
  ]
}
```

### POST /internal/cluster/deploy

Schedule and deploy services to the cluster.

**Request:**

```json
{
  "services": [
    {
      "name": "llama-cpp",
      "image": "ghcr.io/ggml-org/llama.cpp:server-cuda",
      "requires_gpu": true,
      "memory_mb": 16384
    }
  ]
}
```

**Response:**

```json
{
  "deployments": [
    {
      "service_name": "llama-cpp",
      "host": "nezha.local",
      "status": "running"
    }
  ],
  "placements": [
    {
      "service": "llama-cpp",
      "host": "nezha.local",
      "strategy": "gpu-affinity"
    }
  ]
}
```

### POST /internal/cluster/rebalance

Re-evaluate and rebalance placement of existing deployments based on current host conditions.

**Response:**

```json
{
  "placements": [],
  "deployments": [],
  "errors": []
}
```

## Health Endpoint

### GET /internal/health

Aggregated health check across all subsystems.

**Response (healthy):**

```json
{
  "status": "healthy",
  "checks": []
}
```

**Response (unhealthy):** Returns HTTP 503 with:

```json
{
  "status": "unhealthy",
  "checks": [
    {
      "name": "database",
      "status": "unhealthy",
      "error": "connection refused"
    }
  ]
}
```

## Error Responses

All endpoints return errors in a consistent format:

```json
{
  "error": {
    "message": "invalid request body: missing required field 'messages'",
    "type": "invalid_request_error"
  }
}
```

Common HTTP status codes:

| Code | Meaning |
|------|---------|
| 400 | Bad request (invalid JSON, missing fields, validation error) |
| 401 | Unauthorized (missing or invalid API key) |
| 429 | Rate limited (too many requests) |
| 500 | Internal server error |
| 503 | Service unavailable (health check failed) |

## Content Negotiation

**Compression:** The server supports Brotli (`br`) and gzip compression. Set the `Accept-Encoding` header:

```
Accept-Encoding: br, gzip
```

Brotli is preferred when both are accepted.

**Serialization:** The default format is JSON. When TOON is enabled (`HELIX_FEATURE_TOON=true`), you can request TOON format:

```
Accept: application/toon
```

## Using with OpenAI SDK

```python
from openai import OpenAI

client = OpenAI(
    base_url="https://localhost:8443/v1",
    api_key="your-api-key",
)

response = client.chat.completions.create(
    model="Llama-3.1-70B-Instruct-Q4_K_M",
    messages=[{"role": "user", "content": "Hello!"}],
)
print(response.choices[0].message.content)
```

## Using with Anthropic SDK

```python
from anthropic import Anthropic

client = Anthropic(
    base_url="https://localhost:8443",
    api_key="your-api-key",
)

response = client.messages.create(
    model="claude-sonnet-4-20250514",
    max_tokens=1024,
    messages=[{"role": "user", "content": "Hello!"}],
)
print(response.content[0].text)
```
