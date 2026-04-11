package knowledge

// Hermetic tests for LLMReranker. No live LLM server required — an
// httptest.Server stands in for the OpenAI-compatible chat completion
// endpoint and returns canned responses exercising each code path.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func makeLLMChunk(id, content string, score float64) ScoredChunk {
	return ScoredChunk{
		Chunk: Chunk{
			ID:         id,
			DocumentID: "doc-" + id,
			Content:    content,
			Position:   0,
		},
		Score: score,
	}
}

// fakeLLMServer spins up an httptest server that responds to
// /v1/chat/completions with a caller-supplied score function mapping the
// prompt body to the numeric answer.
func fakeLLMServer(t *testing.T, scoreFor func(query, chunk string) string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var req chatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var userMsg string
		for _, m := range req.Messages {
			if m.Role == "user" {
				userMsg = m.Content
			}
		}
		query, chunk := extractQueryAndChunk(userMsg)
		answer := scoreFor(query, chunk)

		resp := chatCompletionResponse{
			Choices: []struct {
				Message chatCompletionMsg `json:"message"`
			}{
				{Message: chatCompletionMsg{Role: "assistant", Content: answer}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// extractQueryAndChunk crudely pulls the query and chunk text back out of
// the prompt assembled by scoreChunk. Used by the fake server to produce
// deterministic scores per-chunk.
func extractQueryAndChunk(prompt string) (string, string) {
	q := ""
	c := ""
	if i := strings.Index(prompt, "Query: "); i >= 0 {
		rest := prompt[i+len("Query: "):]
		if j := strings.Index(rest, "\n\nText: "); j >= 0 {
			q = rest[:j]
			rest = rest[j+len("\n\nText: "):]
			if k := strings.Index(rest, "\n\nScore:"); k >= 0 {
				c = rest[:k]
			} else {
				c = rest
			}
		}
	}
	return q, c
}

func TestLLMReranker_Empty(t *testing.T) {
	r := NewLLMReranker("http://example", "test")
	out, err := r.Rerank("q", nil, 5)
	if err != nil {
		t.Fatalf("empty rerank should not error: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("empty rerank should return empty slice")
	}
}

func TestLLMReranker_HappyPath_SortsByLLMScore(t *testing.T) {
	// Server returns deterministic scores: "top"→9, "middle"→5, default→1.
	srv := fakeLLMServer(t, func(_, chunk string) string {
		switch {
		case strings.Contains(chunk, "top"):
			return "9"
		case strings.Contains(chunk, "middle"):
			return "5"
		default:
			return "1"
		}
	})
	defer srv.Close()

	r := NewLLMReranker(srv.URL, "test-model").WithHTTPClient(srv.Client())

	// Pre-score in the OPPOSITE order of what the LLM will return, so
	// we can prove the LLM scores win.
	chunks := []ScoredChunk{
		makeLLMChunk("a", "bottom of the barrel", 0.99),
		makeLLMChunk("b", "middle content", 0.50),
		makeLLMChunk("c", "top quality answer", 0.01),
	}

	out, err := r.Rerank("any query", chunks, 3)
	if err != nil {
		t.Fatalf("rerank failed: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 results, got %d", len(out))
	}
	if out[0].ID != "c" {
		t.Errorf("expected 'top' chunk first, got %q (score %.2f)", out[0].ID, out[0].Score)
	}
	if out[1].ID != "b" {
		t.Errorf("expected 'middle' chunk second, got %q", out[1].ID)
	}
	if out[2].ID != "a" {
		t.Errorf("expected 'bottom' chunk third, got %q", out[2].ID)
	}
	if out[0].Score < 0.85 || out[0].Score > 0.95 {
		t.Errorf("top score not normalised: %.3f", out[0].Score)
	}
}

func TestLLMReranker_TopKTrims(t *testing.T) {
	srv := fakeLLMServer(t, func(_, chunk string) string {
		return fmt.Sprintf("%d", len(chunk)%10)
	})
	defer srv.Close()

	r := NewLLMReranker(srv.URL, "test-model").WithHTTPClient(srv.Client())
	chunks := []ScoredChunk{
		makeLLMChunk("a", "a", 0.1),
		makeLLMChunk("b", "bb", 0.2),
		makeLLMChunk("c", "ccc", 0.3),
		makeLLMChunk("d", "dddd", 0.4),
		makeLLMChunk("e", "eeeee", 0.5),
	}

	out, err := r.Rerank("q", chunks, 2)
	if err != nil {
		t.Fatalf("rerank failed: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 results after topK=2 trim, got %d", len(out))
	}
}

func TestLLMReranker_ParsesProseResponses(t *testing.T) {
	// Real LLMs sometimes wrap the score in explanation text. The
	// extractor must handle the common shapes without losing the number.
	cases := []struct {
		name      string
		answer    string
		wantScore float64 // normalised 0-1
	}{
		{"bare_int", "7", 0.7},
		{"with_decimal", "7.5", 0.75},
		{"with_prose", "The score is 8 out of 10.", 0.8},
		{"with_prefix", "Score: 4", 0.4},
		{"ten_perfect", "10", 1.0},
		{"ten_with_prose", "I would say 10.", 1.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := fakeLLMServer(t, func(_, _ string) string { return tc.answer })
			defer srv.Close()

			r := NewLLMReranker(srv.URL, "m").WithHTTPClient(srv.Client())
			chunks := []ScoredChunk{makeLLMChunk("x", "probe", 0)}
			out, err := r.Rerank("q", chunks, 1)
			if err != nil {
				t.Fatalf("rerank failed: %v", err)
			}
			if len(out) != 1 {
				t.Fatalf("expected 1, got %d", len(out))
			}
			if got := out[0].Score; got < tc.wantScore-0.01 || got > tc.wantScore+0.01 {
				t.Errorf("answer %q: expected %.2f, got %.2f", tc.answer, tc.wantScore, got)
			}
		})
	}
}

func TestLLMReranker_FallsBackOnServerError(t *testing.T) {
	// Server always errors. The reranker must return the chunks with
	// their ORIGINAL scores (sorted by them) rather than erroring.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	r := NewLLMReranker(srv.URL, "m").WithHTTPClient(srv.Client())
	chunks := []ScoredChunk{
		makeLLMChunk("a", "a", 0.9),
		makeLLMChunk("b", "b", 0.1),
	}
	out, err := r.Rerank("q", chunks, 2)
	if err != nil {
		t.Fatalf("fallback should not error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 results, got %d", len(out))
	}
	if out[0].ID != "a" {
		t.Errorf("original-score fallback should put 'a' first, got %q", out[0].ID)
	}
}

func TestLLMReranker_FallsBackOnUnparseableAnswer(t *testing.T) {
	srv := fakeLLMServer(t, func(_, _ string) string {
		return "I have no idea, please try again."
	})
	defer srv.Close()

	r := NewLLMReranker(srv.URL, "m").WithHTTPClient(srv.Client())
	chunks := []ScoredChunk{
		makeLLMChunk("a", "a", 0.7),
		makeLLMChunk("b", "b", 0.3),
	}
	out, err := r.Rerank("q", chunks, 2)
	if err != nil {
		t.Fatalf("fallback should not error: %v", err)
	}
	if out[0].ID != "a" {
		t.Errorf("expected original-score order (a,b), got %q", out[0].ID)
	}
}

func TestLLMReranker_ConcurrencyCap(t *testing.T) {
	var (
		inflight int32
		peak     int32
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := atomic.AddInt32(&inflight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if now <= p || atomic.CompareAndSwapInt32(&peak, p, now) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond) // force overlap
		atomic.AddInt32(&inflight, -1)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(chatCompletionResponse{
			Choices: []struct {
				Message chatCompletionMsg `json:"message"`
			}{{Message: chatCompletionMsg{Role: "assistant", Content: "5"}}},
		})
	}))
	defer srv.Close()

	r := NewLLMReranker(srv.URL, "m").WithHTTPClient(srv.Client()).WithConcurrency(2)

	chunks := make([]ScoredChunk, 10)
	for i := range chunks {
		chunks[i] = makeLLMChunk(fmt.Sprintf("c%d", i), "content", 0)
	}
	if _, err := r.Rerank("q", chunks, 10); err != nil {
		t.Fatalf("rerank failed: %v", err)
	}

	if got := atomic.LoadInt32(&peak); got > 2 {
		t.Errorf("concurrency cap violated: peak=%d, want <=2", got)
	}
}

func TestLLMReranker_APIKeyHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(chatCompletionResponse{
			Choices: []struct {
				Message chatCompletionMsg `json:"message"`
			}{{Message: chatCompletionMsg{Role: "assistant", Content: "5"}}},
		})
	}))
	defer srv.Close()

	r := NewLLMReranker(srv.URL, "m").WithHTTPClient(srv.Client()).WithAPIKey("secret-key")
	_, _ = r.Rerank("q", []ScoredChunk{makeLLMChunk("a", "x", 0)}, 1)

	if gotAuth != "Bearer secret-key" {
		t.Errorf("expected Authorization=Bearer secret-key, got %q", gotAuth)
	}
}

func TestExtractLeadingNumber(t *testing.T) {
	cases := []struct {
		in        string
		want      string
		wantFound bool
	}{
		{"5", "5", true},
		{"7.5", "7.5", true},
		{"10", "10", true},
		{"  3  ", "3", true},
		{"The answer is 8.", "8", true},
		{"Score: 9 out of 10", "9", true},
		{"not a number", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := extractLeadingNumber(tc.in)
		if ok != tc.wantFound {
			t.Errorf("extract(%q): found=%v, want %v", tc.in, ok, tc.wantFound)
		}
		if got != tc.want {
			t.Errorf("extract(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
