package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Reranker re-scores and re-orders a set of ScoredChunks based on their
// relevance to a query. Implementations range from simple score-based sorting
// to cross-encoder or LLM-based relevance scoring.
type Reranker interface {
	Rerank(query string, chunks []ScoredChunk, topK int) ([]ScoredChunk, error)
}

// ScoreReranker is the default Reranker that re-sorts chunks by their existing
// Score field (descending) and trims to topK.
type ScoreReranker struct{}

// Rerank sorts chunks by Score descending and returns the top topK results.
func (r *ScoreReranker) Rerank(_ string, chunks []ScoredChunk, topK int) ([]ScoredChunk, error) {
	result := make([]ScoredChunk, len(chunks))
	copy(result, chunks)

	sort.Slice(result, func(i, j int) bool {
		return result[i].Score > result[j].Score
	})

	if topK > 0 && topK < len(result) {
		result = result[:topK]
	}
	return result, nil
}

// LLMReranker uses a local LLM (OpenAI-compatible chat completion endpoint)
// to score the relevance of each chunk against the query. Each chunk is
// scored with a dedicated request asking the model to rate relevance on a
// 0-10 scale; the numeric answer is parsed and normalised to the 0-1 range
// expected by downstream consumers.
//
// Robustness: the reranker degrades gracefully. If the LLM endpoint is
// unreachable, returns a non-200 response, produces an unparseable answer,
// or the configured context deadline fires mid-batch, the affected chunks
// keep their original score (from the previous retrieval pass) and the
// pipeline continues with a best-effort ranking. Errors are never fatal —
// a partial LLM-scored ranking is strictly better than no ranking at all.
//
// Concurrency: chunks are scored in parallel with a configurable semaphore
// (default 4 concurrent requests) to keep the local llama.cpp server from
// head-of-line blocking while still producing results in a reasonable
// wall-clock budget.
type LLMReranker struct {
	baseURL       string
	model         string
	client        *http.Client
	maxConcurrent int // 0 → DefaultLLMRerankerConcurrency
	apiKey        string
}

// DefaultLLMRerankerConcurrency bounds the number of parallel chunk-scoring
// requests a single Rerank call will issue. Tuned to the local llama.cpp
// router mode's typical batching behaviour.
const DefaultLLMRerankerConcurrency = 4

// NewLLMReranker creates an LLMReranker that will query the given LLM
// endpoint for relevance scoring. baseURL should be the OpenAI-compatible
// chat completion root (the reranker appends /v1/chat/completions).
func NewLLMReranker(baseURL, model string) *LLMReranker {
	return &LLMReranker{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// WithAPIKey returns a reranker configured with an authorization bearer
// token. Chainable; returns r for fluent construction.
func (r *LLMReranker) WithAPIKey(key string) *LLMReranker {
	r.apiKey = key
	return r
}

// WithConcurrency overrides the default concurrency cap. Values ≤0 revert
// to DefaultLLMRerankerConcurrency.
func (r *LLMReranker) WithConcurrency(n int) *LLMReranker {
	r.maxConcurrent = n
	return r
}

// WithHTTPClient installs a custom *http.Client (primarily for tests that
// point the reranker at an httptest.Server).
func (r *LLMReranker) WithHTTPClient(client *http.Client) *LLMReranker {
	if client != nil {
		r.client = client
	}
	return r
}

// Rerank scores each chunk by asking the LLM to rate its relevance to the
// query on a 0-10 scale, then sorts by the new score and returns topK.
//
// Per-chunk failures (HTTP error, bad parse, timeout) are logged by leaving
// the chunk's existing Score untouched — no single failing chunk can break
// the whole batch. If every chunk fails, the function falls back to pure
// ScoreReranker behaviour (sort by existing score) so callers always get a
// result.
func (r *LLMReranker) Rerank(query string, chunks []ScoredChunk, topK int) ([]ScoredChunk, error) {
	if len(chunks) == 0 {
		return chunks, nil
	}
	// Work on a copy so the caller's slice is never mutated.
	scored := make([]ScoredChunk, len(chunks))
	copy(scored, chunks)

	concurrency := r.maxConcurrent
	if concurrency <= 0 {
		concurrency = DefaultLLMRerankerConcurrency
	}
	if concurrency > len(scored) {
		concurrency = len(scored)
	}

	// Derive a batch-level deadline from the HTTP client timeout so a slow
	// server cannot hang the caller longer than max-per-request × chunks.
	budget := r.client.Timeout
	if budget <= 0 {
		budget = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget*2)
	defer cancel()

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i := range scored {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			select {
			case <-ctx.Done():
				return
			default:
			}
			score, err := r.scoreChunk(ctx, query, scored[idx].Content)
			if err != nil {
				// Leave the existing score in place — fall back to
				// whatever the retrieval layer produced.
				return
			}
			scored[idx].Score = score
		}(i)
	}
	wg.Wait()

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	if topK > 0 && topK < len(scored) {
		scored = scored[:topK]
	}
	return scored, nil
}

// chatCompletionRequest is a minimal OpenAI-compatible request. We keep
// it local to the reranker rather than importing the full provider DTOs
// so this file has no dependency on brain/*.
type chatCompletionRequest struct {
	Model       string              `json:"model"`
	Messages    []chatCompletionMsg `json:"messages"`
	Temperature float64             `json:"temperature"`
	MaxTokens   int                 `json:"max_tokens"`
	Stream      bool                `json:"stream"`
}

type chatCompletionMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message chatCompletionMsg `json:"message"`
	} `json:"choices"`
}

// scoreReExtractor matches the first number in 0-10 (or 0.0-10.0) form in
// an LLM response. LLMs often wrap the number in explanation text, so we
// tolerate any leading/trailing content and take the first match.
//
// The trailing class is [^0-9] (not digit) rather than [^0-9.] so that
// numbers at the end of a sentence — e.g. "The answer is 8." or "10." —
// still match. The decimal-digit sub-pattern `(?:\.[0-9]+)?` ensures a
// bare "8." is parsed as 8 rather than partial "8." — the `[0-9]+` at
// the end of the sub-pattern is required, so "." alone does not extend
// the match.
var scoreReExtractor = regexp.MustCompile(`(?:^|\s)(10(?:\.0+)?|[0-9](?:\.[0-9]+)?)(?:$|[^0-9])`)

// scoreChunk sends a single chunk to the LLM and returns the normalised
// relevance score in [0.0, 1.0]. Any HTTP/parse error returns (0, err)
// so Rerank can fall back to the existing chunk score.
func (r *LLMReranker) scoreChunk(ctx context.Context, query, content string) (float64, error) {
	// Cap the chunk content we send to the LLM. Overlong chunks waste
	// tokens and can confuse small scoring models; 2000 chars covers
	// typical retrieval windows without blowing the prompt budget.
	const maxChars = 2000
	if len(content) > maxChars {
		content = content[:maxChars]
	}

	prompt := fmt.Sprintf(
		"Rate how relevant the following text is to answering the query, on a scale of 0 to 10 "+
			"(0 = completely irrelevant, 10 = perfect answer). Respond with only the number.\n\n"+
			"Query: %s\n\nText: %s\n\nScore:",
		query, content,
	)
	body, err := json.Marshal(chatCompletionRequest{
		Model: r.model,
		Messages: []chatCompletionMsg{
			{Role: "system", Content: "You are a relevance scoring assistant. Respond with only a single integer 0-10."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0,
		MaxTokens:   8,
		Stream:      false,
	})
	if err != nil {
		return 0, fmt.Errorf("reranker: marshal request: %w", err)
	}

	endpoint := r.baseURL + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("reranker: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if r.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+r.apiKey)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("reranker: http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return 0, fmt.Errorf("reranker: status %d: %s", resp.StatusCode, string(snippet))
	}

	var parsed chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return 0, fmt.Errorf("reranker: decode: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return 0, fmt.Errorf("reranker: empty choices")
	}

	text := strings.TrimSpace(parsed.Choices[0].Message.Content)
	raw, ok := extractLeadingNumber(text)
	if !ok {
		return 0, fmt.Errorf("reranker: no number in response %q", text)
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("reranker: parse %q: %w", raw, err)
	}
	// Normalise to [0.0, 1.0] and clamp.
	score := value / 10.0
	if score < 0 {
		score = 0
	} else if score > 1 {
		score = 1
	}
	return score, nil
}

// extractLeadingNumber pulls the first number in 0-10 range out of an LLM
// response, tolerating surrounding prose.
func extractLeadingNumber(text string) (string, bool) {
	// Fast path: the response is just a bare integer/float.
	if _, err := strconv.ParseFloat(strings.TrimSpace(text), 64); err == nil {
		return strings.TrimSpace(text), true
	}
	// Slow path: regexp search with a trailing sentinel space so
	// single-digit matches at the end also hit.
	match := scoreReExtractor.FindStringSubmatch(" " + text + " ")
	if len(match) < 2 {
		return "", false
	}
	return match[1], true
}
