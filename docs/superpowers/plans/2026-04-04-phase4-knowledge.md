# Phase 4: Knowledge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Knowledge layer that provides RAG (Retrieval-Augmented Generation) capabilities for HelixLLM. This covers document ingestion (load, chunk, embed, store), retrieval (query, embed, search, rerank, context assembly), and integration with the Brain layer to inject retrieved context into LLM prompts. All components use in-memory implementations so tests run without external services. Real backends (Qdrant, pgvector) will be activated via `.env` configuration in Phase 6.

**Architecture:** The Knowledge layer defines core types (Document, Chunk, Collection, QueryRequest, QueryResult, IngestRequest), an Embedder interface with a deterministic hash-based mock, a VectorStore interface with an in-memory cosine-similarity implementation, a Chunker that splits text with configurable size and overlap, and a Pipeline that ties ingestion and retrieval together. Gin HTTP handlers expose the knowledge base via `/internal/knowledge/*` endpoints. The Brain's `Complete` flow gains an optional RAG hook that prepends retrieved context to user messages.

**Tech Stack:** Go 1.26+, Gin Gonic, vasic-digital modules (RAG, VectorDB, Embeddings, Document, Filesystem, Database, BackgroundTasks -- added as submodules, not yet wired into implementations), `math` (cosine similarity), `crypto/sha256` (deterministic embeddings), `net/http/httptest` (for API testing)

**Spec Reference:** `docs/superpowers/specs/2026-04-04-helixllm-master-design.md` -- Section 7 (Knowledge Layer Design), Section 5.5 (Internal/Management API for knowledge endpoints)

**Important notes:**
- All implementations are in-memory for this phase. The vasic-digital modules (RAG, VectorDB, Embeddings, etc.) are added as Git submodules and `go.mod` entries but NOT wired into code yet. They will be integrated as a later refinement when real backends are available.
- The mock embedder uses SHA-256 hashing to produce deterministic vectors -- same input always produces the same vector. This allows reliable testing without a real embedding model.
- The in-memory vector store uses brute-force cosine similarity search. This is adequate for testing and small datasets; production will use Qdrant/pgvector via the `digital.vasic.vectordb` interface.
- Tests are written first (TDD) and must pass without any external services running.

---

## File Structure

```
helixllm/
  cmd/helixllm/
    main.go                                Updated to create Knowledge service and wire into server
  internal/
    knowledge/
      types.go                             Core types: Document, Chunk, Collection, QueryRequest, etc.
      types_test.go
      embeddings.go                        Embedder interface + mock hash-based implementation
      embeddings_test.go
      store.go                             VectorStore interface + in-memory cosine-similarity impl
      store_test.go
      chunker.go                           Text chunker with configurable size and overlap
      chunker_test.go
      pipeline.go                          RAG pipeline: ingest + query flows
      pipeline_test.go
      api.go                               Gin HTTP handlers for /internal/knowledge/*
      api_test.go
  submodules/
    RAG/                                   digital.vasic.rag (added, not yet wired)
    VectorDB/                              digital.vasic.vectordb (added, not yet wired)
    Embeddings/                            digital.vasic.embeddings (added, not yet wired)
    Document/                              digital.vasic.document (added, not yet wired)
    Filesystem/                            digital.vasic.filesystem (added, not yet wired)
    Database/                              digital.vasic.database (added, not yet wired)
    BackgroundTasks/                       digital.vasic.backgroundtasks (added, not yet wired)
  go.mod                                   Updated with new submodules + replace directives
  go.sum
```

---

### Task 1: Add Knowledge Submodules

**Files:**
- Modify: `.gitmodules`
- Modify: `go.mod`
- Create: `submodules/` entries for RAG, VectorDB, Embeddings, Document, Filesystem, Database, BackgroundTasks

- [ ] **Step 1: Add Knowledge layer submodules from vasic-digital (GitHub)**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
git submodule add git@github.com:vasic-digital/RAG.git submodules/RAG
git submodule add git@github.com:vasic-digital/VectorDB.git submodules/VectorDB
git submodule add git@github.com:vasic-digital/Embeddings.git submodules/Embeddings
git submodule add git@github.com:vasic-digital/Document.git submodules/Document
git submodule add git@github.com:vasic-digital/Filesystem.git submodules/Filesystem
git submodule add git@github.com:vasic-digital/Database.git submodules/Database
git submodule add git@github.com:vasic-digital/BackgroundTasks.git submodules/BackgroundTasks
```

Expected: each submodule cloned into `submodules/`, `.gitmodules` updated with 7 new entries.

- [ ] **Step 2: Add replace directives to go.mod**

Add these `replace` directives to the existing `replace` block in `go.mod`:

```
replace (
    // ... existing Phase 1 + Phase 2 + Phase 3 replacements ...
    digital.vasic.rag => ./submodules/RAG
    digital.vasic.vectordb => ./submodules/VectorDB
    digital.vasic.embeddings => ./submodules/Embeddings
    digital.vasic.document => ./submodules/Document
    digital.vasic.filesystem => ./submodules/Filesystem
    digital.vasic.database => ./submodules/Database
    digital.vasic.backgroundtasks => ./submodules/BackgroundTasks
)
```

Also add to the `require` block:

```
require (
    // ... existing Phase 1 + Phase 2 + Phase 3 requirements ...
    digital.vasic.rag v0.0.0
    digital.vasic.vectordb v0.0.0
    digital.vasic.embeddings v0.0.0
    digital.vasic.document v0.0.0
    digital.vasic.filesystem v0.0.0
    digital.vasic.database v0.0.0
    digital.vasic.backgroundtasks v0.0.0
)
```

**Note:** These modules are added for availability but are NOT imported by any Go code in this phase. They will be integrated in a follow-up refinement task.

- [ ] **Step 3: Tidy modules**

```bash
go mod tidy
```

Expected: `go.sum` updated, all new dependencies resolved.

- [ ] **Step 4: Verify build**

```bash
go build ./...
```

Expected: builds successfully with all new submodules resolved.

- [ ] **Step 5: Commit**

```bash
git add .gitmodules submodules/ go.mod go.sum
git commit -m "feat: add Phase 4 Knowledge submodules (RAG, VectorDB, Embeddings, Document, Filesystem, Database, BackgroundTasks)"
```

---

### Task 2: Knowledge Types

**Files:**
- Create: `internal/knowledge/types.go`
- Create: `internal/knowledge/types_test.go`

- [ ] **Step 1: Write failing tests for Knowledge types**

Create `internal/knowledge/types_test.go`:

```go
package knowledge_test

import (
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func TestDocumentCreation(t *testing.T) {
	doc := knowledge.Document{
		ID:       "doc-1",
		Title:    "Architecture Guide",
		Content:  "This is the architecture guide content.",
		Source:   "manual-upload",
		MIMEType: "text/plain",
		Metadata: map[string]string{"author": "team"},
	}

	if doc.ID != "doc-1" {
		t.Errorf("expected ID doc-1, got %s", doc.ID)
	}
	if doc.Title != "Architecture Guide" {
		t.Errorf("expected title Architecture Guide, got %s", doc.Title)
	}
	if doc.Content == "" {
		t.Error("expected non-empty content")
	}
	if doc.Metadata["author"] != "team" {
		t.Errorf("expected metadata author=team, got %s", doc.Metadata["author"])
	}
}

func TestChunkCreation(t *testing.T) {
	chunk := knowledge.Chunk{
		ID:         "chunk-1",
		DocumentID: "doc-1",
		Content:    "This is chunk content.",
		Index:      0,
		StartChar:  0,
		EndChar:    22,
		Embedding:  []float64{0.1, 0.2, 0.3},
		Metadata:   map[string]string{"section": "intro"},
	}

	if chunk.ID != "chunk-1" {
		t.Errorf("expected ID chunk-1, got %s", chunk.ID)
	}
	if chunk.DocumentID != "doc-1" {
		t.Errorf("expected DocumentID doc-1, got %s", chunk.DocumentID)
	}
	if chunk.Index != 0 {
		t.Errorf("expected Index 0, got %d", chunk.Index)
	}
	if len(chunk.Embedding) != 3 {
		t.Errorf("expected 3-dim embedding, got %d", len(chunk.Embedding))
	}
}

func TestCollectionCreation(t *testing.T) {
	now := time.Now()
	col := knowledge.Collection{
		Name:        "codebase",
		Description: "Project codebase documentation",
		ChunkCount:  42,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if col.Name != "codebase" {
		t.Errorf("expected name codebase, got %s", col.Name)
	}
	if col.ChunkCount != 42 {
		t.Errorf("expected 42 chunks, got %d", col.ChunkCount)
	}
}

func TestQueryRequest(t *testing.T) {
	req := knowledge.QueryRequest{
		Query:      "How does routing work?",
		Collection: "codebase",
		TopK:       5,
		MinScore:   0.7,
	}

	if req.Query != "How does routing work?" {
		t.Errorf("unexpected query: %s", req.Query)
	}
	if req.TopK != 5 {
		t.Errorf("expected TopK 5, got %d", req.TopK)
	}
	if req.MinScore != 0.7 {
		t.Errorf("expected MinScore 0.7, got %f", req.MinScore)
	}
}

func TestQueryResult(t *testing.T) {
	result := knowledge.QueryResult{
		Chunks: []knowledge.ScoredChunk{
			{
				Chunk: knowledge.Chunk{
					ID:         "chunk-5",
					DocumentID: "doc-2",
					Content:    "Routing selects the best provider.",
				},
				Score: 0.95,
			},
			{
				Chunk: knowledge.Chunk{
					ID:         "chunk-12",
					DocumentID: "doc-3",
					Content:    "The router evaluates strategies.",
				},
				Score: 0.88,
			},
		},
		Context:    "Routing selects the best provider.\n\nThe router evaluates strategies.",
		TotalFound: 2,
	}

	if len(result.Chunks) != 2 {
		t.Errorf("expected 2 scored chunks, got %d", len(result.Chunks))
	}
	if result.Chunks[0].Score != 0.95 {
		t.Errorf("expected first score 0.95, got %f", result.Chunks[0].Score)
	}
	if result.TotalFound != 2 {
		t.Errorf("expected TotalFound 2, got %d", result.TotalFound)
	}
	if result.Context == "" {
		t.Error("expected non-empty assembled context")
	}
}

func TestIngestRequest(t *testing.T) {
	req := knowledge.IngestRequest{
		Title:      "README",
		Content:    "# Project\nThis is the project README.",
		Source:     "api-upload",
		Collection: "docs",
		MIMEType:   "text/markdown",
		Metadata:   map[string]string{"version": "1.0"},
	}

	if req.Title != "README" {
		t.Errorf("expected title README, got %s", req.Title)
	}
	if req.Collection != "docs" {
		t.Errorf("expected collection docs, got %s", req.Collection)
	}
	if req.Metadata["version"] != "1.0" {
		t.Errorf("expected version 1.0, got %s", req.Metadata["version"])
	}
}

func TestIngestResult(t *testing.T) {
	result := knowledge.IngestResult{
		DocumentID: "doc-99",
		ChunkCount: 15,
		Collection: "docs",
	}

	if result.DocumentID != "doc-99" {
		t.Errorf("expected doc-99, got %s", result.DocumentID)
	}
	if result.ChunkCount != 15 {
		t.Errorf("expected 15 chunks, got %d", result.ChunkCount)
	}
}

func TestKnowledgeStats(t *testing.T) {
	stats := knowledge.Stats{
		TotalDocuments:   100,
		TotalChunks:      1500,
		TotalCollections: 3,
		Collections: []knowledge.CollectionStats{
			{Name: "codebase", DocumentCount: 50, ChunkCount: 800},
			{Name: "docs", DocumentCount: 30, ChunkCount: 500},
			{Name: "conversations", DocumentCount: 20, ChunkCount: 200},
		},
	}

	if stats.TotalDocuments != 100 {
		t.Errorf("expected 100 documents, got %d", stats.TotalDocuments)
	}
	if stats.TotalChunks != 1500 {
		t.Errorf("expected 1500 chunks, got %d", stats.TotalChunks)
	}
	if len(stats.Collections) != 3 {
		t.Errorf("expected 3 collections, got %d", len(stats.Collections))
	}
}
```

- [ ] **Step 2: Write the implementation**

Create `internal/knowledge/types.go`:

```go
// Package knowledge implements the RAG (Retrieval-Augmented Generation) pipeline
// for HelixLLM, including document ingestion, chunking, embedding, vector storage,
// retrieval, and context assembly for LLM prompts.
package knowledge

import "time"

// Document represents a source document that has been or will be ingested
// into the knowledge base.
type Document struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Content   string            `json:"content"`
	Source    string            `json:"source"`
	MIMEType  string            `json:"mime_type"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// Chunk represents a segment of a document, with its embedding vector and
// positional metadata.
type Chunk struct {
	ID         string            `json:"id"`
	DocumentID string            `json:"document_id"`
	Content    string            `json:"content"`
	Index      int               `json:"index"`
	StartChar  int               `json:"start_char"`
	EndChar    int               `json:"end_char"`
	Embedding  []float64         `json:"embedding,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// ScoredChunk pairs a Chunk with its similarity score from a vector search.
type ScoredChunk struct {
	Chunk Chunk   `json:"chunk"`
	Score float64 `json:"score"`
}

// Collection represents a named grouping of chunks in the vector store.
type Collection struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	ChunkCount  int       `json:"chunk_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// QueryRequest describes a knowledge base query.
type QueryRequest struct {
	Query      string  `json:"query"`
	Collection string  `json:"collection"`
	TopK       int     `json:"top_k,omitempty"`
	MinScore   float64 `json:"min_score,omitempty"`
}

// QueryResult holds the results of a knowledge base query, including scored
// chunks and the assembled context string ready for injection into an LLM prompt.
type QueryResult struct {
	Chunks     []ScoredChunk `json:"chunks"`
	Context    string        `json:"context"`
	TotalFound int           `json:"total_found"`
}

// IngestRequest describes a document to ingest into the knowledge base.
type IngestRequest struct {
	Title      string            `json:"title"`
	Content    string            `json:"content"`
	Source     string            `json:"source,omitempty"`
	Collection string            `json:"collection"`
	MIMEType   string            `json:"mime_type,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// IngestResult is returned after successful document ingestion.
type IngestResult struct {
	DocumentID string `json:"document_id"`
	ChunkCount int    `json:"chunk_count"`
	Collection string `json:"collection"`
}

// Stats holds knowledge base statistics.
type Stats struct {
	TotalDocuments   int               `json:"total_documents"`
	TotalChunks      int               `json:"total_chunks"`
	TotalCollections int               `json:"total_collections"`
	Collections      []CollectionStats `json:"collections"`
}

// CollectionStats holds per-collection statistics.
type CollectionStats struct {
	Name          string `json:"name"`
	DocumentCount int    `json:"document_count"`
	ChunkCount    int    `json:"chunk_count"`
}
```

- [ ] **Step 3: Run tests**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
go test ./internal/knowledge/ -v -run TestDocument
go test ./internal/knowledge/ -v -run TestChunk
go test ./internal/knowledge/ -v -run TestCollection
go test ./internal/knowledge/ -v -run TestQuery
go test ./internal/knowledge/ -v -run TestIngest
go test ./internal/knowledge/ -v -run TestKnowledgeStats
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/knowledge/types.go internal/knowledge/types_test.go
git commit -m "feat: add Knowledge types (Document, Chunk, Collection, QueryRequest, QueryResult, IngestRequest)"
```

---

### Task 3: Embedding Service

**Files:**
- Create: `internal/knowledge/embeddings.go`
- Create: `internal/knowledge/embeddings_test.go`

- [ ] **Step 1: Write failing tests for Embedder interface and mock implementation**

Create `internal/knowledge/embeddings_test.go`:

```go
package knowledge_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func TestEmbedderInterfaceSatisfied(t *testing.T) {
	// Verify HashEmbedder satisfies the Embedder interface at compile time.
	var _ knowledge.Embedder = (*knowledge.HashEmbedder)(nil)
}

func TestHashEmbedderDimension(t *testing.T) {
	e := knowledge.NewHashEmbedder(128)
	vec := e.Embed("hello world")
	if len(vec) != 128 {
		t.Errorf("expected 128-dim vector, got %d", len(vec))
	}
}

func TestHashEmbedderDeterministic(t *testing.T) {
	e := knowledge.NewHashEmbedder(64)
	v1 := e.Embed("test input")
	v2 := e.Embed("test input")

	if len(v1) != len(v2) {
		t.Fatalf("dimension mismatch: %d vs %d", len(v1), len(v2))
	}
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Errorf("vectors differ at index %d: %f vs %f", i, v1[i], v2[i])
		}
	}
}

func TestHashEmbedderDifferentInputs(t *testing.T) {
	e := knowledge.NewHashEmbedder(64)
	v1 := e.Embed("hello")
	v2 := e.Embed("world")

	same := true
	for i := range v1 {
		if v1[i] != v2[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("expected different vectors for different inputs")
	}
}

func TestHashEmbedderNormalized(t *testing.T) {
	e := knowledge.NewHashEmbedder(64)
	vec := e.Embed("normalize me")

	var sumSq float64
	for _, v := range vec {
		sumSq += v * v
	}
	// Magnitude should be approximately 1.0 (unit vector).
	magnitude := sumSq // sqrt(sumSq)^2 == sumSq for unit check
	if magnitude < 0.99 || magnitude > 1.01 {
		t.Errorf("expected unit vector (magnitude^2 ~ 1.0), got %f", magnitude)
	}
}

func TestEmbedBatch(t *testing.T) {
	e := knowledge.NewHashEmbedder(32)
	texts := []string{"first", "second", "third"}
	vectors := e.EmbedBatch(texts)

	if len(vectors) != 3 {
		t.Fatalf("expected 3 vectors, got %d", len(vectors))
	}
	for i, vec := range vectors {
		if len(vec) != 32 {
			t.Errorf("vector %d: expected 32 dims, got %d", i, len(vec))
		}
	}

	// Each vector should match individual Embed calls.
	for i, text := range texts {
		single := e.Embed(text)
		for j := range single {
			if single[j] != vectors[i][j] {
				t.Errorf("batch vector %d differs from single at index %d", i, j)
				break
			}
		}
	}
}

func TestEmbedEmptyString(t *testing.T) {
	e := knowledge.NewHashEmbedder(16)
	vec := e.Embed("")
	if len(vec) != 16 {
		t.Errorf("expected 16-dim vector for empty string, got %d", len(vec))
	}
}
```

- [ ] **Step 2: Write the implementation**

Create `internal/knowledge/embeddings.go`:

```go
package knowledge

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
)

// Embedder generates vector embeddings for text. Implementations may use local
// models, cloud APIs, or deterministic hashing (for testing).
type Embedder interface {
	// Embed returns a vector embedding for a single text input.
	Embed(text string) []float64
	// EmbedBatch returns vector embeddings for multiple text inputs.
	EmbedBatch(texts []string) [][]float64
	// Dimension returns the embedding vector dimension.
	Dimension() int
}

// HashEmbedder is a deterministic embedder that uses SHA-256 hashing to produce
// fixed-dimension vectors. It is designed for testing: same input always produces
// the same normalized unit vector. It does NOT capture semantic meaning.
type HashEmbedder struct {
	dim int
}

// NewHashEmbedder creates a HashEmbedder with the given vector dimension.
// The dimension must be positive and at most 128 (limited by SHA-256 output).
func NewHashEmbedder(dim int) *HashEmbedder {
	if dim <= 0 {
		dim = 64
	}
	return &HashEmbedder{dim: dim}
}

// Embed produces a deterministic normalized vector from the input text.
// The text is hashed with SHA-256, and the hash bytes are expanded into
// float64 values, then L2-normalized to produce a unit vector.
func (h *HashEmbedder) Embed(text string) []float64 {
	vec := make([]float64, h.dim)

	// Generate enough hash material by repeatedly hashing with a counter.
	// SHA-256 produces 32 bytes = 4 float64 values (8 bytes each).
	// We need ceil(dim/4) hash rounds.
	rounds := (h.dim + 3) / 4
	idx := 0
	for r := 0; r < rounds && idx < h.dim; r++ {
		hasher := sha256.New()
		hasher.Write([]byte(text))
		// Append round counter to differentiate rounds.
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], uint64(r))
		hasher.Write(buf[:])
		hash := hasher.Sum(nil)

		// Extract up to 4 float64 values from the 32-byte hash.
		for off := 0; off < 32 && idx < h.dim; off += 8 {
			bits := binary.LittleEndian.Uint64(hash[off : off+8])
			// Map to [-1, 1] range.
			val := float64(int64(bits)) / float64(math.MaxInt64)
			vec[idx] = val
			idx++
		}
	}

	// L2-normalize to unit vector.
	var norm float64
	for _, v := range vec {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range vec {
			vec[i] /= norm
		}
	}

	return vec
}

// EmbedBatch returns embeddings for each text by calling Embed individually.
func (h *HashEmbedder) EmbedBatch(texts []string) [][]float64 {
	results := make([][]float64, len(texts))
	for i, text := range texts {
		results[i] = h.Embed(text)
	}
	return results
}

// Dimension returns the configured embedding vector dimension.
func (h *HashEmbedder) Dimension() int {
	return h.dim
}
```

- [ ] **Step 3: Run tests**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
go test ./internal/knowledge/ -v -run TestEmbedder
go test ./internal/knowledge/ -v -run TestHashEmbedder
go test ./internal/knowledge/ -v -run TestEmbedBatch
go test ./internal/knowledge/ -v -run TestEmbedEmpty
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/knowledge/embeddings.go internal/knowledge/embeddings_test.go
git commit -m "feat: add Embedder interface with deterministic HashEmbedder for testing"
```

---

### Task 4: Vector Store

**Files:**
- Create: `internal/knowledge/store.go`
- Create: `internal/knowledge/store_test.go`

- [ ] **Step 1: Write failing tests for VectorStore interface and in-memory implementation**

Create `internal/knowledge/store_test.go`:

```go
package knowledge_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func TestVectorStoreInterfaceSatisfied(t *testing.T) {
	var _ knowledge.VectorStore = (*knowledge.MemoryStore)(nil)
}

func TestMemoryStoreUpsertAndSearch(t *testing.T) {
	store := knowledge.NewMemoryStore()
	embedder := knowledge.NewHashEmbedder(64)

	chunks := []knowledge.Chunk{
		{
			ID:         "c1",
			DocumentID: "d1",
			Content:    "Go is a statically typed language.",
			Embedding:  embedder.Embed("Go is a statically typed language."),
		},
		{
			ID:         "c2",
			DocumentID: "d1",
			Content:    "Gin is a web framework for Go.",
			Embedding:  embedder.Embed("Gin is a web framework for Go."),
		},
		{
			ID:         "c3",
			DocumentID: "d2",
			Content:    "Python is a dynamically typed language.",
			Embedding:  embedder.Embed("Python is a dynamically typed language."),
		},
	}

	err := store.Upsert("test-collection", chunks)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Search for something similar to "Go programming language"
	queryVec := embedder.Embed("Go is a statically typed language.")
	results, err := store.Search("test-collection", queryVec, 2)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// The exact match should be first with score ~1.0
	if results[0].Chunk.ID != "c1" {
		t.Errorf("expected first result to be c1 (exact match), got %s", results[0].Chunk.ID)
	}
	if results[0].Score < 0.99 {
		t.Errorf("expected score ~1.0 for exact match, got %f", results[0].Score)
	}
}

func TestMemoryStoreUpsertOverwrite(t *testing.T) {
	store := knowledge.NewMemoryStore()
	embedder := knowledge.NewHashEmbedder(32)

	original := []knowledge.Chunk{
		{
			ID:         "c1",
			DocumentID: "d1",
			Content:    "original content",
			Embedding:  embedder.Embed("original content"),
		},
	}
	err := store.Upsert("col", original)
	if err != nil {
		t.Fatalf("first Upsert failed: %v", err)
	}

	updated := []knowledge.Chunk{
		{
			ID:         "c1",
			DocumentID: "d1",
			Content:    "updated content",
			Embedding:  embedder.Embed("updated content"),
		},
	}
	err = store.Upsert("col", updated)
	if err != nil {
		t.Fatalf("second Upsert failed: %v", err)
	}

	// Search should return the updated content.
	results, err := store.Search("col", embedder.Embed("updated content"), 1)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Chunk.Content != "updated content" {
		t.Errorf("expected updated content, got %s", results[0].Chunk.Content)
	}
}

func TestMemoryStoreDeleteCollection(t *testing.T) {
	store := knowledge.NewMemoryStore()
	embedder := knowledge.NewHashEmbedder(32)

	chunks := []knowledge.Chunk{
		{ID: "c1", DocumentID: "d1", Content: "data", Embedding: embedder.Embed("data")},
	}
	_ = store.Upsert("to-delete", chunks)

	err := store.DeleteCollection("to-delete")
	if err != nil {
		t.Fatalf("DeleteCollection failed: %v", err)
	}

	results, err := store.Search("to-delete", embedder.Embed("data"), 10)
	if err != nil {
		t.Fatalf("Search after delete failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results after delete, got %d", len(results))
	}
}

func TestMemoryStoreCollections(t *testing.T) {
	store := knowledge.NewMemoryStore()
	embedder := knowledge.NewHashEmbedder(16)

	_ = store.Upsert("alpha", []knowledge.Chunk{
		{ID: "a1", Content: "a", Embedding: embedder.Embed("a")},
		{ID: "a2", Content: "b", Embedding: embedder.Embed("b")},
	})
	_ = store.Upsert("beta", []knowledge.Chunk{
		{ID: "b1", Content: "c", Embedding: embedder.Embed("c")},
	})

	cols := store.Collections()
	if len(cols) != 2 {
		t.Fatalf("expected 2 collections, got %d", len(cols))
	}

	// Check that chunk counts are correct.
	counts := make(map[string]int)
	for _, c := range cols {
		counts[c.Name] = c.ChunkCount
	}
	if counts["alpha"] != 2 {
		t.Errorf("expected alpha to have 2 chunks, got %d", counts["alpha"])
	}
	if counts["beta"] != 1 {
		t.Errorf("expected beta to have 1 chunk, got %d", counts["beta"])
	}
}

func TestMemoryStoreSearchEmptyCollection(t *testing.T) {
	store := knowledge.NewMemoryStore()
	results, err := store.Search("nonexistent", []float64{0.1, 0.2}, 5)
	if err != nil {
		t.Fatalf("Search on nonexistent collection should not error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestMemoryStoreSearchTopK(t *testing.T) {
	store := knowledge.NewMemoryStore()
	embedder := knowledge.NewHashEmbedder(32)

	var chunks []knowledge.Chunk
	for i := 0; i < 20; i++ {
		content := "chunk content number " + string(rune('A'+i))
		chunks = append(chunks, knowledge.Chunk{
			ID:        "c" + string(rune('A'+i)),
			Content:   content,
			Embedding: embedder.Embed(content),
		})
	}
	_ = store.Upsert("big", chunks)

	results, err := store.Search("big", embedder.Embed("any query"), 5)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 5 {
		t.Errorf("expected 5 results (topK), got %d", len(results))
	}

	// Results should be sorted by descending score.
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted: index %d score %f > index %d score %f",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}
}

func TestMemoryStoreDeleteNonexistent(t *testing.T) {
	store := knowledge.NewMemoryStore()
	// Deleting a nonexistent collection should not error.
	err := store.DeleteCollection("ghost")
	if err != nil {
		t.Errorf("DeleteCollection on nonexistent should not error: %v", err)
	}
}

func TestMemoryStoreStats(t *testing.T) {
	store := knowledge.NewMemoryStore()
	embedder := knowledge.NewHashEmbedder(16)

	_ = store.Upsert("col1", []knowledge.Chunk{
		{ID: "a", DocumentID: "d1", Content: "x", Embedding: embedder.Embed("x")},
		{ID: "b", DocumentID: "d1", Content: "y", Embedding: embedder.Embed("y")},
		{ID: "c", DocumentID: "d2", Content: "z", Embedding: embedder.Embed("z")},
	})
	_ = store.Upsert("col2", []knowledge.Chunk{
		{ID: "d", DocumentID: "d3", Content: "w", Embedding: embedder.Embed("w")},
	})

	stats := store.Stats()
	if stats.TotalChunks != 4 {
		t.Errorf("expected 4 total chunks, got %d", stats.TotalChunks)
	}
	if stats.TotalCollections != 2 {
		t.Errorf("expected 2 collections, got %d", stats.TotalCollections)
	}
	if stats.TotalDocuments != 3 {
		t.Errorf("expected 3 unique documents, got %d", stats.TotalDocuments)
	}
}
```

- [ ] **Step 2: Write the implementation**

Create `internal/knowledge/store.go`:

```go
package knowledge

import (
	"math"
	"sort"
	"sync"
	"time"
)

// VectorStore provides vector storage and similarity search. Implementations
// may use in-memory storage, Qdrant, pgvector, or other backends.
type VectorStore interface {
	// Upsert adds or replaces chunks in the named collection. Chunks must have
	// their Embedding field populated.
	Upsert(collection string, chunks []Chunk) error
	// Search finds the topK most similar chunks to the query vector in the
	// named collection, ordered by descending similarity score.
	Search(collection string, queryVec []float64, topK int) ([]ScoredChunk, error)
	// DeleteCollection removes an entire collection and all its chunks.
	DeleteCollection(collection string) error
	// Collections returns metadata for all collections.
	Collections() []Collection
	// Stats returns aggregate statistics across all collections.
	Stats() Stats
}

// MemoryStore is a thread-safe in-memory VectorStore that uses brute-force
// cosine similarity for search. Suitable for testing and small datasets.
type MemoryStore struct {
	mu          sync.RWMutex
	collections map[string]*memCollection
}

type memCollection struct {
	chunks    map[string]Chunk // keyed by Chunk.ID
	createdAt time.Time
}

// NewMemoryStore creates an empty in-memory vector store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		collections: make(map[string]*memCollection),
	}
}

// Upsert adds or replaces chunks in the named collection.
func (m *MemoryStore) Upsert(collection string, chunks []Chunk) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	col, ok := m.collections[collection]
	if !ok {
		col = &memCollection{
			chunks:    make(map[string]Chunk),
			createdAt: time.Now(),
		}
		m.collections[collection] = col
	}
	for _, c := range chunks {
		col.chunks[c.ID] = c
	}
	return nil
}

// Search finds the topK most similar chunks using cosine similarity.
func (m *MemoryStore) Search(collection string, queryVec []float64, topK int) ([]ScoredChunk, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	col, ok := m.collections[collection]
	if !ok {
		return nil, nil
	}

	var scored []ScoredChunk
	for _, chunk := range col.chunks {
		score := cosineSimilarity(queryVec, chunk.Embedding)
		scored = append(scored, ScoredChunk{
			Chunk: chunk,
			Score: score,
		})
	}

	// Sort by descending score.
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	if topK > 0 && topK < len(scored) {
		scored = scored[:topK]
	}

	return scored, nil
}

// DeleteCollection removes the named collection entirely.
func (m *MemoryStore) DeleteCollection(collection string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.collections, collection)
	return nil
}

// Collections returns metadata for all stored collections.
func (m *MemoryStore) Collections() []Collection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var cols []Collection
	for name, col := range m.collections {
		cols = append(cols, Collection{
			Name:       name,
			ChunkCount: len(col.chunks),
			CreatedAt:  col.createdAt,
			UpdatedAt:  time.Now(),
		})
	}
	return cols
}

// Stats returns aggregate statistics across all collections.
func (m *MemoryStore) Stats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := Stats{
		TotalCollections: len(m.collections),
	}

	docSet := make(map[string]struct{})
	for name, col := range m.collections {
		chunkCount := len(col.chunks)
		stats.TotalChunks += chunkCount

		colDocSet := make(map[string]struct{})
		for _, chunk := range col.chunks {
			if chunk.DocumentID != "" {
				docSet[chunk.DocumentID] = struct{}{}
				colDocSet[chunk.DocumentID] = struct{}{}
			}
		}

		stats.Collections = append(stats.Collections, CollectionStats{
			Name:          name,
			DocumentCount: len(colDocSet),
			ChunkCount:    chunkCount,
		})
	}
	stats.TotalDocuments = len(docSet)

	return stats
}

// cosineSimilarity computes the cosine similarity between two vectors.
// Returns 0 if either vector has zero magnitude.
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
```

- [ ] **Step 3: Run tests**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
go test ./internal/knowledge/ -v -run TestVectorStore
go test ./internal/knowledge/ -v -run TestMemoryStore
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/knowledge/store.go internal/knowledge/store_test.go
git commit -m "feat: add VectorStore interface with in-memory cosine-similarity implementation"
```

---

### Task 5: Chunker

**Files:**
- Create: `internal/knowledge/chunker.go`
- Create: `internal/knowledge/chunker_test.go`

- [ ] **Step 1: Write failing tests for the Chunker**

Create `internal/knowledge/chunker_test.go`:

```go
package knowledge_test

import (
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func TestChunkerInterfaceSatisfied(t *testing.T) {
	var _ knowledge.Chunker = (*knowledge.FixedSizeChunker)(nil)
}

func TestFixedSizeChunkerBasic(t *testing.T) {
	chunker := knowledge.NewFixedSizeChunker(knowledge.ChunkerConfig{
		ChunkSize: 20,
		Overlap:   0,
	})

	doc := knowledge.Document{
		ID:      "d1",
		Content: "abcdefghijklmnopqrstuvwxyz0123456789",
	}

	chunks := chunker.Chunk(doc)
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	// Reassemble from chunks should cover the full content.
	var combined string
	for _, c := range chunks {
		if !strings.Contains(doc.Content, c.Content) {
			t.Errorf("chunk content not found in document: %q", c.Content)
		}
		combined += c.Content
	}
	// Without overlap, reassembly should exactly match.
	if combined != doc.Content {
		t.Errorf("reassembled content mismatch:\n  got:  %q\n  want: %q", combined, doc.Content)
	}
}

func TestFixedSizeChunkerWithOverlap(t *testing.T) {
	chunker := knowledge.NewFixedSizeChunker(knowledge.ChunkerConfig{
		ChunkSize: 20,
		Overlap:   5,
	})

	// 50 characters of content.
	doc := knowledge.Document{
		ID:      "d2",
		Content: "aaaaaaaaaabbbbbbbbbbccccccccccddddddddddeeeeeeeeee",
	}

	chunks := chunker.Chunk(doc)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	// Each chunk (except the first) should start overlap characters before
	// the previous chunk ended.
	for i, c := range chunks {
		if len(c.Content) > 20 && i < len(chunks)-1 {
			t.Errorf("chunk %d exceeds chunk size: %d", i, len(c.Content))
		}
		if c.DocumentID != "d2" {
			t.Errorf("chunk %d has wrong DocumentID: %s", i, c.DocumentID)
		}
		if c.Index != i {
			t.Errorf("chunk %d has wrong Index: %d", i, c.Index)
		}
	}
}

func TestFixedSizeChunkerSmallContent(t *testing.T) {
	chunker := knowledge.NewFixedSizeChunker(knowledge.ChunkerConfig{
		ChunkSize: 100,
		Overlap:   20,
	})

	doc := knowledge.Document{
		ID:      "d3",
		Content: "short",
	}

	chunks := chunker.Chunk(doc)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for short content, got %d", len(chunks))
	}
	if chunks[0].Content != "short" {
		t.Errorf("expected chunk content 'short', got %q", chunks[0].Content)
	}
}

func TestFixedSizeChunkerEmptyContent(t *testing.T) {
	chunker := knowledge.NewFixedSizeChunker(knowledge.ChunkerConfig{
		ChunkSize: 100,
		Overlap:   20,
	})

	doc := knowledge.Document{ID: "d4", Content: ""}
	chunks := chunker.Chunk(doc)
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty content, got %d", len(chunks))
	}
}

func TestFixedSizeChunkerPositions(t *testing.T) {
	chunker := knowledge.NewFixedSizeChunker(knowledge.ChunkerConfig{
		ChunkSize: 10,
		Overlap:   0,
	})

	doc := knowledge.Document{
		ID:      "d5",
		Content: "0123456789abcdefghij",
	}

	chunks := chunker.Chunk(doc)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	// First chunk: chars 0-9
	if chunks[0].StartChar != 0 || chunks[0].EndChar != 10 {
		t.Errorf("chunk 0: expected start=0 end=10, got start=%d end=%d",
			chunks[0].StartChar, chunks[0].EndChar)
	}
	// Second chunk: chars 10-19
	if chunks[1].StartChar != 10 || chunks[1].EndChar != 20 {
		t.Errorf("chunk 1: expected start=10 end=20, got start=%d end=%d",
			chunks[1].StartChar, chunks[1].EndChar)
	}
}

func TestFixedSizeChunkerChunkIDs(t *testing.T) {
	chunker := knowledge.NewFixedSizeChunker(knowledge.ChunkerConfig{
		ChunkSize: 10,
		Overlap:   0,
	})

	doc := knowledge.Document{
		ID:      "doc-abc",
		Content: "0123456789abcdefghij",
	}

	chunks := chunker.Chunk(doc)
	ids := make(map[string]bool)
	for _, c := range chunks {
		if c.ID == "" {
			t.Error("chunk has empty ID")
		}
		if ids[c.ID] {
			t.Errorf("duplicate chunk ID: %s", c.ID)
		}
		ids[c.ID] = true
	}
}

func TestFixedSizeChunkerDefaultConfig(t *testing.T) {
	// Zero values should use sensible defaults.
	chunker := knowledge.NewFixedSizeChunker(knowledge.ChunkerConfig{})
	doc := knowledge.Document{
		ID:      "d6",
		Content: strings.Repeat("x", 3000),
	}

	chunks := chunker.Chunk(doc)
	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks for 3000-char content with defaults, got %d", len(chunks))
	}
}
```

- [ ] **Step 2: Write the implementation**

Create `internal/knowledge/chunker.go`:

```go
package knowledge

import "fmt"

// Chunker splits a Document into Chunks. Implementations may use fixed-size
// splitting, sentence-aware splitting, recursive splitting, or other strategies.
type Chunker interface {
	// Chunk splits a document into chunks. The returned chunks do NOT have
	// their Embedding fields populated; that is the embedder's job.
	Chunk(doc Document) []Chunk
}

// ChunkerConfig configures chunking behavior.
type ChunkerConfig struct {
	// ChunkSize is the maximum number of characters per chunk.
	// Default: 1000 (matching HELIX_RAG_CHUNK_SIZE).
	ChunkSize int
	// Overlap is the number of characters shared between consecutive chunks.
	// Default: 200 (matching HELIX_RAG_CHUNK_OVERLAP).
	Overlap int
}

// FixedSizeChunker splits documents into fixed-size character chunks with
// optional overlap between consecutive chunks.
type FixedSizeChunker struct {
	chunkSize int
	overlap   int
}

// NewFixedSizeChunker creates a FixedSizeChunker with the given config.
// Zero values are replaced with defaults (chunkSize=1000, overlap=200).
func NewFixedSizeChunker(cfg ChunkerConfig) *FixedSizeChunker {
	size := cfg.ChunkSize
	if size <= 0 {
		size = 1000
	}
	overlap := cfg.Overlap
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= size {
		overlap = size / 5
	}
	return &FixedSizeChunker{
		chunkSize: size,
		overlap:   overlap,
	}
}

// Chunk splits the document content into fixed-size chunks with overlap.
func (f *FixedSizeChunker) Chunk(doc Document) []Chunk {
	content := doc.Content
	if len(content) == 0 {
		return nil
	}

	var chunks []Chunk
	step := f.chunkSize - f.overlap
	if step <= 0 {
		step = 1
	}

	idx := 0
	pos := 0
	for pos < len(content) {
		end := pos + f.chunkSize
		if end > len(content) {
			end = len(content)
		}

		chunks = append(chunks, Chunk{
			ID:         fmt.Sprintf("%s-chunk-%d", doc.ID, idx),
			DocumentID: doc.ID,
			Content:    content[pos:end],
			Index:      idx,
			StartChar:  pos,
			EndChar:    end,
		})

		idx++
		pos += step

		// Avoid producing a tiny trailing chunk that is fully covered by overlap.
		if pos >= len(content) {
			break
		}
	}

	return chunks
}
```

- [ ] **Step 3: Run tests**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
go test ./internal/knowledge/ -v -run TestChunker
go test ./internal/knowledge/ -v -run TestFixedSize
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/knowledge/chunker.go internal/knowledge/chunker_test.go
git commit -m "feat: add Chunker interface with fixed-size character chunker"
```

---

### Task 6: RAG Pipeline

**Files:**
- Create: `internal/knowledge/pipeline.go`
- Create: `internal/knowledge/pipeline_test.go`

- [ ] **Step 1: Write failing tests for the Pipeline**

Create `internal/knowledge/pipeline_test.go`:

```go
package knowledge_test

import (
	"context"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func newTestPipeline() *knowledge.Pipeline {
	embedder := knowledge.NewHashEmbedder(64)
	store := knowledge.NewMemoryStore()
	chunker := knowledge.NewFixedSizeChunker(knowledge.ChunkerConfig{
		ChunkSize: 50,
		Overlap:   10,
	})
	return knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          embedder,
		Store:             store,
		Chunker:           chunker,
		DefaultCollection: "default",
		DefaultTopK:       5,
	})
}

func TestPipelineIngest(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	result, err := p.Ingest(ctx, knowledge.IngestRequest{
		Title:      "Test Doc",
		Content:    "This is a test document with enough content to produce multiple chunks when split.",
		Collection: "docs",
	})
	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}
	if result.DocumentID == "" {
		t.Error("expected non-empty DocumentID")
	}
	if result.ChunkCount == 0 {
		t.Error("expected at least one chunk")
	}
	if result.Collection != "docs" {
		t.Errorf("expected collection 'docs', got %s", result.Collection)
	}
}

func TestPipelineIngestDefaultCollection(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	result, err := p.Ingest(ctx, knowledge.IngestRequest{
		Title:   "No Collection",
		Content: "Some content here.",
	})
	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}
	if result.Collection != "default" {
		t.Errorf("expected default collection, got %s", result.Collection)
	}
}

func TestPipelineQuery(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	// Ingest a document.
	_, err := p.Ingest(ctx, knowledge.IngestRequest{
		Title:      "Go Guide",
		Content:    "Go is a statically typed compiled language designed at Google. It is syntactically similar to C but with memory safety and garbage collection.",
		Collection: "guides",
	})
	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}

	// Query the knowledge base.
	result, err := p.Query(ctx, knowledge.QueryRequest{
		Query:      "What is Go?",
		Collection: "guides",
		TopK:       3,
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(result.Chunks) == 0 {
		t.Error("expected at least one result chunk")
	}
	if result.Context == "" {
		t.Error("expected non-empty assembled context")
	}
	if result.TotalFound == 0 {
		t.Error("expected TotalFound > 0")
	}
}

func TestPipelineQueryEmptyCollection(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	result, err := p.Query(ctx, knowledge.QueryRequest{
		Query:      "anything",
		Collection: "empty",
		TopK:       5,
	})
	if err != nil {
		t.Fatalf("Query on empty collection should not error: %v", err)
	}
	if len(result.Chunks) != 0 {
		t.Errorf("expected 0 results, got %d", len(result.Chunks))
	}
	if result.Context != "" {
		t.Errorf("expected empty context, got %q", result.Context)
	}
}

func TestPipelineQueryDefaultTopK(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	// Ingest a large document to produce many chunks.
	_, err := p.Ingest(ctx, knowledge.IngestRequest{
		Title:      "Large Doc",
		Content:    strings.Repeat("Knowledge is power and this sentence repeats. ", 50),
		Collection: "big",
	})
	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}

	// Query with TopK=0, should use default (5).
	result, err := p.Query(ctx, knowledge.QueryRequest{
		Query:      "knowledge",
		Collection: "big",
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(result.Chunks) > 5 {
		t.Errorf("expected at most 5 results (default TopK), got %d", len(result.Chunks))
	}
}

func TestPipelineQueryMinScore(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	_, err := p.Ingest(ctx, knowledge.IngestRequest{
		Title:      "Specific Doc",
		Content:    "The quick brown fox jumps over the lazy dog near the river bank in the forest.",
		Collection: "filtered",
	})
	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}

	// Query with a very high MinScore -- may filter out low-scoring results.
	result, err := p.Query(ctx, knowledge.QueryRequest{
		Query:      "fox jumps",
		Collection: "filtered",
		TopK:       10,
		MinScore:   0.99,
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	// All returned chunks should have score >= MinScore.
	for _, sc := range result.Chunks {
		if sc.Score < 0.99 {
			t.Errorf("chunk %s has score %f below MinScore 0.99", sc.Chunk.ID, sc.Score)
		}
	}
}

func TestPipelineContextAssembly(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	_, err := p.Ingest(ctx, knowledge.IngestRequest{
		Title:      "Multi Chunk",
		Content:    "First section about databases. Second section about networking. Third section about security.",
		Collection: "multi",
	})
	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}

	result, err := p.Query(ctx, knowledge.QueryRequest{
		Query:      "databases",
		Collection: "multi",
		TopK:       3,
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// Context should contain text from retrieved chunks, separated by newlines.
	if result.Context == "" {
		t.Error("expected non-empty context")
	}
	// Context should not contain duplicate chunks.
	parts := strings.Split(result.Context, "\n\n")
	seen := make(map[string]bool)
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if seen[trimmed] {
			t.Errorf("duplicate chunk in context: %q", trimmed)
		}
		seen[trimmed] = true
	}
}

func TestPipelineCollections(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	_, _ = p.Ingest(ctx, knowledge.IngestRequest{
		Title: "A", Content: "aaa", Collection: "col1",
	})
	_, _ = p.Ingest(ctx, knowledge.IngestRequest{
		Title: "B", Content: "bbb", Collection: "col2",
	})

	cols := p.Collections()
	if len(cols) != 2 {
		t.Errorf("expected 2 collections, got %d", len(cols))
	}
}

func TestPipelineStats(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	_, _ = p.Ingest(ctx, knowledge.IngestRequest{
		Title: "Doc1", Content: "content one for testing", Collection: "stats",
	})
	_, _ = p.Ingest(ctx, knowledge.IngestRequest{
		Title: "Doc2", Content: "content two for testing", Collection: "stats",
	})

	stats := p.Stats()
	if stats.TotalDocuments < 2 {
		t.Errorf("expected at least 2 documents, got %d", stats.TotalDocuments)
	}
	if stats.TotalChunks < 2 {
		t.Errorf("expected at least 2 chunks, got %d", stats.TotalChunks)
	}
}

func TestPipelineIngestEmptyContent(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	_, err := p.Ingest(ctx, knowledge.IngestRequest{
		Title:      "Empty",
		Content:    "",
		Collection: "empty",
	})
	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestPipelineIngestEmptyQuery(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	_, err := p.Query(ctx, knowledge.QueryRequest{
		Query:      "",
		Collection: "any",
	})
	if err == nil {
		t.Error("expected error for empty query")
	}
}
```

- [ ] **Step 2: Write the implementation**

Create `internal/knowledge/pipeline.go`:

```go
package knowledge

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// PipelineConfig configures the RAG pipeline dependencies.
type PipelineConfig struct {
	Embedder          Embedder
	Store             VectorStore
	Chunker           Chunker
	DefaultCollection string
	DefaultTopK       int
}

// Pipeline ties together the RAG components: chunking, embedding, vector storage,
// retrieval, and context assembly.
type Pipeline struct {
	embedder          Embedder
	store             VectorStore
	chunker           Chunker
	defaultCollection string
	defaultTopK       int
}

// NewPipeline creates a Pipeline with the given configuration.
func NewPipeline(cfg PipelineConfig) *Pipeline {
	defaultTopK := cfg.DefaultTopK
	if defaultTopK <= 0 {
		defaultTopK = 5
	}
	defaultCol := cfg.DefaultCollection
	if defaultCol == "" {
		defaultCol = "default"
	}
	return &Pipeline{
		embedder:          cfg.Embedder,
		store:             cfg.Store,
		chunker:           cfg.Chunker,
		defaultCollection: defaultCol,
		defaultTopK:       defaultTopK,
	}
}

// Ingest processes a document through the ingestion pipeline:
// validate -> generate doc ID -> chunk -> embed chunks -> upsert to vector store.
func (p *Pipeline) Ingest(_ context.Context, req IngestRequest) (*IngestResult, error) {
	if strings.TrimSpace(req.Content) == "" {
		return nil, fmt.Errorf("knowledge: cannot ingest empty content")
	}

	collection := req.Collection
	if collection == "" {
		collection = p.defaultCollection
	}

	docID := uuid.New().String()

	doc := Document{
		ID:       docID,
		Title:    req.Title,
		Content:  req.Content,
		Source:   req.Source,
		MIMEType: req.MIMEType,
		Metadata: req.Metadata,
	}

	// Chunk the document.
	chunks := p.chunker.Chunk(doc)
	if len(chunks) == 0 {
		return nil, fmt.Errorf("knowledge: chunker produced no chunks")
	}

	// Embed each chunk.
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}
	embeddings := p.embedder.EmbedBatch(texts)
	for i := range chunks {
		chunks[i].Embedding = embeddings[i]
	}

	// Upsert to vector store.
	if err := p.store.Upsert(collection, chunks); err != nil {
		return nil, fmt.Errorf("knowledge: upsert failed: %w", err)
	}

	return &IngestResult{
		DocumentID: docID,
		ChunkCount: len(chunks),
		Collection: collection,
	}, nil
}

// Query executes the retrieval pipeline:
// validate -> embed query -> search vector store -> filter by min score -> assemble context.
func (p *Pipeline) Query(_ context.Context, req QueryRequest) (*QueryResult, error) {
	if strings.TrimSpace(req.Query) == "" {
		return nil, fmt.Errorf("knowledge: query cannot be empty")
	}

	collection := req.Collection
	if collection == "" {
		collection = p.defaultCollection
	}

	topK := req.TopK
	if topK <= 0 {
		topK = p.defaultTopK
	}

	// Embed the query.
	queryVec := p.embedder.Embed(req.Query)

	// Search the vector store.
	results, err := p.store.Search(collection, queryVec, topK)
	if err != nil {
		return nil, fmt.Errorf("knowledge: search failed: %w", err)
	}

	// Filter by minimum score.
	if req.MinScore > 0 {
		var filtered []ScoredChunk
		for _, sc := range results {
			if sc.Score >= req.MinScore {
				filtered = append(filtered, sc)
			}
		}
		results = filtered
	}

	// Assemble context from retrieved chunks.
	contextStr := assembleContext(results)

	return &QueryResult{
		Chunks:     results,
		Context:    contextStr,
		TotalFound: len(results),
	}, nil
}

// Collections returns metadata for all collections in the vector store.
func (p *Pipeline) Collections() []Collection {
	return p.store.Collections()
}

// Stats returns aggregate statistics from the vector store.
func (p *Pipeline) Stats() Stats {
	return p.store.Stats()
}

// assembleContext joins chunk contents with double newlines, deduplicating
// any chunks that have identical content.
func assembleContext(chunks []ScoredChunk) string {
	if len(chunks) == 0 {
		return ""
	}
	seen := make(map[string]bool)
	var parts []string
	for _, sc := range chunks {
		content := strings.TrimSpace(sc.Chunk.Content)
		if content == "" || seen[content] {
			continue
		}
		seen[content] = true
		parts = append(parts, content)
	}
	return strings.Join(parts, "\n\n")
}
```

- [ ] **Step 3: Run tests**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
go test ./internal/knowledge/ -v -run TestPipeline
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/knowledge/pipeline.go internal/knowledge/pipeline_test.go
git commit -m "feat: add RAG Pipeline with ingest, query, context assembly, and stats"
```

---

### Task 7: Knowledge API Endpoints

**Files:**
- Create: `internal/knowledge/api.go`
- Create: `internal/knowledge/api_test.go`

- [ ] **Step 1: Write failing tests for the API handlers**

Create `internal/knowledge/api_test.go`:

```go
package knowledge_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func setupTestRouter() (*gin.Engine, *knowledge.Pipeline) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	embedder := knowledge.NewHashEmbedder(64)
	store := knowledge.NewMemoryStore()
	chunker := knowledge.NewFixedSizeChunker(knowledge.ChunkerConfig{
		ChunkSize: 50,
		Overlap:   10,
	})
	p := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          embedder,
		Store:             store,
		Chunker:           chunker,
		DefaultCollection: "default",
		DefaultTopK:       5,
	})

	knowledge.RegisterRoutes(r, p)
	return r, p
}

func TestAPIIngest(t *testing.T) {
	r, _ := setupTestRouter()

	body, _ := json.Marshal(knowledge.IngestRequest{
		Title:      "API Test Doc",
		Content:    "This is a document ingested through the API for testing purposes.",
		Collection: "api-test",
	})

	req := httptest.NewRequest(http.MethodPost, "/internal/knowledge/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result knowledge.IngestResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if result.DocumentID == "" {
		t.Error("expected non-empty DocumentID")
	}
	if result.ChunkCount == 0 {
		t.Error("expected at least one chunk")
	}
	if result.Collection != "api-test" {
		t.Errorf("expected collection api-test, got %s", result.Collection)
	}
}

func TestAPIIngestBadRequest(t *testing.T) {
	r, _ := setupTestRouter()

	// Empty content should return 400.
	body, _ := json.Marshal(knowledge.IngestRequest{
		Title:      "Empty",
		Content:    "",
		Collection: "test",
	})

	req := httptest.NewRequest(http.MethodPost, "/internal/knowledge/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestAPIIngestInvalidJSON(t *testing.T) {
	r, _ := setupTestRouter()

	req := httptest.NewRequest(http.MethodPost, "/internal/knowledge/ingest",
		bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestAPIQuery(t *testing.T) {
	r, p := setupTestRouter()

	// First ingest a document.
	_, _ = p.Ingest(nil, knowledge.IngestRequest{
		Title:      "Query Target",
		Content:    "Go programming language features include goroutines and channels for concurrency.",
		Collection: "query-test",
	})

	body, _ := json.Marshal(knowledge.QueryRequest{
		Query:      "What are goroutines?",
		Collection: "query-test",
		TopK:       3,
	})

	req := httptest.NewRequest(http.MethodPost, "/internal/knowledge/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result knowledge.QueryResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(result.Chunks) == 0 {
		t.Error("expected at least one result chunk")
	}
	if result.Context == "" {
		t.Error("expected non-empty context")
	}
}

func TestAPIQueryBadRequest(t *testing.T) {
	r, _ := setupTestRouter()

	// Empty query should return 400.
	body, _ := json.Marshal(knowledge.QueryRequest{
		Query:      "",
		Collection: "test",
	})

	req := httptest.NewRequest(http.MethodPost, "/internal/knowledge/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestAPICollections(t *testing.T) {
	r, p := setupTestRouter()

	_, _ = p.Ingest(nil, knowledge.IngestRequest{
		Title: "A", Content: "aaa content", Collection: "alpha",
	})
	_, _ = p.Ingest(nil, knowledge.IngestRequest{
		Title: "B", Content: "bbb content", Collection: "beta",
	})

	req := httptest.NewRequest(http.MethodGet, "/internal/knowledge/collections", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var cols []knowledge.Collection
	if err := json.Unmarshal(w.Body.Bytes(), &cols); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(cols) != 2 {
		t.Errorf("expected 2 collections, got %d", len(cols))
	}
}

func TestAPIStats(t *testing.T) {
	r, p := setupTestRouter()

	_, _ = p.Ingest(nil, knowledge.IngestRequest{
		Title: "Stats Doc", Content: "some content for statistics", Collection: "stats-col",
	})

	req := httptest.NewRequest(http.MethodGet, "/internal/knowledge/stats", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var stats knowledge.Stats
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if stats.TotalCollections < 1 {
		t.Errorf("expected at least 1 collection, got %d", stats.TotalCollections)
	}
	if stats.TotalChunks < 1 {
		t.Errorf("expected at least 1 chunk, got %d", stats.TotalChunks)
	}
}

func TestAPICollectionsEmpty(t *testing.T) {
	r, _ := setupTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/internal/knowledge/collections", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var cols []knowledge.Collection
	if err := json.Unmarshal(w.Body.Bytes(), &cols); err != nil {
		// Might be null JSON -- that's fine.
		if w.Body.String() != "null" && w.Body.String() != "[]" {
			t.Fatalf("unexpected response: %s", w.Body.String())
		}
	}
}
```

- [ ] **Step 2: Write the implementation**

Create `internal/knowledge/api.go`:

```go
package knowledge

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes attaches knowledge endpoints to the Gin engine under
// /internal/knowledge/*.
func RegisterRoutes(r *gin.Engine, p *Pipeline) {
	kg := r.Group("/internal/knowledge")
	{
		kg.POST("/ingest", handleIngest(p))
		kg.POST("/query", handleQuery(p))
		kg.GET("/collections", handleCollections(p))
		kg.GET("/stats", handleStats(p))
	}
}

// handleIngest processes document ingestion requests.
func handleIngest(p *Pipeline) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req IngestRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
			return
		}

		result, err := p.Ingest(context.Background(), req)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, result)
	}
}

// handleQuery processes knowledge base query requests.
func handleQuery(p *Pipeline) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req QueryRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
			return
		}

		result, err := p.Query(context.Background(), req)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, result)
	}
}

// handleCollections returns metadata for all knowledge base collections.
func handleCollections(p *Pipeline) gin.HandlerFunc {
	return func(c *gin.Context) {
		cols := p.Collections()
		c.JSON(http.StatusOK, cols)
	}
}

// handleStats returns aggregate knowledge base statistics.
func handleStats(p *Pipeline) gin.HandlerFunc {
	return func(c *gin.Context) {
		stats := p.Stats()
		c.JSON(http.StatusOK, stats)
	}
}
```

- [ ] **Step 3: Run tests**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
go test ./internal/knowledge/ -v -run TestAPI
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/knowledge/api.go internal/knowledge/api_test.go
git commit -m "feat: add Knowledge API endpoints (ingest, query, collections, stats)"
```

---

### Task 8: Wire into Server

**Files:**
- Modify: `cmd/helixllm/main.go`
- Modify: `internal/gateway/router.go` (add optional RAG hook)

- [ ] **Step 1: Update main.go to create Knowledge pipeline and register routes**

Modify `cmd/helixllm/main.go` to add the knowledge service initialization after the Brain service and before `gateway.RegisterRoutes`:

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
	"github.com/HelixDevelopment/HelixLLM/internal/mode"
	"github.com/HelixDevelopment/HelixLLM/internal/server"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/config"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/events"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/health"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/logging"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/observability"
)

func main() {
	modeFlag := flag.String("mode", "", "Operating mode (overrides HELIX_MODE env)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	// CLI flag overrides env
	if *modeFlag != "" {
		cfg.Mode = *modeFlag
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "invalid config: %v\n", err)
		os.Exit(1)
	}

	m, err := mode.Parse(cfg.Mode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	log := logging.New(cfg.Log.Level, cfg.Log.Format)
	bus := events.NewBus()
	defer bus.Close()

	obs, err := observability.New(observability.Options{
		ServiceName: "helixllm",
		Environment: "production",
		Exporter:    cfg.Log.OTELExporter,
	})
	if err != nil {
		log.Error(fmt.Sprintf("observability init failed: %v", err))
		os.Exit(1)
	}
	defer obs.Shutdown()

	checker := health.NewChecker()

	log.WithField("mode", m.String()).Info("starting HelixLLM")

	srv := server.New(server.Options{
		Host:    cfg.Server.Host,
		Port:    cfg.Server.Port,
		TLSCert: cfg.Server.TLSCert,
		TLSKey:  cfg.Server.TLSKey,
		Checker: checker,
	})

	// Create Brain — registers whichever providers are configured.
	brainSvc := brain.New(brain.Config{
		LlamaCppURL:     fmt.Sprintf("http://localhost:%d", cfg.LLM.LocalRPCPort),
		LlamaCppModels:  []string{cfg.LLM.LocalModel},
		OpenAIKey:       cfg.LLM.OpenAIKey,
		AnthropicKey:    cfg.LLM.AnthropicKey,
		DefaultProvider: cfg.LLM.DefaultProvider,
	})

	// Create Knowledge pipeline — in-memory for now, real backends in Phase 6.
	embedder := knowledge.NewHashEmbedder(384) // 384-dim matches all-mpnet-base-v2
	store := knowledge.NewMemoryStore()
	chunker := knowledge.NewFixedSizeChunker(knowledge.ChunkerConfig{
		ChunkSize: cfg.Knowledge.RAGChunkSize,
		Overlap:   cfg.Knowledge.RAGChunkOverlap,
	})
	knowledgePipeline := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          embedder,
		Store:             store,
		Chunker:           chunker,
		DefaultCollection: "default",
		DefaultTopK:       cfg.Knowledge.RAGTopK,
	})

	// Register knowledge endpoints on the server router.
	knowledge.RegisterRoutes(srv.Router(), knowledgePipeline)

	// Register gateway routes (OpenAI + Anthropic compatible endpoints).
	gateway.RegisterRoutes(srv.Router(), gateway.RouterOptions{
		APIKeys:   cfg.Auth.APIKeys,
		RateLimit: 0, // TODO: add to config
		Brain:     brainSvc,
	})

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Info("shutting down...")
		bus.Publish(events.TopicServerStopped, "main", nil)
		cancel()
	}()

	bus.Publish(events.TopicServerStarted, "main", m.String())
	log.WithField("addr", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)).
		Info("server listening")

	if err := srv.ListenAndServe(ctx); err != nil {
		log.WithError(err).Error("server error")
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Add optional RAG context injection to Brain's Complete flow**

Create `internal/knowledge/raghook.go`:

```go
package knowledge

import (
	"context"
	"fmt"

	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// RAGHook provides a method to augment InternalChatRequests with retrieved
// knowledge context. It prepends a system message with relevant context
// retrieved from the knowledge base.
type RAGHook struct {
	pipeline   *Pipeline
	collection string
	topK       int
	minScore   float64
	enabled    bool
}

// RAGHookConfig configures the RAG augmentation hook.
type RAGHookConfig struct {
	Pipeline   *Pipeline
	Collection string
	TopK       int
	MinScore   float64
	Enabled    bool
}

// NewRAGHook creates a RAGHook for injecting knowledge context into requests.
func NewRAGHook(cfg RAGHookConfig) *RAGHook {
	topK := cfg.TopK
	if topK <= 0 {
		topK = 5
	}
	collection := cfg.Collection
	if collection == "" {
		collection = "default"
	}
	return &RAGHook{
		pipeline:   cfg.Pipeline,
		collection: collection,
		topK:       topK,
		minScore:   cfg.MinScore,
		enabled:    cfg.Enabled,
	}
}

// Augment retrieves relevant context from the knowledge base based on the
// last user message and prepends a system message with that context.
// If RAG is disabled, the pipeline is nil, or no results are found, the
// request is returned unchanged.
func (h *RAGHook) Augment(ctx context.Context, req *types.InternalChatRequest) *types.InternalChatRequest {
	if !h.enabled || h.pipeline == nil {
		return req
	}

	// Extract the last user message as the query.
	var query string
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == types.RoleUser {
			query = req.Messages[i].Content
			break
		}
	}
	if query == "" {
		return req
	}

	result, err := h.pipeline.Query(ctx, QueryRequest{
		Query:      query,
		Collection: h.collection,
		TopK:       h.topK,
		MinScore:   h.minScore,
	})
	if err != nil || result.Context == "" {
		return req
	}

	// Prepend a system message with the retrieved context.
	ragMessage := types.InternalMessage{
		Role:    types.RoleSystem,
		Content: fmt.Sprintf("[Retrieved Knowledge Context]\n%s", result.Context),
	}

	augmented := *req
	augmented.Messages = make([]types.InternalMessage, 0, len(req.Messages)+1)
	augmented.Messages = append(augmented.Messages, ragMessage)
	augmented.Messages = append(augmented.Messages, req.Messages...)

	return &augmented
}
```

Create `internal/knowledge/raghook_test.go`:

```go
package knowledge_test

import (
	"context"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

func TestRAGHookDisabled(t *testing.T) {
	hook := knowledge.NewRAGHook(knowledge.RAGHookConfig{
		Enabled: false,
	})

	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "Hello"},
		},
	}

	result := hook.Augment(context.Background(), req)
	if len(result.Messages) != 1 {
		t.Errorf("expected 1 message (unchanged), got %d", len(result.Messages))
	}
}

func TestRAGHookNoPipeline(t *testing.T) {
	hook := knowledge.NewRAGHook(knowledge.RAGHookConfig{
		Enabled: true,
		// Pipeline is nil.
	})

	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "Hello"},
		},
	}

	result := hook.Augment(context.Background(), req)
	if len(result.Messages) != 1 {
		t.Errorf("expected 1 message (unchanged), got %d", len(result.Messages))
	}
}

func TestRAGHookAugmentsWithContext(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	// Ingest some knowledge.
	_, err := p.Ingest(ctx, knowledge.IngestRequest{
		Title:      "Go Facts",
		Content:    "Go was designed at Google by Robert Griesemer, Rob Pike, and Ken Thompson.",
		Collection: "default",
	})
	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}

	hook := knowledge.NewRAGHook(knowledge.RAGHookConfig{
		Pipeline:   p,
		Collection: "default",
		TopK:       3,
		Enabled:    true,
	})

	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "Who designed Go?"},
		},
	}

	result := hook.Augment(ctx, req)
	// Should have the original message plus a prepended system message.
	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result.Messages))
	}
	if result.Messages[0].Role != types.RoleSystem {
		t.Errorf("expected first message to be system, got %s", result.Messages[0].Role)
	}
	if result.Messages[1].Role != types.RoleUser {
		t.Errorf("expected second message to be user, got %s", result.Messages[1].Role)
	}
}

func TestRAGHookNoUserMessage(t *testing.T) {
	p := newTestPipeline()
	hook := knowledge.NewRAGHook(knowledge.RAGHookConfig{
		Pipeline: p,
		Enabled:  true,
	})

	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleSystem, Content: "You are a helper."},
		},
	}

	result := hook.Augment(context.Background(), req)
	if len(result.Messages) != 1 {
		t.Errorf("expected 1 message (no user query to augment), got %d", len(result.Messages))
	}
}

func TestRAGHookEmptyResults(t *testing.T) {
	p := newTestPipeline()
	// Don't ingest anything -- pipeline will return empty results.

	hook := knowledge.NewRAGHook(knowledge.RAGHookConfig{
		Pipeline:   p,
		Collection: "empty-col",
		TopK:       5,
		Enabled:    true,
	})

	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "Find something"},
		},
	}

	result := hook.Augment(context.Background(), req)
	// No context found, so request should be unchanged.
	if len(result.Messages) != 1 {
		t.Errorf("expected 1 message (no context found), got %d", len(result.Messages))
	}
}

func TestRAGHookPreservesOriginalRequest(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	_, _ = p.Ingest(ctx, knowledge.IngestRequest{
		Title:      "Data",
		Content:    "Some useful knowledge to retrieve and augment prompts with.",
		Collection: "default",
	})

	hook := knowledge.NewRAGHook(knowledge.RAGHookConfig{
		Pipeline: p,
		Enabled:  true,
	})

	originalReq := &types.InternalChatRequest{
		Model:       "test-model",
		MaxTokens:   100,
		Temperature: 0.5,
		Messages: []types.InternalMessage{
			{Role: types.RoleSystem, Content: "You are helpful."},
			{Role: types.RoleUser, Content: "Tell me about knowledge"},
		},
	}

	result := hook.Augment(ctx, originalReq)

	// Original request should not be mutated.
	if len(originalReq.Messages) != 2 {
		t.Error("original request was mutated")
	}

	// Augmented result should preserve model and other fields.
	if result.Model != "test-model" {
		t.Errorf("expected model test-model, got %s", result.Model)
	}
	if result.MaxTokens != 100 {
		t.Errorf("expected max_tokens 100, got %d", result.MaxTokens)
	}
}
```

- [ ] **Step 3: Run tests**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
go test ./internal/knowledge/ -v -run TestRAGHook
```

Expected: all tests pass.

- [ ] **Step 4: Verify the full build**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
go build ./...
```

Expected: builds successfully with all new code.

- [ ] **Step 5: Run all knowledge tests**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
go test ./internal/knowledge/ -v -count=1
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/knowledge/raghook.go internal/knowledge/raghook_test.go
git add internal/knowledge/api.go internal/knowledge/api_test.go
git add cmd/helixllm/main.go
git commit -m "feat: wire Knowledge layer into server with API endpoints and RAG hook for Brain integration"
```

---

## Dependency Notes

- `github.com/google/uuid` is already an indirect dependency in `go.mod` (used by existing modules). The Pipeline uses it for generating document IDs. If `go mod tidy` does not promote it to direct, use `go get github.com/google/uuid` before building.
- `github.com/gin-gonic/gin` is already a direct dependency.
- The seven new submodules (RAG, VectorDB, Embeddings, Document, Filesystem, Database, BackgroundTasks) are added to `.gitmodules` and `go.mod` but have NO Go import statements in Phase 4 code. They serve as placeholders for Phase 6 integration with real backends.

## Testing Strategy

All tests use in-memory components:
- **HashEmbedder** -- deterministic SHA-256-based embeddings (no model server needed)
- **MemoryStore** -- brute-force cosine similarity (no Qdrant/pgvector needed)
- **FixedSizeChunker** -- character-based splitting (no NLP models needed)
- **httptest** -- Gin router testing (no HTTP server needed)

Tests cover:
- Type construction and field access
- Embedding determinism, normalization, batch correctness
- Vector store CRUD, search ordering, collection management, statistics
- Chunker size, overlap, positions, edge cases (empty, small)
- Pipeline end-to-end: ingest -> query -> context assembly
- API endpoints: status codes, JSON request/response, error handling
- RAG hook: augmentation, disabled state, no-results, request preservation

## Summary of Commits

| # | Message | Files |
|---|---------|-------|
| 1 | `feat: add Phase 4 Knowledge submodules (RAG, VectorDB, Embeddings, Document, Filesystem, Database, BackgroundTasks)` | `.gitmodules`, `submodules/*`, `go.mod`, `go.sum` |
| 2 | `feat: add Knowledge types (Document, Chunk, Collection, QueryRequest, QueryResult, IngestRequest)` | `internal/knowledge/types.go`, `types_test.go` |
| 3 | `feat: add Embedder interface with deterministic HashEmbedder for testing` | `internal/knowledge/embeddings.go`, `embeddings_test.go` |
| 4 | `feat: add VectorStore interface with in-memory cosine-similarity implementation` | `internal/knowledge/store.go`, `store_test.go` |
| 5 | `feat: add Chunker interface with fixed-size character chunker` | `internal/knowledge/chunker.go`, `chunker_test.go` |
| 6 | `feat: add RAG Pipeline with ingest, query, context assembly, and stats` | `internal/knowledge/pipeline.go`, `pipeline_test.go` |
| 7 | `feat: add Knowledge API endpoints (ingest, query, collections, stats)` | `internal/knowledge/api.go`, `api_test.go` |
| 8 | `feat: wire Knowledge layer into server with API endpoints and RAG hook for Brain integration` | `internal/knowledge/raghook.go`, `raghook_test.go`, `internal/knowledge/api.go`, `api_test.go`, `cmd/helixllm/main.go` |
