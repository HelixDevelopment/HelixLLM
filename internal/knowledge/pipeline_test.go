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
