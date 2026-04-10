package knowledge_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func TestExpandQuery_WithSynonym(t *testing.T) {
	results := knowledge.ExpandQuery("find the function definition")
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results (original + expansions), got %d", len(results))
	}
	// First should always be the original.
	if results[0] != "find the function definition" {
		t.Errorf("first result should be original query, got %q", results[0])
	}

	// Should contain expansions with synonyms of "function".
	foundMethod := false
	foundDef := false
	foundFunc := false
	for _, r := range results[1:] {
		if r != results[0] {
			switch {
			case contains(r, "method"):
				foundMethod = true
			case contains(r, "def"):
				foundDef = true
			case contains(r, "func"):
				foundFunc = true
			}
		}
	}
	if !foundMethod && !foundDef && !foundFunc {
		t.Errorf("expected at least one synonym expansion, got: %v", results)
	}
}

func TestExpandQuery_NoSynonym(t *testing.T) {
	results := knowledge.ExpandQuery("kubernetes deployment yaml")
	if len(results) != 1 {
		t.Errorf("expected only original query when no synonyms match, got %d: %v", len(results), results)
	}
	if results[0] != "kubernetes deployment yaml" {
		t.Errorf("expected original query, got %q", results[0])
	}
}

func TestExpandQuery_EmptyQuery(t *testing.T) {
	results := knowledge.ExpandQuery("")
	if len(results) != 1 {
		t.Errorf("expected 1 result for empty query, got %d", len(results))
	}
	if results[0] != "" {
		t.Errorf("expected empty string, got %q", results[0])
	}
}

func TestExpandQuery_MultipleSynonymsInQuery(t *testing.T) {
	// Query contains both "function" and "error" which both have synonyms.
	results := knowledge.ExpandQuery("function error handling")
	if len(results) < 3 {
		t.Fatalf("expected at least 3 results (original + function synonyms + error synonyms), got %d", len(results))
	}
}

func TestExpandQuery_ClassSynonyms(t *testing.T) {
	results := knowledge.ExpandQuery("define a class")
	if len(results) < 2 {
		t.Fatalf("expected expansions for 'class', got %d results", len(results))
	}
	// Should have expansions like "define a struct", "define a type", etc.
	foundExpansion := false
	for _, r := range results[1:] {
		if r != results[0] {
			foundExpansion = true
		}
	}
	if !foundExpansion {
		t.Error("expected at least one expansion for 'class'")
	}
}

func TestExpandQuery_TestSynonyms(t *testing.T) {
	results := knowledge.ExpandQuery("write a test")
	if len(results) < 2 {
		t.Fatalf("expected expansions for 'test', got %d results", len(results))
	}
}

func TestExpandQuery_DatabaseSynonyms(t *testing.T) {
	results := knowledge.ExpandQuery("connect to database")
	if len(results) < 2 {
		t.Fatalf("expected expansions for 'database', got %d results", len(results))
	}
}

func TestExpandQuery_APISynonyms(t *testing.T) {
	results := knowledge.ExpandQuery("create api")
	if len(results) < 2 {
		t.Fatalf("expected expansions for 'api', got %d results", len(results))
	}
}

func TestExpandQuery_ImportSynonyms(t *testing.T) {
	results := knowledge.ExpandQuery("add import")
	if len(results) < 2 {
		t.Fatalf("expected expansions for 'import', got %d results", len(results))
	}
}

func TestExpandQuery_AsyncSynonyms(t *testing.T) {
	results := knowledge.ExpandQuery("async operation")
	if len(results) < 2 {
		t.Fatalf("expected expansions for 'async', got %d results", len(results))
	}
}

func TestExpandQuery_OriginalAlwaysFirst(t *testing.T) {
	queries := []string{
		"function call",
		"class definition",
		"error handling",
		"no synonyms here xyz",
	}
	for _, q := range queries {
		results := knowledge.ExpandQuery(q)
		if results[0] != q {
			t.Errorf("ExpandQuery(%q): first element = %q, want original", q, results[0])
		}
	}
}

func TestExpandQuery_WordBoundary(t *testing.T) {
	// "classification" contains "class" but should NOT trigger synonym
	// expansion because it's not a whole word.
	results := knowledge.ExpandQuery("classification algorithm")
	if len(results) != 1 {
		t.Errorf("expected no expansion for 'classification' (not whole word 'class'), got %d: %v",
			len(results), results)
	}
}

// contains checks if substr appears in s (case-sensitive).
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
