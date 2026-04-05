---
title: "RAG Knowledge Pipeline"
weight: 1
bookToC: true
---


HelixLLM includes a Retrieval-Augmented Generation (RAG) pipeline for ingesting documents, storing them as vector embeddings, and retrieving relevant context to augment LLM responses.

## Overview

The knowledge pipeline has two main flows:

**Ingestion:**
```
Document -> Chunker -> Embedder -> Vector Store
```

**Query:**
```
Query -> Embedder -> Vector Search -> Ranked Results
```

## Document Ingestion

### Ingest via API

```bash
curl -k -X POST https://localhost:8443/internal/knowledge/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "content": "HelixLLM is an enterprise-grade distributed LLM system built in Go with Gin Gonic. It operates as a single binary with a mode system that enables flexible deployment from single-host development to multi-host production clusters.",
    "collection": "project-docs",
    "metadata": {
      "source": "readme.md",
      "title": "Project Overview"
    }
  }'
```

**Response:**

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "collection": "project-docs",
  "chunks": 1,
  "status": "completed"
}
```

### Request Fields

| Field | Required | Description |
|-------|----------|-------------|
| `content` | Yes | The text content to ingest. Must not be empty. |
| `collection` | Yes | Name of the vector collection to store in. Must not be empty. Created automatically if it does not exist. |
| `metadata` | No | Arbitrary key-value metadata attached to each chunk. |

### Chunking

Documents are split into chunks before embedding. Configure chunk size and overlap:

```bash
HELIX_RAG_CHUNK_SIZE=1000      # Maximum characters per chunk
HELIX_RAG_CHUNK_OVERLAP=200    # Character overlap between chunks
```

The default chunker uses fixed-size splitting. Overlap ensures context is not lost at chunk boundaries. For a 2500-character document with chunk size 1000 and overlap 200:

- Chunk 1: characters 0-999
- Chunk 2: characters 800-1799
- Chunk 3: characters 1600-2499

### Embedding

Each chunk is converted to a vector embedding. Configure the embedding provider:

```bash
HELIX_EMBEDDING_PROVIDER=local           # local | openai | cohere | voyage | jina
HELIX_EMBEDDING_MODEL=all-mpnet-base-v2  # Model name
```

The default local embedder produces 768-dimensional vectors.

## Querying the Knowledge Base

### Query via API

```bash
curl -k -X POST https://localhost:8443/internal/knowledge/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "How does the mode system work?",
    "collection": "project-docs",
    "top_k": 5
  }'
```

**Response:**

```json
{
  "results": [
    {
      "content": "HelixLLM operates as a single binary with a mode system...",
      "score": 0.92,
      "metadata": {
        "source": "readme.md",
        "title": "Project Overview",
        "chunk_index": 0
      }
    }
  ]
}
```

### Query Fields

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `query` | Yes | -- | The search query text. Must not be empty. |
| `collection` | No | `default` | Collection to search in. |
| `top_k` | No | `5` (from `HELIX_RAG_TOP_K`) | Number of results to return. |

## Collection Management

### List Collections

```bash
curl -k https://localhost:8443/internal/knowledge/collections
```

Returns all collections with document and chunk counts.

### Knowledge Base Statistics

```bash
curl -k https://localhost:8443/internal/knowledge/stats
```

Returns aggregate statistics: total documents, total chunks, number of collections, and embedding dimensions.

## Vector Database Backends

Configure the vector store:

```bash
HELIX_VECTOR_DB=qdrant    # qdrant | pgvector | milvus | pinecone
```

| Backend | Description | Best For |
|---------|-------------|----------|
| Qdrant | Dedicated vector database (containerized) | Production, large-scale |
| pgvector | PostgreSQL extension | When PostgreSQL is already available |
| Milvus | Distributed vector database | Very large datasets |
| Pinecone | Managed cloud vector database | Serverless deployments |

In development, the system uses an in-memory vector store that requires no external services.

## Agent Integration

The agent system automatically uses the knowledge base through a RAG hook. When an agent receives a query:

1. The RAG hook searches the knowledge base for relevant context
2. Retrieved chunks are prepended to the conversation as system context
3. The LLM generates a response informed by the retrieved knowledge

This happens transparently. The agent's `knowledge_query` tool also allows explicit knowledge base queries during the ReAct loop.

## Tuning Retrieval Quality

### Chunk Size

- **Smaller chunks** (500 chars): More precise retrieval, but may lose context
- **Larger chunks** (2000 chars): Better context preservation, but less precise matching
- **Default** (1000 chars): Good balance for most use cases

### Overlap

- **No overlap** (0): Fastest ingestion, risk of losing context at boundaries
- **20% of chunk size** (200 for 1000-char chunks): Good default
- **50% of chunk size**: Maximum context preservation, more storage

### Top K

- **Lower K** (3): Faster, more focused results
- **Higher K** (10): More comprehensive, may include less relevant results
- **Default** (5): Balanced retrieval

### Embedding Model

The embedding model significantly affects retrieval quality. The default `all-mpnet-base-v2` provides good general-purpose embeddings. For domain-specific use cases, consider specialized models via the OpenAI or Cohere embedding providers.
