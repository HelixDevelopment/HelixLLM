package gateway_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

// HXC-235 (§11.4.115 RED-baseline-on-the-broken-artifact + polarity switch).
//
// Defect: POST /v1/embeddings, when wired with the production-default
// fallback embedder (knowledge.HashEmbedder — see F07 / §11.4.146 /
// cmd/helixllm/main.go buildEmbedder), returns a well-formed
// api.EmbeddingResponse whose JSON is byte-for-byte indistinguishable, at
// the point of use, from a response produced by a real semantic provider.
// A caller of this HTTP endpoint (the actual production surface, not just
// a startup log a human operator might read) has no machine-readable way
// to detect the degradation.
//
// One test source, two roles via the RED_MODE polarity switch
// (RED_MODE=1 default = reproduce; RED_MODE=0 = standing GREEN guard).
func hxc235GatewayRedMode() bool {
	// DEFAULT IS THE STANDING GUARD (green), not RED — see the identical
	// note in internal/knowledge/hxc235_semantic_signal_test.go. The RED
	// assertion needs semantic_embeddings ABSENT, but the field has no
	// omitempty and so always marshals once the fix is in source; RED is
	// only meaningful against a pre-fix artifact. Defaulting to RED made a
	// bare `go test ./...` fail on a correct tree.
	return os.Getenv("RED_MODE") == "1"
}

func TestHXC235_Embeddings_SemanticSignal_AtPointOfUse(t *testing.T) {
	// Real (non-mock) HashEmbedder — the actual documented
	// HELIX_EMBEDDING_PROVIDER=local/unset production default, wired here
	// exactly as production wires it, not a test-double stand-in for the
	// fallback itself (§11.4.146 / CONST-050(A)).
	emb := knowledge.NewHashEmbedder(768)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/embeddings", gateway.HandleEmbeddings(nil, emb))

	body, _ := json.Marshal(api.EmbeddingRequest{
		Model: "text-embedding-ada-002",
		Input: "does RAG actually see semantic embeddings here?",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/embeddings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}

	var asMap map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &asMap); err != nil {
		t.Fatalf("unmarshal response to map: %v", err)
	}
	t.Logf("EmbeddingResponse JSON (RED default=%v): %s", hxc235GatewayRedMode(), w.Body.String())

	semanticVal, hasSemanticField := asMap["semantic_embeddings"]

	if hxc235GatewayRedMode() {
		if hasSemanticField {
			t.Fatalf("RED_MODE=1 (default) expected NO semantic-embeddings signal in /v1/embeddings response — the pre-fix defect must reproduce here — but found one: %s", w.Body.String())
		}
		t.Log("RED confirmed: /v1/embeddings response carries no machine-readable field distinguishing HashEmbedder (non-semantic) results from real embeddings — a caller cannot tell at the point of use.")
		return
	}

	if !hasSemanticField {
		t.Fatalf("RED_MODE=0 expected a semantic_embeddings signal in /v1/embeddings response for a HashEmbedder, found none: %s", w.Body.String())
	}
	if b, ok := semanticVal.(bool); !ok || b {
		t.Fatalf("RED_MODE=0 expected semantic_embeddings=false for a HashEmbedder, got %v: %s", semanticVal, w.Body.String())
	}
}

// TestHXC235_Embeddings_SemanticSignal_TrueForRealEmbedder is the GREEN
// positive control using the existing mockEmbedder (a non-HashEmbedder
// type), proving the signal correctly flips to true rather than being
// hardcoded false.
func TestHXC235_Embeddings_SemanticSignal_TrueForRealEmbedder(t *testing.T) {
	emb := &mockEmbedder{dim: 3, vec: []float64{0.1, 0.2, 0.3}}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/embeddings", gateway.HandleEmbeddings(nil, emb))

	body, _ := json.Marshal(api.EmbeddingRequest{
		Model: "nomic-embed-text",
		Input: "hello world",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/embeddings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var asMap map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &asMap); err != nil {
		t.Fatalf("unmarshal response to map: %v", err)
	}

	semanticVal, hasSemanticField := asMap["semantic_embeddings"]
	if hxc235GatewayRedMode() {
		if hasSemanticField {
			t.Fatalf("RED_MODE=1 (default) expected NO semantic-embeddings signal pre-fix, but found one: %s", w.Body.String())
		}
		t.Log("RED (positive-control path): confirmed no semantic_embeddings field exists pre-fix, as expected.")
		return
	}
	if !hasSemanticField {
		t.Fatalf("expected a semantic_embeddings signal in /v1/embeddings response, found none: %s", w.Body.String())
	}
	if b, ok := semanticVal.(bool); !ok || !b {
		t.Fatalf("expected semantic_embeddings=true for a non-hash embedder, got %v: %s", semanticVal, w.Body.String())
	}
	t.Logf("GREEN confirmed: real-embedder /v1/embeddings response: %s", w.Body.String())
}

// TestHXC235_Embeddings_SemanticSignal_NilEmbedder covers the zero-vector
// fallback path (embedder == nil): also non-semantic, must be signalled
// false, never silently omitted.
func TestHXC235_Embeddings_SemanticSignal_NilEmbedder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/embeddings", gateway.HandleEmbeddings(nil, nil))

	body, _ := json.Marshal(api.EmbeddingRequest{
		Model: "text-embedding-ada-002",
		Input: "hello",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/embeddings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var asMap map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &asMap); err != nil {
		t.Fatalf("unmarshal response to map: %v", err)
	}

	semanticVal, hasSemanticField := asMap["semantic_embeddings"]
	if hxc235GatewayRedMode() {
		if hasSemanticField {
			t.Fatalf("RED_MODE=1 (default) expected NO semantic-embeddings signal pre-fix, but found one: %s", w.Body.String())
		}
		return
	}
	if !hasSemanticField {
		t.Fatalf("expected a semantic_embeddings signal for the nil-embedder zero-vector path, found none: %s", w.Body.String())
	}
	if b, ok := semanticVal.(bool); !ok || b {
		t.Fatalf("expected semantic_embeddings=false for the nil-embedder zero-vector path, got %v: %s", semanticVal, w.Body.String())
	}
}
