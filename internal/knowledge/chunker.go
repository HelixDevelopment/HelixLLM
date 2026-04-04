package knowledge

import (
	"fmt"
)

// Chunker splits a Document into a slice of Chunks.
type Chunker interface {
	Chunk(doc Document) ([]Chunk, error)
}

// FixedSizeChunker splits document content into fixed-size character windows
// with a configurable overlap between consecutive chunks.
type FixedSizeChunker struct {
	ChunkSize int
	Overlap   int
}

// NewFixedSizeChunker returns a FixedSizeChunker with the given window size
// and overlap. If size < 1 it is clamped to 1; overlap is clamped to [0, size-1].
func NewFixedSizeChunker(size, overlap int) *FixedSizeChunker {
	if size < 1 {
		size = 1
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= size {
		overlap = size - 1
	}
	return &FixedSizeChunker{ChunkSize: size, Overlap: overlap}
}

// Chunk splits doc.Content into overlapping windows of ChunkSize runes.
// Each chunk receives:
//   - ID formatted as "{docID}-{position}"
//   - Position field set to its 0-based index
//   - A copy of doc.Metadata
//   - DocumentID set to doc.ID
//
// An empty document (no content) returns an error.
func (f *FixedSizeChunker) Chunk(doc Document) ([]Chunk, error) {
	runes := []rune(doc.Content)
	if len(runes) == 0 {
		return nil, fmt.Errorf("document %q has no content", doc.ID)
	}

	step := f.ChunkSize - f.Overlap
	if step < 1 {
		step = 1
	}

	var chunks []Chunk
	position := 0

	for start := 0; start < len(runes); start += step {
		end := start + f.ChunkSize
		if end > len(runes) {
			end = len(runes)
		}

		meta := copyMeta(doc.Metadata)
		chunks = append(chunks, Chunk{
			ID:         fmt.Sprintf("%s-%d", doc.ID, position),
			DocumentID: doc.ID,
			Content:    string(runes[start:end]),
			Position:   position,
			Metadata:   meta,
		})
		position++

		// If this window already reached the end there is nothing more to emit.
		if end == len(runes) {
			break
		}
	}

	return chunks, nil
}

// copyMeta returns a shallow copy of a metadata map (nil-safe).
func copyMeta(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
