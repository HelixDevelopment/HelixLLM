---
title: "API Reference"
weight: 3
bookToC: true
---

# API Reference

HelixLLM provides OpenAI-compatible and Anthropic-compatible REST APIs.

## Base URL

    https://localhost:8443

## Authentication

    Authorization: Bearer your-api-key

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | /v1/chat/completions | Chat completion (OpenAI-compatible) |
| POST | /v1/completions | Text completion (OpenAI-compatible) |
| GET | /v1/models | List available models |
| POST | /v1/embeddings | Generate embeddings |
| POST | /v1/messages | Chat (Anthropic-compatible) |
| POST | /v1/agents/chat | Agent chat with tool calling |
| GET | /v1/agents/tools | List available agent tools |
| GET | /internal/health | Health check |
| GET | /internal/metrics | Prometheus metrics |
