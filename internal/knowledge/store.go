package knowledge

import (
	"fmt"
	"math"
	"sort"
	"sync"
)

// VectorStore is the interface for storing and searching embedded chunks.
type VectorStore interface {
	Upsert(collection string, chunks []Chunk) error
	Search(collection string, vector []float64, topK int) ([]ScoredChunk, error)
	Delete(collection string, ids []string) error
	Collections() ([]Collection, error)
	DeleteCollection(name string) error
	Stats() (*Stats, error)
}

// MemoryStore is an in-memory VectorStore backed by a nested map.
// Concurrency is guarded with a read-write mutex.
type MemoryStore struct {
	mu          sync.RWMutex
	collections map[string]map[string]Chunk // collection -> id -> chunk
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		collections: make(map[string]map[string]Chunk),
	}
}

// Upsert inserts or overwrites chunks in the named collection.
func (m *MemoryStore) Upsert(collection string, chunks []Chunk) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.collections[collection]; !ok {
		m.collections[collection] = make(map[string]Chunk)
	}
	for _, c := range chunks {
		m.collections[collection][c.ID] = c
	}
	return nil
}

// Search finds the topK chunks in collection most similar to vector,
// ranked by cosine similarity (highest first).
// Returns an empty slice when the collection does not exist.
func (m *MemoryStore) Search(collection string, vector []float64, topK int) ([]ScoredChunk, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	col, ok := m.collections[collection]
	if !ok || len(col) == 0 {
		return []ScoredChunk{}, nil
	}

	scored := make([]ScoredChunk, 0, len(col))
	for _, chunk := range col {
		score := cosineSimilarity(vector, chunk.Embedding)
		scored = append(scored, ScoredChunk{Chunk: chunk, Score: score})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	if topK > 0 && topK < len(scored) {
		scored = scored[:topK]
	}
	return scored, nil
}

// Delete removes the chunks with the given IDs from collection.
// Non-existent IDs are silently ignored.
func (m *MemoryStore) Delete(collection string, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	col, ok := m.collections[collection]
	if !ok {
		return nil
	}
	for _, id := range ids {
		delete(col, id)
	}
	return nil
}

// Collections returns a summary of every collection currently held.
func (m *MemoryStore) Collections() ([]Collection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]Collection, 0, len(m.collections))
	for name, col := range m.collections {
		dim := 0
		docSet := make(map[string]struct{})
		for _, chunk := range col {
			docSet[chunk.DocumentID] = struct{}{}
			if dim == 0 {
				dim = len(chunk.Embedding)
			}
		}
		result = append(result, Collection{
			Name:      name,
			Documents: len(docSet),
			Chunks:    len(col),
			Dimension: dim,
		})
	}
	return result, nil
}

// DeleteCollection removes an entire collection and all its chunks.
// Returns an error if the collection does not exist.
func (m *MemoryStore) DeleteCollection(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.collections[name]; !ok {
		return fmt.Errorf("collection %q not found", name)
	}
	delete(m.collections, name)
	return nil
}

// Stats returns aggregate counts across all collections.
func (m *MemoryStore) Stats() (*Stats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s := &Stats{}
	for name, col := range m.collections {
		dim := 0
		docSet := make(map[string]struct{})
		for _, chunk := range col {
			docSet[chunk.DocumentID] = struct{}{}
			if dim == 0 {
				dim = len(chunk.Embedding)
			}
		}
		s.Collections = append(s.Collections, Collection{
			Name:      name,
			Documents: len(docSet),
			Chunks:    len(col),
			Dimension: dim,
		})
		s.TotalDocs += len(docSet)
		s.TotalChunks += len(col)
	}
	return s, nil
}

// cosineSimilarity returns the cosine similarity between two equal-length vectors.
// Returns 0 if either vector has zero magnitude.
func cosineSimilarity(a, b []float64) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
