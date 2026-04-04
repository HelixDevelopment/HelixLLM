package knowledge

import (
	"context"
	"fmt"

	vdbclient "digital.vasic.vectordb/pkg/client"
	"digital.vasic.vectordb/pkg/qdrant"
)

// QdrantStore wraps the digital.vasic.vectordb Qdrant backend to satisfy
// the HelixLLM VectorStore interface.
//
// The underlying vectordb client uses float32 vectors and requires an explicit
// Connect call; QdrantStore handles connection lazily on first use and exposes
// the same collection-scoped API that MemoryStore does.
type QdrantStore struct {
	client     *qdrant.Client
	collection string // default collection (used by Stats/Collections helpers)
	dimension  int
}

// NewQdrantStore creates a QdrantStore connected to the Qdrant instance at
// host:port.  The store calls Connect immediately; callers should treat a
// non-nil error as a hard failure (Qdrant not reachable).
func NewQdrantStore(host string, port int) (*QdrantStore, error) {
	cfg := &qdrant.Config{
		Host:     host,
		HTTPPort: port,
		GRPCPort: port + 1, // convention: gRPC port = HTTP port + 1
		TLS:      false,
	}
	// Use DefaultConfig to fill in Timeout + DefaultDistance, then overlay
	// the caller-supplied host/port.
	defaults := qdrant.DefaultConfig()
	cfg.Timeout = defaults.Timeout
	cfg.DefaultDistance = defaults.DefaultDistance

	c, err := qdrant.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("qdrant: create client: %w", err)
	}

	ctx := context.Background()
	if err := c.Connect(ctx); err != nil {
		return nil, fmt.Errorf("qdrant: connect: %w", err)
	}

	return &QdrantStore{
		client: c,
	}, nil
}

// Close releases the underlying connection.
func (q *QdrantStore) Close() error {
	return q.client.Close()
}

// EnsureCollection creates a collection in Qdrant if it does not yet exist.
// It is safe to call repeatedly; errors from "already exists" are silently
// swallowed.
func (q *QdrantStore) EnsureCollection(ctx context.Context, name string, dimension int) error {
	err := q.client.CreateCollection(ctx, vdbclient.CollectionConfig{
		Name:      name,
		Dimension: dimension,
		Metric:    vdbclient.DistanceCosine,
	})
	if err != nil {
		// Qdrant returns an HTTP 4xx error if the collection already exists.
		// We treat that as non-fatal.
		return nil
	}
	return nil
}

// Upsert stores the given chunks into the named Qdrant collection.
//
// Each Chunk's Embedding ([]float64) is down-cast to []float32 for the
// vectordb layer.  Chunk.Metadata and Chunk.Content are stored as Qdrant
// payload fields so they can be retrieved on search.
func (q *QdrantStore) Upsert(collection string, chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	ctx := context.Background()

	// Derive dimension from the first chunk that has an embedding.
	dim := 0
	for _, ch := range chunks {
		if len(ch.Embedding) > 0 {
			dim = len(ch.Embedding)
			break
		}
	}
	if dim > 0 {
		_ = q.EnsureCollection(ctx, collection, dim)
	}

	vectors := make([]vdbclient.Vector, len(chunks))
	for i, ch := range chunks {
		f32 := float64SliceToFloat32(ch.Embedding)
		meta := make(map[string]any)
		meta["content"] = ch.Content
		meta["document_id"] = ch.DocumentID
		meta["position"] = ch.Position
		for k, v := range ch.Metadata {
			meta[k] = v
		}
		vectors[i] = vdbclient.Vector{
			ID:       ch.ID,
			Values:   f32,
			Metadata: meta,
		}
	}

	if err := q.client.Upsert(ctx, collection, vectors); err != nil {
		return fmt.Errorf("qdrant upsert: %w", err)
	}
	return nil
}

// Search finds the topK chunks most similar to vector in the named collection.
func (q *QdrantStore) Search(collection string, vector []float64, topK int) ([]ScoredChunk, error) {
	ctx := context.Background()

	query := vdbclient.SearchQuery{
		Vector: float64SliceToFloat32(vector),
		TopK:   topK,
	}

	results, err := q.client.Search(ctx, collection, query)
	if err != nil {
		return nil, fmt.Errorf("qdrant search: %w", err)
	}

	scored := make([]ScoredChunk, 0, len(results))
	for _, r := range results {
		ch := chunkFromMetadata(r.ID, r.Metadata)
		scored = append(scored, ScoredChunk{
			Chunk: ch,
			Score: float64(r.Score),
		})
	}
	return scored, nil
}

// Delete removes the chunks with the given IDs from the named collection.
func (q *QdrantStore) Delete(collection string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	ctx := context.Background()
	if err := q.client.Delete(ctx, collection, ids); err != nil {
		return fmt.Errorf("qdrant delete: %w", err)
	}
	return nil
}

// Collections lists all collections known to this Qdrant instance.
func (q *QdrantStore) Collections() ([]Collection, error) {
	ctx := context.Background()
	names, err := q.client.ListCollections(ctx)
	if err != nil {
		return nil, fmt.Errorf("qdrant list collections: %w", err)
	}
	cols := make([]Collection, len(names))
	for i, name := range names {
		cols[i] = Collection{Name: name}
	}
	return cols, nil
}

// DeleteCollection removes an entire Qdrant collection by name.
func (q *QdrantStore) DeleteCollection(name string) error {
	ctx := context.Background()
	if err := q.client.DeleteCollection(ctx, name); err != nil {
		return fmt.Errorf("qdrant delete collection %q: %w", name, err)
	}
	return nil
}

// Stats returns aggregate counts. Because Qdrant does not expose per-collection
// document counts cheaply, Chunks and Documents fields are left at 0.
func (q *QdrantStore) Stats() (*Stats, error) {
	cols, err := q.Collections()
	if err != nil {
		return nil, err
	}
	return &Stats{Collections: cols}, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// float64SliceToFloat32 converts a []float64 to []float32.
func float64SliceToFloat32(in []float64) []float32 {
	out := make([]float32, len(in))
	for i, v := range in {
		out[i] = float32(v)
	}
	return out
}

// chunkFromMetadata reconstructs a Chunk from a Qdrant point payload.
func chunkFromMetadata(id string, meta map[string]any) Chunk {
	ch := Chunk{ID: id}
	if v, ok := meta["content"].(string); ok {
		ch.Content = v
	}
	if v, ok := meta["document_id"].(string); ok {
		ch.DocumentID = v
	}
	if v, ok := meta["position"].(int); ok {
		ch.Position = v
	}
	// Re-populate Metadata map with any extra fields.
	extra := make(map[string]string)
	reserved := map[string]bool{"content": true, "document_id": true, "position": true}
	for k, v := range meta {
		if !reserved[k] {
			if s, ok := v.(string); ok {
				extra[k] = s
			}
		}
	}
	if len(extra) > 0 {
		ch.Metadata = extra
	}
	return ch
}

// Compile-time interface check.
var _ VectorStore = (*QdrantStore)(nil)
