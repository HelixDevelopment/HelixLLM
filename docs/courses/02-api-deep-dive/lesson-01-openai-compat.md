# Lesson 1: OpenAI Compatibility

**Duration:** 25 minutes
**Prerequisites:** Course 1 (Getting Started)
**Learning Objectives:**
- Use the full OpenAI chat completions API through HelixLLM
- Configure system messages, temperature, and token limits
- Use function calling to let the model invoke structured operations
- Connect existing OpenAI SDK clients to HelixLLM

---

## Scene 1: OpenAI API Surface (4 min)

**Narration:** "HelixLLM implements the OpenAI API specification. This means any client built for OpenAI works with HelixLLM by changing just the base URL. In this lesson we will explore every feature of the chat completions endpoint."

**Screen:** Show the OpenAI-compatible endpoints.

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/chat/completions` | POST | Chat completion (main endpoint) |
| `/v1/completions` | POST | Text completion (legacy) |
| `/v1/models` | GET | List available models |
| `/v1/models/:id` | GET | Get model details |
| `/v1/embeddings` | POST | Generate embeddings |

**Key points:**
- Full OpenAI specification compliance
- Same request and response formats
- Supports all major parameters: temperature, top_p, max_tokens, stop sequences
- Streaming via SSE with identical event format

---

## Scene 2: Chat Completions in Depth (6 min)

**Narration:** "Let me walk through every parameter of the chat completions endpoint."

**Demo steps:**

```bash
# Full-featured chat completion request
curl -sk https://localhost:8443/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "Llama-3.1-70B-Instruct-Q4_K_M",
    "messages": [
      {
        "role": "system",
        "content": "You are a Go programming expert. Be concise and precise."
      },
      {
        "role": "user",
        "content": "Write a function that reverses a string in Go."
      }
    ],
    "temperature": 0.3,
    "max_tokens": 1024,
    "stream": false
  }' | python3 -m json.tool
```

**Narration:** "The messages array supports three roles: system sets the model's behavior, user provides the input, and assistant represents previous model responses for multi-turn conversations."

**Screen:** Show multi-turn conversation example.

```bash
# Multi-turn conversation
curl -sk https://localhost:8443/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "Llama-3.1-70B-Instruct-Q4_K_M",
    "messages": [
      {"role": "system", "content": "You are a helpful math tutor."},
      {"role": "user", "content": "What is the quadratic formula?"},
      {"role": "assistant", "content": "The quadratic formula is x = (-b +/- sqrt(b^2 - 4ac)) / 2a"},
      {"role": "user", "content": "Use it to solve x^2 + 5x + 6 = 0"}
    ],
    "temperature": 0.2,
    "max_tokens": 512
  }' | python3 -m json.tool
```

**Key points:**
- `temperature` (0.0-2.0) -- lower is more deterministic, higher is more creative
- `max_tokens` -- maximum tokens in the response
- `stream` -- set to true for Server-Sent Events
- Messages maintain conversation context across turns

---

## Scene 3: Using the OpenAI Python SDK (5 min)

**Narration:** "One of the most powerful features of HelixLLM's OpenAI compatibility is that existing SDK clients work without modification. You only need to change the base URL."

**Screen:** Show Python code in an editor.

```python
from openai import OpenAI

# Point the SDK at HelixLLM instead of api.openai.com
client = OpenAI(
    base_url="https://localhost:8443/v1",
    api_key="your-api-key",  # Or any string if auth is disabled
)

# Non-streaming completion
response = client.chat.completions.create(
    model="Llama-3.1-70B-Instruct-Q4_K_M",
    messages=[
        {"role": "system", "content": "You are a helpful assistant."},
        {"role": "user", "content": "Explain Go interfaces in two sentences."},
    ],
    temperature=0.7,
    max_tokens=256,
)

print(response.choices[0].message.content)
print(f"Tokens used: {response.usage.total_tokens}")
```

**Narration:** "For self-signed certificates in development, you may need to disable SSL verification."

```python
import httpx

# Development only: skip TLS verification for self-signed certs
client = OpenAI(
    base_url="https://localhost:8443/v1",
    api_key="your-api-key",
    http_client=httpx.Client(verify=False),
)
```

**Narration:** "Streaming works identically to the OpenAI SDK."

```python
# Streaming completion
stream = client.chat.completions.create(
    model="Llama-3.1-70B-Instruct-Q4_K_M",
    messages=[{"role": "user", "content": "Write a haiku about distributed systems."}],
    stream=True,
)

for chunk in stream:
    if chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="", flush=True)
print()
```

**Key points:**
- Only the `base_url` changes -- all other code stays identical
- Works with any OpenAI-compatible SDK (Python, Node.js, Go, etc.)
- Self-signed certificates require disabling TLS verification in development

---

## Scene 4: Function Calling (6 min)

**Narration:** "Function calling lets the model request structured operations from your application. The model decides when a function is needed and returns the arguments in a structured format."

**Demo steps:**

```bash
# Function calling request
curl -sk https://localhost:8443/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "Llama-3.1-70B-Instruct-Q4_K_M",
    "messages": [
      {"role": "user", "content": "What is the weather in Tokyo?"}
    ],
    "tools": [
      {
        "type": "function",
        "function": {
          "name": "get_weather",
          "description": "Get the current weather for a location",
          "parameters": {
            "type": "object",
            "properties": {
              "location": {
                "type": "string",
                "description": "City name"
              },
              "unit": {
                "type": "string",
                "enum": ["celsius", "fahrenheit"],
                "description": "Temperature unit"
              }
            },
            "required": ["location"]
          }
        }
      }
    ]
  }' | python3 -m json.tool
```

**Narration:** "When the model decides to use a function, the response contains a tool_calls array instead of a text content field. Your application executes the function and sends the result back."

**Expected response:**

```json
{
  "choices": [
    {
      "message": {
        "role": "assistant",
        "tool_calls": [
          {
            "id": "call_abc123",
            "type": "function",
            "function": {
              "name": "get_weather",
              "arguments": "{\"location\":\"Tokyo\",\"unit\":\"celsius\"}"
            }
          }
        ]
      },
      "finish_reason": "tool_calls"
    }
  ]
}
```

**Narration:** "You then send the function result back as a tool message and the model incorporates it into a natural language response."

```bash
# Send function result back
curl -sk https://localhost:8443/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "Llama-3.1-70B-Instruct-Q4_K_M",
    "messages": [
      {"role": "user", "content": "What is the weather in Tokyo?"},
      {"role": "assistant", "tool_calls": [{"id": "call_abc123", "type": "function", "function": {"name": "get_weather", "arguments": "{\"location\":\"Tokyo\",\"unit\":\"celsius\"}"}}]},
      {"role": "tool", "tool_call_id": "call_abc123", "content": "{\"temperature\": 22, \"condition\": \"partly cloudy\"}"}
    ]
  }' | python3 -m json.tool
```

**Key points:**
- Tools are defined using JSON Schema for structured parameter descriptions
- The model decides when to call functions based on the user query
- `finish_reason: "tool_calls"` indicates the model wants to use a function
- Send tool results back as messages with `role: "tool"`

---

## Scene 5: Legacy Completions (2 min)

**Narration:** "HelixLLM also supports the legacy completions endpoint for text completion without the chat format."

**Demo steps:**

```bash
# Legacy text completion
curl -sk https://localhost:8443/v1/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "Llama-3.1-70B-Instruct-Q4_K_M",
    "prompt": "The three main benefits of Go are",
    "max_tokens": 100,
    "temperature": 0.5
  }' | python3 -m json.tool
```

**Key points:**
- Uses `prompt` string instead of `messages` array
- Response has `choices[0].text` instead of `choices[0].message.content`
- Useful for simple text generation without conversation structure

---

## Scene 6: What's Next (2 min)

**Narration:** "You now know how to use every feature of the OpenAI-compatible API in HelixLLM. In the next lesson, we will explore the Anthropic Messages API, which provides an alternative interface with its own strengths."

---

## Exercises

1. Use the OpenAI Python SDK to connect to HelixLLM and send a multi-turn conversation with system, user, and assistant messages
2. Define two functions (get_weather and get_time) in a chat completion request and craft a prompt that triggers the model to call one of them
3. Write a script that sends the same prompt to both `/v1/chat/completions` and `/v1/completions` and compare the response formats
