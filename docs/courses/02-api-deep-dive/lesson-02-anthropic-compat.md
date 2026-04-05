# Lesson 2: Anthropic Compatibility

**Duration:** 25 minutes
**Prerequisites:** Course 1 (Getting Started)
**Learning Objectives:**
- Use the Anthropic Messages API through HelixLLM
- Understand the differences between the OpenAI and Anthropic API formats
- Use tool_use for structured tool calling in the Anthropic format
- Connect existing Anthropic SDK clients to HelixLLM

---

## Scene 1: Anthropic Messages API Overview (4 min)

**Narration:** "HelixLLM implements the Anthropic Messages API alongside the OpenAI API. This means clients built for either platform work out of the box. The Anthropic API has a different message format and its own streaming protocol. Let us explore both."

**Screen:** Show the Anthropic endpoint.

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/messages` | POST | Anthropic Messages API |

**Key points:**
- Single endpoint: `POST /v1/messages`
- Requires the `anthropic-version` header
- Response format uses content blocks instead of a single string
- Streaming uses Anthropic's SSE event types

**Narration:** "The key difference from the OpenAI API is how responses are structured. Anthropic uses an array of content blocks, where each block has a type. This enables mixed content responses -- text, tool calls, and images in a single response."

---

## Scene 2: Basic Message Request (5 min)

**Narration:** "Let us start with a simple message request and examine the response format."

**Demo steps:**

```bash
# Basic Anthropic Messages request
curl -sk https://localhost:8443/v1/messages \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "messages": [
      {"role": "user", "content": "What is HelixLLM? Explain in two sentences."}
    ]
  }' | python3 -m json.tool
```

**Expected response:**

```json
{
  "id": "msg_abc123",
  "type": "message",
  "role": "assistant",
  "content": [
    {
      "type": "text",
      "text": "HelixLLM is an enterprise-grade distributed LLM system built in Go..."
    }
  ],
  "model": "claude-sonnet-4-20250514",
  "stop_reason": "end_turn",
  "usage": {
    "input_tokens": 12,
    "output_tokens": 45
  }
}
```

**Narration:** "Notice the differences from the OpenAI format. The response has content as an array of blocks, not a single string. Usage reports input_tokens and output_tokens instead of prompt_tokens and completion_tokens. The stop_reason field uses end_turn instead of stop."

**Key points:**
- `max_tokens` is required in the Anthropic format (unlike OpenAI where it is optional)
- `content` is an array of blocks, each with a `type` field
- `stop_reason` values: `end_turn`, `max_tokens`, `tool_use`
- `usage` fields: `input_tokens`, `output_tokens`

---

## Scene 3: System Messages and Multi-Turn (5 min)

**Narration:** "In the Anthropic API, system messages are handled differently. Instead of being a message in the array, the system prompt is a top-level field."

**Demo steps:**

```bash
# System prompt as top-level field
curl -sk https://localhost:8443/v1/messages \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "system": "You are a senior Go developer. Be concise and include code examples.",
    "messages": [
      {"role": "user", "content": "Show me how to use Go channels for fan-out."}
    ]
  }' | python3 -m json.tool
```

**Narration:** "For multi-turn conversations, alternate between user and assistant messages. The assistant messages use the same content block format."

```bash
# Multi-turn conversation
curl -sk https://localhost:8443/v1/messages \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "system": "You are a helpful programming tutor.",
    "messages": [
      {"role": "user", "content": "What is an interface in Go?"},
      {"role": "assistant", "content": [{"type": "text", "text": "An interface in Go defines a set of method signatures..."}]},
      {"role": "user", "content": "Show me the io.Reader interface as an example."}
    ]
  }' | python3 -m json.tool
```

**Key points:**
- System prompt is a top-level `system` field, not a message
- Assistant messages in history use content blocks: `[{"type": "text", "text": "..."}]`
- Messages must alternate between `user` and `assistant` roles
- The first message must always be from the `user`

---

## Scene 4: Tool Use (6 min)

**Narration:** "Anthropic has its own tool calling format called tool_use. Let me show you how it works."

**Demo steps:**

```bash
# Tool use request
curl -sk https://localhost:8443/v1/messages \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "tools": [
      {
        "name": "get_weather",
        "description": "Get the current weather for a city",
        "input_schema": {
          "type": "object",
          "properties": {
            "city": {
              "type": "string",
              "description": "The city name"
            }
          },
          "required": ["city"]
        }
      }
    ],
    "messages": [
      {"role": "user", "content": "What is the weather in Berlin?"}
    ]
  }' | python3 -m json.tool
```

**Narration:** "When the model decides to use a tool, the response contains a tool_use content block with the tool name and input parameters."

**Expected response:**

```json
{
  "id": "msg_abc123",
  "type": "message",
  "role": "assistant",
  "content": [
    {
      "type": "text",
      "text": "I'll check the weather in Berlin for you."
    },
    {
      "type": "tool_use",
      "id": "toolu_abc123",
      "name": "get_weather",
      "input": {"city": "Berlin"}
    }
  ],
  "stop_reason": "tool_use"
}
```

**Narration:** "You execute the tool and send the result back as a tool_result content block in a user message."

```bash
# Send tool result back
curl -sk https://localhost:8443/v1/messages \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "messages": [
      {"role": "user", "content": "What is the weather in Berlin?"},
      {"role": "assistant", "content": [{"type": "text", "text": "I will check the weather in Berlin for you."}, {"type": "tool_use", "id": "toolu_abc123", "name": "get_weather", "input": {"city": "Berlin"}}]},
      {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "toolu_abc123", "content": "18C, partly cloudy, wind 12 km/h"}]}
    ]
  }' | python3 -m json.tool
```

**Key points:**
- Tool definitions use `input_schema` (not `parameters` as in OpenAI)
- Tool calls appear as `tool_use` content blocks in the response
- Tool results are sent as `tool_result` content blocks in a `user` message
- `stop_reason: "tool_use"` signals the model wants to call a tool

---

## Scene 5: Using the Anthropic Python SDK (3 min)

**Narration:** "Just like the OpenAI SDK, the Anthropic Python SDK works by changing the base URL."

**Screen:** Show Python code.

```python
from anthropic import Anthropic

# Point the SDK at HelixLLM
client = Anthropic(
    base_url="https://localhost:8443",
    api_key="your-api-key",
)

# Create a message
response = client.messages.create(
    model="claude-sonnet-4-20250514",
    max_tokens=1024,
    system="You are a helpful assistant.",
    messages=[
        {"role": "user", "content": "Hello!"},
    ],
)

print(response.content[0].text)
print(f"Input tokens: {response.usage.input_tokens}")
print(f"Output tokens: {response.usage.output_tokens}")
```

**Narration:** "Notice the base URL does not include /v1 for the Anthropic SDK -- the SDK appends the path automatically. The API key can be any string if authentication is disabled."

**Key points:**
- Base URL is `https://localhost:8443` (no `/v1` suffix for Anthropic SDK)
- All Anthropic SDK features work: streaming, tool use, multi-turn
- Disable TLS verification in development for self-signed certificates

---

## Scene 6: OpenAI vs Anthropic Comparison (2 min)

**Narration:** "Let me summarize the key differences between the two API formats so you can choose which one fits your use case."

**Screen:** Comparison table.

| Feature | OpenAI Format | Anthropic Format |
|---------|--------------|------------------|
| Endpoint | `/v1/chat/completions` | `/v1/messages` |
| System message | In messages array | Top-level `system` field |
| Response content | Single string | Array of content blocks |
| Token usage | `prompt_tokens` / `completion_tokens` | `input_tokens` / `output_tokens` |
| Tool calling | `tools` with `parameters` | `tools` with `input_schema` |
| Tool results | `role: "tool"` message | `tool_result` content block |
| Streaming events | OpenAI SSE format | Anthropic SSE events |
| Stop reason | `finish_reason: "stop"` | `stop_reason: "end_turn"` |

**Narration:** "Both formats are fully supported. You can even mix them -- use the OpenAI endpoint for some requests and the Anthropic endpoint for others. HelixLLM routes both to the same provider backend."

---

## Exercises

1. Send the same query to both `/v1/chat/completions` and `/v1/messages` and compare the response structures field by field
2. Define a tool using the Anthropic `input_schema` format, trigger a tool call, send the result back, and get the final response
3. Use the Anthropic Python SDK to build a multi-turn conversation with HelixLLM that maintains context across three exchanges
