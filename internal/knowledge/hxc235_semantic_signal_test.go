package knowledge_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

// HXC-235 (§11.4.115 RED-baseline-on-the-broken-artifact + polarity switch,
// §11.4.135 standing regression guard).
//
// Defect: HELIX_EMBEDDING_PROVIDER unset / "local" / unrecognised / on
// construction error all silently resolve to knowledge.HashEmbedder (see
// cmd/helixllm/main.go buildEmbedder, F07 / §11.4.146) — a deterministic
// but NON-SEMANTIC embedder (SHA-256 bytes mapped into a unit-length
// vector; no semantic-similarity relationship between texts whatsoever).
// F07 already made this observable at STARTUP (a WARN log line), but a
// caller driving the RAG query API (/internal/knowledge/query, backed by
// Pipeline.Query -> QueryResult, this test) never sees process
// stdout/stderr — before this fix the JSON payload returned to that
// caller carried NO field distinguishing "real semantic embeddings" from
// "hash fallback", so a downstream RAG consumer received confident-looking
// but semantically-meaningless retrieval results with no way to tell.
//
// hxc235RedMode reports whether the test is in RED (defect-reproduction,
// default) or GREEN (post-fix regression-guard) mode. RED_MODE=0 flips to
// GREEN. One test source, two roles (§11.4.115).
func hxc235RedMode() bool {
	// DEFAULT IS THE STANDING GUARD (green), not RED. Flipped from
	// `!= "0"` (HXC-235 completion): the RED assertion here requires the
	// semantic_embeddings field to be ABSENT from the marshalled JSON, but
	// the field is a plain bool with no omitempty, so Go marshals it
	// unconditionally the moment the fix exists in source. RED is therefore
	// unsatisfiable on a fixed artifact by construction — it can only hold
	// against a PRE-FIX tree (the HXC-215 pattern: `git archive` the parent
	// commit, build that, run RED against it).
	//
	// With the old default a bare `go test ./...` failed on a correctly
	// fixed tree, which trains everyone to ignore a red suite. Explicit
	// RED_MODE=1 still reproduces the defect for anyone running it against
	// the pre-fix artifact where it is meaningful.
	return os.Getenv("RED_MODE") == "1"
}

// fakeSemanticEmbedder is a minimal non-hash Embedder standing in for a
// real semantic provider (openai/llama) without performing network I/O —
// permissible per CONST-050(A) / §11.4.27(A), this file is a *_test.go
// unit test. It exists purely to exercise the "true" branch of the
// semantic-signal contract; its vectors carry no semantic meaning either,
// but its CONCRETE TYPE is what knowledge.IsSemanticEmbedder discriminates
// on (mirroring how the real OpenAIEmbedder / LlamaEmbedder types work).
type fakeSemanticEmbedder struct{ dim int }

func (f *fakeSemanticEmbedder) Embed(text string) ([]float64, error) {
	vec := make([]float64, f.dim)
	for i := range vec {
		vec[i] = float64(len(text)%7) / 7.0
	}
	return vec, nil
}

func (f *fakeSemanticEmbedder) EmbedBatch(texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i, t := range texts {
		v, err := f.Embed(t)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func (f *fakeSemanticEmbedder) Dimension() int { return f.dim }

// TestHXC235_QueryResult_SemanticSignal_AtPointOfUse reproduces (RED) /
// guards against (GREEN) the defect for the HashEmbedder path — the
// documented HELIX_EMBEDDING_PROVIDER=local/unset production default,
// wired here exactly as production wires it (cmd/helixllm/main.go
// buildEmbedder) — no mock, no test double standing in for the fallback
// itself (§11.4.146 / CONST-050(A)).
func TestHXC235_QueryResult_SemanticSignal_AtPointOfUse(t *testing.T) {
	p := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          knowledge.NewHashEmbedder(768),
		Store:             knowledge.NewMemoryStore(),
		Chunker:           knowledge.NewFixedSizeChunker(100, 10),
		DefaultCollection: "default",
		DefaultTopK:       5,
	})
	ctx := context.Background()

	if _, err := p.Ingest(ctx, knowledge.IngestRequest{
		Title:      "doc",
		Content:    "The quick brown fox jumps over the lazy dog. Repeated content so chunking produces real chunks for retrieval.",
		Collection: "hxc235",
	}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	result, err := p.Query(ctx, knowledge.QueryRequest{
		Query:      "quick fox",
		Collection: "hxc235",
		TopK:       5,
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal QueryResult: %v", err)
	}
	var asMap map[string]interface{}
	if err := json.Unmarshal(raw, &asMap); err != nil {
		t.Fatalf("unmarshal QueryResult JSON to map: %v", err)
	}
	t.Logf("QueryResult JSON (RED_MODE default=1, actual RED=%v): %s", hxc235RedMode(), raw)

	semanticVal, hasSemanticField := asMap["semantic_embeddings"]

	if hxc235RedMode() {
		// RED: reproduce the defect on the current (pre-fix) artifact — a
		// caller reading this exact JSON payload CANNOT tell these are
		// hash-fallback, non-semantic embeddings.
		if hasSemanticField {
			t.Fatalf("RED_MODE=1 (default) expected NO semantic-embeddings signal in QueryResult JSON — the pre-fix defect must reproduce here — but found one: %s", raw)
		}
		t.Log("RED confirmed: QueryResult JSON carries no machine-readable field distinguishing HashEmbedder (non-semantic) results from real embeddings at the point of use.")
		return
	}

	// GREEN: post-fix — the caller CAN tell, and correctly sees "false"
	// for a HashEmbedder-backed pipeline.
	if !hasSemanticField {
		t.Fatalf("RED_MODE=0 expected a semantic_embeddings signal in QueryResult JSON for a HashEmbedder-backed pipeline, found none: %s", raw)
	}
	if b, ok := semanticVal.(bool); !ok || b {
		t.Fatalf("RED_MODE=0 expected semantic_embeddings=false for a HashEmbedder-backed pipeline, got %v: %s", semanticVal, raw)
	}
}

// TestHXC235_QueryResult_SemanticSignal_TrueForRealEmbedder is the GREEN
// positive control: a genuinely non-hash Embedder must be signalled TRUE,
// proving the signal is not merely hardcoded false.
func TestHXC235_QueryResult_SemanticSignal_TrueForRealEmbedder(t *testing.T) {
	p := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          &fakeSemanticEmbedder{dim: 8},
		Store:             knowledge.NewMemoryStore(),
		Chunker:           knowledge.NewFixedSizeChunker(100, 10),
		DefaultCollection: "default",
		DefaultTopK:       5,
	})
	ctx := context.Background()

	if _, err := p.Ingest(ctx, knowledge.IngestRequest{
		Title:      "doc",
		Content:    "Real semantic embedder path, exercised for the positive control.",
		Collection: "hxc235-real",
	}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	result, err := p.Query(ctx, knowledge.QueryRequest{
		Query:      "semantic embedder",
		Collection: "hxc235-real",
		TopK:       5,
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal QueryResult: %v", err)
	}
	var asMap map[string]interface{}
	if err := json.Unmarshal(raw, &asMap); err != nil {
		t.Fatalf("unmarshal QueryResult JSON to map: %v", err)
	}

	semanticVal, hasSemanticField := asMap["semantic_embeddings"]
	if hxc235RedMode() {
		// Pre-fix: no field exists at all yet — this positive control has
		// nothing to assert against until the fix lands.
		if hasSemanticField {
			t.Fatalf("RED_MODE=1 (default) expected NO semantic-embeddings signal pre-fix, but found one: %s", raw)
		}
		t.Log("RED (positive-control path): confirmed no semantic_embeddings field exists pre-fix, as expected.")
		return
	}
	if !hasSemanticField {
		t.Fatalf("expected a semantic_embeddings signal in QueryResult JSON, found none: %s", raw)
	}
	if b, ok := semanticVal.(bool); !ok || !b {
		t.Fatalf("expected semantic_embeddings=true for a non-hash embedder, got %v: %s", semanticVal, raw)
	}
	t.Logf("GREEN confirmed: real-embedder QueryResult JSON: %s", raw)
}
