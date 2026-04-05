# Lesson 4: RAG Integration

**Duration:** 25 minutes
**Prerequisites:** Lesson 3 (Retrieval Tuning)
**Learning Objectives:**
- Understand how the RAG hook augments agent conversations with knowledge context
- Use the knowledge_query tool for explicit knowledge base searches during agent reasoning
- Build a complete RAG-augmented chat workflow from ingestion to answer
- Manage context window limits when using retrieved documents

---

## Scene 1: The RAG Hook (5 min)

**Narration:** "The RAG hook is the bridge between the knowledge pipeline and the agent system. When an agent receives a query, the RAG hook automatically searches the knowledge base for relevant context and prepends it to the conversation before the LLM sees it. This happens transparently -- the user does not need to request it."

**Screen:** Show the RAG-augmented request flow.

```
User Query: "How does the mode system work?"
    |
    v
RAG Hook
    | 1. Embed the query
    | 2. Search the knowledge base
    | 3. Retrieve top-k relevant chunks
    |
    v
Augmented Prompt:
    [System] You are a helpful assistant.
    [System] Context from knowledge base:
             - "HelixLLM operates as a single binary with a mode system..."
             - "The mode is set via HELIX_MODE environment variable..."
    [User]   How does the mode system work?
    |
    v
LLM generates response using retrieved context
```

**Key points:**
- The RAG hook runs before the first LLM call in the agent loop
- Retrieved chunks are injected as system context
- The LLM sees the relevant knowledge alongside the user query
- This happens automatically for every agent chat request

---

## Scene 2: RAG-Augmented Agent Chat (6 min)

**Narration:** "Let me demonstrate the full workflow: ingest documents, then ask the agent questions that require that knowledge."

**Demo steps:**

```bash
# Step 1: Ingest project documentation
curl -sk -X POST https://localhost:8443/internal/knowledge/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "content": "HelixLLM supports six deployment modes controlled by the HELIX_MODE environment variable. In full mode, all layers run in a single process with direct function calls. In distributed mode, separate binaries communicate via gRPC, SSE, and Kafka. The available modes are: full (all layers), gateway (HTTP frontend), brain (LLM inference), knowledge (RAG pipeline), agents (tool calling), and control (cluster management).",
    "collection": "default",
    "metadata": {"source": "architecture.md"}
  }'

curl -sk -X POST https://localhost:8443/internal/knowledge/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "content": "The scheduling strategies in HelixLLM control how services are placed on cluster hosts. Available strategies include: binpack (minimize host usage), spread (maximize availability), gpu-affinity (place on best GPU), memory-first (place on most RAM), and latency (place near clients). The auto strategy selects the best approach per service based on resource requirements.",
    "collection": "default",
    "metadata": {"source": "scheduling.md"}
  }'
```

**Narration:** "Now let us ask the agent a question that requires this knowledge."

```bash
# Step 2: Ask the agent a question
curl -sk -X POST https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "user", "content": "What deployment modes does HelixLLM support and when would I use each one?"}
    ]
  }' | python3 -m json.tool
```

**Narration:** "The agent's response should reference the six modes and their use cases because the RAG hook found and injected the relevant document chunks. Without the knowledge base, the LLM would have to rely solely on its training data."

**Key points:**
- Ingest documents into the `default` collection for automatic RAG
- The agent chat endpoint at `/v1/agents/chat` triggers the RAG hook
- The response is informed by both the LLM's training and your specific documents
- No special parameters needed -- RAG augmentation is automatic

---

## Scene 3: The knowledge_query Tool (5 min)

**Narration:** "In addition to the automatic RAG hook, the agent has a knowledge_query tool for explicit knowledge base searches during its reasoning loop. The agent uses this tool when it needs more specific information beyond what the initial RAG context provided."

**Demo steps:**

```bash
# List available tools to see knowledge_query
curl -sk https://localhost:8443/v1/agents/tools | python3 -m json.tool
```

**Expected output (relevant portion):**

```json
{
  "tools": [
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

**Narration:** "When the agent encounters a question and decides it needs more context, it calls knowledge_query as part of the ReAct loop. Let me trigger this with a more specific question."

```bash
# Ask something that may trigger explicit knowledge search
curl -sk -X POST https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "user", "content": "What scheduling strategy should I use for a GPU-heavy LLM inference workload?"}
    ]
  }' | python3 -m json.tool
```

**Narration:** "The agent may reason: I need to know about scheduling strategies, let me query the knowledge base. It calls knowledge_query with a relevant search term, gets back the scheduling documentation, and uses that to form its answer."

**Key points:**
- `knowledge_query` is a built-in agent tool
- The agent decides when to use it during the ReAct loop
- It searches the `default` collection by default
- Provides a second opportunity to retrieve knowledge beyond the initial RAG hook
- Up to 10 tool calls per agent request (configurable MaxTurns)

---

## Scene 4: Multi-Turn RAG Sessions (5 min)

**Narration:** "The agent supports multi-turn conversations with session tracking. Combined with RAG, this creates a powerful knowledge assistant that remembers context across exchanges."

**Demo steps:**

```bash
# Turn 1: Ask about modes
curl -sk -X POST https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "rag-demo-session",
    "messages": [
      {"role": "user", "content": "What is full mode in HelixLLM?"}
    ]
  }' | python3 -m json.tool
```

```bash
# Turn 2: Follow up (session remembers context)
curl -sk -X POST https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "rag-demo-session",
    "messages": [
      {"role": "user", "content": "How does it differ from distributed mode?"}
    ]
  }' | python3 -m json.tool
```

```bash
# Turn 3: Ask about a related topic
curl -sk -X POST https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "rag-demo-session",
    "messages": [
      {"role": "user", "content": "Which scheduling strategy would you recommend for that distributed setup?"}
    ]
  }' | python3 -m json.tool
```

**Narration:** "Each turn benefits from both the conversation history and fresh RAG context. The session ID ties the turns together, and the RAG hook runs on each turn to find relevant knowledge for the current question."

**Key points:**
- `session_id` maintains conversation history across requests
- RAG hook runs on every turn, not just the first
- The agent sees: session history + RAG context + current message
- Up to 100 concurrent sessions supported in memory
- Sessions do not persist across server restarts

---

## Scene 5: Context Window Management (4 min)

**Narration:** "There is a practical limit to how much retrieved context you can provide. LLMs have finite context windows, and stuffing too many chunks can actually hurt response quality by diluting the relevant information."

**Screen:** Context budget diagram.

```
LLM Context Window (e.g., 8192 tokens)
|---------------------------------------------|
| System prompt    |  ~200 tokens             |
| RAG context      |  ~2000 tokens (5 chunks) |
| Session history  |  ~1000 tokens            |
| Current message  |  ~100 tokens             |
| Response budget  |  ~4892 tokens remaining  |
|---------------------------------------------|
```

**Narration:** "With the default top-k of 5 and chunk size of 1000 characters, the RAG context uses roughly 2000 tokens. Combined with session history and the system prompt, you have a predictable context budget."

**Key points:**
- Each retrieved chunk consumes roughly 200-400 tokens (depending on content)
- With top-k=5, RAG context uses about 1000-2000 tokens
- Long session histories can crowd out RAG context and response budget
- Reduce top-k or chunk size if hitting context limits
- Monitor token usage in API responses to track context consumption

**Narration:** "Best practices for context management: use a moderate top-k (3-5), keep chunk sizes reasonable (800-1200 characters), and monitor the token usage in your responses. If completion_tokens or output_tokens are consistently low, you may be using too much of the context window on input."

---

## Exercises

1. Ingest five documents about different HelixLLM features, then use the agent chat endpoint to ask questions that require knowledge from multiple documents
2. Start a three-turn session with the agent, asking progressively more specific questions, and observe how RAG context complements session history
3. Experiment with top-k values of 1, 5, and 15 by modifying `HELIX_RAG_TOP_K`, ask the same question, and compare both the answer quality and the token usage reported in each response
