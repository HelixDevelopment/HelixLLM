# Phase 9: Video Course Scripts & Outlines

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create complete video course scripts for 6 courses (26 lessons, ~10 hours) covering every aspect of HelixLLM — from first install to production operations. Scripts are markdown documents ready for recording.

**Architecture:** Each course is a directory under `docs/courses/`. Each lesson is a markdown file with: learning objectives, prerequisites, scene-by-scene script with narration notes, code examples, demo steps, key talking points, and exercises. A top-level README.md serves as course catalog.

**Tech Stack:** Markdown, existing documentation as source material

---

### Task 1: Create course catalog

**Files:**
- Create: `docs/courses/README.md`

- [ ] **Step 1: Create courses directory structure**

Run: `mkdir -p docs/courses/01-getting-started docs/courses/02-api-deep-dive docs/courses/03-rag-pipeline docs/courses/04-agents docs/courses/05-production docs/courses/06-development`

- [ ] **Step 2: Create course catalog**

Create `docs/courses/README.md`:

```markdown
# HelixLLM Video Course Catalog

Complete video course scripts covering every aspect of HelixLLM. Each lesson is a self-contained markdown document with narration scripts, code examples, and demo steps ready for recording.

## Recording Guide

- **Format:** Screen recording with voiceover narration
- **Resolution:** 1920x1080 minimum
- **Terminal:** Use a clean terminal with large font (16pt+)
- **Code editor:** VS Code or GoLand with Go extension
- **Environment:** Fresh HelixLLM install on Linux for consistency

## Courses

### Course 1: Getting Started with HelixLLM (5 lessons, ~95 min)

| # | Lesson | Duration | Prerequisites |
|---|--------|----------|---------------|
| 1 | [Introduction](01-getting-started/lesson-01-introduction.md) | 15 min | None |
| 2 | [Installation](01-getting-started/lesson-02-installation.md) | 20 min | None |
| 3 | [First API Call](01-getting-started/lesson-03-first-api-call.md) | 15 min | Lesson 2 |
| 4 | [Configuration](01-getting-started/lesson-04-configuration.md) | 20 min | Lesson 2 |
| 5 | [Local LLM Setup](01-getting-started/lesson-05-local-llm.md) | 25 min | Lesson 4 |

### Course 2: API Deep Dive (4 lessons, ~90 min)

| # | Lesson | Duration | Prerequisites |
|---|--------|----------|---------------|
| 1 | [OpenAI Compatibility](02-api-deep-dive/lesson-01-openai-compat.md) | 25 min | Course 1 |
| 2 | [Anthropic Compatibility](02-api-deep-dive/lesson-02-anthropic-compat.md) | 25 min | Course 1 |
| 3 | [Streaming](02-api-deep-dive/lesson-03-streaming.md) | 20 min | Lesson 1 or 2 |
| 4 | [Embeddings](02-api-deep-dive/lesson-04-embeddings.md) | 20 min | Course 1 |

### Course 3: RAG Knowledge Pipeline (4 lessons, ~105 min)

| # | Lesson | Duration | Prerequisites |
|---|--------|----------|---------------|
| 1 | [Document Ingestion](03-rag-pipeline/lesson-01-ingestion.md) | 25 min | Course 1 |
| 2 | [Vector Stores](03-rag-pipeline/lesson-02-vector-stores.md) | 30 min | Lesson 1 |
| 3 | [Retrieval Tuning](03-rag-pipeline/lesson-03-retrieval.md) | 25 min | Lesson 2 |
| 4 | [RAG Integration](03-rag-pipeline/lesson-04-integration.md) | 25 min | Lesson 3 |

### Course 4: Agent System (4 lessons, ~100 min)

| # | Lesson | Duration | Prerequisites |
|---|--------|----------|---------------|
| 1 | [ReAct Agents](04-agents/lesson-01-react-agents.md) | 25 min | Courses 1-2 |
| 2 | [Built-in Tools](04-agents/lesson-02-built-in-tools.md) | 20 min | Lesson 1 |
| 3 | [Custom Tools](04-agents/lesson-03-custom-tools.md) | 30 min | Lesson 2 |
| 4 | [MCP Integration](04-agents/lesson-04-mcp-integration.md) | 25 min | Lesson 3 |

### Course 5: Production Deployment (5 lessons, ~135 min)

| # | Lesson | Duration | Prerequisites |
|---|--------|----------|---------------|
| 1 | [Containerization](05-production/lesson-01-containerization.md) | 25 min | Course 1 |
| 2 | [Multi-Host Deployment](05-production/lesson-02-multi-host.md) | 30 min | Lesson 1 |
| 3 | [Monitoring](05-production/lesson-03-monitoring.md) | 30 min | Lesson 1 |
| 4 | [Security Hardening](05-production/lesson-04-security.md) | 25 min | Lesson 1 |
| 5 | [Operations](05-production/lesson-05-operations.md) | 25 min | Lessons 1-4 |

### Course 6: Development & Testing (4 lessons, ~100 min)

| # | Lesson | Duration | Prerequisites |
|---|--------|----------|---------------|
| 1 | [Development Setup](06-development/lesson-01-dev-setup.md) | 25 min | Course 1 |
| 2 | [Testing Strategy](06-development/lesson-02-testing.md) | 30 min | Lesson 1 |
| 3 | [Challenge Banks](06-development/lesson-03-challenge-banks.md) | 25 min | Lesson 2 |
| 4 | [CI/CD Pipeline](06-development/lesson-04-ci-cd.md) | 20 min | Lesson 3 |

**Total: 6 courses, 26 lessons, ~625 minutes (~10.4 hours)**
```

- [ ] **Step 3: Commit**

```bash
git add docs/courses/README.md
git commit -m "docs: add video course catalog with 6 courses, 26 lessons"
```

---

### Task 2: Write Course 1 — Getting Started

**Files:**
- Create: `docs/courses/01-getting-started/lesson-01-introduction.md`
- Create: `docs/courses/01-getting-started/lesson-02-installation.md`
- Create: `docs/courses/01-getting-started/lesson-03-first-api-call.md`
- Create: `docs/courses/01-getting-started/lesson-04-configuration.md`
- Create: `docs/courses/01-getting-started/lesson-05-local-llm.md`

- [ ] **Step 1: Write lesson 01 — Introduction**

Create `docs/courses/01-getting-started/lesson-01-introduction.md`:

```markdown
# Lesson 1: Introduction to HelixLLM

**Duration:** 15 minutes
**Prerequisites:** None
**Learning Objectives:**
- Understand what HelixLLM is and why it exists
- Identify the 5-layer architecture and how layers compose
- See a live demo of HelixLLM serving API requests

---

## Scene 1: Welcome (2 min)

**Narration:** "Welcome to this course on HelixLLM — an enterprise-grade distributed LLM system built entirely in Go. By the end of this lesson, you'll understand what HelixLLM does, how it's architected, and see it in action."

**Screen:** Title slide with HelixLLM logo.

---

## Scene 2: What is HelixLLM? (3 min)

**Narration:** "HelixLLM is a single Go binary that provides a complete LLM infrastructure stack. It's API-compatible with both OpenAI and Anthropic, so any existing client library works out of the box. It can run locally with llama.cpp for complete privacy, or proxy to cloud providers like OpenAI and Anthropic."

**Screen:** Show README.md feature list.

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

**Screen:** Show `docs/diagrams/architecture-overview.mmd` rendered as a diagram.

**Walk through each layer:**

1. **Gateway** — "The Gateway handles all HTTP traffic. It supports HTTP/3 with QUIC for modern clients and falls back to HTTP/2. It includes authentication, rate limiting, security headers, and Brotli compression."

2. **Brain** — "The Brain is the LLM coordination layer. It routes requests to the right provider — local llama.cpp, OpenAI, or Anthropic — based on configuration or automatic selection."

3. **Knowledge** — "The Knowledge layer provides RAG — Retrieval-Augmented Generation. It chunks documents, generates embeddings, stores them in a vector database, and retrieves relevant context for LLM prompts."

4. **Agents** — "The Agents layer implements a ReAct loop with tool calling. Agents can use built-in tools like time and knowledge search, or connect to external tools via the Model Context Protocol."

5. **Control** — "The Control plane manages multi-host deployment. It probes hosts over SSH, schedules workloads, deploys containers, and monitors health."

---

## Scene 4: Live Demo (4 min)

**Narration:** "Let me show you HelixLLM in action. I'll start the server and make a few API calls."

**Demo steps:**
```bash
# Terminal 1: Start HelixLLM
make dev

# Terminal 2: List models
curl -sk https://localhost:8443/v1/models | python3 -m json.tool

# Chat completion
curl -sk https://localhost:8443/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"auto","messages":[{"role":"user","content":"Hello!"}]}' | python3 -m json.tool

# Health check
curl -sk https://localhost:8443/internal/health | python3 -m json.tool
```

---

## Scene 5: What's Next (1 min)

**Narration:** "In the next lesson, we'll install HelixLLM from source and get it running on your machine. See you there."

---

## Exercises

1. Read the README.md file and identify the 6 deployment modes
2. Look at the `cmd/helixllm/main.go` entry point and identify where each layer is initialized
3. Explore the `internal/` directory structure and match each subdirectory to an architecture layer
```

- [ ] **Step 2: Write lesson 02 — Installation**

Create `docs/courses/01-getting-started/lesson-02-installation.md` following the same scene-by-scene format. Cover: prerequisites (Go 1.26+, git, OpenSSL), cloning the repo, `make deps`, `make certs`, `make build`, `make dev`, verifying the server starts.

- [ ] **Step 3: Write lesson 03 — First API Call**

Create `docs/courses/01-getting-started/lesson-03-first-api-call.md`. Cover: curl to /v1/models, /v1/chat/completions (non-streaming and streaming), /v1/embeddings, response anatomy, error handling.

- [ ] **Step 4: Write lesson 04 — Configuration**

Create `docs/courses/01-getting-started/lesson-04-configuration.md`. Cover: .env.example walkthrough, HELIX_MODE, provider selection, vector DB configuration, feature flags.

- [ ] **Step 5: Write lesson 05 — Local LLM**

Create `docs/courses/01-getting-started/lesson-05-local-llm.md`. Cover: llama.cpp container setup, model download (GGUF format), GPU vs CPU inference, HELIX_LLM_LOCAL_MODEL configuration.

- [ ] **Step 6: Commit Course 1**

```bash
git add docs/courses/01-getting-started/
git commit -m "docs: write Course 1 — Getting Started with HelixLLM (5 lessons)"
```

---

### Task 3: Write Course 2 — API Deep Dive

**Files:**
- Create: `docs/courses/02-api-deep-dive/lesson-01-openai-compat.md`
- Create: `docs/courses/02-api-deep-dive/lesson-02-anthropic-compat.md`
- Create: `docs/courses/02-api-deep-dive/lesson-03-streaming.md`
- Create: `docs/courses/02-api-deep-dive/lesson-04-embeddings.md`

- [ ] **Step 1: Write all 4 lessons**

Each lesson follows the same format: learning objectives, 4-6 scenes with narration and demo steps, exercises.

- Lesson 01: OpenAI chat completions, function calling, system messages, temperature/top_p
- Lesson 02: Anthropic Messages API, tool_use, system prompt, max_tokens
- Lesson 03: SSE streaming (text/event-stream), WebSocket streaming, error handling mid-stream
- Lesson 04: Embedding generation, provider comparison (local vs OpenAI vs Cohere), batch processing

- [ ] **Step 2: Commit Course 2**

```bash
git add docs/courses/02-api-deep-dive/
git commit -m "docs: write Course 2 — API Deep Dive (4 lessons)"
```

---

### Task 4: Write Course 3 — RAG Pipeline

**Files:**
- Create: `docs/courses/03-rag-pipeline/lesson-01-ingestion.md`
- Create: `docs/courses/03-rag-pipeline/lesson-02-vector-stores.md`
- Create: `docs/courses/03-rag-pipeline/lesson-03-retrieval.md`
- Create: `docs/courses/03-rag-pipeline/lesson-04-integration.md`

- [ ] **Step 1: Write all 4 lessons**

- Lesson 01: Document upload via API, `make ingest`, chunking strategies (fixed-size with overlap), supported file formats
- Lesson 02: Qdrant setup (via compose), pgvector with PostgreSQL, Milvus, Pinecone cloud — switching via HELIX_VECTOR_DB
- Lesson 03: Semantic search mechanics, top-k tuning, relevance scoring, cosine similarity, re-ranking
- Lesson 04: RAG hook in agent conversations, knowledge-aware prompts, context window management, hybrid search

- [ ] **Step 2: Commit Course 3**

```bash
git add docs/courses/03-rag-pipeline/
git commit -m "docs: write Course 3 — RAG Knowledge Pipeline (4 lessons)"
```

---

### Task 5: Write Course 4 — Agent System

**Files:**
- Create: `docs/courses/04-agents/lesson-01-react-agents.md`
- Create: `docs/courses/04-agents/lesson-02-built-in-tools.md`
- Create: `docs/courses/04-agents/lesson-03-custom-tools.md`
- Create: `docs/courses/04-agents/lesson-04-mcp-integration.md`

- [ ] **Step 1: Write all 4 lessons**

- Lesson 01: ReAct loop explanation (Reason → Act → Observe), /v1/agents/chat endpoint, session management, max 10 tool turns
- Lesson 02: echo tool, time tool, knowledge_query tool, /v1/agents/tools listing, tool schemas
- Lesson 03: Implementing Tool interface (Name, Description, Parameters, Execute), registering in ToolRegistry, testing with httptest
- Lesson 04: MCP protocol overview, MCPServerConfig, RegisterMCPTools, connecting external tool servers, LSP integration

- [ ] **Step 2: Commit Course 4**

```bash
git add docs/courses/04-agents/
git commit -m "docs: write Course 4 — Agent System (4 lessons)"
```

---

### Task 6: Write Course 5 — Production Deployment

**Files:**
- Create: `docs/courses/05-production/lesson-01-containerization.md`
- Create: `docs/courses/05-production/lesson-02-multi-host.md`
- Create: `docs/courses/05-production/lesson-03-monitoring.md`
- Create: `docs/courses/05-production/lesson-04-security.md`
- Create: `docs/courses/05-production/lesson-05-operations.md`

- [ ] **Step 1: Write all 5 lessons**

- Lesson 01: Containerfile anatomy, `make container`, compose.yaml services, GPU passthrough, volume persistence
- Lesson 02: SSH key setup, HELIX_HOSTS configuration, `make probe`, scheduling strategies (resource_aware, round_robin, affinity, spread, bin_pack), `make deploy`
- Lesson 03: Prometheus metrics endpoint, Grafana dashboard setup, Loki log aggregation, OpenTelemetry tracing, alerting rules
- Lesson 04: TLS 1.3 enforcement, API key/JWT auth, rate limiting, security headers, `make scan-all`, vulnerability triage
- Lesson 05: Horizontal scaling, backup procedures, disaster recovery, `make rebalance`, `make monitor` TUI, troubleshooting production issues

- [ ] **Step 2: Commit Course 5**

```bash
git add docs/courses/05-production/
git commit -m "docs: write Course 5 — Production Deployment (5 lessons)"
```

---

### Task 7: Write Course 6 — Development & Testing

**Files:**
- Create: `docs/courses/06-development/lesson-01-dev-setup.md`
- Create: `docs/courses/06-development/lesson-02-testing.md`
- Create: `docs/courses/06-development/lesson-03-challenge-banks.md`
- Create: `docs/courses/06-development/lesson-04-ci-cd.md`

- [ ] **Step 1: Write all 4 lessons**

- Lesson 01: Cloning with submodules, `make deps`, IDE setup (VS Code with Go extension, GoLand), delve debugger, pprof profiling, `make dev`
- Lesson 02: Unit tests (`make test-unit`), integration tests, E2E tests, stress tests, benchmarks, monitoring tests, race detection, coverage threshold
- Lesson 03: Challenge bank YAML format, categories (api, security, chaos, stress, benchmarks, regression, e2e), writing custom assertions, running banks with `make test-challenges`
- Lesson 04: GitHub Actions CI workflow, security scanning workflow, release workflow, branch protection rules, PR review process

- [ ] **Step 2: Commit Course 6**

```bash
git add docs/courses/06-development/
git commit -m "docs: write Course 6 — Development & Testing (4 lessons)"
```

---

### Task 8: Final verification

- [ ] **Step 1: Verify all 26 lesson files exist**

Run: `find docs/courses -name "lesson-*.md" | wc -l`
Expected: 26

- [ ] **Step 2: Verify no empty lesson files**

Run: `find docs/courses -name "lesson-*.md" -empty`
Expected: No output (no empty files)

- [ ] **Step 3: Verify README catalog links are correct**

Run: `grep -c '\[.*\](.*lesson.*\.md)' docs/courses/README.md`
Expected: 26 (one link per lesson)
