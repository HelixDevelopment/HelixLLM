---
title: "Agents"
weight: 1
bookToC: true
---


HelixLLM includes a ReAct (Reason-Act-Observe) agent system that can reason about queries, call tools, query the knowledge base, and maintain conversation sessions across multiple turns.

## Overview

The agent loop works as follows:

1. Receive user messages (with optional session history)
2. Augment with RAG context from the knowledge base
3. Send to the LLM (Brain) for reasoning
4. If the LLM requests a tool call, execute it and loop back
5. If the LLM returns a final answer, return it to the user
6. Save the exchange to the session (if session_id provided)

The loop runs for a maximum of 10 turns (configurable) to prevent runaway execution.

## Chat Endpoint

### Basic Request

```bash
curl -k -X POST https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "user", "content": "What time is it?"}
    ]
  }'
```

### With Session Tracking

Provide a `session_id` to maintain conversation history across requests:

```bash
# First message
curl -k -X POST https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "my-session-123",
    "messages": [
      {"role": "user", "content": "My name is Alice."}
    ]
  }'

# Follow-up (agent remembers the conversation)
curl -k -X POST https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "my-session-123",
    "messages": [
      {"role": "user", "content": "What is my name?"}
    ]
  }'
```

The conversation context stores up to 100 sessions in memory.

### With Model Selection

```bash
curl -k -X POST https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [
      {"role": "user", "content": "Analyze this code for bugs."}
    ]
  }'
```

## Built-in Tools

The agent has access to these tools out of the box:

| Tool | Description |
|------|-------------|
| `echo` | Echoes back the input message. Useful for testing. |
| `time` | Returns the current UTC time. |
| `knowledge_query` | Queries the knowledge base for relevant documents. |

### Listing Tools

```bash
curl -k https://localhost:8443/v1/agents/tools
```

Response:

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

## Tool Calling Flow

When the LLM decides it needs external information, the agent loop handles tool calls automatically:

1. **LLM reasons:** "I need to check the current time to answer this question"
2. **LLM outputs a tool call:** `{"name": "time", "arguments": {}}`
3. **Agent executes the tool** and gets the result
4. **Agent sends the result back** to the LLM as an observation
5. **LLM generates the final answer** using the tool result

This loop can chain multiple tool calls in sequence (up to `MaxTurns`).

## The Tool Interface

Tools implement this interface:

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() map[string]interface{}
    Execute(ctx context.Context, args map[string]interface{}) (string, error)
}
```

- `Name()` returns a unique identifier the LLM uses to invoke the tool
- `Description()` is included in the system prompt so the LLM knows when to use it
- `Parameters()` returns a JSON Schema-like map describing accepted arguments
- `Execute()` runs the tool and returns a string result

## Adding Custom Tools

Register tools in `cmd/helixllm/main.go`:

```go
toolReg := agents.NewToolRegistry()
toolReg.Register(&tools.EchoTool{})
toolReg.Register(&tools.TimeTool{})
toolReg.Register(tools.NewKnowledgeQueryTool(pipeline, "default"))
toolReg.Register(&myCustomTool{})  // Your tool here
```

A custom tool example:

```go
type WeatherTool struct{}

func (w *WeatherTool) Name() string        { return "weather" }
func (w *WeatherTool) Description() string { return "Get the current weather for a city" }
func (w *WeatherTool) Parameters() map[string]interface{} {
    return map[string]interface{}{
        "city": map[string]interface{}{
            "type":        "string",
            "description": "City name",
        },
    }
}
func (w *WeatherTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
    city, _ := args["city"].(string)
    // Fetch weather data...
    return fmt.Sprintf("Weather in %s: 22C, sunny", city), nil
}
```

## RAG Integration

The agent has a RAG hook that automatically enriches requests with knowledge base context before the first LLM call. This means the agent can answer questions about ingested documents without explicit tool calls.

The `knowledge_query` tool provides an additional way for the agent to explicitly search the knowledge base during its reasoning loop, which is useful when the initial RAG context is insufficient.

## Conversation Context

The `ConversationContext` maintains session state:

- Stores message history per session ID
- Supports up to 100 concurrent sessions
- History is prepended to new requests automatically
- Both user messages and assistant responses are saved

Sessions are stored in memory and do not persist across server restarts.

## Configuration

Agent behavior is configured in code (not environment variables):

| Setting | Default | Description |
|---------|---------|-------------|
| `MaxTurns` | `10` | Maximum ReAct loop iterations per request |
| `RAGHook` | Enabled | Augments requests with knowledge base context |
| `Tools` | 3 built-in | Registered tool set available to the agent |
