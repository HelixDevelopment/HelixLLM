# Lesson 3: Your First API Call

**Duration:** 15 minutes
**Prerequisites:** Lesson 2 (Installation)
**Learning Objectives:**
- List available models through the API
- Send chat completion requests in both non-streaming and streaming modes
- Understand the anatomy of OpenAI-compatible responses
- Handle common error scenarios

---

## Scene 1: Listing Models (3 min)

**Narration:** "With HelixLLM running, the first thing to explore is which models are available. The models endpoint follows the OpenAI specification exactly."

**Screen:** Terminal with HelixLLM running in the background.

**Demo steps:**

```bash
# List all available models
curl -sk https://localhost:8443/v1/models | python3 -m json.tool
```

**Expected response:**

```json
{
  "object": "list",
  "data": [
    {
      "id": "Llama-3.1-70B-Instruct-Q4_K_M",
      "object": "model",
      "created": 1712188800,
      "owned_by": "local"
    }
  ]
}
```

**Narration:** "Each model entry shows its ID, which you use in API requests, and its owner -- local for llama.cpp models, openai or anthropic for cloud-provided models. The number of models depends on your configuration. If you have OpenAI or Anthropic API keys set, their models appear here too."

**Key points:**
- Endpoint: `GET /v1/models`
- Returns all models from all configured providers
- Model IDs are used in chat completion requests

---

## Scene 2: Chat Completion -- Non-Streaming (4 min)

**Narration:** "Now let us send a chat completion request. This is the most common API call -- you send messages and get a response."

**Demo steps:**

```bash
# Send a chat completion request
curl -sk https://localhost:8443/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "Llama-3.1-70B-Instruct-Q4_K_M",
    "messages": [
      {"role": "system", "content": "You are a helpful assistant."},
      {"role": "user", "content": "Explain what an LLM is in one sentence."}
    ],
    "temperature": 0.7,
    "max_tokens": 256
  }' | python3 -m json.tool
```

**Narration:** "Let me walk through the response structure."

**Expected response:**

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
        "content": "A Large Language Model is a neural network trained on vast amounts of text data..."
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 25,
    "completion_tokens": 30,
    "total_tokens": 55
  }
}
```

**Key points:**
- `id` -- unique identifier for this completion
- `choices[0].message.content` -- the model's response text
- `finish_reason` -- "stop" means the model finished naturally, "length" means it hit `max_tokens`
- `usage` -- token counts for billing and monitoring

---

## Scene 3: Chat Completion -- Streaming (3 min)

**Narration:** "For real-time applications, you want streaming responses. Set stream to true and the server returns Server-Sent Events."

**Demo steps:**

```bash
# Streaming chat completion
curl -sk https://localhost:8443/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "Llama-3.1-70B-Instruct-Q4_K_M",
    "messages": [
      {"role": "user", "content": "Count from 1 to 5."}
    ],
    "stream": true
  }'
```

**Narration:** "Notice how the response arrives as a series of data events. Each event contains a delta with a small piece of text. The stream ends with a data: [DONE] marker."

**Expected output:**

```
data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"1"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":", "},"finish_reason":null}]}

data: [DONE]
```

**Key points:**
- Content-Type of the response is `text/event-stream`
- Each SSE event contains a `delta` instead of a full `message`
- The first event sets the role, subsequent events add content
- The stream terminates with `data: [DONE]`

---

## Scene 4: Embeddings (2 min)

**Narration:** "HelixLLM also generates text embeddings, which are essential for the RAG pipeline we will cover in Course 3."

**Demo steps:**

```bash
# Generate embeddings
curl -sk https://localhost:8443/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{
    "model": "all-mpnet-base-v2",
    "input": "HelixLLM is a distributed LLM system."
  }' | python3 -m json.tool
```

**Expected response (truncated):**

```json
{
  "object": "list",
  "data": [
    {
      "object": "embedding",
      "index": 0,
      "embedding": [0.0123, -0.0456, 0.0789, "..."]
    }
  ],
  "model": "all-mpnet-base-v2",
  "usage": {
    "prompt_tokens": 9,
    "total_tokens": 9
  }
}
```

**Key points:**
- Endpoint: `POST /v1/embeddings`
- The default local embedder produces 768-dimensional vectors
- Embeddings power semantic search in the RAG pipeline

---

## Scene 5: Error Handling (2 min)

**Narration:** "Let me show you what happens when things go wrong, so you know how to debug issues."

**Demo steps:**

```bash
# Missing messages field
curl -sk https://localhost:8443/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"auto"}' | python3 -m json.tool
```

**Expected error response:**

```json
{
  "error": {
    "message": "invalid request body: missing required field 'messages'",
    "type": "invalid_request_error"
  }
}
```

**Narration:** "Errors follow a consistent format with a message and a type. Common HTTP status codes are 400 for bad requests, 401 for missing authentication, 429 for rate limiting, and 503 for unhealthy services."

**Key points:**
- All errors return a consistent JSON format
- HTTP 400 -- invalid request (missing fields, bad JSON)
- HTTP 401 -- missing or invalid API key
- HTTP 429 -- rate limited
- HTTP 503 -- service unavailable

---

## Scene 6: What's Next (1 min)

**Narration:** "You have now made your first API calls to HelixLLM. In the next lesson, we will explore the full configuration system -- environment variables, modes, providers, and feature flags."

---

## Exercises

1. Use curl to list models and identify which providers are active in your installation
2. Send a multi-turn conversation with a system message and two user messages, then examine the token usage in the response
3. Try a streaming request and pipe it through a script that extracts just the content deltas
