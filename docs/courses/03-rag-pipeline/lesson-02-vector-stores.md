# Lesson 2: Vector Stores

**Duration:** 30 minutes
**Prerequisites:** Lesson 1 (Document Ingestion)
**Learning Objectives:**
- Set up and configure each supported vector database backend
- Understand the trade-offs between Qdrant, pgvector, Milvus, and Pinecone
- Switch between vector stores using configuration
- Verify vector store health and inspect stored data

---

## Scene 1: Vector Store Overview (4 min)

**Narration:** "The vector store is where your embedded chunks live. HelixLLM supports four production backends plus an in-memory store for development. Each backend has different strengths, and the choice depends on your scale, existing infrastructure, and operational requirements."

**Screen:** Show the comparison table.

| Backend | Type | Best For | Scaling | Operations |
|---------|------|----------|---------|------------|
| Qdrant | Dedicated vector DB | Production, large-scale | Horizontal | Container |
| pgvector | PostgreSQL extension | When PostgreSQL exists | Vertical | Existing DB |
| Milvus | Distributed vector DB | Very large datasets | Horizontal | Complex |
| Pinecone | Managed cloud | Serverless deployments | Automatic | Zero-ops |
| In-memory | Built-in | Development, testing | None | Zero config |

**Key points:**
- Configure via `HELIX_VECTOR_DB` in `.env`
- In-memory store is the automatic fallback when no external database is available
- All backends implement the same `VectorStore` interface: `Upsert`, `Search`, `Delete`, `Collections`, `Stats`
- Switching backends requires only a configuration change -- no code changes

---

## Scene 2: Qdrant Setup (7 min)

**Narration:** "Qdrant is the recommended vector database for production use. It is purpose-built for vector search, supports filtering, and scales horizontally. Let me show you how to set it up."

**Demo steps:**

```bash
# Start Qdrant as a container
podman run -d \
  --name qdrant \
  -p 6333:6333 \
  -p 6334:6334 \
  -v qdrant-data:/qdrant/storage:z \
  qdrant/qdrant
```

**Narration:** "Qdrant exposes two ports: 6333 for the REST API and 6334 for gRPC. The volume mount ensures data persists across container restarts."

```bash
# Verify Qdrant is running
curl -s http://localhost:6333/healthz
```

**Narration:** "Now configure HelixLLM to use Qdrant."

```bash
# Configure in .env
HELIX_VECTOR_DB=qdrant
```

```bash
# Restart HelixLLM and verify
make dev

# Ingest a test document
curl -sk -X POST https://localhost:8443/internal/knowledge/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "content": "Qdrant is a vector similarity search engine built in Rust.",
    "collection": "test",
    "metadata": {"source": "test"}
  }'

# Query to verify
curl -sk -X POST https://localhost:8443/internal/knowledge/query \
  -H "Content-Type: application/json" \
  -d '{"query": "vector search engine", "collection": "test", "top_k": 3}' \
  | python3 -m json.tool
```

**Key points:**
- Qdrant is the default `HELIX_VECTOR_DB` value
- Runs as a lightweight container (~100 MB image)
- Supports collection-level configuration and metadata filtering
- Volume mount at `/qdrant/storage` for data persistence
- Snapshots for backup: `curl -X POST http://localhost:6333/collections/test/snapshots`

---

## Scene 3: pgvector Setup (6 min)

**Narration:** "If you already run PostgreSQL, pgvector adds vector search as an extension. This avoids introducing another service to your stack."

**Demo steps:**

```bash
# Start PostgreSQL with pgvector extension
podman run -d \
  --name postgres-vector \
  -p 5432:5432 \
  -e POSTGRES_DB=helixllm \
  -e POSTGRES_USER=helix \
  -e POSTGRES_PASSWORD=helix123 \
  -v pgdata:/var/lib/postgresql/data:z \
  pgvector/pgvector:pg16
```

**Narration:** "The pgvector/pgvector image is PostgreSQL with the vector extension pre-installed. After starting, enable the extension."

```bash
# Enable the vector extension
podman exec postgres-vector psql -U helix -d helixllm \
  -c "CREATE EXTENSION IF NOT EXISTS vector;"
```

**Narration:** "Now configure HelixLLM to use pgvector."

```bash
# Configure in .env
HELIX_VECTOR_DB=pgvector
HELIX_DB_HOST=localhost
HELIX_DB_PORT=5432
HELIX_DB_NAME=helixllm
HELIX_DB_USER=helix
HELIX_DB_PASSWORD=helix123
```

```bash
# Restart and test
make dev

curl -sk -X POST https://localhost:8443/internal/knowledge/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "content": "pgvector adds vector similarity search to PostgreSQL.",
    "collection": "test",
    "metadata": {"source": "test"}
  }'
```

**Key points:**
- Uses standard PostgreSQL with the `vector` extension
- Leverages existing PostgreSQL infrastructure and tooling
- Supports SQL-based filtering alongside vector search
- Best for teams already running PostgreSQL
- Vertical scaling via PostgreSQL configuration

---

## Scene 4: Milvus and Pinecone (6 min)

**Narration:** "For very large datasets, Milvus provides distributed vector search. For managed cloud deployments, Pinecone eliminates operational overhead entirely."

**Screen:** Milvus setup.

```bash
# Milvus requires multiple components -- use their compose file
curl -sL https://github.com/milvus-io/milvus/releases/download/v2.4.0/milvus-standalone-docker-compose.yml \
  -o milvus-compose.yaml

podman compose -f milvus-compose.yaml up -d
```

```bash
# Configure HelixLLM for Milvus
HELIX_VECTOR_DB=milvus
```

**Screen:** Pinecone setup.

```bash
# Pinecone is cloud-managed -- no containers needed
# Configure in .env with your Pinecone API key
HELIX_VECTOR_DB=pinecone
# Pinecone credentials are configured via the pinecone client settings
```

**Narration:** "Milvus is the right choice when you have billions of vectors and need distributed search. Pinecone is ideal when you want zero operational burden -- it handles scaling, backups, and availability automatically."

**Key points:**
- Milvus: distributed, handles billions of vectors, requires more infrastructure
- Pinecone: fully managed cloud service, zero operations, pay-per-use
- Both support the same HelixLLM VectorStore interface
- Choose based on your scale, team, and operational preferences

---

## Scene 5: In-Memory Fallback (4 min)

**Narration:** "When no external vector database is available, HelixLLM automatically uses an in-memory vector store. This is the zero-configuration option for development and testing."

**Demo steps:**

```bash
# No vector DB configuration needed -- just start the server
# Remove or comment out HELIX_VECTOR_DB in .env
make dev
```

**Narration:** "The in-memory store implements the full VectorStore interface. Documents, collections, and search all work. The only limitation is that data does not persist across server restarts."

```bash
# Verify in-memory store works
curl -sk -X POST https://localhost:8443/internal/knowledge/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "content": "The in-memory store requires no external services.",
    "collection": "test",
    "metadata": {"source": "test"}
  }'

curl -sk -X POST https://localhost:8443/internal/knowledge/query \
  -H "Content-Type: application/json" \
  -d '{"query": "no external services", "collection": "test"}' \
  | python3 -m json.tool

# Check stats
curl -sk https://localhost:8443/internal/knowledge/stats | python3 -m json.tool
```

**Key points:**
- Automatic fallback when no external vector DB is configured or reachable
- Full API compatibility -- same endpoints, same response format
- Data lives in process memory and is lost on restart
- Perfect for development, CI/CD pipelines, and testing

---

## Scene 6: Choosing the Right Backend (3 min)

**Narration:** "Let me summarize when to use each backend."

**Screen:** Decision flowchart.

| Scenario | Recommended Backend |
|----------|-------------------|
| Local development | In-memory (default fallback) |
| Small production (< 100K vectors) | Qdrant or pgvector |
| Existing PostgreSQL infrastructure | pgvector |
| Large scale (millions of vectors) | Qdrant or Milvus |
| Very large scale (billions) | Milvus |
| Serverless / zero-ops | Pinecone |
| CI/CD test pipeline | In-memory |

**Key points:**
- Start with the in-memory store, graduate to Qdrant for production
- pgvector if you already run PostgreSQL
- Milvus for very large datasets that need distributed search
- Pinecone for managed cloud with no infrastructure management

---

## Exercises

1. Set up Qdrant in a container, configure HelixLLM to use it, ingest five documents, and verify search results
2. Switch to pgvector by changing only the `.env` file, re-ingest the same documents, and compare search scores with Qdrant
3. Use the in-memory store to run the full ingest-query cycle, then restart the server and confirm that data was not persisted
