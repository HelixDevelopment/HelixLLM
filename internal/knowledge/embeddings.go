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

// IsSemanticEmbedder reports whether e produces real, semantically
// meaningful embeddings (true) as opposed to the deterministic,
// non-semantic HashEmbedder fallback (false). A nil Embedder is reported
// as non-semantic — a caller degraded all the way to a zero-vector
// fallback is exactly as unable to do meaningful similarity search as one
// backed by HashEmbedder.
//
// HXC-235: HELIX_EMBEDDING_PROVIDER unset / "local" / unrecognised / on
// construction error all resolve to HashEmbedder (buildEmbedder in
// cmd/helixllm/main.go, F07 / §11.4.146). F07 made that fact observable
// at STARTUP via a WARN log line; this function is the single point of
// truth every RESPONSE-level caller (the /v1/embeddings and
// /internal/knowledge/query handlers) uses to surface the SAME fact at
// the point of use, so a programmatic caller — which never sees process
// stdout/stderr — can tell hash-fallback results from real ones without
// depending on log output.
func IsSemanticEmbedder(e Embedder) bool {
	if e == nil {
		return false
	}
	_, isHash := e.(*HashEmbedder)
	return !isHash
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
