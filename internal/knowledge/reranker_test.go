package knowledge

import (
	"testing"
)

func TestScoreReranker_SortsDescending(t *testing.T) {
	reranker := &ScoreReranker{}

	chunks := []ScoredChunk{
		{Chunk: Chunk{ID: "a", Content: "low"}, Score: 0.2},
		{Chunk: Chunk{ID: "b", Content: "high"}, Score: 0.9},
		{Chunk: Chunk{ID: "c", Content: "mid"}, Score: 0.5},
	}

	result, err := reranker.Rerank("query", chunks, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}
	if result[0].ID != "b" {
		t.Errorf("expected first result to be 'b' (highest score), got %q", result[0].ID)
	}
	if result[1].ID != "c" {
		t.Errorf("expected second result to be 'c', got %q", result[1].ID)
	}
	if result[2].ID != "a" {
		t.Errorf("expected third result to be 'a' (lowest score), got %q", result[2].ID)
	}
}

func TestScoreReranker_TopKTrimming(t *testing.T) {
	reranker := &ScoreReranker{}

	chunks := []ScoredChunk{
		{Chunk: Chunk{ID: "a"}, Score: 0.9},
		{Chunk: Chunk{ID: "b"}, Score: 0.7},
		{Chunk: Chunk{ID: "c"}, Score: 0.5},
		{Chunk: Chunk{ID: "d"}, Score: 0.3},
		{Chunk: Chunk{ID: "e"}, Score: 0.1},
	}

	result, err := reranker.Rerank("query", chunks, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 results after topK trim, got %d", len(result))
	}
	if result[0].ID != "a" || result[1].ID != "b" || result[2].ID != "c" {
		t.Errorf("unexpected order: %v", result)
	}
}

func TestScoreReranker_TopKLargerThanInput(t *testing.T) {
	reranker := &ScoreReranker{}

	chunks := []ScoredChunk{
		{Chunk: Chunk{ID: "a"}, Score: 0.5},
	}

	result, err := reranker.Rerank("query", chunks, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 result when topK > input size, got %d", len(result))
	}
}

func TestScoreReranker_EmptyInput(t *testing.T) {
	reranker := &ScoreReranker{}

	result, err := reranker.Rerank("query", nil, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty result for nil input, got %d", len(result))
	}
}

func TestScoreReranker_DoesNotMutateInput(t *testing.T) {
	reranker := &ScoreReranker{}

	chunks := []ScoredChunk{
		{Chunk: Chunk{ID: "a"}, Score: 0.2},
		{Chunk: Chunk{ID: "b"}, Score: 0.9},
	}

	_, err := reranker.Rerank("query", chunks, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Original slice should be unchanged.
	if chunks[0].ID != "a" || chunks[1].ID != "b" {
		t.Error("Rerank mutated the original input slice")
	}
}

func TestLLMReranker_FallbackToScoreReranker(t *testing.T) {
	reranker := NewLLMReranker("http://localhost:8080", "test-model")

	chunks := []ScoredChunk{
		{Chunk: Chunk{ID: "a"}, Score: 0.3},
		{Chunk: Chunk{ID: "b"}, Score: 0.8},
	}

	result, err := reranker.Rerank("test query", chunks, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// LLMReranker currently falls back to ScoreReranker, so order should be
	// by score descending.
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if result[0].ID != "b" {
		t.Errorf("expected 'b' first (highest score), got %q", result[0].ID)
	}
}
