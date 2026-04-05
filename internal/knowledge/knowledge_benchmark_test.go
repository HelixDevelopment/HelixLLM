package knowledge_test

import (
	"fmt"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func BenchmarkMemoryStore_Upsert(b *testing.B) {
	store := knowledge.NewMemoryStore()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chunk := knowledge.Chunk{
			ID:         fmt.Sprintf("bench-chunk-%d", i),
			DocumentID: "bench-doc",
			Content:    "benchmark document content for testing vector store performance",
			Embedding:  make([]float64, 384),
		}
		_ = store.Upsert("bench", []knowledge.Chunk{chunk})
	}
}

func BenchmarkMemoryStore_Search(b *testing.B) {
	store := knowledge.NewMemoryStore()

	// Pre-populate with 1000 chunks.
	for i := 0; i < 1000; i++ {
		emb := make([]float64, 384)
		emb[i%384] = 1.0
		_ = store.Upsert("bench", []knowledge.Chunk{{
			ID:         fmt.Sprintf("doc-%d", i),
			DocumentID: fmt.Sprintf("parent-%d", i),
			Content:    fmt.Sprintf("document %d content", i),
			Embedding:  emb,
		}})
	}

	query := make([]float64, 384)
	query[0] = 1.0

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.Search("bench", query, 10)
	}
}
