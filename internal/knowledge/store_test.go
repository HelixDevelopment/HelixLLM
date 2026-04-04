package knowledge_test

import (
	"math"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

// makeChunk builds a Chunk with a hand-crafted unit-length embedding.
func makeChunk(id, docID, content string, embedding []float64) knowledge.Chunk {
	return knowledge.Chunk{
		ID:         id,
		DocumentID: docID,
		Content:    content,
		Embedding:  embedding,
	}
}

// unitVec returns a normalised version of v (mutates in place, returns same slice).
func unitVec(v []float64) []float64 {
	var norm float64
	for _, x := range v {
		norm += x * x
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return v
	}
	for i := range v {
		v[i] /= norm
	}
	return v
}

func TestMemoryStore_UpsertAndSearch(t *testing.T) {
	store := knowledge.NewMemoryStore()
	embedder := knowledge.NewHashEmbedder(16)

	v1, _ := embedder.Embed("machine learning")
	v2, _ := embedder.Embed("deep neural networks")
	v3, _ := embedder.Embed("cooking recipes")

	chunks := []knowledge.Chunk{
		makeChunk("c1", "doc1", "machine learning basics", v1),
		makeChunk("c2", "doc1", "deep neural networks", v2),
		makeChunk("c3", "doc2", "cooking recipes", v3),
	}

	if err := store.Upsert("test", chunks); err != nil {
		t.Fatalf("Upsert error: %v", err)
	}

	// Query close to "machine learning".
	query, _ := embedder.Embed("machine learning")
	results, err := store.Search("test", query, 2)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// The best match should be the machine-learning chunk.
	if results[0].ID != "c1" {
		t.Errorf("top result = %q, want c1", results[0].ID)
	}
	// Results must be sorted descending by score.
	if results[0].Score < results[1].Score {
		t.Errorf("results not sorted: scores %f < %f", results[0].Score, results[1].Score)
	}
}

func TestMemoryStore_UpsertOverwrites(t *testing.T) {
	store := knowledge.NewMemoryStore()
	embedder := knowledge.NewHashEmbedder(16)
	v, _ := embedder.Embed("original")

	orig := makeChunk("c1", "doc1", "original content", v)
	_ = store.Upsert("col", []knowledge.Chunk{orig})

	v2, _ := embedder.Embed("updated")
	updated := makeChunk("c1", "doc1", "updated content", v2)
	_ = store.Upsert("col", []knowledge.Chunk{updated})

	results, _ := store.Search("col", v2, 1)
	if len(results) == 0 {
		t.Fatal("expected 1 result after upsert")
	}
	if results[0].Content != "updated content" {
		t.Errorf("content = %q, want %q", results[0].Content, "updated content")
	}
}

func TestMemoryStore_Delete(t *testing.T) {
	store := knowledge.NewMemoryStore()
	embedder := knowledge.NewHashEmbedder(16)

	v, _ := embedder.Embed("some text")
	chunks := []knowledge.Chunk{
		makeChunk("c1", "doc1", "keep me", v),
		makeChunk("c2", "doc1", "delete me", v),
	}
	_ = store.Upsert("col", chunks)

	if err := store.Delete("col", []string{"c2"}); err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	results, _ := store.Search("col", v, 10)
	for _, r := range results {
		if r.ID == "c2" {
			t.Error("c2 should have been deleted")
		}
	}
	if len(results) != 1 || results[0].ID != "c1" {
		t.Errorf("expected only c1, got %d results", len(results))
	}
}

func TestMemoryStore_Collections(t *testing.T) {
	store := knowledge.NewMemoryStore()
	embedder := knowledge.NewHashEmbedder(16)

	v, _ := embedder.Embed("text")
	_ = store.Upsert("alpha", []knowledge.Chunk{makeChunk("c1", "d1", "a", v)})
	_ = store.Upsert("beta", []knowledge.Chunk{makeChunk("c2", "d2", "b", v), makeChunk("c3", "d2", "c", v)})

	cols, err := store.Collections()
	if err != nil {
		t.Fatalf("Collections error: %v", err)
	}
	if len(cols) != 2 {
		t.Fatalf("expected 2 collections, got %d", len(cols))
	}

	byName := make(map[string]knowledge.Collection)
	for _, c := range cols {
		byName[c.Name] = c
	}

	if byName["alpha"].Chunks != 1 {
		t.Errorf("alpha chunks = %d, want 1", byName["alpha"].Chunks)
	}
	if byName["beta"].Chunks != 2 {
		t.Errorf("beta chunks = %d, want 2", byName["beta"].Chunks)
	}
	if byName["beta"].Documents != 1 {
		t.Errorf("beta documents = %d, want 1 (same docID)", byName["beta"].Documents)
	}
}

func TestMemoryStore_DeleteCollection(t *testing.T) {
	store := knowledge.NewMemoryStore()
	embedder := knowledge.NewHashEmbedder(16)
	v, _ := embedder.Embed("text")

	_ = store.Upsert("removeme", []knowledge.Chunk{makeChunk("c1", "d1", "x", v)})
	_ = store.Upsert("keepme", []knowledge.Chunk{makeChunk("c2", "d2", "y", v)})

	if err := store.DeleteCollection("removeme"); err != nil {
		t.Fatalf("DeleteCollection error: %v", err)
	}

	cols, _ := store.Collections()
	for _, c := range cols {
		if c.Name == "removeme" {
			t.Error("collection 'removeme' should have been deleted")
		}
	}
	if len(cols) != 1 || cols[0].Name != "keepme" {
		t.Errorf("expected only 'keepme', got %+v", cols)
	}
}

func TestMemoryStore_DeleteCollectionNotFound(t *testing.T) {
	store := knowledge.NewMemoryStore()
	err := store.DeleteCollection("does-not-exist")
	if err == nil {
		t.Error("expected error for missing collection, got nil")
	}
}

func TestMemoryStore_Stats(t *testing.T) {
	store := knowledge.NewMemoryStore()
	embedder := knowledge.NewHashEmbedder(16)
	v, _ := embedder.Embed("text")

	_ = store.Upsert("col1", []knowledge.Chunk{
		makeChunk("c1", "d1", "a", v),
		makeChunk("c2", "d1", "b", v),
	})
	_ = store.Upsert("col2", []knowledge.Chunk{
		makeChunk("c3", "d2", "c", v),
	})

	stats, err := store.Stats()
	if err != nil {
		t.Fatalf("Stats error: %v", err)
	}
	if stats.TotalChunks != 3 {
		t.Errorf("TotalChunks = %d, want 3", stats.TotalChunks)
	}
	if stats.TotalDocs != 2 {
		t.Errorf("TotalDocs = %d, want 2", stats.TotalDocs)
	}
	if len(stats.Collections) != 2 {
		t.Errorf("len(Collections) = %d, want 2", len(stats.Collections))
	}
}

func TestMemoryStore_EmptyCollectionSearch(t *testing.T) {
	store := knowledge.NewMemoryStore()
	embedder := knowledge.NewHashEmbedder(16)
	v, _ := embedder.Embed("query")

	results, err := store.Search("nonexistent", v, 5)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for missing collection, got %d", len(results))
	}
}

func TestMemoryStore_SearchTopKRespected(t *testing.T) {
	store := knowledge.NewMemoryStore()
	embedder := knowledge.NewHashEmbedder(16)

	texts := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	chunks := make([]knowledge.Chunk, len(texts))
	for i, text := range texts {
		v, _ := embedder.Embed(text)
		chunks[i] = makeChunk("c"+string(rune('0'+i)), "doc1", text, v)
	}
	_ = store.Upsert("col", chunks)

	query, _ := embedder.Embed("alpha")
	results, _ := store.Search("col", query, 3)
	if len(results) != 3 {
		t.Errorf("expected 3 results (topK=3), got %d", len(results))
	}
}
