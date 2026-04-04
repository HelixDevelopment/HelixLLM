package knowledge_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

// newOpenAIEmbeddingsServer returns an httptest.Server that mimics the
// OpenAI POST /embeddings endpoint.
func newOpenAIEmbeddingsServer(t *testing.T, dim int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/embeddings" {
			http.NotFound(w, r)
			return
		}

		var req struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			vec := make([]float32, dim)
			for j := range vec {
				vec[j] = float32(i+1) * 0.01
			}
			data[i] = map[string]any{
				"embedding": vec,
				"index":     i,
			}
		}

		resp := map[string]any{
			"data":  data,
			"model": req.Model,
			"usage": map[string]any{"prompt_tokens": 10, "total_tokens": 10},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// openAIHTTPEmbedder is a test-only Embedder that calls an OpenAI-compatible
// HTTP endpoint at baseURL, exercising the same JSON contract used by
// OpenAIEmbedder without coupling to the real OpenAI URL.
type openAIHTTPEmbedder struct {
	baseURL   string
	dimension int
}

func (e *openAIHTTPEmbedder) Dimension() int { return e.dimension }

func (e *openAIHTTPEmbedder) Embed(text string) ([]float64, error) {
	results, err := e.EmbedBatch([]string{text})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("openAIHTTPEmbedder: no embedding returned")
	}
	return results[0], nil
}

func (e *openAIHTTPEmbedder) EmbedBatch(texts []string) ([][]float64, error) {
	if len(texts) == 0 {
		return [][]float64{}, nil
	}

	reqBody, err := json.Marshal(map[string]any{
		"input": texts,
		"model": "text-embedding-3-small",
	})
	if err != nil {
		return nil, err
	}

	resp, err := http.Post( //nolint:gosec // test-only mock server
		e.baseURL+"/embeddings",
		"application/json",
		bytes.NewReader(reqBody),
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	out := make([][]float64, len(result.Data))
	for _, item := range result.Data {
		f64 := make([]float64, len(item.Embedding))
		for i, v := range item.Embedding {
			f64[i] = float64(v)
		}
		out[item.Index] = f64
	}
	return out, nil
}

// compile-time check that openAIHTTPEmbedder satisfies Embedder.
var _ knowledge.Embedder = (*openAIHTTPEmbedder)(nil)

// ---------------------------------------------------------------------------
// Tests for NewEmbedder factory
// ---------------------------------------------------------------------------

func TestNewEmbedder_HashProvider(t *testing.T) {
	for _, provider := range []string{"hash", "local", "", "unknown"} {
		e, err := knowledge.NewEmbedder(provider, "", "", 64)
		if err != nil {
			t.Fatalf("NewEmbedder(%q): unexpected error: %v", provider, err)
		}
		if e == nil {
			t.Fatalf("NewEmbedder(%q): got nil embedder", provider)
		}
		if e.Dimension() != 64 {
			t.Errorf("NewEmbedder(%q): Dimension() = %d, want 64", provider, e.Dimension())
		}
		vec, err := e.Embed("hello world")
		if err != nil {
			t.Fatalf("NewEmbedder(%q).Embed: %v", provider, err)
		}
		if len(vec) != 64 {
			t.Errorf("NewEmbedder(%q): embed len = %d, want 64", provider, len(vec))
		}
	}
}

func TestNewEmbedder_OpenAIProvider_EmptyKey(t *testing.T) {
	_, err := knowledge.NewEmbedder("openai", "", "text-embedding-3-small", 1536)
	if err == nil {
		t.Fatal("expected error for empty API key, got nil")
	}
}

func TestNewEmbedder_OpenAIProvider_ReturnsEmbedder(t *testing.T) {
	e, err := knowledge.NewEmbedder("openai", "fake-key", "text-embedding-3-small", 1536)
	if err != nil {
		t.Fatalf("NewEmbedder(openai): %v", err)
	}
	if e == nil {
		t.Fatal("NewEmbedder(openai): got nil")
	}
	if e.Dimension() != 1536 {
		t.Errorf("Dimension() = %d, want 1536", e.Dimension())
	}
}

// ---------------------------------------------------------------------------
// Tests for the HTTP path via openAIHTTPEmbedder (mocked server)
// ---------------------------------------------------------------------------

func TestOpenAIEmbedder_Embed_MockServer(t *testing.T) {
	const dim = 16
	srv := newOpenAIEmbeddingsServer(t, dim)
	defer srv.Close()

	embedder := &openAIHTTPEmbedder{baseURL: srv.URL, dimension: dim}

	vec, err := embedder.Embed("test text")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != dim {
		t.Errorf("Embed: len = %d, want %d", len(vec), dim)
	}
}

func TestOpenAIEmbedder_EmbedBatch_MockServer(t *testing.T) {
	const dim = 16
	srv := newOpenAIEmbeddingsServer(t, dim)
	defer srv.Close()

	embedder := &openAIHTTPEmbedder{baseURL: srv.URL, dimension: dim}

	texts := []string{"alpha", "beta", "gamma"}
	results, err := embedder.EmbedBatch(texts)
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(results) != len(texts) {
		t.Fatalf("EmbedBatch: got %d results, want %d", len(results), len(texts))
	}
	for i, vec := range results {
		if len(vec) != dim {
			t.Errorf("result[%d]: len = %d, want %d", i, len(vec), dim)
		}
	}
}

func TestOpenAIEmbedder_EmbedBatch_Empty(t *testing.T) {
	const dim = 16
	srv := newOpenAIEmbeddingsServer(t, dim)
	defer srv.Close()

	embedder := &openAIHTTPEmbedder{baseURL: srv.URL, dimension: dim}

	results, err := embedder.EmbedBatch([]string{})
	if err != nil {
		t.Fatalf("EmbedBatch(empty): %v", err)
	}
	if len(results) != 0 {
		t.Errorf("EmbedBatch(empty): got %d results, want 0", len(results))
	}
}

func TestOpenAIEmbedder_Embed_ServerError(t *testing.T) {
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer errSrv.Close()

	embedder := &openAIHTTPEmbedder{baseURL: errSrv.URL, dimension: 16}
	_, err := embedder.Embed("trigger error")
	if err == nil {
		t.Fatal("expected error from server 500, got nil")
	}
}
