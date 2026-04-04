package knowledge

import (
	"context"
	"fmt"

	"digital.vasic.embeddings/pkg/openai"
)

// OpenAIEmbedder wraps digital.vasic.embeddings' OpenAI provider to satisfy
// the HelixLLM Embedder interface.
//
// The upstream provider returns []float32; this adapter converts to []float64
// to match the Embedder contract used throughout the knowledge package.
type OpenAIEmbedder struct {
	provider  *openai.Client
	dimension int
}

// NewOpenAIEmbedder creates an OpenAIEmbedder using the given API key and
// model name (e.g. "text-embedding-3-small").
func NewOpenAIEmbedder(apiKey, model string) (*OpenAIEmbedder, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("openai embedder: apiKey must not be empty")
	}
	cfg := openai.Config{
		APIKey: apiKey,
		Model:  model,
	}
	c := openai.NewClient(cfg)
	return &OpenAIEmbedder{
		provider:  c,
		dimension: c.Dimensions(),
	}, nil
}

// Dimension returns the number of dimensions produced by this embedder.
func (o *OpenAIEmbedder) Dimension() int {
	return o.dimension
}

// Embed generates a single embedding vector for text.
func (o *OpenAIEmbedder) Embed(text string) ([]float64, error) {
	f32, err := o.provider.Embed(context.Background(), text)
	if err != nil {
		return nil, fmt.Errorf("openai embed: %w", err)
	}
	return float32SliceToFloat64(f32), nil
}

// EmbedBatch generates embeddings for multiple texts in one API call.
func (o *OpenAIEmbedder) EmbedBatch(texts []string) ([][]float64, error) {
	if len(texts) == 0 {
		return [][]float64{}, nil
	}
	batches, err := o.provider.EmbedBatch(context.Background(), texts)
	if err != nil {
		return nil, fmt.Errorf("openai embed batch: %w", err)
	}
	result := make([][]float64, len(batches))
	for i, f32 := range batches {
		result[i] = float32SliceToFloat64(f32)
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Embedder factory
// ---------------------------------------------------------------------------

// NewEmbedder constructs an Embedder by provider name.
//
// Supported providers:
//   - "openai"        → OpenAIEmbedder (requires apiKey)
//   - "hash", "local" → HashEmbedder (deterministic, no external API)
//
// Any unrecognised provider name falls back to HashEmbedder.
func NewEmbedder(provider, apiKey, model string, dimension int) (Embedder, error) {
	switch provider {
	case "openai":
		return NewOpenAIEmbedder(apiKey, model)
	case "hash", "local", "":
		return NewHashEmbedder(dimension), nil
	default:
		return NewHashEmbedder(dimension), nil
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// float32SliceToFloat64 converts []float32 → []float64.
func float32SliceToFloat64(in []float32) []float64 {
	out := make([]float64, len(in))
	for i, v := range in {
		out[i] = float64(v)
	}
	return out
}

// Compile-time interface check.
var _ Embedder = (*OpenAIEmbedder)(nil)
