package knowledge

import (
	"context"
	"fmt"
	"strconv"

	"github.com/google/uuid"

	vdbclient "digital.vasic.vectordb/pkg/client"
	"digital.vasic.vectordb/pkg/qdrant"
)

// qdrantChunkIDPayloadKey is the Qdrant point-payload field that carries the
// ORIGINAL semantic chunk ID (as minted by the Chunker, e.g.
// FixedSizeChunker's "<documentID>-<position>") whenever that ID is not
// itself a valid Qdrant point ID. See qdrantPointID for the full rationale.
const qdrantChunkIDPayloadKey = "chunk_id"

// qdrantIDNamespace is a fixed, arbitrary namespace UUID used to derive
// deterministic UUIDv5 point IDs from semantic chunk IDs (see qdrantPointID).
// It has no meaning beyond providing a stable seed so the SAME chunk ID
// always maps to the SAME Qdrant point ID across upserts (idempotent
// re-ingestion) and across processes (no per-run randomness).
var qdrantIDNamespace = uuid.NameSpaceOID

// qdrantPointID derives a Qdrant-valid point ID from a semantic chunk ID.
//
// Root cause (§11.4.146 STEP 1): Qdrant's REST API requires every point ID
// to be either an unsigned 64-bit integer or a UUID
// (https://qdrant.tech/documentation/concepts/points/#point-ids). HelixLLM's
// FixedSizeChunker mints chunk IDs shaped "<documentID-uuid>-<position>"
// (internal/knowledge/chunker.go Chunk), which is NEITHER — so a production
// Pipeline.Ingest -> QdrantStore.Upsert call sends Qdrant a point whose "id"
// field it rejects, failing the whole batch upsert with no chunks ever
// stored and Pipeline.Query returning nothing for that document.
//
// Fix: map every semantic chunk ID to a valid Qdrant point ID AT THE STORE
// BOUNDARY (this adapter), rather than changing what IDs the Chunker mints.
// The boundary-mapping approach is deliberately chosen over widening the
// chunk-ID format itself because (a) MemoryStore and every other
// VectorStore backend have no such ID-shape constraint and already rely on
// the current human-readable "<documentID>-<position>" shape (changing it
// would be a needless, wider-blast-radius behaviour change for backends
// that were never broken), and (b) it keeps the Qdrant-specific hygiene
// localized to the Qdrant adapter, matching how Upsert already down-casts
// []float64 embeddings to []float32 for this same backend.
//
// Already-valid IDs (an unsigned integer, or a real UUID) pass through
// unchanged so a caller that already mints Qdrant-valid IDs sees no
// behaviour change. Everything else is mapped via a deterministic UUIDv5
// derivation (same chunk ID -> same point ID on every call), so re-ingesting
// an edited document overwrites the same Qdrant points instead of leaking
// duplicates. The original semantic ID is preserved in the point payload
// (qdrantChunkIDPayloadKey) so Search can hand back the ID the chunk was
// ingested with, not the internal Qdrant point ID.
func qdrantPointID(chunkID string) string {
	if isValidQdrantPointID(chunkID) {
		return chunkID
	}
	return uuid.NewSHA1(qdrantIDNamespace, []byte(chunkID)).String()
}

// isValidQdrantPointID reports whether id already satisfies Qdrant's point-ID
// shape: an unsigned 64-bit integer, or a valid UUID (any RFC 4122 variant).
func isValidQdrantPointID(id string) bool {
	if id == "" {
		return false
	}
	if _, err := strconv.ParseUint(id, 10, 64); err == nil {
		return true
	}
	if _, err := uuid.Parse(id); err == nil {
		return true
	}
	return false
}

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
		// Preserve the original semantic chunk ID in the payload AFTER
		// copying ch.Metadata so it can never be shadowed by a colliding
		// user-supplied metadata key — Search/chunkFromMetadata depends on
		// this field being correct to reconstruct ch.ID on retrieval.
		meta[qdrantChunkIDPayloadKey] = ch.ID
		vectors[i] = vdbclient.Vector{
			ID:       qdrantPointID(ch.ID),
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
//
// ids are semantic chunk IDs (the same shape Upsert receives), so each is
// translated through qdrantPointID before being sent to Qdrant — otherwise
// a delete-by-semantic-ID would silently no-op against Qdrant's differently
// shaped point IDs (the same root cause as the Upsert/Search bug this file
// fixes).
func (q *QdrantStore) Delete(collection string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	pointIDs := make([]string, len(ids))
	for i, id := range ids {
		pointIDs[i] = qdrantPointID(id)
	}
	ctx := context.Background()
	if err := q.client.Delete(ctx, collection, pointIDs); err != nil {
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
//
// id is the Qdrant POINT id (possibly a UUIDv5 derived by qdrantPointID from
// a semantic chunk ID that wasn't itself Qdrant-valid); when the payload
// carries the original semantic ID under qdrantChunkIDPayloadKey (which
// Upsert always sets), that value wins so callers see the same ID the chunk
// was ingested with, not Qdrant's internal point ID.
func chunkFromMetadata(id string, meta map[string]any) Chunk {
	ch := Chunk{ID: id}
	if v, ok := meta[qdrantChunkIDPayloadKey].(string); ok && v != "" {
		ch.ID = v
	}
	if v, ok := meta["content"].(string); ok {
		ch.Content = v
	}
	if v, ok := meta["document_id"].(string); ok {
		ch.DocumentID = v
	}
	// NEW FINDING (surfaced while fixing the chunk-ID mapping above, same
	// file/function): the Qdrant REST client decodes point payloads via
	// encoding/json into map[string]any, so a JSON number ALWAYS comes back
	// as float64, never int — a bare `.(int)` type assertion here silently
	// fails on every real Search() round-trip and Position stays 0. Handle
	// both shapes so Position round-trips correctly from real Qdrant
	// responses, not just from in-process test fixtures that happen to
	// construct the map with a Go int literal.
	switch v := meta["position"].(type) {
	case float64:
		ch.Position = int(v)
	case int:
		ch.Position = v
	}
	// Re-populate Metadata map with any extra fields.
	extra := make(map[string]string)
	reserved := map[string]bool{"content": true, "document_id": true, "position": true, qdrantChunkIDPayloadKey: true}
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
