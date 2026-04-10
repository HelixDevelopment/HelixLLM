package knowledge_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

// newLlamaEmbeddingsServer returns an httptest.Server that mimics the
// llama-server POST /v1/embeddings endpoint. It records how many times it was
// called so batch tests can verify per-text sequencing.
func newLlamaEmbeddingsServer(t *testing.T, dim int, callCount *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/embeddings" {
			http.NotFound(w, r)
			return
		}

		if callCount != nil {
			atomic.AddInt32(callCount, 1)
		}

		var req api.EmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Build a deterministic 768-dim vector filled with 0.1 * (index+1).
		vec := make([]float64, dim)
		for j := range vec {
			vec[j] = float64(j+1) * 0.001
		}

		resp := api.EmbeddingResponse{
			Object: "list",
			Data: []api.EmbeddingData{
				{Object: "embedding", Embedding: vec, Index: 0},
			},
			Model: "nomic-embed-text",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestLlamaEmbedder_Embed_Success(t *testing.T) {
	const dim = 768
	srv := newLlamaEmbeddingsServer(t, dim, nil)
	defer srv.Close()

	e := knowledge.NewLlamaEmbedder(srv.URL, "nomic-embed-text", dim)

	vec, err := e.Embed("hello llama")
	if err != nil {
		t.Fatalf("Embed: unexpected error: %v", err)
	}
	if len(vec) != dim {
		t.Errorf("Embed: len = %d, want %d", len(vec), dim)
	}
	// The mock server fills vec[j] = (j+1)*0.001; spot-check first value.
	const want0 = 0.001
	if vec[0] != want0 {
		t.Errorf("Embed: vec[0] = %f, want %f", vec[0], want0)
	}
}

func TestLlamaEmbedder_Dimension(t *testing.T) {
	const dim = 768
	e := knowledge.NewLlamaEmbedder("http://127.0.0.1:8080", "nomic-embed-text", dim)
	if e.Dimension() != dim {
		t.Errorf("Dimension() = %d, want %d", e.Dimension(), dim)
	}
}

func TestLlamaEmbedder_EmbedBatch(t *testing.T) {
	const dim = 768
	var callCount int32
	srv := newLlamaEmbeddingsServer(t, dim, &callCount)
	defer srv.Close()

	e := knowledge.NewLlamaEmbedder(srv.URL, "nomic-embed-text", dim)

	texts := []string{"first", "second", "third"}
	results, err := e.EmbedBatch(texts)
	if err != nil {
		t.Fatalf("EmbedBatch: unexpected error: %v", err)
	}
	if len(results) != len(texts) {
		t.Fatalf("EmbedBatch: got %d results, want %d", len(results), len(texts))
	}
	for i, vec := range results {
		if len(vec) != dim {
			t.Errorf("result[%d]: len = %d, want %d", i, len(vec), dim)
		}
	}
	// Sequential implementation must call the server once per text.
	if int(callCount) != len(texts) {
		t.Errorf("server called %d times, want %d (one per text)", callCount, len(texts))
	}
}

func TestLlamaEmbedder_ServerDown(t *testing.T) {
	// Port 1 is reserved and always unreachable — connection will be refused.
	e := knowledge.NewLlamaEmbedder("http://127.0.0.1:1", "nomic-embed-text", 768)
	_, err := e.Embed("trigger connection error")
	if err == nil {
		t.Fatal("Embed: expected error when server is down, got nil")
	}
}
