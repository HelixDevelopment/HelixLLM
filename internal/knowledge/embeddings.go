package knowledge

import (
	"crypto/sha256"
	"fmt"
	"math"
)

// Embedder converts text into a fixed-dimension float64 vector.
type Embedder interface {
	Embed(text string) ([]float64, error)
	EmbedBatch(texts []string) ([][]float64, error)
	Dimension() int
}

// HashEmbedder produces deterministic unit-length vectors by hashing
// the input with SHA-256 and distributing the hash bytes across the
// requested number of dimensions.
type HashEmbedder struct {
	dimension int
}

// NewHashEmbedder returns a HashEmbedder with the given vector dimension.
// dimension must be >= 1.
func NewHashEmbedder(dimension int) *HashEmbedder {
	if dimension < 1 {
		dimension = 1
	}
	return &HashEmbedder{dimension: dimension}
}

// Dimension returns the number of dimensions produced by this embedder.
func (h *HashEmbedder) Dimension() int {
	return h.dimension
}

// Embed hashes text with SHA-256 and maps the bytes into a unit-length
// float64 vector of length h.dimension.
func (h *HashEmbedder) Embed(text string) ([]float64, error) {
	vec := make([]float64, h.dimension)

	// Seed with successive SHA-256 hashes so we can fill dimensions > 32.
	seed := text
	bytePos := 0
	var hashBytes []byte

	for i := 0; i < h.dimension; i++ {
		if bytePos >= len(hashBytes) {
			sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", seed, bytePos/32)))
			hashBytes = append(hashBytes, sum[:]...)
		}
		// Map byte [0,255] to [-1, 1].
		vec[i] = (float64(hashBytes[bytePos]) / 127.5) - 1.0
		bytePos++
	}

	// Normalize to unit length.
	var norm float64
	for _, v := range vec {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return vec, nil
	}
	for i := range vec {
		vec[i] /= norm
	}

	return vec, nil
}

// EmbedBatch embeds each text in the slice sequentially.
func (h *HashEmbedder) EmbedBatch(texts []string) ([][]float64, error) {
	results := make([][]float64, len(texts))
	for i, text := range texts {
		vec, err := h.Embed(text)
		if err != nil {
			return nil, err
		}
		results[i] = vec
	}
	return results, nil
}
