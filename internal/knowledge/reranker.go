package knowledge

import (
	"net/http"
	"sort"
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

// LLMReranker uses a local LLM to score relevance of each chunk against the
// query. It sends a scoring prompt to the LLM and parses the numeric response
// as the new relevance score. This is a future enhancement; the current
// implementation falls back to ScoreReranker.
type LLMReranker struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewLLMReranker creates an LLMReranker that will query the given LLM
// endpoint for relevance scoring.
func NewLLMReranker(baseURL, model string) *LLMReranker {
	return &LLMReranker{
		baseURL: baseURL,
		model:   model,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// Rerank scores each chunk by asking the LLM to rate relevance. The current
// implementation falls back to score-based re-ranking; future versions will
// send prompts like "Rate the relevance of this text to the query on a scale
// of 0-10" and parse the numeric response.
func (r *LLMReranker) Rerank(query string, chunks []ScoredChunk, topK int) ([]ScoredChunk, error) {
	// TODO: Implement LLM-based relevance scoring.
	// For each chunk, create a prompt:
	//   "Rate the relevance of this text to the query on a scale of 0-10.
	//    Query: {query}. Text: {chunk.Content}. Score:"
	// Parse the numeric response and use it as the new score.
	// For now, fall back to score-based re-ranking.
	return (&ScoreReranker{}).Rerank(query, chunks, topK)
}
