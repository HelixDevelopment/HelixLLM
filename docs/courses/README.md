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
