package knowledge_test

import (
	"context"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func setupHybrid(t *testing.T) (*knowledge.HybridRetriever, *knowledge.BM25Index, *knowledge.MemoryStore) {
	t.Helper()
	embedder := knowledge.NewHashEmbedder(64)
	store := knowledge.NewMemoryStore()
	bm25 := knowledge.NewBM25Index()
	retriever := knowledge.NewHybridRetriever(embedder, store, bm25)

	// Add some test chunks to both stores.
	chunks := []knowledge.Chunk{
		{ID: "ch1", DocumentID: "d1", Content: "Go is a statically typed language"},
		{ID: "ch2", DocumentID: "d1", Content: "Python is a dynamic scripting language"},
		{ID: "ch3", DocumentID: "d1", Content: "Rust focuses on memory safety"},
	}

	// Embed and store.
	for i := range chunks {
		vec, err := embedder.Embed(chunks[i].Content)
		if err != nil {
			t.Fatalf("embed error: %v", err)
		}
		chunks[i].Embedding = vec
	}
	if err := store.Upsert("test", chunks); err != nil {
		t.Fatalf("upsert error: %v", err)
	}

	// Add to BM25 index.
	for _, ch := range chunks {
		bm25.Add(ch.ID, ch.Content)
	}

	return retriever, bm25, store
}

func TestHybridRetriever_Search_ReturnsResults(t *testing.T) {
	retriever, _, _ := setupHybrid(t)
	ctx := context.Background()

	results, err := retriever.Search(ctx, "test", "Go language", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one result from hybrid search")
	}
}

func TestHybridRetriever_Search_TopKLimit(t *testing.T) {
	retriever, _, _ := setupHybrid(t)
	ctx := context.Background()

	results, err := retriever.Search(ctx, "test", "language", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) > 1 {
		t.Errorf("expected at most 1 result, got %d", len(results))
	}
}

func TestHybridRetriever_Search_CombinesScores(t *testing.T) {
	retriever, _, _ := setupHybrid(t)
	ctx := context.Background()

	results, err := retriever.Search(ctx, "test", "statically typed Go", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}

	// The Go chunk should rank highly due to both semantic and keyword match.
	foundGo := false
	for _, r := range results {
		if r.ID == "ch1" {
			foundGo = true
		}
	}
	if !foundGo {
		t.Error("expected Go chunk (ch1) to appear in hybrid results for 'statically typed Go'")
	}
}

func TestHybridRetriever_Search_EmptyCollection(t *testing.T) {
	retriever, _, _ := setupHybrid(t)
	ctx := context.Background()

	results, err := retriever.Search(ctx, "empty", "query", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// BM25 may still return results since it's not collection-scoped,
	// but vector store should return empty.
	_ = results // no error is the key check
}

func TestHybridRetriever_SetWeights(t *testing.T) {
	embedder := knowledge.NewHashEmbedder(64)
	store := knowledge.NewMemoryStore()
	bm25 := knowledge.NewBM25Index()
	retriever := knowledge.NewHybridRetriever(embedder, store, bm25)

	// Override weights.
	retriever.SetWeights(0.5, 0.5)

	// Add a document.
	chunks := []knowledge.Chunk{
		{ID: "ch1", DocumentID: "d1", Content: "test document"},
	}
	vec, _ := embedder.Embed(chunks[0].Content)
	chunks[0].Embedding = vec
	_ = store.Upsert("col", chunks)
	bm25.Add("ch1", "test document")

	ctx := context.Background()
	results, err := retriever.Search(ctx, "col", "test", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one result with equal weights")
	}
}

func TestHybridRetriever_Search_ScoresDescending(t *testing.T) {
	retriever, _, _ := setupHybrid(t)
	ctx := context.Background()

	results, err := retriever.Search(ctx, "test", "language", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted: score[%d]=%f > score[%d]=%f",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}
}
