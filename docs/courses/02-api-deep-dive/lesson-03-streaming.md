# Lesson 3: Streaming

**Duration:** 20 minutes
**Prerequisites:** Lesson 1 (OpenAI Compatibility) or Lesson 2 (Anthropic Compatibility)
**Learning Objectives:**
- Implement SSE streaming for real-time token delivery
- Handle both OpenAI and Anthropic streaming event formats
- Build robust error handling for mid-stream failures
- Use streaming with SDK clients in Python

---

## Scene 1: Why Streaming Matters (3 min)

**Narration:** "Without streaming, the client waits for the entire response to generate before seeing any output. For a 500-token response at 40 tokens per second, that is a 12-second wait. With streaming, the first token arrives in under a second and text flows continuously. This is essential for interactive applications."

**Screen:** Diagram comparing non-streaming versus streaming response timelines.

**Key points:**
- Non-streaming: client waits for full response (high perceived latency)
- Streaming: first token arrives immediately, text flows in real time
- Both OpenAI and Anthropic formats support streaming
- HelixLLM uses Server-Sent Events (SSE) over HTTP

---

## Scene 2: OpenAI SSE Format (5 min)

**Narration:** "The OpenAI streaming format sends each token as a separate SSE event. Let me demonstrate and then break down the event structure."

**Demo steps:**

```bash
# OpenAI streaming request
curl -sk https://localhost:8443/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "Llama-3.1-70B-Instruct-Q4_K_M",
    "messages": [
      {"role": "user", "content": "List three benefits of Go."}
    ],
    "stream": true
  }'
```

**Narration:** "Each line starting with data: is an SSE event. The response Content-Type is text/event-stream."

**Expected output:**

```
data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"1"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":". "},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Fast"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]
```

**Key points:**
- First event sets the `role` in the delta
- Subsequent events contain `content` fragments in the delta
- Final content event has an empty delta and `finish_reason: "stop"`
- Stream terminates with `data: [DONE]`
- Each event is separated by a blank line

---

## Scene 3: Anthropic SSE Format (5 min)

**Narration:** "The Anthropic streaming format uses named event types. Each SSE event has both an event field and a data field."

**Demo steps:**

```bash
# Anthropic streaming request
curl -sk https://localhost:8443/v1/messages \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "messages": [
      {"role": "user", "content": "List three benefits of Go."}
    ],
    "stream": true
  }'
```

**Expected output:**

```
event: message_start
data: {"type":"message_start","message":{"id":"msg_abc123","type":"message","role":"assistant","model":"claude-sonnet-4-20250514"}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"1. "}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Fast compilation"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":45}}

event: message_stop
data: {"type":"message_stop"}
```

**Narration:** "The Anthropic format has more structure. There are distinct events for message start, content block start, content deltas, content block stop, and message stop. This makes it easier to handle mixed content responses with multiple content blocks."

**Key points:**
- `message_start` -- contains the message metadata
- `content_block_start` -- begins a content block (text, tool_use)
- `content_block_delta` -- incremental content updates
- `content_block_stop` -- ends a content block
- `message_delta` -- final message metadata (stop_reason, usage)
- `message_stop` -- stream complete

---

## Scene 4: Client-Side Streaming in Python (4 min)

**Narration:** "Let me show you how to consume streaming responses in Python using both SDKs."

**Screen:** Show Python code for OpenAI streaming.

```python
from openai import OpenAI
import httpx

client = OpenAI(
    base_url="https://localhost:8443/v1",
    api_key="your-api-key",
    http_client=httpx.Client(verify=False),
)

# OpenAI streaming
stream = client.chat.completions.create(
    model="Llama-3.1-70B-Instruct-Q4_K_M",
    messages=[{"role": "user", "content": "Write a short poem about servers."}],
    stream=True,
)

full_response = ""
for chunk in stream:
    delta = chunk.choices[0].delta
    if delta.content:
        print(delta.content, end="", flush=True)
        full_response += delta.content

print(f"\n\nTotal length: {len(full_response)} characters")
```

**Screen:** Show Python code for Anthropic streaming.

```python
from anthropic import Anthropic
import httpx

client = Anthropic(
    base_url="https://localhost:8443",
    api_key="your-api-key",
    http_client=httpx.Client(verify=False),
)

# Anthropic streaming
with client.messages.stream(
    model="claude-sonnet-4-20250514",
    max_tokens=1024,
    messages=[{"role": "user", "content": "Write a short poem about servers."}],
) as stream:
    for text in stream.text_stream:
        print(text, end="", flush=True)

print()
```

**Key points:**
- OpenAI SDK yields chunks with `delta.content`
- Anthropic SDK provides a convenient `text_stream` iterator
- Both handle SSE parsing and reconnection internally
- Always flush output for real-time display

---

## Scene 5: Error Handling During Streaming (3 min)

**Narration:** "Errors can occur mid-stream -- a provider might timeout, the connection might drop, or the model might hit a safety filter. Let me show you how to handle these cases."

**Screen:** Show error handling code.

```python
from openai import OpenAI, APIError, APIConnectionError
import httpx

client = OpenAI(
    base_url="https://localhost:8443/v1",
    api_key="your-api-key",
    http_client=httpx.Client(verify=False),
)

try:
    stream = client.chat.completions.create(
        model="Llama-3.1-70B-Instruct-Q4_K_M",
        messages=[{"role": "user", "content": "Hello!"}],
        stream=True,
    )

    collected = []
    for chunk in stream:
        delta = chunk.choices[0].delta
        if delta.content:
            collected.append(delta.content)
            print(delta.content, end="", flush=True)

    print(f"\nReceived {len(collected)} chunks")

except APIConnectionError:
    print("\nConnection lost during streaming. Partial response collected.")
    print("".join(collected))

except APIError as e:
    print(f"\nAPI error: {e.status_code} - {e.message}")
```

**Key points:**
- Wrap the stream iteration in try/except for connection errors
- Collect partial responses so they are not lost on failure
- Check `finish_reason` -- `"stop"` means normal completion, `"length"` means truncated
- HTTP errors before streaming starts return standard JSON error responses

---

## Exercises

1. Send a streaming request using curl and count the number of SSE events (data: lines) for a 100-token response
2. Write a Python script that consumes an OpenAI-format stream and measures the time between the first and last token
3. Implement a streaming client that handles mid-stream disconnection by saving partial output and retrying
