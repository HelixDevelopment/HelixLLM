# Lesson 1: ReAct Agents

**Duration:** 25 minutes
**Prerequisites:** Courses 1-2 (Getting Started, API Deep Dive)
**Learning Objectives:**
- Understand the ReAct (Reason-Act-Observe) pattern for agent behavior
- Use the agent chat endpoint for tool-augmented conversations
- Track conversation sessions across multiple turns
- Observe the agent loop executing tool calls and returning final answers

---

## Scene 1: What is ReAct? (5 min)

**Narration:** "ReAct stands for Reason, Act, Observe. It is a pattern where the LLM reasons about a problem, decides on an action -- typically a tool call -- observes the result, and then reasons again. This loop continues until the LLM has enough information to produce a final answer."

**Screen:** Show the ReAct loop diagram.

```
User Query
    |
    v
+-------------------+
| 1. REASON         |  LLM thinks about the query
|    "I need to     |  and available tools
|     check the     |
|     current time" |
+-------------------+
    |
    v
+-------------------+
| 2. ACT            |  LLM requests a tool call
|    tool: "time"   |  with structured arguments
|    args: {}       |
+-------------------+
    |
    v
+-------------------+
| 3. OBSERVE        |  Tool executes and returns result
|    result:        |  Result is fed back to LLM
|    "2026-04-05    |
|     T14:30:00Z"   |
+-------------------+
    |
    v
+-------------------+
| 4. REASON (again) |  LLM incorporates observation
|    "Now I can     |  and decides: final answer or
|     answer"       |  another tool call?
+-------------------+
    |
    v
Final Answer: "The current time is 2:30 PM UTC."
```

**Narration:** "The key insight is that the LLM decides when to use tools. It is not a fixed pipeline -- the model reasons about whether a tool call is needed and which tool to use. The loop runs for a maximum of 10 turns to prevent runaway execution."

**Key points:**
- ReAct combines reasoning (chain of thought) with acting (tool use)
- The LLM decides autonomously when and which tools to call
- Each loop iteration: reason about the problem, act with a tool, observe the result
- Maximum 10 turns per request (configurable `MaxTurns`)
- The loop ends when the LLM produces a final text answer instead of a tool call

---

## Scene 2: Agent Chat Endpoint (5 min)

**Narration:** "The agent chat endpoint is at /v1/agents/chat. It accepts messages just like the chat completions endpoint, but runs the full ReAct loop with tool access and RAG augmentation."

**Demo steps:**

```bash
# Simple agent chat (may use tools internally)
curl -sk -X POST https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "user", "content": "What time is it?"}
    ]
  }' | python3 -m json.tool
```

**Expected response:**

```json
{
  "session_id": "sess_abc123",
  "response": {
    "message": {
      "role": "assistant",
      "content": "The current time is 2026-04-05T14:30:00Z."
    },
    "usage": {
      "prompt_tokens": 50,
      "completion_tokens": 20,
      "total_tokens": 70
    }
  }
}
```

**Narration:** "Behind the scenes, the agent may have called the time tool to get the current time before formulating its answer. The response looks simple, but the ReAct loop may have run multiple turns internally."

```bash
# Specify a model for the agent to use
curl -sk -X POST https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "model": "Llama-3.1-70B-Instruct-Q4_K_M",
    "messages": [
      {"role": "user", "content": "Echo back the phrase: hello world"}
    ]
  }' | python3 -m json.tool
```

**Key points:**
- Endpoint: `POST /v1/agents/chat`
- Accepts `messages`, optional `session_id`, optional `model`
- Response includes the final answer after all tool calls complete
- Token usage reflects total consumption across all ReAct turns
- A `session_id` is returned for session tracking

---

## Scene 3: Session Management (5 min)

**Narration:** "Sessions let the agent remember previous turns. Provide a session_id and the agent prepends the conversation history to each new request."

**Demo steps:**

```bash
# Turn 1: Introduce yourself
curl -sk -X POST https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "demo-session-001",
    "messages": [
      {"role": "user", "content": "My name is Alice and I work on the HelixLLM project."}
    ]
  }' | python3 -m json.tool
```

```bash
# Turn 2: Ask a question that requires session context
curl -sk -X POST https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "demo-session-001",
    "messages": [
      {"role": "user", "content": "What is my name and what project do I work on?"}
    ]
  }' | python3 -m json.tool
```

**Narration:** "The agent remembers that Alice works on HelixLLM because the session history is loaded and prepended. Without the session_id, each request would be independent."

```bash
# Turn 3: Continue the conversation
curl -sk -X POST https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "demo-session-001",
    "messages": [
      {"role": "user", "content": "What scheduling strategy should I use for GPU workloads?"}
    ]
  }' | python3 -m json.tool
```

**Key points:**
- `session_id` is a client-chosen string identifier
- Up to 100 concurrent sessions stored in memory
- Both user messages and assistant responses are saved to the session
- Sessions do not persist across server restarts
- Omit `session_id` for stateless single-turn interactions

---

## Scene 4: The Agent Request Flow (5 min)

**Narration:** "Let me walk through exactly what happens inside the agent when you send a request."

**Screen:** Show the detailed request flow.

```
1. HTTP request arrives at /v1/agents/chat
       |
2. Load session history (if session_id provided)
   Append new messages to history
       |
3. RAG Hook: search knowledge base for relevant context
   Prepend retrieved chunks as system context
       |
4. Send augmented prompt to Brain (LLM provider)
       |
5. Check LLM response:
   - If tool_call: execute tool, append observation, go to step 4
   - If text answer: break loop
       |
6. (Loop repeats, max 10 turns)
       |
7. Save exchange to session context
   Return final response to client
```

**Narration:** "The RAG hook runs once at the beginning to augment the context. Then the ReAct loop iterates: send to LLM, check for tool calls, execute tools, loop. When the LLM returns a text answer instead of a tool call, the loop ends and the response is saved to the session."

**Key points:**
- Session loading happens before any LLM call
- RAG augmentation runs once per request (before the first LLM call)
- The tool execution loop can run up to 10 times
- Each tool call adds an observation to the conversation
- The final response is always a text message from the assistant

---

## Scene 5: What's Next (2 min)

**Narration:** "You now understand the ReAct pattern and how HelixLLM implements it. In the next lesson, we will explore the built-in tools that ship with HelixLLM -- echo, time, and knowledge_query."

---

## Exercises

1. Send an agent chat request that triggers a tool call (e.g., "What time is it?") and compare the response with a direct chat completion to the same question
2. Create a three-turn session and verify the agent maintains context by asking "What did I ask first?" in the third turn
3. Read the agent implementation in `internal/agents/agent.go` and trace the ReAct loop code to see how tool calls and observations are handled
