// Package integration_test — Wave-2 production-path proof for the
// QDRANT-CHUNK-ID-INVALID V&V finding (2026-07-11).
//
// Prior state (honestly discovered + documented, NOT fixed, in
// docs/qa/reranker_wave2_20260711T154720Z/RESULTS.md "Honest, out-of-scope
// discovery" section): internal/knowledge/chunker.go's FixedSizeChunker
// always mints chunk IDs shaped "<documentID-uuid>-<position>", which is
// NOT a valid Qdrant point ID (Qdrant requires an unsigned 64-bit integer
// or a UUID — https://qdrant.tech/documentation/concepts/points/#point-ids).
// A production knowledge.Pipeline.Ingest call into a real QdrantStore
// therefore failed the whole batch upsert, and knowledge.Pipeline.Query
// against that (never-populated) collection returned nothing.
//
// This test proves the FIX: it drives the REAL, unmodified
// knowledge.Pipeline.Ingest and knowledge.Pipeline.Query (imported from
// internal/knowledge, not reimplemented) against a REAL Qdrant vector
// store, with a multi-document corpus, and empirically demonstrates:
//   - RED: reverting internal/knowledge/qdrant.go to its pre-fix content
//     and re-running the SAME Ingest call against the SAME real Qdrant
//     server reproduces the historical rejection (captured in
//     docs/qa/<run-id>/10_red_qdrant_ingest_reject.txt).
//   - GREEN: with the fix restored, Pipeline.Ingest succeeds (real Qdrant
//     upsert, no rejection) and Pipeline.Query retrieves the ingested
//     chunks end-to-end.
//
// Gated behind HELIX_LIVE_RAG_QDRANT_INGEST_TEST=true — honestly SKIPs
// otherwise (§11.4.3). Boots/tears down qdrant+tei-embed via the SAME
// containers-submodule-orchestrated compose file the reranker wave already
// proved (§11.4.74 reuse-before-reimplement), rootless podman (§11.4.161),
// on a DISTINCT port range so this run never collides with that harness or
// any concurrent track (§11.4.111/§11.4.119/§11.4.176). The live coder
// (:18434, read-only, never restarted per §11.4.122) is not used by this
// test at all.
package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"digital.vasic.containers/pkg/compose"
)

type qdrantIngestCorpusDoc struct {
	ID    string
	Title string
	Text  string
}

var qdrantIngestCorpus = []qdrantIngestCorpusDoc{
	{"doc_alpha", "Alpha", "The Cindervale telemetry collector batches events every thirty seconds before flushing to storage."},
	{"doc_beta", "Beta", "Emberkiln's active production embeddings registry is rebuilt nightly from the canonical document store."},
	{"doc_gamma", "Gamma", "The HelixCode release pipeline tags every artifact with a monotonically increasing build number."},
	{"doc_delta", "Delta", "Qdrant point IDs must be an unsigned 64-bit integer or a UUID; any other shape is rejected by the server."},
	{"doc_epsilon", "Epsilon", "Coffee brewing techniques improve flavor extraction using controlled water temperature."},
}

// TestRAGQdrantChunkIDFix_LiveEndToEnd is the permanent, re-runnable,
// production-path regression guard for the chunk-ID/point-ID mapping fix in
// internal/knowledge/qdrant.go (QdrantStore.Upsert/Search/Delete).
func TestRAGQdrantChunkIDFix_LiveEndToEnd(t *testing.T) {
	if os.Getenv("HELIX_LIVE_RAG_QDRANT_INGEST_TEST") != "true" {
		t.Skip("SKIP-OK: opt-in live test (requires containers submodule + rootless podman + qdrant/tei-embed images). " +
			"Set HELIX_LIVE_RAG_QDRANT_INGEST_TEST=true to run.")
	}

	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	qdrantGoPath := filepath.Join(repoRoot, "internal", "knowledge", "qdrant.go")
	fixedSource, err := os.ReadFile(qdrantGoPath)
	if err != nil {
		t.Fatalf("read current (fixed) qdrant.go: %v", err)
	}

	runID := "qdrant_chunkid_fix_wave2_" + time.Now().UTC().Format("20060102T150405Z")
	evidDir := filepath.Join(repoRoot, "docs", "qa", runID)
	if err := os.MkdirAll(evidDir, 0o755); err != nil {
		t.Fatalf("mkdir evidence dir: %v", err)
	}
	var evidence strings.Builder
	logf := func(format string, args ...interface{}) {
		line := fmt.Sprintf(format, args...)
		t.Log(line)
		evidence.WriteString(line)
		evidence.WriteString("\n")
	}
	defer func() {
		_ = os.WriteFile(filepath.Join(evidDir, "RESULTS.md"), []byte(evidence.String()), 0o644)
	}()

	logf("### TestRAGQdrantChunkIDFix_LiveEndToEnd — %s UTC", time.Now().UTC().Format(time.RFC3339))
	logf("run_id=%s", runID)
	logf("§11.4.119 single-resource-owner: coder :18434 is read-only and untouched by this test.")

	// Distinct port range from the reranker-wave harness (18480-18483) and
	// from any other concurrent track (§11.4.111/§11.4.119/§11.4.176).
	env := map[string]string{
		"QDRANT_HTTP_PORT":     "18490",
		"QDRANT_GRPC_PORT":     "18491",
		"QDRANT_MEM_LIMIT":     "2g",
		"QDRANT_CPUS":          "2",
		"TEI_EMBED_HOST_PORT":  "18492",
		"TEI_EMBED_MEM_LIMIT":  "4g",
		"TEI_EMBED_CPUS":       "4",
		"TEI_EMBED_MODEL_ID":   "BAAI/bge-small-en-v1.5",
		"TEI_RERANK_HOST_PORT": "18493",
		"TEI_RERANK_MEM_LIMIT": "1g",
		"TEI_RERANK_CPUS":      "1",
		"TEI_RERANK_MODEL_ID":  "BAAI/bge-reranker-base",
	}
	for k := range env {
		if existing := os.Getenv(k); existing != "" {
			env[k] = existing
		}
		t.Setenv(k, env[k])
	}
	qdrantBase := "http://localhost:" + env["QDRANT_HTTP_PORT"]
	teiEmbedBase := "http://localhost:" + env["TEI_EMBED_HOST_PORT"]

	// Reuse the SAME compose file the reranker wave already proved
	// (§11.4.74) — only boot qdrant + tei-embed (tei-rerank is not needed
	// by this fix's proof).
	composeFile, err := filepath.Abs("testdata/rag_rerank_pipeline_live.compose.yml")
	if err != nil {
		t.Fatalf("resolve compose file: %v", err)
	}
	project := compose.ComposeProject{
		Name:     "helixllmqdrantchunkidfix",
		File:     composeFile,
		Services: []string{"qdrant", "tei-embed"},
	}

	orch, err := compose.NewDefaultOrchestrator(filepath.Dir(composeFile), nil)
	if err != nil {
		t.Fatalf("orchestrator: %v", err)
	}

	teardown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := orch.Down(ctx, project,
			compose.WithDownRemoveVolumes(false), // keep the shared HF-model cache volume
			compose.WithDownRemoveOrphans(true),
		); err != nil {
			logf("teardown WARNING: compose down: %v", err)
		} else {
			logf("teardown OK: qdrant+tei-embed removed (shared cache volume preserved)")
		}
	}
	t.Cleanup(teardown)
	// Ensure the fixed source is back on disk no matter how the test exits
	// (panic, t.Fatal, or normal return) — never leave the repo mid-RED.
	t.Cleanup(func() {
		if err := os.WriteFile(qdrantGoPath, fixedSource, 0o644); err != nil {
			t.Errorf("CRITICAL: failed to restore fixed qdrant.go after test: %v", err)
			return
		}
		if out, err := exec.Command("gofmt", "-l", qdrantGoPath).CombinedOutput(); err != nil || len(strings.TrimSpace(string(out))) > 0 {
			logf("post-restore gofmt check: err=%v out=%s", err, string(out))
		}
	})
	// Pre-clean any stale project from a prior interrupted run.
	{
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = orch.Down(ctx, project, compose.WithDownRemoveVolumes(false), compose.WithDownRemoveOrphans(true))
		cancel()
	}

	logf("booting qdrant+tei-embed(%s) via containers submodule orchestrator (§11.4.76, rootless §11.4.161)", env["TEI_EMBED_MODEL_ID"])
	{
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if err := orch.Up(ctx, project, compose.WithUpDetach(true), compose.WithRemoveOrphans(true)); err != nil {
			t.Fatalf("compose up: %v", err)
		}
	}

	if err := pollHealth2(qdrantBase, teiEmbedBase, 6*time.Minute); err != nil {
		logf("BLOCKED: %v", err)
		t.Fatalf("services did not become healthy: %v", err)
	}
	logf("health OK: qdrant=%s tei-embed=%s", qdrantBase, teiEmbedBase)

	// ==================================================================
	// RED — revert qdrant.go to its pre-fix content, rebuild the test
	// binary implicitly via `go run`, and reproduce the historical
	// rejection against the REAL Qdrant server booted above
	// (§11.4.115 RED-baseline-on-the-broken-artifact).
	// ==================================================================
	preFixSource := buildPreFixQdrantGoSource(string(fixedSource))
	if preFixSource == string(fixedSource) {
		t.Fatalf("preFixSource transform produced no change — RED reconstruction is broken, refusing to run a blind RED")
	}
	if err := os.WriteFile(qdrantGoPath, []byte(preFixSource), 0o644); err != nil {
		t.Fatalf("write pre-fix qdrant.go for RED phase: %v", err)
	}
	logf("RED phase: internal/knowledge/qdrant.go reverted to its pre-fix content on disk")

	redCollection := "helixrag_chunkidfix_red_" + time.Now().UTC().Format("20060102T150405Z")
	redOut, redErr := runQdrantIngestSubprocess(repoRoot, teiEmbedBase, env["TEI_EMBED_MODEL_ID"], qdrantBase[len("http://localhost:"):], redCollection)
	_ = os.WriteFile(filepath.Join(evidDir, "10_red_qdrant_ingest_reject.txt"), []byte(redOut), 0o644)
	logf("RED result: subprocess exit=%v", redErr)
	logf("RED captured output:\n%s", redOut)
	if redErr == nil {
		t.Fatalf("RED FAILED TO REPRODUCE: Pipeline.Ingest succeeded against the pre-fix qdrant.go — the RED reconstruction does not match the historical defect; refusing to claim a fix for a bug that did not reproduce")
	}
	if !strings.Contains(redOut, "INGEST_FAILED") {
		t.Fatalf("RED subprocess did not fail in the expected place (INGEST_FAILED marker absent):\n%s", redOut)
	}
	logf("RED CONFIRMED: real Pipeline.Ingest into real Qdrant rejected the pre-fix chunk-ID shape, exactly as documented in docs/qa/reranker_wave2_20260711T154720Z/RESULTS.md")

	// ==================================================================
	// GREEN — restore the fix, re-run the SAME Ingest against a FRESH
	// collection on the SAME live Qdrant server, then Query end-to-end.
	// ==================================================================
	if err := os.WriteFile(qdrantGoPath, fixedSource, 0o644); err != nil {
		t.Fatalf("restore fixed qdrant.go for GREEN phase: %v", err)
	}
	logf("GREEN phase: internal/knowledge/qdrant.go restored to the fixed content on disk")

	greenCollection := "helixrag_chunkidfix_green_" + time.Now().UTC().Format("20060102T150405Z")
	greenOut, greenErr := runQdrantIngestSubprocess(repoRoot, teiEmbedBase, env["TEI_EMBED_MODEL_ID"], qdrantBase[len("http://localhost:"):], greenCollection)
	_ = os.WriteFile(filepath.Join(evidDir, "11_green_qdrant_ingest_query.txt"), []byte(greenOut), 0o644)
	logf("GREEN result: subprocess exit=%v", greenErr)
	logf("GREEN captured output:\n%s", greenOut)
	if greenErr != nil {
		t.Fatalf("GREEN FAILED: fixed qdrant.go still could not Ingest+Query real Qdrant: %v\n%s", greenErr, greenOut)
	}
	if !strings.Contains(greenOut, "INGEST_OK") {
		t.Fatalf("GREEN subprocess did not report INGEST_OK:\n%s", greenOut)
	}
	if !strings.Contains(greenOut, "QUERY_RETRIEVED_ALL_DOCS") {
		t.Fatalf("GREEN subprocess did not report QUERY_RETRIEVED_ALL_DOCS (end-to-end retrieval not proven):\n%s", greenOut)
	}
	logf("GREEN CONFIRMED: real Pipeline.Ingest of a %d-doc corpus succeeded against real Qdrant (no rejection), and real Pipeline.Query retrieved the ingested chunks end-to-end.", len(qdrantIngestCorpus))

	logf("RESULT: PASS — chunk-ID -> Qdrant-point-ID mapping fix proven RED (pre-fix rejection reproduced on live Qdrant) -> GREEN (post-fix production Pipeline.Ingest -> Qdrant upsert -> Pipeline.Query works end-to-end).")
}

// buildPreFixQdrantGoSource reconstructs the EXACT pre-fix content of
// internal/knowledge/qdrant.go by removing the fix's ID-mapping helpers and
// reverting Upsert/Search/Delete/chunkFromMetadata to their original bodies.
// This mirrors the reranker wave's `git stash` RED technique (see
// docs/qa/reranker_wave2_20260711T154720Z/RESULTS.md) without depending on
// git working-tree state, so this test is re-runnable regardless of what
// else is staged/committed in a concurrent track's checkout.
func buildPreFixQdrantGoSource(fixed string) string {
	const preFix = `package knowledge

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
`
	return preFix
}

// runQdrantIngestSubprocess runs a throwaway `go run` subprocess (a real,
// separate OS process, so the currently-on-disk internal/knowledge/qdrant.go
// content — RED or GREEN — is genuinely compiled and exercised, not merely
// imported from an already-built test binary) that performs a real
// knowledge.Pipeline.Ingest of the multi-doc corpus followed by a real
// knowledge.Pipeline.Query, against the live tei-embed + Qdrant servers
// booted by the parent test. Returns combined stdout+stderr and the process
// error (non-nil on any failure, including a real Qdrant rejection).
func runQdrantIngestSubprocess(repoRoot, teiEmbedBase, embedModel, qdrantHostPort, collection string) (string, error) {
	harnessPath := filepath.Join(repoRoot, "tests", "integration", "testdata", "qdrant_ingest_harness")
	cmd := exec.Command("go", "run", harnessPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"HARNESS_TEI_EMBED_BASE="+teiEmbedBase,
		"HARNESS_TEI_EMBED_MODEL="+embedModel,
		"HARNESS_QDRANT_HOST_PORT="+qdrantHostPort,
		"HARNESS_QDRANT_COLLECTION="+collection,
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// pollHealth2 polls qdrant/tei-embed health endpoints until both report 200
// or the deadline elapses. Distinct name from pollHealth (already defined in
// rag_rerank_pipeline_live_test.go, same package) to avoid a redeclaration.
func pollHealth2(qdrantBase, teiEmbedBase string, timeout time.Duration) error {
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(timeout)
	var lastQ, lastE int
	for time.Now().Before(deadline) {
		lastQ = probeStatus(client, qdrantBase+"/collections")
		lastE = probeStatus(client, teiEmbedBase+"/health")
		if lastQ == http.StatusOK && lastE == http.StatusOK {
			return nil
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("health poll timed out after %s (last: qdrant=%d tei-embed=%d)", timeout, lastQ, lastE)
}
