# Lesson 4: Embeddings

**Duration:** 20 minutes
**Prerequisites:** Course 1 (Getting Started)
**Learning Objectives:**
- Generate text embeddings through the HelixLLM API
- Understand embedding dimensions and their impact on search quality
- Compare local and cloud embedding providers
- Process batch embedding requests efficiently

---

## Scene 1: What Are Embeddings? (3 min)

**Narration:** "Embeddings are numerical representations of text -- vectors that capture semantic meaning. Two sentences with similar meanings produce vectors that are close together in vector space. This is the foundation of semantic search and the RAG pipeline we will cover in Course 3."

**Screen:** Diagram showing text mapped to vectors and similarity between them.

**Key points:**
- Embeddings convert text to fixed-length numerical vectors
- Similar text produces similar vectors (measured by cosine similarity)
- Used for semantic search, clustering, classification, and RAG
- HelixLLM supports multiple embedding providers

---

## Scene 2: Generating Embeddings (5 min)

**Narration:** "The embeddings endpoint follows the OpenAI specification. Let me generate an embedding and examine the response."

**Demo steps:**

```bash
# Generate an embedding for a single text
curl -sk https://localhost:8443/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{
    "model": "all-mpnet-base-v2",
    "input": "HelixLLM is a distributed LLM system built in Go."
  }' | python3 -m json.tool
```

**Expected response (truncated):**

```json
{
  "object": "list",
  "data": [
    {
      "object": "embedding",
      "index": 0,
      "embedding": [0.0123, -0.0456, 0.0789, -0.0234, 0.0567, "..."]
    }
  ],
  "model": "all-mpnet-base-v2",
  "usage": {
    "prompt_tokens": 12,
    "total_tokens": 12
  }
}
```

**Narration:** "The embedding field is an array of floating-point numbers. The default local model produces 768-dimensional vectors. Each dimension captures a different aspect of the text's meaning."

```bash
# Check the embedding dimensions
curl -sk https://localhost:8443/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{
    "model": "all-mpnet-base-v2",
    "input": "Hello world"
  }' | python3 -c "import sys, json; data=json.load(sys.stdin); print(f'Dimensions: {len(data[\"data\"][0][\"embedding\"])}')"
```

**Key points:**
- Endpoint: `POST /v1/embeddings`
- Response contains an array of embedding objects
- Each embedding is a vector of floating-point numbers
- Dimensions depend on the model (768 for all-mpnet-base-v2)

---

## Scene 3: Embedding Providers (5 min)

**Narration:** "HelixLLM supports five embedding providers. Each has different trade-offs in quality, speed, and cost."

**Screen:** Provider comparison table.

| Provider | Config Value | Dimensions | Quality | Speed | Cost |
|----------|-------------|------------|---------|-------|------|
| Local (default) | `local` | 768 | Good | Fast (no network) | Free |
| OpenAI | `openai` | 1536/3072 | Excellent | Medium | Per-token |
| Cohere | `cohere` | 1024 | Excellent | Medium | Per-token |
| Voyage | `voyage` | 1024 | Very good | Medium | Per-token |
| Jina | `jina` | 768 | Very good | Medium | Per-token |

**Narration:** "Configure the embedding provider in your .env file."

```bash
# Switch embedding providers
HELIX_EMBEDDING_PROVIDER=local           # Default, no API key needed
HELIX_EMBEDDING_MODEL=all-mpnet-base-v2

# Or use OpenAI embeddings
HELIX_EMBEDDING_PROVIDER=openai
HELIX_EMBEDDING_MODEL=text-embedding-3-small

# Or Cohere
HELIX_EMBEDDING_PROVIDER=cohere
HELIX_EMBEDDING_MODEL=embed-english-v3.0
```

**Narration:** "The local provider works without any external services. If the configured provider is unreachable, HelixLLM falls back to a hash-based embedder. This ensures the system always starts, though search quality will be reduced."

**Key points:**
- `HELIX_EMBEDDING_PROVIDER` selects the backend
- `HELIX_EMBEDDING_MODEL` specifies the model name
- Local provider requires no API keys or network access
- Graceful fallback to hash-based embedder when provider is unavailable

---

## Scene 4: Batch Embeddings (4 min)

**Narration:** "For efficiency, you can embed multiple texts in a single request by passing an array."

**Demo steps:**

```bash
# Batch embedding request
curl -sk https://localhost:8443/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{
    "model": "all-mpnet-base-v2",
    "input": [
      "HelixLLM supports local inference with llama.cpp.",
      "The RAG pipeline uses vector embeddings for semantic search.",
      "Agents can call tools during the ReAct loop.",
      "The control plane manages multi-host deployments."
    ]
  }' | python3 -c "
import sys, json
data = json.load(sys.stdin)
for item in data['data']:
    dims = len(item['embedding'])
    print(f'Index {item[\"index\"]}: {dims} dimensions, first 3 values: {item[\"embedding\"][:3]}')
print(f'Total tokens: {data[\"usage\"][\"total_tokens\"]}')
"
```

**Expected output:**

```
Index 0: 768 dimensions, first 3 values: [0.0123, -0.0456, 0.0789]
Index 1: 768 dimensions, first 3 values: [0.0234, -0.0567, 0.0891]
Index 2: 768 dimensions, first 3 values: [0.0345, -0.0678, 0.0912]
Index 3: 768 dimensions, first 3 values: [0.0456, -0.0789, 0.0123]
Total tokens: 48
```

**Narration:** "Batch requests are more efficient than individual calls because they share connection overhead and allow the embedding provider to parallelize internally."

**Key points:**
- Pass an array of strings in `input` for batch processing
- Each embedding in the response has an `index` matching its position
- Batch requests reduce network round trips
- Token usage is summed across all inputs

---

## Scene 5: Computing Similarity (3 min)

**Narration:** "Once you have embeddings, you compute similarity using cosine similarity. Two vectors with a cosine similarity close to 1.0 are semantically similar."

**Screen:** Python code for computing similarity.

```python
import numpy as np
import httpx
import json

def get_embedding(text):
    resp = httpx.post(
        "https://localhost:8443/v1/embeddings",
        json={"model": "all-mpnet-base-v2", "input": text},
        verify=False,
    )
    return resp.json()["data"][0]["embedding"]

def cosine_similarity(a, b):
    a, b = np.array(a), np.array(b)
    return np.dot(a, b) / (np.linalg.norm(a) * np.linalg.norm(b))

# Compare semantic similarity
e1 = get_embedding("How do I configure HelixLLM?")
e2 = get_embedding("What are the configuration options?")
e3 = get_embedding("The weather is nice today.")

print(f"Similar queries:   {cosine_similarity(e1, e2):.4f}")  # ~0.85+
print(f"Unrelated queries: {cosine_similarity(e1, e3):.4f}")  # ~0.10
```

**Narration:** "The two configuration-related queries have a high similarity score, while the unrelated weather query scores much lower. This is exactly how the RAG pipeline finds relevant documents -- it embeds the query, then finds stored chunks with the highest cosine similarity."

**Key points:**
- Cosine similarity ranges from -1 to 1 (higher = more similar)
- Semantically similar text scores above 0.7
- Unrelated text typically scores below 0.3
- This is the mathematical basis for vector search in RAG

---

## Exercises

1. Generate embeddings for five sentences about Go programming and five about Python, then compute pairwise cosine similarity to verify that same-language sentences cluster together
2. Switch between the local and OpenAI embedding providers and compare the dimensions and similarity scores for the same set of inputs
3. Write a batch embedding script that processes a text file line by line and stores the embeddings as a JSON array for later use
