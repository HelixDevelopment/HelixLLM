# RAG: Qdrant Vector Store + Cross-Encoder Reranker (TEI)

Companion to [`rag-knowledge.md`](rag-knowledge.md), covering the real
**Qdrant** vector-store backend and the real **cross-encoder reranker**
(HuggingFace Text Embeddings Inference, TEI) that HelixLLM's RAG pipeline
(`internal/knowledge/`) can use.

As of `submodules/helix_llm` HEAD `e2ce163` (wave-2 fixes), all of the
following are production-wired and end-to-end verified, not just harness-proven:

- Qdrant vector-store backing (`HELIX_VECTOR_DB=qdrant`, the default).
- Qdrant host/port are env-configurable (`HELIX_VECTOR_DB_HOST` /
  `HELIX_VECTOR_DB_PORT`).
- Document ingest into Qdrant (chunk-ID -> Qdrant point-ID mapping fixed).
- The cross-encoder reranker (`TEIReranker`) is wired into
  `Pipeline.Query`'s production ranking path, config-gated.
- The default (hash-based) embedder now logs a startup WARNING so an
  operator can immediately tell RAG retrieval quality is degraded.

## Components

| Component | Real implementation | Source |
|---|---|---|
| Vector store | `QdrantStore` (`internal/knowledge/qdrant.go`) wraps `digital.vasic.vectordb/pkg/qdrant`, a real HTTP client to Qdrant's REST API | `internal/knowledge/qdrant.go` |
| Embeddings (dev/prod default) | `HashEmbedder` — deterministic, NOT a semantic embedder (see caveat below); a startup WARNING now fires when it is in use | `internal/knowledge/embedding_providers.go`, `cmd/helixllm/main.go: buildEmbedder` |
| Embeddings (real semantic, opt-in) | `OpenAIEmbedder` (OpenAI API) or `LlamaEmbedder` (local llama.cpp `/embedding` endpoint) | `internal/knowledge/embedding_providers.go`, `internal/knowledge/llama_embedder.go` |
| Cross-encoder reranker (production-wired, opt-in) | `TEIReranker` (`internal/knowledge/reranker.go`) hits a real TEI `/rerank` endpoint; wired into `Pipeline.Query` via `PipelineConfig.Reranker` | `internal/knowledge/reranker.go`, `internal/knowledge/pipeline.go`, `cmd/helixllm/main.go` |
| Cross-encoder reranker (other implementations) | `ScoreReranker` (passthrough sort, used when no reranker is configured) and `LLMReranker` (asks the chat-completion LLM to score relevance — a different mechanism from a cross-encoder) also implement the `Reranker` interface | `internal/knowledge/reranker.go` |

## Setup

### 1. Vector store: point HelixLLM at a real Qdrant instance

```bash
# Boot Qdrant via the containers submodule orchestrator (§11.4.76) —
# never a raw `docker run`/`podman run` (§11.4.161 rootless podman).
# Minimal standalone example (adapt host port to your compose file):
#   image: docker.io/qdrant/qdrant:latest
#   ports: ["6333:6333", "6334:6334"]   # REST : gRPC

export HELIX_VECTOR_DB=qdrant   # this IS the default — env var is optional
```

**Host/port are env-configurable.** `cmd/helixllm/main.go` resolves the
Qdrant target via `cfg.Knowledge.VectorDBHost` / `cfg.Knowledge.VectorDBPort`
(`internal/shared/config/config.go`):

```bash
export HELIX_VECTOR_DB_HOST=localhost   # default; set to a remote host as needed
export HELIX_VECTOR_DB_PORT=6333        # default REST port
```

gRPC is assumed at REST port + 1 (`6334` by default,
`internal/knowledge/qdrant.go: NewQdrantStore`). If Qdrant is unreachable at
startup, HelixLLM logs a warning and falls back to the in-memory store
(`cmd/helixllm/main.go`) — ingested data will NOT persist across a restart
in that case. Check server startup logs for `"failed to connect to vector
store, falling back to memory store"` to confirm which backend is actually
live.

### 2. Embeddings: choose a real semantic embedder

The default (`HELIX_EMBEDDING_PROVIDER=local`, and also the `hash`/`""`
values, or embedder-construction failure) resolves to `HashEmbedder` — a
**deterministic hash-based pseudo-embedding**, not a trained semantic model.
It is useful for wiring/plumbing tests (deterministic, no external
dependency) but produces **no meaningful semantic similarity** — cosine
search over hash vectors will not rank documents by meaning.

`cmd/helixllm/main.go: buildEmbedder` now logs a startup WARNING whenever
the constructed embedder is a `HashEmbedder`, regardless of which of the
above four causes led there — the discriminator is a type assertion on the
constructed embedder, not a string match on the configured provider name, so
the warning fires reliably including on embedder-construction-error
fallback:

```
level=warning embedding_provider=local msg="RAG embeddings are using the
non-semantic HashEmbedder (HELIX_EMBEDDING_PROVIDER=local/hash, unset, or
unrecognised, or embedder construction failed) — embeddings do NOT capture
semantic similarity and RAG retrieval quality will be significantly
degraded; set HELIX_EMBEDDING_PROVIDER to a real provider (e.g. \"openai\"
or \"llama\" pointing at a real embedding-serving endpoint) for
production-quality RAG"
```

For real retrieval quality, set one of:

```bash
# Option A — OpenAI embeddings API
export HELIX_EMBEDDING_PROVIDER=openai
export HELIX_EMBEDDING_MODEL=text-embedding-3-small
# API key is read from cfg.LLM.OpenAIKey (the same OpenAI key used for chat)

# Option B — local llama.cpp embedding server (no external API call)
export HELIX_EMBEDDING_PROVIDER=llama
export HELIX_EMBEDDING_BASE_URL=http://localhost:8080   # llama-server with an embedding-capable model
export HELIX_EMBEDDING_MODEL=<model-name-as-loaded-by-llama-server>
```

There is **no `tei` embedding provider** in `NewEmbedder`'s switch — if you
want a TEI-served embedding model (e.g. `BAAI/bge-small-en-v1.5`), run it
behind TEI's OpenAI-compatible `/v1/embeddings` endpoint and select
`HELIX_EMBEDDING_PROVIDER=openai` with `HELIX_EMBEDDING_BASE_URL` pointed at
that TEI instance.

All embedders are constructed with a fixed **768** dimension
(`cmd/helixllm/main.go: knowledge.NewEmbedder(..., 768)`), regardless of the
model's native dimension — if you wire a smaller-dimension model (e.g.
`BAAI/bge-small-en-v1.5`'s native 384-dim) into production you must
reconcile this dimension mismatch; Qdrant collections are created with
whatever dimension the first upserted chunk's embedding actually has
(`QdrantStore.Upsert`'s `EnsureCollection` call), so a naive swap will
silently create a differently-sized collection than the rest of the code
assumes.

### 3. Ingest and query

```bash
curl -k -X POST https://localhost:8443/internal/knowledge/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "content": "HelixLLM is an enterprise-grade distributed LLM system.",
    "collection": "project-docs",
    "metadata": {"source": "readme.md"}
  }'

curl -k -X POST https://localhost:8443/internal/knowledge/query \
  -H "Content-Type: application/json" \
  -d '{"query": "What is HelixLLM?", "collection": "project-docs", "top_k": 5}'
```

**Ingest into Qdrant now works correctly.** `FixedSizeChunker` mints chunk
IDs like `"<uuid>-<n>"`, which is not by itself a valid Qdrant point ID
(Qdrant requires an unsigned integer or a UUID). `internal/knowledge/qdrant.go`
now maps any non-Qdrant-valid chunk ID to a deterministic UUIDv5 at the
store boundary (`qdrantPointID()`), while preserving the original semantic
chunk ID in the point payload so `Search`/`chunkFromMetadata` still returns
it — `Delete()` uses the same mapping. Before this fix, a production
`Pipeline.Ingest` -> `QdrantStore.Upsert` call would silently lose every
chunk (Qdrant rejected the point IDs). Live proof (real Qdrant HTTP 400
pre-fix -> real 5-doc ingest + query retrieval post-fix) is captured under
`docs/qa/qdrant_chunkid_fix_wave2_20260711T162425Z/`; fast hermetic unit
coverage lives in `internal/knowledge/qdrant_pointid_test.go`.

## Cross-encoder reranker (TEI `/rerank`) — production-wired

`internal/knowledge/reranker.go` ships `TEIReranker`, which calls a real TEI
`/rerank` HTTP endpoint. `internal/knowledge/pipeline.go`'s `PipelineConfig`
now carries a `Reranker` field: when non-nil, `Pipeline.Query` retrieves a
wider candidate pool (`topK * RerankFetchMultiplier`, default multiplier
`3`) than requested, hands the whole pool to `Reranker.Rerank(query, chunks,
topK)`, and uses the reranker's returned order/trim as the final result —
on **both** the plain vector-only retrieval path and the hybrid
(`HybridEnabled`) + MMR-diversity path. The full flow with reranking enabled
is:

```
embed query -> retrieve (topK * multiplier candidates) -> RERANK (TEI cross-encoder) -> trim to topK -> ground
```

When `Reranker` is nil (the default — reranking is opt-in), `Pipeline.Query`
behaves exactly as it did before the reranker field existed: no over-fetch,
no rerank call, plain ANN order (or MMR order if hybrid search is enabled).
A reranker failure is returned to the caller as an error rather than
silently falling back to unranked order — a caller that explicitly
configured reranking is able to tell when it did not actually happen.

### Enabling the reranker in production

```bash
export HELIX_RAG_RERANK_ENABLED=true
export HELIX_RAG_RERANK_BASE_URL=http://localhost:<tei-rerank-port>   # TEI /rerank instance
export HELIX_RAG_RERANK_FETCH_MULTIPLIER=3   # optional, default 3
```

`cmd/helixllm/main.go` wires `knowledge.NewTEIReranker(cfg.Knowledge.RerankBaseURL)`
into the `Pipeline` when `HELIX_RAG_RERANK_ENABLED=true`. Reranking is
disabled by default (`RerankEnabled` defaults to `false`), so existing
deployments that do not set these variables see no behavior change.

### Running the standalone TEI proof harness

The wave-2 harness that originally proved the TEI `/rerank` protocol live
(before it was wired into `Pipeline`) still exists as a standalone,
independently runnable proof:

```bash
cd submodules/helix_llm/docs/qa/phase3_rag_qdrant_rerank_20260711T142237Z/harness

# 1. Boot qdrant + tei-embed + tei-rerank (containers submodule orchestrator,
#    rootless podman — see compose.qdrant_rerank.yml for the exact 3 services)
go run . boot-up compose.qdrant_rerank.yml <project-name>

# 2. Embed the fixture corpus + a query, upsert to Qdrant, ANN-search
go run . embed-corpus  <tei-embed-base-url>  corpus-emb.json
go run . embed-query   <tei-embed-base-url>  <qkey> query-emb.json
go run . qdrant-upsert <qdrant-base-url> <collection> corpus-emb.json out.json
go run . qdrant-search <qdrant-base-url> <collection> query-emb.json <qkey> <topN> ann.json

# 3. Real cross-encoder rerank via TEI's /rerank endpoint
go run . rerank <tei-rerank-base-url> ann.json <qkey> reranked.json

# 4. RED (no context) vs GREEN (reranked context) generation against the
#    live resident coder, then self-validated analysis
go run . red    <coder-base-url> <qkey> red-out.json
go run . green  <coder-base-url> reranked.json <qkey> green-out.json
go run . analyze ann.json reranked.json green-out.json <qkey>
go run . selfvalidate ann.json reranked.json green-out.json <qkey>

go run . boot-down compose.qdrant_rerank.yml <project-name>
```

`compose.qdrant_rerank.yml` boots exactly three services, each on its own
host port (env-injected, never hardcoded — §CONST-045/046):

| Service | Image | Purpose |
|---|---|---|
| `qdrant` | `docker.io/qdrant/qdrant:latest` | real vector DB, REST `${QDRANT_HTTP_PORT}` + gRPC `${QDRANT_GRPC_PORT}` |
| `tei-embed` | `ghcr.io/huggingface/text-embeddings-inference:cpu-1.9` | `BAAI/bge-small-en-v1.5`, `/embed` |
| `tei-rerank` | `ghcr.io/huggingface/text-embeddings-inference:cpu-1.9` | `BAAI/bge-reranker-base`, `/rerank` |

### TEI `/rerank` request/response shape (verified against upstream docs)

```bash
curl <tei-rerank-base-url>/rerank \
  -X POST \
  -H 'Content-Type: application/json' \
  -d '{"query": "What is Deep Learning?", "texts": ["candidate 1", "candidate 2"], "raw_scores": false}'
```

Response is a JSON array of `{index, score}` objects (one per input text,
`index` referring back to the `texts` array position), sorted by `score`
descending by default. `TEIReranker.Rerank` (`internal/knowledge/reranker.go`)
issues this same request/response shape against `cfg.Knowledge.RerankBaseURL`.

### Qdrant REST shape actually used by `QdrantStore`

`digital.vasic.vectordb/pkg/qdrant` (vendored dependency) drives the
standard Qdrant v1.x REST surface:

- `PUT /collections/{name}` with body `{"vectors": {"size": <dim>,
  "distance": "Cosine"}}` — `QdrantStore.EnsureCollection` sets
  `vdbclient.DistanceCosine` unconditionally; Qdrant also supports `Dot`,
  `Euclid`, `Manhattan`, but HelixLLM's Qdrant integration only ever
  requests Cosine.
- `POST /collections/{name}/points/upsert` — `QdrantStore.Upsert` (point IDs
  passed through `qdrantPointID()`, see the Ingest note above).
- `POST /collections/{name}/points/search` — `QdrantStore.Search`.
- `DELETE /collections/{name}` and point-id deletes — `QdrantStore.DeleteCollection` / `Delete`.

## Sources verified 2026-07-11 (code re-cross-checked same date against `submodules/helix_llm` HEAD `e2ce163`, post wave-2 commits `5f3553b` / `8e18b0c` / `e2ce163`):
- https://qdrant.tech/documentation/concepts/collections/ (collection creation REST shape, distance metrics)
- https://huggingface.co/docs/text-embeddings-inference/en/quick_tour (TEI Docker quick-start, `/embed` and `/rerank` request/response shapes)
