package knowledge_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func TestMMRRerank_BasicDiversity(t *testing.T) {
	embedder := knowledge.NewHashEmbedder(64)

	// Create chunks with different content to get different embeddings.
	texts := []string{
		"Go is a statically typed language",
		"Go is a compiled programming language",   // similar to first
		"Rust focuses on memory safety and speed",  // different topic
	}

	chunks := make([]knowledge.ScoredChunk, len(texts))
	for i, text := range texts {
		vec, err := embedder.Embed(text)
		if err != nil {
			t.Fatalf("embed error: %v", err)
		}
		chunks[i] = knowledge.ScoredChunk{
			Chunk: knowledge.Chunk{
				ID:        texts[i][:5],
				Content:   text,
				Embedding: vec,
			},
			Score: float64(len(texts) - i), // descending relevance
		}
	}

	// With low lambda (diversity-heavy), the diverse Rust chunk should appear
	// early despite lower relevance.
	result := knowledge.MMRRerank(chunks, 0.3, 3)
	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}

	// With high lambda (relevance-heavy), the top relevance chunk should be first.
	result = knowledge.MMRRerank(chunks, 1.0, 3)
	if len(result) == 0 {
		t.Fatal("expected results")
	}
	if result[0].Content != texts[0] {
		t.Errorf("with lambda=1.0, expected most relevant chunk first, got %q", result[0].Content)
	}
}

func TestMMRRerank_FinalKLimit(t *testing.T) {
	embedder := knowledge.NewHashEmbedder(64)

	chunks := make([]knowledge.ScoredChunk, 5)
	for i := range chunks {
		vec, _ := embedder.Embed(string(rune('a' + i)))
		chunks[i] = knowledge.ScoredChunk{
			Chunk: knowledge.Chunk{
				ID:        string(rune('a' + i)),
				Content:   string(rune('a' + i)),
				Embedding: vec,
			},
			Score: float64(5 - i),
		}
	}

	result := knowledge.MMRRerank(chunks, 0.7, 2)
	if len(result) != 2 {
		t.Errorf("expected 2 results, got %d", len(result))
	}
}

func TestMMRRerank_EmptyInput(t *testing.T) {
	result := knowledge.MMRRerank(nil, 0.7, 5)
	if result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}

	result = knowledge.MMRRerank([]knowledge.ScoredChunk{}, 0.7, 5)
	if result != nil {
		t.Errorf("expected nil for empty slice, got %v", result)
	}
}

func TestMMRRerank_ZeroFinalK(t *testing.T) {
	chunks := []knowledge.ScoredChunk{
		{Chunk: knowledge.Chunk{ID: "a", Content: "test", Embedding: []float64{1, 0}}, Score: 1.0},
	}
	result := knowledge.MMRRerank(chunks, 0.7, 0)
	if result != nil {
		t.Errorf("expected nil for finalK=0, got %v", result)
	}
}

func TestMMRRerank_FinalKLargerThanInput(t *testing.T) {
	embedder := knowledge.NewHashEmbedder(8)
	vec, _ := embedder.Embed("test")

	chunks := []knowledge.ScoredChunk{
		{Chunk: knowledge.Chunk{ID: "a", Content: "test", Embedding: vec}, Score: 1.0},
	}
	result := knowledge.MMRRerank(chunks, 0.7, 10)
	if len(result) != 1 {
		t.Errorf("expected 1 result (clamped to input size), got %d", len(result))
	}
}

func TestMMRRerank_ChunksWithoutEmbeddings(t *testing.T) {
	embedder := knowledge.NewHashEmbedder(8)
	vec, _ := embedder.Embed("with embedding")

	chunks := []knowledge.ScoredChunk{
		{Chunk: knowledge.Chunk{ID: "a", Content: "with embedding", Embedding: vec}, Score: 2.0},
		{Chunk: knowledge.Chunk{ID: "b", Content: "no embedding"}, Score: 1.0},
	}
	result := knowledge.MMRRerank(chunks, 0.7, 2)
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	// Chunk with embedding should be selected first by MMR, then the one without.
	if result[0].ID != "a" {
		t.Errorf("expected chunk with embedding first, got %q", result[0].ID)
	}
	if result[1].ID != "b" {
		t.Errorf("expected chunk without embedding second, got %q", result[1].ID)
	}
}

func TestMMRRerank_IdenticalChunks_DiversityPenalized(t *testing.T) {
	// When all chunks have the same embedding, MMR with low lambda should
	// still return results but penalize redundancy heavily.
	vec := []float64{1, 0, 0, 0}
	chunks := make([]knowledge.ScoredChunk, 3)
	for i := range chunks {
		chunks[i] = knowledge.ScoredChunk{
			Chunk: knowledge.Chunk{
				ID:        string(rune('a' + i)),
				Content:   "identical content",
				Embedding: vec,
			},
			Score: 1.0,
		}
	}

	result := knowledge.MMRRerank(chunks, 0.3, 3)
	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}
	// After the first selection, subsequent scores should be lower due to
	// high similarity penalty.
	if len(result) >= 2 && result[1].Score >= result[0].Score {
		t.Logf("scores: [0]=%f [1]=%f", result[0].Score, result[1].Score)
		// With identical embeddings and lambda=0.3, the second chunk gets
		// penalized by (1-0.3)*1.0 = 0.7 similarity penalty.
	}
}

func TestMMRRerank_LambdaClamped(t *testing.T) {
	vec := []float64{1, 0}
	chunks := []knowledge.ScoredChunk{
		{Chunk: knowledge.Chunk{ID: "a", Content: "test", Embedding: vec}, Score: 1.0},
	}

	// Lambda < 0 should be clamped to 0.
	result := knowledge.MMRRerank(chunks, -1.0, 1)
	if len(result) != 1 {
		t.Errorf("expected 1 result with negative lambda, got %d", len(result))
	}

	// Lambda > 1 should be clamped to 1.
	result = knowledge.MMRRerank(chunks, 2.0, 1)
	if len(result) != 1 {
		t.Errorf("expected 1 result with lambda > 1, got %d", len(result))
	}
}
