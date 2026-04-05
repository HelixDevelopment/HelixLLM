# Lesson 1: Document Ingestion

**Duration:** 25 minutes
**Prerequisites:** Course 1 (Getting Started)
**Learning Objectives:**
- Ingest documents into the HelixLLM knowledge base via the API
- Understand how documents are chunked before embedding
- Configure chunk size and overlap for different use cases
- Organize documents into collections with metadata

---

## Scene 1: The Ingestion Pipeline (4 min)

**Narration:** "Retrieval-Augmented Generation starts with getting your documents into the system. The ingestion pipeline has three stages: chunking, embedding, and storage. A document goes in as raw text, gets split into manageable pieces, each piece is converted to a vector embedding, and the vectors are stored in a database for later retrieval."

**Screen:** Show the ingestion flow diagram.

```
Document -> Chunker -> Embedder -> Vector Store
              |            |            |
         Split into    Convert to   Store vectors
         overlapping   768-dim      with metadata
         chunks        vectors
```

**Key points:**
- Ingestion is a three-stage pipeline: chunk, embed, store
- Documents are split into chunks because LLMs have limited context windows
- Each chunk is independently searchable
- Metadata is preserved for provenance tracking

---

## Scene 2: Ingesting Your First Document (6 min)

**Narration:** "Let us ingest a document through the API. The endpoint is /internal/knowledge/ingest. You provide the text content, a collection name, and optional metadata."

**Demo steps:**

```bash
# Ingest a document
curl -sk -X POST https://localhost:8443/internal/knowledge/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "content": "HelixLLM is an enterprise-grade distributed LLM system built in Go with Gin Gonic. It operates as a single binary with a mode system that enables flexible deployment from single-host development to multi-host production clusters. The system supports HTTP/3 with QUIC for modern clients and falls back to HTTP/2. It includes authentication via API keys and JWT tokens, rate limiting with sliding window algorithms, and security headers following OWASP best practices.",
    "collection": "project-docs",
    "metadata": {
      "source": "overview.md",
      "title": "Project Overview",
      "author": "engineering"
    }
  }' | python3 -m json.tool
```

**Expected response:**

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "collection": "project-docs",
  "chunks": 1,
  "status": "completed"
}
```

**Narration:** "The response tells us the document was split into one chunk, which makes sense because our text was under the default chunk size of 1000 characters. Let me ingest a longer document to see chunking in action."

```bash
# Ingest a longer document that will be split into multiple chunks
curl -sk -X POST https://localhost:8443/internal/knowledge/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "content": "The Gateway layer handles all HTTP traffic in HelixLLM. It supports HTTP/3 with QUIC for modern clients and falls back to HTTP/2 via ALPN negotiation. The gateway includes authentication middleware that validates API keys and JWT tokens, rate limiting middleware using sliding window algorithms, security headers middleware following OWASP guidelines, and content negotiation for JSON and TOON formats. All endpoints are served over TLS 1.3 with no plaintext HTTP option. The Brain layer is the LLM coordination layer. It routes requests to the appropriate provider based on the model name in each request. Three providers are built in: llama.cpp for local inference using GGUF models, OpenAI for cloud inference with GPT models, and Anthropic for Claude models. The router uses pattern matching on model names -- models starting with gpt route to OpenAI, models starting with claude route to Anthropic, and all others route to the local llama.cpp instance. When set to auto mode, the brain selects the best provider automatically. The Knowledge layer implements the RAG pipeline. It chunks incoming documents using a fixed-size chunker with configurable overlap, generates embeddings using one of five supported providers, and stores the vectors in a configured vector database. At query time, the user query is embedded using the same model, and the closest vectors are retrieved using cosine similarity search. The top results are returned with their original text content and metadata for use as context in LLM prompts.",
    "collection": "project-docs",
    "metadata": {
      "source": "architecture.md",
      "title": "Architecture Deep Dive"
    }
  }' | python3 -m json.tool
```

**Expected response:**

```json
{
  "id": "660e8400-e29b-41d4-a716-446655440001",
  "collection": "project-docs",
  "chunks": 2,
  "status": "completed"
}
```

**Narration:** "This longer document was split into two chunks because it exceeds the 1000-character default chunk size."

**Key points:**
- Endpoint: `POST /internal/knowledge/ingest`
- Required fields: `content` (text) and `collection` (name)
- Optional: `metadata` (arbitrary key-value pairs)
- Collections are created automatically on first use
- Response includes the number of chunks created

---

## Scene 3: Chunking Strategies (5 min)

**Narration:** "Chunking is the most important decision in the ingestion pipeline. The chunk size determines how much context each vector captures, and the overlap prevents information loss at chunk boundaries."

**Screen:** Show chunking visualization.

```
Document: |------- 2500 characters -------|

Chunk size: 1000, Overlap: 200

Chunk 1: |---- chars 0-999 ----|
Chunk 2:          |---- chars 800-1799 ----|
Chunk 3:                    |---- chars 1600-2499 ----|
                  ^overlap^           ^overlap^
```

**Narration:** "The default chunker uses fixed-size splitting with overlap. For a 2500-character document with chunk size 1000 and overlap 200, you get three chunks. The 200-character overlap ensures that sentences spanning a boundary are captured in both adjacent chunks."

**Demo steps:**

```bash
# View current chunking configuration
# These are set in .env:
# HELIX_RAG_CHUNK_SIZE=1000
# HELIX_RAG_CHUNK_OVERLAP=200
```

**Screen:** Show the trade-offs table.

| Chunk Size | Pros | Cons | Best For |
|-----------|------|------|----------|
| Small (500) | Precise retrieval, focused results | May lose broader context | FAQ, short answers |
| Medium (1000) | Good balance of precision and context | Default choice | General documents |
| Large (2000) | Better context preservation | Less precise matching | Technical docs, code |

| Overlap | Pros | Cons |
|---------|------|------|
| None (0) | Fastest ingestion, least storage | Risk of losing context at boundaries |
| 20% (200 for 1000) | Good default, captures boundary text | Moderate storage increase |
| 50% (500 for 1000) | Maximum context preservation | Doubles storage requirements |

**Key points:**
- `HELIX_RAG_CHUNK_SIZE` -- maximum characters per chunk (default: 1000)
- `HELIX_RAG_CHUNK_OVERLAP` -- character overlap between chunks (default: 200)
- Smaller chunks give more precise search but less context per result
- Larger chunks preserve context but may include irrelevant text
- Overlap prevents information loss at chunk boundaries

---

## Scene 4: Collections and Metadata (5 min)

**Narration:** "Collections let you organize documents by domain, project, or access level. Each collection is an independent namespace in the vector store."

**Demo steps:**

```bash
# Ingest into different collections
curl -sk -X POST https://localhost:8443/internal/knowledge/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "content": "The deployment process involves building the container image with make container, then deploying to the cluster using the control plane API.",
    "collection": "ops-docs",
    "metadata": {"source": "deployment.md", "category": "operations"}
  }'

curl -sk -X POST https://localhost:8443/internal/knowledge/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "content": "All API endpoints return errors in a consistent JSON format with message and type fields. Common HTTP status codes include 400 for bad requests and 401 for unauthorized access.",
    "collection": "api-docs",
    "metadata": {"source": "api-reference.md", "category": "api"}
  }'
```

**Narration:** "Now let us list all collections and check statistics."

```bash
# List all collections
curl -sk https://localhost:8443/internal/knowledge/collections | python3 -m json.tool

# Get overall statistics
curl -sk https://localhost:8443/internal/knowledge/stats | python3 -m json.tool
```

**Expected stats response:**

```json
{
  "total_documents": 4,
  "total_chunks": 5,
  "collections": 3,
  "embedding_dimensions": 768
}
```

**Key points:**
- Collections are created automatically when you ingest into them
- Metadata is attached to every chunk for filtering and provenance
- Use collections to separate domains (docs, code, support tickets)
- The stats endpoint shows aggregate counts across all collections

---

## Scene 5: Verifying Ingestion (3 min)

**Narration:** "After ingesting documents, verify they are searchable by running a quick query."

**Demo steps:**

```bash
# Query the knowledge base to verify ingestion
curl -sk -X POST https://localhost:8443/internal/knowledge/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "How does the mode system work?",
    "collection": "project-docs",
    "top_k": 3
  }' | python3 -m json.tool
```

**Expected response:**

```json
{
  "results": [
    {
      "content": "HelixLLM is an enterprise-grade distributed LLM system...",
      "score": 0.89,
      "metadata": {
        "source": "overview.md",
        "title": "Project Overview",
        "chunk_index": 0
      }
    }
  ]
}
```

**Narration:** "The query returns the most relevant chunks ranked by similarity score. A score above 0.8 indicates a strong semantic match. We will dive deeper into retrieval tuning in Lesson 3."

**Key points:**
- Always verify ingestion by running a test query
- Scores above 0.8 indicate strong relevance
- Results include the original text content and metadata
- `chunk_index` tracks which chunk of the original document was matched

---

## Exercises

1. Ingest three different documents into a collection called "test-docs" and verify the chunk count matches your expectations based on document length and chunk size
2. Experiment with `HELIX_RAG_CHUNK_SIZE` set to 500 and 2000, ingest the same document, and compare the number of chunks produced
3. Ingest documents into two separate collections, then query each collection independently to verify isolation
