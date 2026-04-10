package knowledge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Pipeline orchestrates document ingestion and retrieval-augmented generation.
type Pipeline struct {
	embedder          Embedder
	store             VectorStore
	chunker           Chunker
	defaultCollection string
	defaultTopK       int

	// Hybrid search fields (opt-in via HybridEnabled).
	hybridRetriever *HybridRetriever
	bm25Index       *BM25Index
	hybridEnabled   bool
	mmrLambda       float64
}

// PipelineConfig holds the dependencies and defaults for a Pipeline.
type PipelineConfig struct {
	Embedder          Embedder
	Store             VectorStore
	Chunker           Chunker
	DefaultCollection string
	DefaultTopK       int

	// Hybrid search configuration (opt-in).
	HybridEnabled  bool
	SemanticWeight float64
	KeywordWeight  float64
	MMRLambda      float64
}

// NewPipeline returns a Pipeline configured with the provided options.
func NewPipeline(cfg PipelineConfig) *Pipeline {
	topK := cfg.DefaultTopK
	if topK <= 0 {
		topK = 5
	}

	p := &Pipeline{
		embedder:          cfg.Embedder,
		store:             cfg.Store,
		chunker:           cfg.Chunker,
		defaultCollection: cfg.DefaultCollection,
		defaultTopK:       topK,
		hybridEnabled:     cfg.HybridEnabled,
		mmrLambda:         cfg.MMRLambda,
	}

	if cfg.HybridEnabled {
		p.bm25Index = NewBM25Index()
		p.hybridRetriever = NewHybridRetriever(cfg.Embedder, cfg.Store, p.bm25Index)
		if cfg.SemanticWeight > 0 || cfg.KeywordWeight > 0 {
			p.hybridRetriever.SetWeights(cfg.SemanticWeight, cfg.KeywordWeight)
		}
		if p.mmrLambda <= 0 {
			p.mmrLambda = 0.7 // default: slightly favor relevance over diversity
		}
	}

	return p
}

// Ingest chunks, embeds, and stores a document from the given request.
//
// Validation rules:
//   - Content must be non-empty.
//   - Collection must be non-empty.
func (p *Pipeline) Ingest(ctx context.Context, req IngestRequest) (*IngestResult, error) {
	if strings.TrimSpace(req.Content) == "" {
		return nil, fmt.Errorf("ingest: content must not be empty")
	}
	if strings.TrimSpace(req.Collection) == "" {
		return nil, fmt.Errorf("ingest: collection must not be empty")
	}

	doc := Document{
		ID:        uuid.NewString(),
		Title:     req.Title,
		Content:   req.Content,
		Source:    req.Source,
		CreatedAt: time.Now().UTC(),
	}

	chunks, err := p.chunker.Chunk(doc)
	if err != nil {
		return nil, fmt.Errorf("ingest: chunk document: %w", err)
	}

	texts := make([]string, len(chunks))
	for i, ch := range chunks {
		texts[i] = ch.Content
	}

	embeddings, err := p.embedder.EmbedBatch(texts)
	if err != nil {
		return nil, fmt.Errorf("ingest: embed chunks: %w", err)
	}

	for i := range chunks {
		chunks[i].Embedding = embeddings[i]
	}

	if err := p.store.Upsert(req.Collection, chunks); err != nil {
		return nil, fmt.Errorf("ingest: store upsert: %w", err)
	}

	// Index chunks in BM25 for hybrid keyword search.
	if p.hybridEnabled && p.bm25Index != nil {
		for _, ch := range chunks {
			p.bm25Index.Add(ch.ID, ch.Content)
		}
	}

	return &IngestResult{
		DocumentID: doc.ID,
		Chunks:     len(chunks),
		Collection: req.Collection,
	}, nil
}

// Query embeds the query string, searches the vector store, and returns the
// top-K matching chunks together with an assembled context string.
//
// When hybrid search is enabled, the query is expanded with code synonyms,
// searched via both semantic and keyword retrieval, and the results are
// re-ranked with MMR for diversity.
//
// Validation rules:
//   - Query must be non-empty.
//   - Collection must be non-empty.
func (p *Pipeline) Query(ctx context.Context, req QueryRequest) (*QueryResult, error) {
	if strings.TrimSpace(req.Query) == "" {
		return nil, fmt.Errorf("query: query string must not be empty")
	}
	if strings.TrimSpace(req.Collection) == "" {
		return nil, fmt.Errorf("query: collection must not be empty")
	}

	topK := req.TopK
	if topK <= 0 {
		topK = p.defaultTopK
	}

	var scored []ScoredChunk

	if p.hybridEnabled && p.hybridRetriever != nil {
		// 1. Expand query with code synonyms.
		expandedQueries := ExpandQuery(req.Query)

		// 2. Run hybrid search for each expanded query.
		seen := make(map[string]ScoredChunk)
		for _, q := range expandedQueries {
			results, err := p.hybridRetriever.Search(ctx, req.Collection, q, topK)
			if err != nil {
				return nil, fmt.Errorf("query: hybrid search: %w", err)
			}
			for _, sc := range results {
				if existing, ok := seen[sc.ID]; ok {
					// Keep the higher score.
					if sc.Score > existing.Score {
						seen[sc.ID] = sc
					}
				} else {
					seen[sc.ID] = sc
				}
			}
		}

		// 3. Collect deduplicated results.
		candidates := make([]ScoredChunk, 0, len(seen))
		for _, sc := range seen {
			candidates = append(candidates, sc)
		}

		// 4. Apply MMR for diversity.
		scored = MMRRerank(candidates, p.mmrLambda, topK)
	} else {
		// Original vector-only path.
		vec, err := p.embedder.Embed(req.Query)
		if err != nil {
			return nil, fmt.Errorf("query: embed query: %w", err)
		}

		scored, err = p.store.Search(req.Collection, vec, topK)
		if err != nil {
			return nil, fmt.Errorf("query: search store: %w", err)
		}
	}

	if req.MinScore > 0 {
		filtered := scored[:0]
		for _, sc := range scored {
			if sc.Score >= req.MinScore {
				filtered = append(filtered, sc)
			}
		}
		scored = filtered
	}

	parts := make([]string, len(scored))
	for i, sc := range scored {
		parts[i] = sc.Content
	}

	return &QueryResult{
		Query:   req.Query,
		Chunks:  scored,
		Context: strings.Join(parts, "\n"),
	}, nil
}

// Collections returns a summary of all collections currently in the store.
func (p *Pipeline) Collections(ctx context.Context) ([]Collection, error) {
	cols, err := p.store.Collections()
	if err != nil {
		return nil, fmt.Errorf("pipeline: collections: %w", err)
	}
	return cols, nil
}

// Stats returns aggregate document and chunk counts across all collections.
func (p *Pipeline) Stats(ctx context.Context) (*Stats, error) {
	s, err := p.store.Stats()
	if err != nil {
		return nil, fmt.Errorf("pipeline: stats: %w", err)
	}
	return s, nil
}
