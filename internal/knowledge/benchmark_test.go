package knowledge

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkBM25Search(b *testing.B) {
	idx := NewBM25Index()
	// Add 100 documents.
	for i := 0; i < 100; i++ {
		idx.Add(fmt.Sprintf("doc-%d", i), fmt.Sprintf("function definition for handler number %d with error handling", i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Search("error handling function", 5)
	}
}

func BenchmarkQueryExpansion(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ExpandQuery("find the async function that handles database errors")
	}
}

func BenchmarkCodeAwareChunker(b *testing.B) {
	chunker := NewCodeAwareChunker(512, 128)
	doc := Document{
		ID:      "test",
		Content: strings.Repeat("func example() {\n    // code\n}\n\n", 50),
		Source:  "test.go",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = chunker.Chunk(doc)
	}
}
