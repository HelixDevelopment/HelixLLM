package knowledge

// Hermetic tests for TEIReranker. No live TEI server required — an
// httptest.Server stands in for HuggingFace Text-Embeddings-Inference's
// /rerank endpoint (https://github.com/huggingface/text-embeddings-inference)
// and returns canned responses exercising each code path. Live-infra
// coverage (a real BAAI/bge-reranker-base TEI container) lives in
// tests/integration/rag_rerank_pipeline_live_test.go, gated behind an
// explicit opt-in env var per §11.4.98.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func makeTEIChunk(id, content string, score float64) ScoredChunk {
	return ScoredChunk{
		Chunk: Chunk{ID: id, DocumentID: "doc-" + id, Content: content},
		Score: score,
	}
}

// fakeTEIServer stands in for a TEI /rerank endpoint, returning hits built
// by the caller-supplied hitsFor function from the decoded request.
func fakeTEIServer(t *testing.T, hitsFor func(req teiRerankRequest) []teiRerankHit) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rerank" {
			http.NotFound(w, r)
			return
		}
		var req teiRerankRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		hits := hitsFor(req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(hits)
	}))
}

func TestTEIReranker_ReordersByRealScores(t *testing.T) {
	// The cross-encoder scores index 1 ("the fact-bearing text") highest
	// even though it arrived last in raw retrieval order — this is the
	// exact shape of the bi-encoder-wrong / cross-encoder-fixes-it case
	// this reranker exists to serve.
	srv := fakeTEIServer(t, func(req teiRerankRequest) []teiRerankHit {
		return []teiRerankHit{
			{Index: 1, Score: 0.97},
			{Index: 0, Score: 0.42},
			{Index: 2, Score: 0.10},
		}
	})
	defer srv.Close()

	reranker := NewTEIReranker(srv.URL)
	chunks := []ScoredChunk{
		makeTEIChunk("distractor", "lexically similar but wrong entity", 0.91),
		makeTEIChunk("fact", "the fact-bearing text", 0.55),
		makeTEIChunk("irrelevant", "totally unrelated content", 0.20),
	}

	result, err := reranker.Rerank("query about the fact", chunks, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}
	if result[0].ID != "fact" {
		t.Errorf("expected 'fact' promoted to rank-1 by the cross-encoder score, got %q", result[0].ID)
	}
	if result[0].Score != 0.97 {
		t.Errorf("expected result[0].Score to be the TEI cross-encoder score 0.97, got %v", result[0].Score)
	}
	if result[1].ID != "distractor" || result[2].ID != "irrelevant" {
		t.Errorf("unexpected order: %q, %q", result[1].ID, result[2].ID)
	}
}

func TestTEIReranker_TopKTrimming(t *testing.T) {
	srv := fakeTEIServer(t, func(req teiRerankRequest) []teiRerankHit {
		hits := make([]teiRerankHit, len(req.Texts))
		for i := range req.Texts {
			hits[i] = teiRerankHit{Index: i, Score: float64(len(req.Texts) - i)}
		}
		return hits
	})
	defer srv.Close()

	reranker := NewTEIReranker(srv.URL)
	chunks := []ScoredChunk{
		makeTEIChunk("a", "a", 0), makeTEIChunk("b", "b", 0),
		makeTEIChunk("c", "c", 0), makeTEIChunk("d", "d", 0),
	}
	result, err := reranker.Rerank("q", chunks, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected topK=2 results, got %d", len(result))
	}
}

func TestTEIReranker_EmptyInput_NoOp(t *testing.T) {
	reranker := NewTEIReranker("http://unreachable.invalid:1")
	result, err := reranker.Rerank("q", nil, 5)
	if err != nil {
		t.Fatalf("unexpected error on empty input: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 results for empty input, got %d", len(result))
	}
}

func TestTEIReranker_NonOKStatus_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	reranker := NewTEIReranker(srv.URL)
	_, err := reranker.Rerank("q", []ScoredChunk{makeTEIChunk("a", "a", 0)}, 5)
	if err == nil {
		t.Fatal("expected error for non-200 TEI response, got nil")
	}
}

func TestTEIReranker_MalformedResponse_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	reranker := NewTEIReranker(srv.URL)
	_, err := reranker.Rerank("q", []ScoredChunk{makeTEIChunk("a", "a", 0)}, 5)
	if err == nil {
		t.Fatal("expected error for malformed TEI response, got nil")
	}
}

func TestTEIReranker_EmptyHitsArray_ReturnsError(t *testing.T) {
	srv := fakeTEIServer(t, func(req teiRerankRequest) []teiRerankHit { return nil })
	defer srv.Close()

	reranker := NewTEIReranker(srv.URL)
	_, err := reranker.Rerank("q", []ScoredChunk{makeTEIChunk("a", "a", 0)}, 5)
	if err == nil {
		t.Fatal("expected error for empty TEI hits array, got nil")
	}
}

func TestTEIReranker_OutOfRangeIndex_ReturnsError(t *testing.T) {
	// §1.1 load-bearing mutation target: a TEI response index pointing past
	// the request's Texts slice MUST be rejected, never silently truncated
	// or wrapped, so a protocol mismatch surfaces loudly instead of quietly
	// mis-attributing a score to the wrong chunk.
	srv := fakeTEIServer(t, func(req teiRerankRequest) []teiRerankHit {
		return []teiRerankHit{{Index: 99, Score: 1.0}}
	})
	defer srv.Close()

	reranker := NewTEIReranker(srv.URL)
	_, err := reranker.Rerank("q", []ScoredChunk{makeTEIChunk("a", "a", 0)}, 5)
	if err == nil {
		t.Fatal("expected error for out-of-range TEI response index, got nil")
	}
}

func TestTEIReranker_RequestShape_MatchesTEIProtocol(t *testing.T) {
	var captured teiRerankRequest
	srv := fakeTEIServer(t, func(req teiRerankRequest) []teiRerankHit {
		captured = req
		return []teiRerankHit{{Index: 0, Score: 1.0}, {Index: 1, Score: 0.5}}
	})
	defer srv.Close()

	reranker := NewTEIReranker(srv.URL)
	_, err := reranker.Rerank("what is the capital of France", []ScoredChunk{
		makeTEIChunk("a", "Paris is the capital of France", 0),
		makeTEIChunk("b", "Berlin is the capital of Germany", 0),
	}, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured.Query != "what is the capital of France" {
		t.Errorf("captured.Query = %q, want %q", captured.Query, "what is the capital of France")
	}
	if len(captured.Texts) != 2 || captured.Texts[0] != "Paris is the capital of France" {
		t.Errorf("captured.Texts = %v, unexpected shape", captured.Texts)
	}
}
