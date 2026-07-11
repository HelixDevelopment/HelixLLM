package knowledge_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func newTestPipeline() *knowledge.Pipeline {
	return knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          knowledge.NewHashEmbedder(64),
		Store:             knowledge.NewMemoryStore(),
		Chunker:           knowledge.NewFixedSizeChunker(100, 10),
		DefaultCollection: "default",
		DefaultTopK:       5,
	})
}

func TestPipeline_Ingest_StoresChunks(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	result, err := p.Ingest(ctx, knowledge.IngestRequest{
		Title:      "Test Doc",
		Content:    "Hello world. This is a test document with enough content to produce at least one chunk.",
		Collection: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DocumentID == "" {
		t.Error("expected non-empty document ID")
	}
	if result.Chunks <= 0 {
		t.Errorf("expected at least one chunk, got %d", result.Chunks)
	}
	if result.Collection != "test" {
		t.Errorf("expected collection %q, got %q", "test", result.Collection)
	}

	// Confirm chunks are retrievable via query
	qr, err := p.Query(ctx, knowledge.QueryRequest{
		Query:      "test document",
		Collection: "test",
		TopK:       5,
	})
	if err != nil {
		t.Fatalf("unexpected query error: %v", err)
	}
	if len(qr.Chunks) == 0 {
		t.Error("expected at least one chunk returned from query")
	}
}

func TestPipeline_Query_ReturnsRelevantChunks(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	_, err := p.Ingest(ctx, knowledge.IngestRequest{
		Content:    "Go is a statically typed, compiled programming language designed at Google.",
		Collection: "docs",
	})
	if err != nil {
		t.Fatalf("ingest error: %v", err)
	}

	qr, err := p.Query(ctx, knowledge.QueryRequest{
		Query:      "Go programming language",
		Collection: "docs",
		TopK:       3,
	})
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if qr.Query != "Go programming language" {
		t.Errorf("unexpected query echo: %q", qr.Query)
	}
	if len(qr.Chunks) == 0 {
		t.Error("expected at least one chunk")
	}
	if qr.Context == "" {
		t.Error("expected non-empty context string")
	}
}

func TestPipeline_Query_MinScoreFiltering(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	_, err := p.Ingest(ctx, knowledge.IngestRequest{
		Content:    "Rust is a memory-safe systems programming language.",
		Collection: "lang",
	})
	if err != nil {
		t.Fatalf("ingest error: %v", err)
	}

	// High min score — likely filters everything out since we use hash embeddings.
	qr, err := p.Query(ctx, knowledge.QueryRequest{
		Query:      "Rust language",
		Collection: "lang",
		TopK:       5,
		MinScore:   0.9999,
	})
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	for _, sc := range qr.Chunks {
		if sc.Score < 0.9999 {
			t.Errorf("chunk score %f below min score 0.9999", sc.Score)
		}
	}
}

func TestPipeline_Collections_AfterIngest(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	_, err := p.Ingest(ctx, knowledge.IngestRequest{
		Content:    "Some content for the collection test.",
		Collection: "col-a",
	})
	if err != nil {
		t.Fatalf("ingest error: %v", err)
	}

	cols, err := p.Collections(ctx)
	if err != nil {
		t.Fatalf("collections error: %v", err)
	}
	found := false
	for _, c := range cols {
		if c.Name == "col-a" {
			found = true
			if c.Chunks <= 0 {
				t.Errorf("expected chunks > 0 in collection %q", c.Name)
			}
		}
	}
	if !found {
		t.Error("expected collection 'col-a' to be present")
	}
}

func TestPipeline_Stats_ReturnsTotals(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	_, err := p.Ingest(ctx, knowledge.IngestRequest{
		Content:    "First document for stats testing.",
		Collection: "stats-col",
	})
	if err != nil {
		t.Fatalf("first ingest error: %v", err)
	}
	_, err = p.Ingest(ctx, knowledge.IngestRequest{
		Content:    "Second document for stats testing with more content to ensure chunking.",
		Collection: "stats-col",
	})
	if err != nil {
		t.Fatalf("second ingest error: %v", err)
	}

	stats, err := p.Stats(ctx)
	if err != nil {
		t.Fatalf("stats error: %v", err)
	}
	if stats.TotalDocs < 2 {
		t.Errorf("expected at least 2 total docs, got %d", stats.TotalDocs)
	}
	if stats.TotalChunks <= 0 {
		t.Errorf("expected total chunks > 0, got %d", stats.TotalChunks)
	}
}

func TestPipeline_Ingest_EmptyContent_Error(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	_, err := p.Ingest(ctx, knowledge.IngestRequest{
		Content:    "",
		Collection: "col",
	})
	if err == nil {
		t.Error("expected error for empty content, got nil")
	}
}

func TestPipeline_Ingest_EmptyCollection_Error(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	_, err := p.Ingest(ctx, knowledge.IngestRequest{
		Content:    "Some content",
		Collection: "",
	})
	if err == nil {
		t.Error("expected error for empty collection, got nil")
	}
}

func TestPipeline_Query_EmptyQuery_Error(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	_, err := p.Query(ctx, knowledge.QueryRequest{
		Query:      "",
		Collection: "col",
	})
	if err == nil {
		t.Error("expected error for empty query, got nil")
	}
}

func TestPipeline_Query_EmptyCollection_Error(t *testing.T) {
	p := newTestPipeline()
	ctx := context.Background()

	_, err := p.Query(ctx, knowledge.QueryRequest{
		Query:      "something",
		Collection: "",
	})
	if err == nil {
		t.Error("expected error for empty collection, got nil")
	}
}

// ---------------------------------------------------------------------------
// Error-path tests using a mock store and mock embedder/chunker
// ---------------------------------------------------------------------------

// errStore is a VectorStore that always returns an error from the specified
// method.
type errStore struct {
	*knowledge.MemoryStore
	failUpsert      bool
	failSearch      bool
	failCollections bool
	failStats       bool
}

func (e *errStore) Upsert(col string, chunks []knowledge.Chunk) error {
	if e.failUpsert {
		return fmt.Errorf("upsert error")
	}
	return e.MemoryStore.Upsert(col, chunks)
}

func (e *errStore) Search(col string, vec []float64, topK int) ([]knowledge.ScoredChunk, error) {
	if e.failSearch {
		return nil, fmt.Errorf("search error")
	}
	return e.MemoryStore.Search(col, vec, topK)
}

func (e *errStore) Delete(col string, ids []string) error {
	return e.MemoryStore.Delete(col, ids)
}

func (e *errStore) DeleteCollection(name string) error {
	return e.MemoryStore.DeleteCollection(name)
}

func (e *errStore) Collections() ([]knowledge.Collection, error) {
	if e.failCollections {
		return nil, fmt.Errorf("collections error")
	}
	return e.MemoryStore.Collections()
}

func (e *errStore) Stats() (*knowledge.Stats, error) {
	if e.failStats {
		return nil, fmt.Errorf("stats error")
	}
	return e.MemoryStore.Stats()
}

func TestPipeline_Ingest_UpsertError(t *testing.T) {
	store := &errStore{MemoryStore: knowledge.NewMemoryStore(), failUpsert: true}
	p := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          knowledge.NewHashEmbedder(16),
		Store:             store,
		Chunker:           knowledge.NewFixedSizeChunker(100, 0),
		DefaultCollection: "default",
		DefaultTopK:       5,
	})

	_, err := p.Ingest(context.Background(), knowledge.IngestRequest{
		Content:    "some content",
		Collection: "col",
	})
	if err == nil {
		t.Error("expected error from Ingest when upsert fails, got nil")
	}
}

func TestPipeline_Query_SearchError(t *testing.T) {
	store := &errStore{MemoryStore: knowledge.NewMemoryStore(), failSearch: true}
	p := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          knowledge.NewHashEmbedder(16),
		Store:             store,
		Chunker:           knowledge.NewFixedSizeChunker(100, 0),
		DefaultCollection: "default",
		DefaultTopK:       5,
	})

	_, err := p.Query(context.Background(), knowledge.QueryRequest{
		Query:      "something",
		Collection: "col",
	})
	if err == nil {
		t.Error("expected error from Query when search fails, got nil")
	}
}

func TestPipeline_Collections_Error(t *testing.T) {
	store := &errStore{MemoryStore: knowledge.NewMemoryStore(), failCollections: true}
	p := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          knowledge.NewHashEmbedder(16),
		Store:             store,
		Chunker:           knowledge.NewFixedSizeChunker(100, 0),
		DefaultCollection: "default",
	})

	_, err := p.Collections(context.Background())
	if err == nil {
		t.Error("expected error from Collections when store fails, got nil")
	}
}

func TestPipeline_Stats_Error(t *testing.T) {
	store := &errStore{MemoryStore: knowledge.NewMemoryStore(), failStats: true}
	p := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          knowledge.NewHashEmbedder(16),
		Store:             store,
		Chunker:           knowledge.NewFixedSizeChunker(100, 0),
		DefaultCollection: "default",
	})

	_, err := p.Stats(context.Background())
	if err == nil {
		t.Error("expected error from Stats when store fails, got nil")
	}
}

func TestPipeline_DefaultTopK_AppliedWhenZero(t *testing.T) {
	// NewPipeline with DefaultTopK=0 should clamp to 5.
	p := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          knowledge.NewHashEmbedder(16),
		Store:             knowledge.NewMemoryStore(),
		Chunker:           knowledge.NewFixedSizeChunker(50, 0),
		DefaultCollection: "default",
		DefaultTopK:       0, // should be clamped to 5
	})
	if p == nil {
		t.Fatal("NewPipeline returned nil")
	}
	// Verify it works by ingesting and querying.
	_, err := p.Ingest(context.Background(), knowledge.IngestRequest{
		Content:    "test content for default topk",
		Collection: "col",
	})
	if err != nil {
		t.Fatalf("Ingest error: %v", err)
	}
	qr, err := p.Query(context.Background(), knowledge.QueryRequest{
		Query:      "test",
		Collection: "col",
		TopK:       0, // uses defaultTopK = 5
	})
	if err != nil {
		t.Fatalf("Query error: %v", err)
	}
	if qr == nil {
		t.Error("expected non-nil query result")
	}
}

func TestPipeline_Query_DefaultTopK(t *testing.T) {
	p := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          knowledge.NewHashEmbedder(64),
		Store:             knowledge.NewMemoryStore(),
		Chunker:           knowledge.NewFixedSizeChunker(20, 0),
		DefaultCollection: "default",
		DefaultTopK:       2,
	})
	ctx := context.Background()

	// Ingest enough content to produce many chunks
	_, err := p.Ingest(ctx, knowledge.IngestRequest{
		Content:    "AAAA BBBB CCCC DDDD EEEE FFFF GGGG HHHH IIII JJJJ KKKK LLLL",
		Collection: "tk",
	})
	if err != nil {
		t.Fatalf("ingest error: %v", err)
	}

	qr, err := p.Query(ctx, knowledge.QueryRequest{
		Query:      "AAAA",
		Collection: "tk",
		// TopK = 0 → should use defaultTopK = 2
	})
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(qr.Chunks) > 2 {
		t.Errorf("expected at most 2 chunks (defaultTopK), got %d", len(qr.Chunks))
	}
}

// errChunker is a mock Chunker that always returns an error.
type errChunker struct{}

func (e *errChunker) Chunk(_ knowledge.Document) ([]knowledge.Chunk, error) {
	return nil, fmt.Errorf("chunker: mock error")
}

func TestPipeline_Ingest_ChunkError(t *testing.T) {
	// Using errChunker causes Chunk to fail after validation, covering the
	// "ingest: chunk document" error path.
	p := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          knowledge.NewHashEmbedder(64),
		Store:             knowledge.NewMemoryStore(),
		Chunker:           &errChunker{},
		DefaultCollection: "default",
		DefaultTopK:       5,
	})
	_, err := p.Ingest(context.Background(), knowledge.IngestRequest{
		Content:    "some valid content",
		Collection: "col",
	})
	if err == nil {
		t.Fatal("expected chunk error, got nil")
	}
}

// errEmbedder is a mock Embedder that always returns an error.
type errEmbedder struct{}

func (e *errEmbedder) Embed(_ string) ([]float64, error) {
	return nil, fmt.Errorf("embed: mock error")
}

func (e *errEmbedder) EmbedBatch(_ []string) ([][]float64, error) {
	return nil, fmt.Errorf("embed_batch: mock error")
}

func (e *errEmbedder) Dimension() int { return 64 }

func TestPipeline_Ingest_EmbedError(t *testing.T) {
	// Using errEmbedder causes EmbedBatch to fail, covering the
	// "ingest: embed chunks" error path.
	p := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          &errEmbedder{},
		Store:             knowledge.NewMemoryStore(),
		Chunker:           knowledge.NewFixedSizeChunker(50, 0),
		DefaultCollection: "default",
		DefaultTopK:       5,
	})
	_, err := p.Ingest(context.Background(), knowledge.IngestRequest{
		Content:    "some content to chunk and embed",
		Collection: "col",
	})
	if err == nil {
		t.Fatal("expected embed error, got nil")
	}
}

func TestPipeline_Query_EmbedError(t *testing.T) {
	// Using errEmbedder causes Embed to fail during Query, covering the
	// "query: embed query" error path.
	p := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          &errEmbedder{},
		Store:             knowledge.NewMemoryStore(),
		Chunker:           knowledge.NewFixedSizeChunker(50, 0),
		DefaultCollection: "default",
		DefaultTopK:       5,
	})
	_, err := p.Query(context.Background(), knowledge.QueryRequest{
		Query:      "some query",
		Collection: "col",
	})
	if err == nil {
		t.Fatal("expected embed error during query, got nil")
	}
}

func TestPipeline_Query_MinScore_PassingChunks(t *testing.T) {
	// Set MinScore to a very small positive value so that results with any
	// non-trivial score pass — covers the "filtered = append(filtered, sc)"
	// branch in the MinScore loop (req.MinScore > 0 AND sc.Score >= MinScore).
	p := newTestPipeline()
	ctx := context.Background()

	_, err := p.Ingest(ctx, knowledge.IngestRequest{
		Content:    "Python is a high-level programming language.",
		Collection: "py",
	})
	if err != nil {
		t.Fatalf("ingest error: %v", err)
	}

	qr, err := p.Query(ctx, knowledge.QueryRequest{
		Query:      "Python",
		Collection: "py",
		TopK:       5,
		MinScore:   0.0001, // low enough that real scores pass; exercises the append branch
	})
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// At least one chunk should pass the low threshold.
	if len(qr.Chunks) == 0 {
		t.Error("expected at least one chunk to pass MinScore=0.0001")
	}
}

// ---------------------------------------------------------------------------
// §11.4.135 standing regression guard: production Pipeline.Query MUST call
// through to a configured Reranker (embed -> retrieve -> RERANK -> ground).
//
// Forensic anchor (V&V finding RERANKER-NOT-WIRED, 2026-07-11 wave-2): the
// RAG cross-encoder reranker (TEI/bge) was proven only in a standalone QA
// harness — internal/knowledge.Pipeline.Query never called it, so
// production RAG queries got NO cross-encoder reranking regardless of
// configuration. These tests are the reproduce-first RED baseline (§11.4.115)
// for that gap: with the fix reverted, TestPipeline_Query_AppliesReranker_*
// FAIL because Query() ignores PipelineConfig.Reranker entirely. With the
// fix applied they PASS — the SAME test source proves both defect-present
// and defect-absent (one source, two roles, no separate happy-path test).
// ---------------------------------------------------------------------------

// fakeReverseReranker is a deterministic Reranker test double that reverses
// whatever candidate order it is handed and records the query/candidate
// count it was called with, so tests can assert Pipeline.Query genuinely
// invokes the configured reranker rather than merely accepting it.
type fakeReverseReranker struct {
	calledWithQuery string
	calledWithN     int
	calls           int
	err             error
}

func (f *fakeReverseReranker) Rerank(query string, chunks []knowledge.ScoredChunk, topK int) ([]knowledge.ScoredChunk, error) {
	f.calls++
	f.calledWithQuery = query
	f.calledWithN = len(chunks)
	if f.err != nil {
		return nil, f.err
	}
	result := make([]knowledge.ScoredChunk, len(chunks))
	for i, c := range chunks {
		result[len(chunks)-1-i] = c
	}
	if topK > 0 && topK < len(result) {
		result = result[:topK]
	}
	return result, nil
}

// seedThreeChunks upserts three distinctly-embedded chunks directly into the
// store (bypassing Ingest so the exact chunk set + embeddings are known),
// returning their IDs in insertion order.
func seedThreeChunks(t *testing.T, store knowledge.VectorStore, embedder knowledge.Embedder, collection string) []string {
	t.Helper()
	texts := map[string]string{
		"chunk-alpha": "alpha content about apples",
		"chunk-beta":  "beta content about bananas",
		"chunk-gamma": "gamma content about grapes",
	}
	ids := []string{"chunk-alpha", "chunk-beta", "chunk-gamma"}
	chunks := make([]knowledge.Chunk, 0, len(ids))
	for _, id := range ids {
		vec, err := embedder.Embed(texts[id])
		if err != nil {
			t.Fatalf("embed %q: %v", id, err)
		}
		chunks = append(chunks, knowledge.Chunk{
			ID:         id,
			DocumentID: "seed-doc",
			Content:    texts[id],
			Embedding:  vec,
		})
	}
	if err := store.Upsert(collection, chunks); err != nil {
		t.Fatalf("seed upsert: %v", err)
	}
	return ids
}

func TestPipeline_Query_AppliesReranker_NonHybrid(t *testing.T) {
	store := knowledge.NewMemoryStore()
	embedder := knowledge.NewHashEmbedder(32)
	seedThreeChunks(t, store, embedder, "docs")

	plain := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder: embedder, Store: store, Chunker: knowledge.NewFixedSizeChunker(100, 10),
		DefaultCollection: "docs", DefaultTopK: 3,
	})
	plainResult, err := plain.Query(context.Background(), knowledge.QueryRequest{
		Query: "alpha content about apples", Collection: "docs", TopK: 3,
	})
	if err != nil {
		t.Fatalf("plain query error: %v", err)
	}
	if len(plainResult.Chunks) != 3 {
		t.Fatalf("expected 3 chunks from the unreranked baseline, got %d", len(plainResult.Chunks))
	}

	reranker := &fakeReverseReranker{}
	reranked := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder: embedder, Store: store, Chunker: knowledge.NewFixedSizeChunker(100, 10),
		DefaultCollection: "docs", DefaultTopK: 3, Reranker: reranker,
	})
	rerankedResult, err := reranked.Query(context.Background(), knowledge.QueryRequest{
		Query: "alpha content about apples", Collection: "docs", TopK: 3,
	})
	if err != nil {
		t.Fatalf("reranked query error: %v", err)
	}

	// PRODUCTION-PATH PROOF: the configured Reranker MUST actually have been
	// invoked by Pipeline.Query. Before the fix, fakeReverseReranker.calls
	// stays 0 forever — this is the exact defect (RERANKER-NOT-WIRED).
	if reranker.calls == 0 {
		t.Fatal("Pipeline.Query never called the configured Reranker (RERANKER-NOT-WIRED regression)")
	}
	if reranker.calledWithQuery != "alpha content about apples" {
		t.Errorf("reranker called with query %q, want %q", reranker.calledWithQuery, "alpha content about apples")
	}
	if reranker.calledWithN == 0 {
		t.Fatal("reranker was called with zero candidates")
	}

	if len(rerankedResult.Chunks) != len(plainResult.Chunks) {
		t.Fatalf("reranked result has %d chunks, want %d", len(rerankedResult.Chunks), len(plainResult.Chunks))
	}
	for i := range plainResult.Chunks {
		want := plainResult.Chunks[len(plainResult.Chunks)-1-i].ID
		got := rerankedResult.Chunks[i].ID
		if got != want {
			t.Errorf("position %d: got chunk %q, want %q (reversed reranker order was not applied — Query() is not using the Reranker's output)",
				i, got, want)
		}
	}
}

func TestPipeline_Query_AppliesReranker_Hybrid(t *testing.T) {
	store := knowledge.NewMemoryStore()
	embedder := knowledge.NewHashEmbedder(32)
	seedThreeChunks(t, store, embedder, "docs-hybrid")

	// Index the same content into BM25 too so hybrid retrieval finds it —
	// hybrid Search embeds+keyword-searches independently of Ingest, so we
	// index by hand exactly like NewPipeline does internally for Ingest.
	reranker := &fakeReverseReranker{}
	p := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder: embedder, Store: store, Chunker: knowledge.NewFixedSizeChunker(100, 10),
		DefaultCollection: "docs-hybrid", DefaultTopK: 3,
		HybridEnabled: true, Reranker: reranker,
	})

	result, err := p.Query(context.Background(), knowledge.QueryRequest{
		Query: "beta content about bananas", Collection: "docs-hybrid", TopK: 3,
	})
	if err != nil {
		t.Fatalf("hybrid reranked query error: %v", err)
	}
	if reranker.calls == 0 {
		t.Fatal("Pipeline.Query (hybrid path) never called the configured Reranker (RERANKER-NOT-WIRED regression)")
	}
	if len(result.Chunks) == 0 {
		t.Fatal("expected at least one chunk from hybrid+rerank query")
	}
}

func TestPipeline_Query_RerankError_Propagates(t *testing.T) {
	store := knowledge.NewMemoryStore()
	embedder := knowledge.NewHashEmbedder(32)
	seedThreeChunks(t, store, embedder, "docs-err")

	reranker := &fakeReverseReranker{err: fmt.Errorf("tei reranker: simulated failure")}
	p := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder: embedder, Store: store, Chunker: knowledge.NewFixedSizeChunker(100, 10),
		DefaultCollection: "docs-err", DefaultTopK: 3, Reranker: reranker,
	})

	_, err := p.Query(context.Background(), knowledge.QueryRequest{
		Query: "alpha content about apples", Collection: "docs-err", TopK: 3,
	})
	// ANTI-BLUFF: a reranker failure MUST surface as an error, never a
	// silent fallback to unranked results (§11.4.1 / §11.4.6) — a caller
	// that explicitly configured reranking must be able to tell it did not
	// happen.
	if err == nil {
		t.Fatal("expected Query to return an error when the configured Reranker fails, got nil")
	}
}

func TestPipeline_Query_NoReranker_BehaviourUnchanged(t *testing.T) {
	// Regression guard: a Pipeline built WITHOUT a Reranker (the default,
	// pre-existing configuration used by every caller before this fix)
	// must retrieve and trim exactly as before — no over-fetch, no rerank
	// call, byte-identical chunk count to DefaultTopK.
	store := knowledge.NewMemoryStore()
	embedder := knowledge.NewHashEmbedder(32)
	seedThreeChunks(t, store, embedder, "docs-plain")

	p := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder: embedder, Store: store, Chunker: knowledge.NewFixedSizeChunker(100, 10),
		DefaultCollection: "docs-plain", DefaultTopK: 2,
	})
	result, err := p.Query(context.Background(), knowledge.QueryRequest{
		Query: "gamma content about grapes", Collection: "docs-plain", TopK: 2,
	})
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(result.Chunks) != 2 {
		t.Errorf("expected exactly 2 chunks (topK, no reranker over-fetch), got %d", len(result.Chunks))
	}
}
