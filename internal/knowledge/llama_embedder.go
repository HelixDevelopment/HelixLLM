package knowledge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

// LlamaEmbedder calls a running llama-server /v1/embeddings endpoint to
// produce dense embeddings locally (e.g. nomic-embed-text-v1.5 via llama.cpp).
type LlamaEmbedder struct {
	baseURL   string
	model     string
	dimension int
	client    *http.Client
}

// NewLlamaEmbedder creates a LlamaEmbedder that targets the llama-server at
// baseURL (e.g. "http://127.0.0.1:8080") using the given model name.
// A 30-second HTTP timeout is applied to every request.
func NewLlamaEmbedder(baseURL, model string, dimension int) *LlamaEmbedder {
	return &LlamaEmbedder{
		baseURL:   baseURL,
		model:     model,
		dimension: dimension,
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Dimension returns the number of dimensions produced by this embedder.
func (e *LlamaEmbedder) Dimension() int {
	return e.dimension
}

// Embed generates a single embedding vector for the given text by calling
// POST {baseURL}/v1/embeddings and returning the first embedding in the
// response.
func (e *LlamaEmbedder) Embed(text string) ([]float64, error) {
	reqBody, err := json.Marshal(api.EmbeddingRequest{
		Model: e.model,
		Input: text,
	})
	if err != nil {
		return nil, fmt.Errorf("llama embedder: marshal request: %w", err)
	}

	resp, err := e.client.Post( //nolint:gosec // URL is caller-supplied configuration
		e.baseURL+"/v1/embeddings",
		"application/json",
		bytes.NewReader(reqBody),
	)
	if err != nil {
		return nil, fmt.Errorf("llama embedder: http post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("llama embedder: server returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var embResp api.EmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
		return nil, fmt.Errorf("llama embedder: decode response: %w", err)
	}

	if len(embResp.Data) == 0 {
		return nil, fmt.Errorf("llama embedder: no embedding data in response")
	}

	return embResp.Data[0].Embedding, nil
}

// EmbedBatch generates embeddings for multiple texts by calling Embed
// sequentially for each text.
func (e *LlamaEmbedder) EmbedBatch(texts []string) ([][]float64, error) {
	results := make([][]float64, len(texts))
	for i, text := range texts {
		vec, err := e.Embed(text)
		if err != nil {
			return nil, fmt.Errorf("llama embedder: batch item %d: %w", i, err)
		}
		results[i] = vec
	}
	return results, nil
}

// Compile-time interface check.
var _ Embedder = (*LlamaEmbedder)(nil)
