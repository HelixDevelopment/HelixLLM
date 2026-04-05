# Lesson 3: Retrieval Tuning

**Duration:** 25 minutes
**Prerequisites:** Lesson 2 (Vector Stores)
**Learning Objectives:**
- Understand how semantic search works at the vector level
- Tune top-k, chunk size, and overlap for optimal retrieval quality
- Interpret relevance scores and set quality thresholds
- Apply re-ranking strategies to improve result ordering

---

## Scene 1: How Semantic Search Works (5 min)

**Narration:** "When you query the knowledge base, three things happen: your query is embedded into a vector, that vector is compared against all stored chunk vectors using cosine similarity, and the top-k most similar chunks are returned. Let me show you this step by step."

**Screen:** Diagram of the query flow.

```
Query Text
    |
    v
Embedder (same model used during ingestion)
    |
    v
Query Vector (768 dimensions)
    |
    v
Vector Store: cosine_similarity(query_vector, chunk_vector) for all chunks
    |
    v
Top-K results sorted by score (highest first)
    |
    v
Return: text content + score + metadata
```

**Demo steps:**

```bash
# First, ingest some documents for testing
for i in 1 2 3; do
curl -sk -X POST https://localhost:8443/internal/knowledge/ingest \
  -H "Content-Type: application/json" \
  -d "{
    \"content\": \"Document $i about HelixLLM architecture and deployment.\",
    \"collection\": \"retrieval-test\",
    \"metadata\": {\"doc_id\": \"$i\"}
  }"
done

# Now query and observe scores
curl -sk -X POST https://localhost:8443/internal/knowledge/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "How is HelixLLM deployed?",
    "collection": "retrieval-test",
    "top_k": 5
  }' | python3 -m json.tool
```

**Key points:**
- The query is embedded using the same model and provider as during ingestion
- Cosine similarity measures the angle between two vectors (1.0 = identical direction)
- Scores range from -1 to 1, but typical results fall between 0.0 and 1.0
- Using a different embedding model for query versus ingestion produces poor results

---

## Scene 2: Top-K Tuning (5 min)

**Narration:** "The top-k parameter controls how many results are returned from vector search. This is the most impactful retrieval parameter."

**Screen:** Show the effect of different top-k values.

| Top-K | Effect | Best For |
|-------|--------|----------|
| 1-3 | Highly focused, only the most relevant chunks | Simple factual questions |
| 5 (default) | Balanced -- good relevance with context breadth | General use |
| 10 | Comprehensive -- catches more relevant material | Complex queries |
| 20+ | Very broad -- may include noise | Exhaustive research |

**Demo steps:**

```bash
# Compare top-k=1 vs top-k=10
echo "=== top_k=1 ==="
curl -sk -X POST https://localhost:8443/internal/knowledge/query \
  -H "Content-Type: application/json" \
  -d '{"query": "deployment architecture", "collection": "retrieval-test", "top_k": 1}' \
  | python3 -m json.tool

echo "=== top_k=10 ==="
curl -sk -X POST https://localhost:8443/internal/knowledge/query \
  -H "Content-Type: application/json" \
  -d '{"query": "deployment architecture", "collection": "retrieval-test", "top_k": 10}' \
  | python3 -m json.tool
```

**Narration:** "With top-k=1, you get only the single best match. With top-k=10, you get more results but the lower-ranked ones may be less relevant. The default of 5 is a good starting point. Tune it based on your use case -- factual lookups benefit from lower k, while research queries benefit from higher k."

**Key points:**
- `HELIX_RAG_TOP_K=5` is the default
- Can be overridden per-query in the API request
- Lower k is faster and more focused
- Higher k provides more context but may dilute relevance
- There is no benefit to setting k higher than the number of stored chunks

---

## Scene 3: Chunk Size Impact on Retrieval (5 min)

**Narration:** "Chunk size affects retrieval in two ways: it determines the granularity of your search results and how much context each result provides."

**Screen:** Show the chunk size trade-off diagram.

```
Small chunks (500 chars):
  [chunk A] [chunk B] [chunk C] [chunk D] [chunk E]
  - More precise matching (each chunk is focused)
  - Less context per result
  - More chunks to search through

Large chunks (2000 chars):
  [    chunk A    ] [    chunk B    ]
  - More context per result
  - Less precise matching (mixed topics in one chunk)
  - Fewer chunks to search through
```

**Demo steps:**

```bash
# Ingest the same long document with different chunk sizes to compare

# Default: 1000 chars
HELIX_RAG_CHUNK_SIZE=1000 make dev &
# Ingest a document, note chunk count in response

# Small chunks: 500 chars
HELIX_RAG_CHUNK_SIZE=500 make dev &
# Ingest the same document, note higher chunk count

# Large chunks: 2000 chars
HELIX_RAG_CHUNK_SIZE=2000 make dev &
# Ingest the same document, note lower chunk count
```

**Narration:** "A practical approach is to start with the default 1000 characters, test with your actual documents, and adjust. If your retrieval results contain too much irrelevant text, reduce the chunk size. If results lack context, increase it."

**Key points:**
- Chunk size must be tuned to your content type
- Code and technical docs often benefit from larger chunks (1500-2000)
- FAQ-style content benefits from smaller chunks (300-500)
- Always re-ingest documents after changing chunk size
- Overlap should be roughly 20% of chunk size

---

## Scene 4: Relevance Scoring (5 min)

**Narration:** "Understanding relevance scores helps you decide which results to trust. Let me show you how to interpret scores and set quality thresholds."

**Demo steps:**

```bash
# Ingest documents with varying relevance to a test query
curl -sk -X POST https://localhost:8443/internal/knowledge/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "content": "The HelixLLM mode system uses HELIX_MODE to select which layers are active. In full mode, all layers run in a single process. In distributed mode, each host runs a specific mode.",
    "collection": "scoring-test",
    "metadata": {"topic": "modes"}
  }'

curl -sk -X POST https://localhost:8443/internal/knowledge/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "content": "Go is a statically typed, compiled programming language designed at Google. It is known for its simplicity, concurrency support, and fast compilation.",
    "collection": "scoring-test",
    "metadata": {"topic": "go-language"}
  }'

# Query and examine scores
curl -sk -X POST https://localhost:8443/internal/knowledge/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "How do deployment modes work in HelixLLM?",
    "collection": "scoring-test",
    "top_k": 5
  }' | python3 -m json.tool
```

**Narration:** "The first document about modes should score much higher than the Go language document. Here is how to interpret the scores."

**Screen:** Score interpretation guide.

| Score Range | Interpretation | Action |
|-------------|---------------|--------|
| 0.90 - 1.00 | Near-exact match | High confidence, use directly |
| 0.80 - 0.90 | Strong relevance | Good for RAG context |
| 0.70 - 0.80 | Moderate relevance | May be useful, review carefully |
| 0.50 - 0.70 | Weak relevance | Likely noise, consider filtering |
| Below 0.50 | Not relevant | Discard |

**Key points:**
- Scores are relative to the embedding model and content
- Set a minimum threshold (e.g., 0.7) to filter noise
- The absolute score value varies by embedding model
- Compare scores within the same collection and model for consistency

---

## Scene 5: Re-Ranking Strategies (5 min)

**Narration:** "Vector similarity is fast but imprecise. Re-ranking applies a more sophisticated model to reorder the initial results. The idea is: retrieve broadly with vector search, then refine with a re-ranker."

**Screen:** Show the two-stage retrieval pipeline.

```
Query
  |
  v
Stage 1: Vector Search (fast, approximate)
  - Retrieve top-20 candidates from vector store
  |
  v
Stage 2: Re-Ranking (slower, more accurate)
  - Score each candidate with a cross-encoder model
  - Return top-5 after re-ranking
```

**Narration:** "HelixLLM's vector search provides the first stage. For production systems with high accuracy requirements, you can implement a re-ranking step in your client application. Retrieve more results than needed with a higher top-k, then re-rank on the client side."

**Demo steps:**

```bash
# Retrieve a broad set of candidates
curl -sk -X POST https://localhost:8443/internal/knowledge/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "How do I configure authentication?",
    "collection": "project-docs",
    "top_k": 20
  }' | python3 -c "
import sys, json
results = json.load(sys.stdin).get('results', [])
print(f'Retrieved {len(results)} candidates')
for i, r in enumerate(results):
    print(f'  {i+1}. score={r[\"score\"]:.4f} source={r[\"metadata\"].get(\"source\", \"unknown\")}')
"
```

**Key points:**
- Two-stage retrieval: broad vector search followed by precise re-ranking
- Over-retrieve with higher top-k, then filter on the client side
- Cross-encoder models provide more accurate relevance scoring
- Re-ranking adds latency but improves result quality
- For most use cases, vector search with well-tuned parameters is sufficient

---

## Exercises

1. Ingest 10 documents on different topics, then query with varying top-k values (1, 5, 10, 20) and chart how the average relevance score changes
2. Ingest the same document set with chunk sizes 500, 1000, and 2000, query each, and compare which chunk size produces the highest-scoring results for your queries
3. Implement a client-side re-ranking script: retrieve top-20 results, filter out any with scores below 0.7, and return the top 5 remaining
