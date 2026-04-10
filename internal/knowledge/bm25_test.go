package knowledge_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func TestBM25Index_AddAndSearch(t *testing.T) {
	idx := knowledge.NewBM25Index()
	idx.Add("doc1", "the quick brown fox jumps over the lazy dog")
	idx.Add("doc2", "a fast brown fox leaps over a sleeping dog")
	idx.Add("doc3", "go is a statically typed programming language")

	results := idx.Search("quick fox", 3)
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}

	// doc1 should rank highest because it contains "quick" exactly.
	if results[0].ID != "doc1" {
		t.Errorf("expected doc1 to rank first, got %q", results[0].ID)
	}
}

func TestBM25Index_Search_TopKLimit(t *testing.T) {
	idx := knowledge.NewBM25Index()
	idx.Add("a", "alpha beta gamma")
	idx.Add("b", "alpha delta epsilon")
	idx.Add("c", "alpha zeta eta")

	results := idx.Search("alpha", 2)
	if len(results) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(results))
	}
}

func TestBM25Index_Search_EmptyQuery(t *testing.T) {
	idx := knowledge.NewBM25Index()
	idx.Add("doc1", "hello world")

	results := idx.Search("", 5)
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty query, got %d", len(results))
	}
}

func TestBM25Index_Search_EmptyIndex(t *testing.T) {
	idx := knowledge.NewBM25Index()
	results := idx.Search("hello", 5)
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty index, got %d", len(results))
	}
}

func TestBM25Index_Search_NoMatch(t *testing.T) {
	idx := knowledge.NewBM25Index()
	idx.Add("doc1", "apples oranges bananas")

	results := idx.Search("kubernetes docker", 5)
	if len(results) != 0 {
		t.Errorf("expected 0 results for non-matching query, got %d", len(results))
	}
}

func TestBM25Index_Remove(t *testing.T) {
	idx := knowledge.NewBM25Index()
	idx.Add("doc1", "unique special term")
	idx.Add("doc2", "another document here")

	// Confirm doc1 is found.
	results := idx.Search("unique special", 5)
	if len(results) == 0 {
		t.Fatal("expected results before removal")
	}

	// Remove and verify it's gone.
	idx.Remove("doc1")
	results = idx.Search("unique special", 5)
	for _, r := range results {
		if r.ID == "doc1" {
			t.Error("doc1 should not appear after removal")
		}
	}
}

func TestBM25Index_Remove_NonExistent(t *testing.T) {
	idx := knowledge.NewBM25Index()
	// Should not panic.
	idx.Remove("nonexistent")
}

func TestBM25Index_Add_Replacement(t *testing.T) {
	idx := knowledge.NewBM25Index()
	idx.Add("doc1", "original content here")
	idx.Add("doc1", "replaced content now")

	results := idx.Search("original", 5)
	for _, r := range results {
		if r.ID == "doc1" {
			t.Error("doc1 should not match 'original' after replacement")
		}
	}

	results = idx.Search("replaced", 5)
	found := false
	for _, r := range results {
		if r.ID == "doc1" {
			found = true
		}
	}
	if !found {
		t.Error("doc1 should match 'replaced' after replacement")
	}
}

func TestBM25Index_Ranking_TFMatters(t *testing.T) {
	idx := knowledge.NewBM25Index()
	idx.Add("low", "go lang")
	idx.Add("high", "go go go go go lang programming")

	results := idx.Search("go", 2)
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// The document with higher TF for "go" should score higher.
	if results[0].ID != "high" {
		t.Errorf("expected 'high' to rank first due to higher TF, got %q", results[0].ID)
	}
}

func TestBM25Index_ContentPreserved(t *testing.T) {
	idx := knowledge.NewBM25Index()
	idx.Add("doc1", "Hello World!")

	results := idx.Search("hello", 1)
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].Content != "Hello World!" {
		t.Errorf("content = %q, want %q", results[0].Content, "Hello World!")
	}
}

func TestBM25Index_ScoresPositive(t *testing.T) {
	idx := knowledge.NewBM25Index()
	idx.Add("doc1", "function definition in go")
	idx.Add("doc2", "class definition in python")

	results := idx.Search("function go", 5)
	for _, r := range results {
		if r.Score <= 0 {
			t.Errorf("expected positive score, got %f for %q", r.Score, r.ID)
		}
	}
}

func TestBM25Index_CaseInsensitive(t *testing.T) {
	idx := knowledge.NewBM25Index()
	idx.Add("doc1", "Kubernetes Docker Container")

	results := idx.Search("kubernetes", 5)
	if len(results) == 0 {
		t.Error("expected case-insensitive match")
	}

	results = idx.Search("DOCKER", 5)
	if len(results) == 0 {
		t.Error("expected case-insensitive match for uppercase query")
	}
}
