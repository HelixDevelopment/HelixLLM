# Lesson 2: Built-in Tools

**Duration:** 20 minutes
**Prerequisites:** Lesson 1 (ReAct Agents)
**Learning Objectives:**
- List and inspect all registered agent tools
- Understand the echo, time, and knowledge_query tools
- Observe how the agent selects and invokes tools during the ReAct loop
- Use tool schemas to understand expected inputs and outputs

---

## Scene 1: Listing Available Tools (3 min)

**Narration:** "HelixLLM ships with three built-in tools that the agent can use during its ReAct loop. Let us start by listing them through the API."

**Demo steps:**

```bash
# List all registered tools
curl -sk https://localhost:8443/v1/agents/tools | python3 -m json.tool
```

**Expected response:**

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

**Narration:** "Each tool has a name that the LLM uses to invoke it, a description that helps the LLM decide when to use it, and a parameters schema that defines the expected input. The LLM reads these descriptions as part of its system prompt."

**Key points:**
- Endpoint: `GET /v1/agents/tools`
- Three built-in tools: echo, time, knowledge_query
- Tool descriptions are injected into the LLM's system prompt
- Parameter schemas use JSON Schema-like format
- Tools are registered in `cmd/helixllm/main.go` at startup

---

## Scene 2: The Echo Tool (4 min)

**Narration:** "The echo tool is the simplest tool -- it returns exactly what you send it. While it seems trivial, it is invaluable for testing the tool calling pipeline."

**Demo steps:**

```bash
# Trigger the echo tool
curl -sk -X POST https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "user", "content": "Use the echo tool to echo back: Hello from HelixLLM"}
    ]
  }' | python3 -m json.tool
```

**Narration:** "The agent recognizes that it should use the echo tool, calls it with the message, observes the echoed result, and returns it in its answer."

**Screen:** Show the tool implementation from `internal/agents/tools/`.

```go
// EchoTool echoes back the input message
type EchoTool struct{}

func (t *EchoTool) Name() string        { return "echo" }
func (t *EchoTool) Description() string  { return "Echoes back the input message" }
func (t *EchoTool) Parameters() map[string]interface{} {
    return map[string]interface{}{
        "message": map[string]interface{}{
            "type":        "string",
            "description": "Message to echo",
        },
    }
}
func (t *EchoTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
    message, _ := args["message"].(string)
    return message, nil
}
```

**Key points:**
- Takes a single `message` string parameter
- Returns the message unchanged
- Useful for verifying the tool calling pipeline works end-to-end
- Tests that the LLM correctly formats tool call arguments

---

## Scene 3: The Time Tool (4 min)

**Narration:** "The time tool returns the current UTC time. This is a practical tool that gives the agent awareness of the current moment."

**Demo steps:**

```bash
# Ask a question that triggers the time tool
curl -sk -X POST https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "user", "content": "What is the current date and time?"}
    ]
  }' | python3 -m json.tool
```

**Narration:** "The agent reasons: I need the current time to answer this, calls the time tool, observes the result, and incorporates it into a natural language response."

```bash
# A more complex query that uses time
curl -sk -X POST https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "user", "content": "Is it morning or afternoon right now in UTC?"}
    ]
  }' | python3 -m json.tool
```

**Narration:** "The time tool takes no parameters. It simply reads the system clock and returns the formatted UTC time."

**Screen:** Show the tool code.

```go
type TimeTool struct{}

func (t *TimeTool) Name() string        { return "time" }
func (t *TimeTool) Description() string  { return "Returns the current UTC time" }
func (t *TimeTool) Parameters() map[string]interface{} {
    return map[string]interface{}{}
}
func (t *TimeTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
    return time.Now().UTC().Format(time.RFC3339), nil
}
```

**Key points:**
- No parameters required -- empty parameters map
- Returns time in RFC3339 format (e.g., `2026-04-05T14:30:00Z`)
- The LLM can interpret and reformat the time in its response
- Demonstrates a zero-argument tool pattern

---

## Scene 4: The Knowledge Query Tool (5 min)

**Narration:** "The knowledge_query tool lets the agent explicitly search the knowledge base during its reasoning loop. This is different from the automatic RAG hook -- here the agent actively decides to search for information."

**Demo steps:**

```bash
# First, make sure we have some knowledge ingested
curl -sk -X POST https://localhost:8443/internal/knowledge/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "content": "HelixLLM rate limiting uses a sliding window algorithm. When the limit is exceeded, the server returns HTTP 429 with a Retry-After header. In distributed mode, rate limiting is backed by Redis for consistency across gateway instances.",
    "collection": "default",
    "metadata": {"source": "security.md"}
  }'

# Ask a question that triggers knowledge_query
curl -sk -X POST https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "user", "content": "How does rate limiting work in HelixLLM?"}
    ]
  }' | python3 -m json.tool
```

**Narration:** "The agent may use knowledge_query to search for rate limiting information. It sends a query string, receives ranked results from the vector store, and uses them to form an accurate answer."

**Screen:** Show the tool code.

```go
type KnowledgeQueryTool struct {
    pipeline   *Pipeline
    collection string
}

func (t *KnowledgeQueryTool) Name() string        { return "knowledge_query" }
func (t *KnowledgeQueryTool) Description() string  { return "Query the knowledge base for relevant documents" }
func (t *KnowledgeQueryTool) Parameters() map[string]interface{} {
    return map[string]interface{}{
        "query": map[string]interface{}{
            "type":        "string",
            "description": "Search query",
        },
    }
}
func (t *KnowledgeQueryTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
    query, _ := args["query"].(string)
    results, err := t.pipeline.Query(ctx, query, t.collection, 5)
    // ... format and return results
}
```

**Key points:**
- Takes a `query` string parameter
- Searches the configured vector store collection
- Returns the top-k results with content and scores
- Complements the automatic RAG hook with on-demand search
- The agent can refine its search query based on earlier observations

---

## Scene 5: Tool Selection and Chaining (4 min)

**Narration:** "The agent can chain multiple tool calls in a single request. Let me demonstrate with a query that requires both time and knowledge."

**Demo steps:**

```bash
# Query that may trigger multiple tools
curl -sk -X POST https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "user", "content": "What time is it, and based on the HelixLLM documentation, what rate limiting behavior should I expect at peak hours?"}
    ]
  }' | python3 -m json.tool
```

**Narration:** "The agent may call the time tool first, then knowledge_query for rate limiting information, combining both observations into a coherent answer. Each tool call is a separate turn in the ReAct loop -- reason, act, observe, and repeat."

**Key points:**
- The agent can call different tools in sequence within one request
- Each tool call is one turn of the ReAct loop (max 10 turns total)
- The agent sees all previous tool results when deciding the next action
- Tool calls stop when the agent has enough information for a final answer
- Token usage increases with each additional turn

---

## Exercises

1. Use the echo tool by asking the agent to echo a specific phrase, then verify the response contains the exact phrase
2. Ask the agent a question that requires both the time tool and knowledge_query, and observe how it chains the tool calls
3. Inspect the tool registration code in `cmd/helixllm/main.go` and the tool implementations in `internal/agents/tools/` to understand how tools are wired together
