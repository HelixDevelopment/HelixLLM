package knowledge

import "time"

type Document struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Content   string            `json:"content"`
	Source    string            `json:"source"`
	Format    string            `json:"format"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

type Chunk struct {
	ID         string            `json:"id"`
	DocumentID string            `json:"document_id"`
	Content    string            `json:"content"`
	Position   int               `json:"position"`
	Embedding  []float64         `json:"embedding,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type ScoredChunk struct {
	Chunk
	Score float64 `json:"score"`
}

type Collection struct {
	Name      string `json:"name"`
	Documents int    `json:"documents"`
	Chunks    int    `json:"chunks"`
	Dimension int    `json:"dimension"`
}

type QueryRequest struct {
	Query      string  `json:"query"`
	Collection string  `json:"collection"`
	TopK       int     `json:"top_k,omitempty"`
	MinScore   float64 `json:"min_score,omitempty"`
}

type QueryResult struct {
	Query   string        `json:"query"`
	Chunks  []ScoredChunk `json:"chunks"`
	Context string        `json:"context"`

	// SemanticEmbeddings reports whether the retrieval that produced this
	// result used a real semantic Embedder (true) or the deterministic,
	// non-semantic HashEmbedder fallback (false) — see IsSemanticEmbedder
	// (HXC-235). Always set explicitly by Pipeline.Query so a caller can
	// tell hash-fallback results from real ones at the point of use,
	// never only from a startup log line.
	SemanticEmbeddings bool `json:"semantic_embeddings"`
}

type IngestRequest struct {
	Title      string `json:"title"`
	Content    string `json:"content"`
	Source     string `json:"source,omitempty"`
	Collection string `json:"collection"`
}

type IngestResult struct {
	DocumentID string `json:"document_id"`
	Chunks     int    `json:"chunks"`
	Collection string `json:"collection"`
}

type Stats struct {
	Collections []Collection `json:"collections"`
	TotalDocs   int          `json:"total_documents"`
	TotalChunks int          `json:"total_chunks"`
}
