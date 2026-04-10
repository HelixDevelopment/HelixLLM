package knowledge

import (
	"context"
	"sort"
)

// HybridRetriever combines semantic (vector) search with keyword (BM25) search
// to produce results that benefit from both dense and sparse retrieval signals.
type HybridRetriever struct {
	embedder       Embedder
	vectorStore    VectorStore
	bm25Index      *BM25Index
	semanticWeight float64
	keywordWeight  float64
	reranker       Reranker
}

// NewHybridRetriever returns a HybridRetriever with default weights of 0.7 for
// semantic search and 0.3 for keyword search.
func NewHybridRetriever(embedder Embedder, store VectorStore, bm25 *BM25Index) *HybridRetriever {
	return &HybridRetriever{
		embedder:       embedder,
		vectorStore:    store,
		bm25Index:      bm25,
		semanticWeight: 0.7,
		keywordWeight:  0.3,
	}
}

// SetWeights overrides the default semantic and keyword weights.
func (h *HybridRetriever) SetWeights(semantic, keyword float64) {
	h.semanticWeight = semantic
	h.keywordWeight = keyword
}

// SetReranker configures an optional Reranker that is applied after score
// fusion and before returning results. Pass nil to disable re-ranking.
func (h *HybridRetriever) SetReranker(r Reranker) {
	h.reranker = r
}

// Search performs hybrid retrieval by running both vector search and BM25
// keyword search, combining scores with the configured weights, and returning
// the top K results.
func (h *HybridRetriever) Search(ctx context.Context, collection, query string, topK int) ([]ScoredChunk, error) {
	// Fetch 2x topK from each source for re-ranking headroom.
	fetchK := topK * 2
	if fetchK < 1 {
		fetchK = 1
	}

	// 1. Semantic (vector) search.
	queryVec, err := h.embedder.Embed(query)
	if err != nil {
		return nil, err
	}

	vectorResults, err := h.vectorStore.Search(collection, queryVec, fetchK)
	if err != nil {
		return nil, err
	}

	// 2. BM25 keyword search.
	bm25Results := h.bm25Index.Search(query, fetchK)

	// 3. Normalize scores within each result set to [0, 1] before combining.
	normalizeScores(vectorResults)
	normalizeScores(bm25Results)

	// 4. Merge by chunk ID, combining weighted scores.
	type merged struct {
		chunk         ScoredChunk
		semanticScore float64
		keywordScore  float64
	}
	mergeMap := make(map[string]*merged)

	for _, sc := range vectorResults {
		m := &merged{chunk: sc, semanticScore: sc.Score}
		mergeMap[sc.ID] = m
	}

	for _, sc := range bm25Results {
		if existing, ok := mergeMap[sc.ID]; ok {
			existing.keywordScore = sc.Score
			// Prefer the vector result's chunk data (it has embeddings).
		} else {
			mergeMap[sc.ID] = &merged{chunk: sc, keywordScore: sc.Score}
		}
	}

	// Compute final combined scores.
	results := make([]ScoredChunk, 0, len(mergeMap))
	for _, m := range mergeMap {
		finalScore := h.semanticWeight*m.semanticScore + h.keywordWeight*m.keywordScore
		m.chunk.Score = finalScore
		results = append(results, m.chunk)
	}

	// Sort by final score descending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Apply optional re-ranker after score fusion.
	if h.reranker != nil {
		reranked, err := h.reranker.Rerank(query, results, topK)
		if err != nil {
			return nil, err
		}
		return reranked, nil
	}

	if topK > 0 && topK < len(results) {
		results = results[:topK]
	}

	return results, nil
}

// normalizeScores scales scores in the slice to [0, 1] using min-max
// normalization. If all scores are identical they are set to 1.0.
func normalizeScores(chunks []ScoredChunk) {
	if len(chunks) == 0 {
		return
	}

	minScore := chunks[0].Score
	maxScore := chunks[0].Score
	for _, sc := range chunks[1:] {
		if sc.Score < minScore {
			minScore = sc.Score
		}
		if sc.Score > maxScore {
			maxScore = sc.Score
		}
	}

	spread := maxScore - minScore
	if spread == 0 {
		for i := range chunks {
			chunks[i].Score = 1.0
		}
		return
	}

	for i := range chunks {
		chunks[i].Score = (chunks[i].Score - minScore) / spread
	}
}
