---
title: "HelixLLM"
type: docs
---

# HelixLLM

**Enterprise-grade distributed LLM system built in Go.**

HelixLLM is a single binary that provides a complete LLM infrastructure stack — API-compatible with OpenAI and Anthropic, supporting local inference via llama.cpp, RAG pipelines, ReAct agents, and multi-host cluster deployment.

## Key Features

- **Drop-in API compatibility** — OpenAI and Anthropic clients work without modification
- **Local LLM inference** — Run models locally via llama.cpp for complete privacy
- **RAG pipeline** — Ingest documents, generate embeddings, semantic search
- **Agent system** — ReAct loop with tool calling and MCP integration
- **Multi-host distribution** — Deploy across cluster via SSH, auto-schedule workloads
- **Production-ready** — HTTP/3, TLS 1.3, rate limiting, Prometheus metrics, OTEL tracing

## Quick Start

    git clone https://github.com/HelixDevelopment/HelixLLM.git
    cd HelixLLM
    make deps && make certs && make dev

    curl -sk https://localhost:8443/v1/chat/completions \
      -H "Content-Type: application/json" \
      -d '{"model":"auto","messages":[{"role":"user","content":"Hello!"}]}'

## Documentation

- [Getting Started](/docs/user-guide/getting-started/)
- [API Reference](/docs/user-guide/api-reference/)
- [Architecture](/docs/manual/architecture/)
- [Video Courses](/courses/)
