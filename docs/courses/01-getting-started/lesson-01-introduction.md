# Lesson 1: Introduction to HelixLLM

**Duration:** 15 minutes
**Prerequisites:** None
**Learning Objectives:**
- Understand what HelixLLM is and why it exists
- Identify the 5-layer architecture and how layers compose
- See a live demo of HelixLLM serving API requests

---

## Scene 1: Welcome (2 min)

**Narration:** "Welcome to this course on HelixLLM -- an enterprise-grade distributed LLM system built entirely in Go. By the end of this lesson, you will understand what HelixLLM does, how it is architected, and see it in action."

**Screen:** Title slide with HelixLLM logo.

**Key points:**
- This is the first lesson in a six-course series
- No prior knowledge of HelixLLM is needed
- By the end of this course you will be able to install, configure, and use HelixLLM

---

## Scene 2: What is HelixLLM? (3 min)

**Narration:** "HelixLLM is a single Go binary that provides a complete LLM infrastructure stack. It is API-compatible with both OpenAI and Anthropic, so any existing client library works out of the box. It can run locally with llama.cpp for complete privacy, or proxy to cloud providers like OpenAI and Anthropic. What makes it unique is the mode system -- one binary, six deployment modes, from a single laptop to a multi-host production cluster."

**Screen:** Show the project README feature list.

**Key points:**
- Single binary, multiple deployment modes
- OpenAI and Anthropic API compatibility
- Local LLM inference via llama.cpp
- RAG pipeline with multiple vector database backends
- ReAct agent system with tool calling
- Multi-host cluster distribution via SSH

---

## Scene 3: Architecture Overview (5 min)

**Narration:** "HelixLLM has a layered architecture with five main layers, plus a shared foundation. Let me walk through each one."

**Screen:** Show the architecture diagram with all six layers stacked.

```
+-----------------------------------------------------------+
|                     Gateway Layer                          |
|  HTTP/3 Server, OpenAI/Anthropic APIs, Auth, Streaming    |
+-----------------------------------------------------------+
|                      Brain Layer                           |
|  LLM Routing, llama.cpp, OpenAI, Anthropic, Optimization  |
+-----------------------------------------------------------+
|                    Knowledge Layer                         |
|  RAG Pipeline, Embeddings, Vector Store, Chunking          |
+-----------------------------------------------------------+
|                     Agents Layer                           |
|  ReAct Loop, Tools, Conversation Context, RAG Hook         |
+-----------------------------------------------------------+
|                   Control Plane Layer                      |
|  Host Probing, Scheduling, Deployment, Monitoring          |
+-----------------------------------------------------------+
|                    Shared Foundation                       |
|  Config, Events, Logging, Observability, Health            |
+-----------------------------------------------------------+
```

**Walk through each layer:**

1. **Gateway** -- "The Gateway handles all HTTP traffic. It supports HTTP/3 with QUIC for modern clients and falls back to HTTP/2. It includes authentication, rate limiting, security headers, and Brotli compression."

2. **Brain** -- "The Brain is the LLM coordination layer. It routes requests to the right provider -- local llama.cpp, OpenAI, or Anthropic -- based on configuration or automatic selection."

3. **Knowledge** -- "The Knowledge layer provides RAG -- Retrieval-Augmented Generation. It chunks documents, generates embeddings, stores them in a vector database, and retrieves relevant context for LLM prompts."

4. **Agents** -- "The Agents layer implements a ReAct loop with tool calling. Agents can use built-in tools like time and knowledge search, or connect to external tools via the Model Context Protocol."

5. **Control** -- "The Control plane manages multi-host deployment. It probes hosts over SSH, schedules workloads, deploys containers, and monitors health."

**Narration:** "In full mode, all five layers run together in a single process and communicate through direct Go function calls -- zero serialization, zero network overhead. In distributed mode, you run the binary with different mode flags on different hosts, and they communicate via gRPC, SSE, and Kafka."

---

## Scene 4: Mode System (2 min)

**Narration:** "The mode system is the key to HelixLLM's flexibility. The HELIX_MODE environment variable or the --mode CLI flag selects which layers are active."

**Screen:** Show the mode table.

| Mode | Layers Active | Use Case |
|------|---------------|----------|
| `full` | All | Single-host development and production |
| `gateway` | Gateway + Shared | Dedicated API frontend |
| `brain` | Brain + Shared | Dedicated LLM inference node |
| `knowledge` | Knowledge + Shared | Dedicated RAG pipeline |
| `agents` | Agents + Shared | Dedicated agent workers |
| `control` | Control + Shared | Cluster management node |

**Key points:**
- `full` mode is the default and best for getting started
- Distributed modes let you scale each layer independently
- The same binary is used everywhere -- only configuration changes

---

## Scene 5: Live Demo (3 min)

**Narration:** "Let me show you HelixLLM in action. I will start the server and make a few API calls."

**Demo steps:**

```bash
# Terminal 1: Start HelixLLM
make dev
```

**Narration:** "The server starts in full mode on port 8443 with self-signed TLS. Let me open a second terminal and make some requests."

```bash
# Terminal 2: List available models
curl -sk https://localhost:8443/v1/models | python3 -m json.tool
```

**Narration:** "Here we see the models available from all configured providers. Now let me send a chat completion."

```bash
# Chat completion
curl -sk https://localhost:8443/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"auto","messages":[{"role":"user","content":"Hello!"}]}' \
  | python3 -m json.tool
```

**Narration:** "That is the OpenAI-compatible endpoint. Let me check the health endpoint too."

```bash
# Health check
curl -sk https://localhost:8443/internal/health | python3 -m json.tool
```

**Narration:** "The health endpoint reports all subsystems healthy. That is HelixLLM running in full mode with just one command."

---

## Scene 6: What's Next (1 min)

**Narration:** "In the next lesson, we will install HelixLLM from source and get it running on your machine. See you there."

**Screen:** Preview of Lesson 2 topics: prerequisites, cloning, building, first run.

---

## Exercises

1. Read the project README and identify the six deployment modes available via the `HELIX_MODE` variable
2. Look at the entry point `cmd/helixllm/main.go` and identify where each layer is initialized in the startup sequence
3. Explore the `internal/` directory structure and match each subdirectory to an architecture layer
