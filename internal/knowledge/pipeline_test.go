package knowledge_test

import (
	"context"
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
