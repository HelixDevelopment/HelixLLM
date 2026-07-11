// Package integration_test — Wave-2 production-path proof for the
// RERANKER-NOT-WIRED V&V finding (2026-07-11).
//
// Prior state: the RAG cross-encoder reranker (TEI/bge) was proven working
// only inside a standalone QA harness
// (docs/qa/phase3_rag_qdrant_rerank_20260711T142237Z/harness/main.go) that
// re-implemented the embed/retrieve/rerank/ground pipeline by hand — the
// REAL production knowledge.Pipeline.Query never called any Reranker.
//
// This test proves the FIX: it drives the REAL, unmodified
// knowledge.Pipeline.Query (imported from internal/knowledge, not
// reimplemented) against a REAL Qdrant vector store + REAL HuggingFace
// Text-Embeddings-Inference (TEI) tei-embed + tei-rerank containers, and
// empirically demonstrates a concrete case where the raw bi-encoder ANN
// retrieval order is WRONG and the production Pipeline.Query — now that the
// reranker is wired — corrects it via a REAL cross-encoder call.
//
// It reuses the exact adversarial fixture corpus + queries (q3, q4) already
// empirically validated in docs/qa/rag_qdrant_liveproof_20260711T142237Z
// (§11.4.74 reuse-before-reimplement, §11.4.82 iteration-speedup) — that
// prior run recorded (11_pipeline_q3.txt / 11_pipeline_q4.txt):
//
//	q3: real ANN top1=doc_fact_collection(WRONG) -> reranked top1=doc_fact_primary(CORRECT)
//	q4: real ANN top1=doc_distractor_deprecated(WRONG) -> reranked top1=doc_fact_active(CORRECT)
//
// Gated behind HELIX_LIVE_RAG_RERANK_TEST=true — honestly SKIPs otherwise
// (§11.4.3). Boots/tears down qdrant+tei-embed+tei-rerank via the containers
// submodule orchestrator (§11.4.76), rootless podman (§11.4.161), CPU-only
// throughout (§11.4.119 — the concurrent video-analysis stream owns the
// GPU and is never touched here). The live coder (:18434, read-only, never
// restarted per §11.4.122) is not used by this test at all.
package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"digital.vasic.containers/pkg/compose"
	"github.com/google/uuid"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

type rerankCorpusDoc struct {
	ID   string
	Text string
}

// Reused verbatim from the already-proven adversarial fixture at
// docs/qa/phase3_rag_qdrant_rerank_20260711T142237Z/harness/main.go
// (§11.4.74) — q3/q4 fact+distractor pairs plus filler/noise docs so
// retrieval is not trivially a 2-document choice.
var rerankCorpus = []rerankCorpusDoc{
	{"doc_fact_primary", "HelixCode's primary telemetry index is served from Qdrant collection alias Cindervale-Prime."},
	{"doc_distractor_staging", "HelixCode's staging telemetry index is the non-primary counterpart of the primary telemetry index; this staging telemetry index is a separate Qdrant collection kept apart from the primary telemetry index and used only for pre-release testing of the telemetry index."},
	{"doc_fact_active", "The active production embeddings registry is published under Qdrant alias Emberkiln-Live."},
	{"doc_distractor_deprecated", "The deprecated sandbox embeddings registry is the non-production sibling of the active production embeddings registry; this deprecated embeddings registry is a separate Qdrant alias, retained only for archival and never used as the active production embeddings registry."},
	{"doc_cat", "The cat sat on the mat."},
	{"doc_coffee", "Coffee brewing techniques improve flavor extraction using controlled water temperature."},
	{"doc_revenue", "Quarterly revenue rose four percent."},
	{"doc_ci", "The HelixCode continuous integration pipeline runs unit tests before every merge to main."},
}

type rerankQueryFixture struct {
	Key           string
	Text          string
	ExpectDocID   string
	WrongDistract string
}

var rerankQueries = []rerankQueryFixture{
	{
		Key:           "q3",
		Text:          "Which Qdrant collection alias serves HelixCode's primary telemetry index?",
		ExpectDocID:   "doc_fact_primary",
		WrongDistract: "doc_distractor_staging",
	},
	{
		Key:           "q4",
		Text:          "Which Qdrant alias holds HelixCode's active production embeddings registry?",
		ExpectDocID:   "doc_fact_active",
		WrongDistract: "doc_distractor_deprecated",
	},
}

func TestRAGRerankPipeline_LiveEndToEnd(t *testing.T) {
	if os.Getenv("HELIX_LIVE_RAG_RERANK_TEST") != "true" {
		t.Skip("SKIP-OK: opt-in live test (requires containers submodule + rootless podman + tei-embed/tei-rerank/qdrant images). " +
			"Set HELIX_LIVE_RAG_RERANK_TEST=true to run.")
	}

	// repoRoot resolves to submodules/helix_llm (2 levels up from
	// tests/integration/) — evidence stays WITHIN this submodule's own
	// docs/qa/ tree per this stream's file-scope constraint.
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	runID := "reranker_wave2_" + time.Now().UTC().Format("20060102T150405Z")
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

	logf("### TestRAGRerankPipeline_LiveEndToEnd — %s UTC", time.Now().UTC().Format(time.RFC3339))
	logf("run_id=%s", runID)
	logf("§11.4.119 single-resource-owner: coder :18434 is read-only and untouched by this test.")

	// ---- config injection (§CONST-045/046 — no hardcoded ports beyond
	// this local map, distinct from every other lane on the host) ----
	env := map[string]string{
		"QDRANT_HTTP_PORT":     "18480",
		"QDRANT_GRPC_PORT":     "18481",
		"QDRANT_MEM_LIMIT":     "2g",
		"QDRANT_CPUS":          "2",
		"TEI_EMBED_HOST_PORT":  "18482",
		"TEI_EMBED_MEM_LIMIT":  "4g",
		"TEI_EMBED_CPUS":       "4",
		"TEI_EMBED_MODEL_ID":   "BAAI/bge-small-en-v1.5",
		"TEI_RERANK_HOST_PORT": "18483",
		"TEI_RERANK_MEM_LIMIT": "4g",
		"TEI_RERANK_CPUS":      "4",
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
	teiRerankBase := "http://localhost:" + env["TEI_RERANK_HOST_PORT"]

	composeFile, err := filepath.Abs("testdata/rag_rerank_pipeline_live.compose.yml")
	if err != nil {
		t.Fatalf("resolve compose file: %v", err)
	}
	project := compose.ComposeProject{
		Name:     "helixllmragrerank",
		File:     composeFile,
		Services: []string{"qdrant", "tei-embed", "tei-rerank"},
	}

	orch, err := compose.NewDefaultOrchestrator(filepath.Dir(composeFile), nil)
	if err != nil {
		t.Fatalf("orchestrator: %v", err)
	}

	teardown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := orch.Down(ctx, project,
			compose.WithDownRemoveVolumes(false), // keep the shared HF-model cache volumes
			compose.WithDownRemoveOrphans(true),
		); err != nil {
			logf("teardown WARNING: compose down: %v", err)
		} else {
			logf("teardown OK: qdrant+tei-embed+tei-rerank removed (shared cache volumes preserved)")
		}
	}
	t.Cleanup(teardown)
	// Pre-clean any stale project from a prior interrupted run.
	{
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = orch.Down(ctx, project, compose.WithDownRemoveVolumes(false), compose.WithDownRemoveOrphans(true))
		cancel()
	}

	logf("booting qdrant+tei-embed(%s)+tei-rerank(%s) via containers submodule orchestrator (§11.4.76, rootless §11.4.161)",
		env["TEI_EMBED_MODEL_ID"], env["TEI_RERANK_MODEL_ID"])
	{
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if err := orch.Up(ctx, project, compose.WithUpDetach(true), compose.WithRemoveOrphans(true)); err != nil {
			t.Fatalf("compose up: %v", err)
		}
	}

	if err := pollHealth(qdrantBase, teiEmbedBase, teiRerankBase, 6*time.Minute); err != nil {
		logf("BLOCKED: %v", err)
		t.Fatalf("services did not become healthy: %v", err)
	}
	logf("health OK: qdrant=%s tei-embed=%s tei-rerank=%s", qdrantBase, teiEmbedBase, teiRerankBase)

	qdrantPort := 18480
	store, err := knowledge.NewQdrantStore("localhost", qdrantPort)
	if err != nil {
		t.Fatalf("connect real QdrantStore: %v", err)
	}
	embedder := knowledge.NewLlamaEmbedder(teiEmbedBase, env["TEI_EMBED_MODEL_ID"], 384)
	chunker := knowledge.NewFixedSizeChunker(2000, 0)
	collection := "helixrag_wave2_" + time.Now().UTC().Format("20060102T150405Z")

	// ---- REAL production Pipeline WITHOUT a reranker: this is the exact
	// pre-fix production configuration every caller had before this wave —
	// establishes the RED baseline (raw bi-encoder ANN order) empirically,
	// never assumed (§11.4.6). ----
	rawPipeline := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          embedder,
		Store:             store,
		Chunker:           chunker,
		DefaultCollection: collection,
		DefaultTopK:       len(rerankCorpus),
	})

	ctx := context.Background()

	// NOTE (honest, out-of-scope discovery — NOT part of this wave's three
	// assigned findings, so NOT fixed here per the task's scope discipline):
	// knowledge.Pipeline.Ingest -> FixedSizeChunker always mints chunk IDs
	// as "<docUUID>-<position>" (see internal/knowledge/chunker.go), which
	// is NOT a valid Qdrant point ID (Qdrant requires an unsigned integer
	// or a bare UUID) — Ingest() into a REAL QdrantStore fails with
	// "value <uuid>-0 is not a valid point ID". This reproduces
	// unconditionally (verified empirically this run) regardless of the
	// reranker fix under test here. Documented for a future, separately
	// tracked V&V pass; worked around below by upserting chunks directly
	// through the SAME real, public knowledge.VectorStore.Upsert API that
	// Pipeline.Ingest itself calls internally, using valid bare-UUID chunk
	// IDs, so the production Pipeline.Query path under test is unaffected.
	chunks := make([]knowledge.Chunk, 0, len(rerankCorpus))
	for _, doc := range rerankCorpus {
		vec, err := embedder.Embed(doc.Text)
		if err != nil {
			t.Fatalf("embed %s via real tei-embed: %v", doc.ID, err)
		}
		chunks = append(chunks, knowledge.Chunk{
			ID:         uuid.NewString(),
			DocumentID: uuid.NewString(),
			Content:    doc.Text,
			Embedding:  vec,
		})
	}
	if err := store.Upsert(collection, chunks); err != nil {
		t.Fatalf("upsert corpus into real Qdrant: %v", err)
	}
	logf("ingested %d corpus docs into REAL Qdrant collection %s (real tei-embed embeddings, real Qdrant upsert)", len(rerankCorpus), collection)

	// ---- REAL production Pipeline WITH the TEIReranker wired in exactly
	// as cmd/helixllm/main.go now wires it (same Store/Embedder/collection,
	// so the ONLY difference from rawPipeline is the reranker). ----
	rerankedPipeline := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          embedder,
		Store:             store,
		Chunker:           chunker,
		DefaultCollection: collection,
		DefaultTopK:       len(rerankCorpus),
		Reranker:          knowledge.NewTEIReranker(teiRerankBase),
	})

	improvementDemonstrated := false
	for _, q := range rerankQueries {
		rawResult, err := rawPipeline.Query(ctx, knowledge.QueryRequest{
			Query: q.Text, Collection: collection, TopK: len(rerankCorpus),
		})
		if err != nil {
			t.Fatalf("qkey=%s: raw (unreranked) Query error: %v", q.Key, err)
		}
		if len(rawResult.Chunks) == 0 {
			t.Fatalf("qkey=%s: raw Query returned zero chunks", q.Key)
		}
		rawTop1DocID := docIDForContent(rawResult.Chunks[0].Content)

		rerankedResult, err := rerankedPipeline.Query(ctx, knowledge.QueryRequest{
			Query: q.Text, Collection: collection, TopK: 3,
		})
		if err != nil {
			t.Fatalf("qkey=%s: reranked Query error: %v", q.Key, err)
		}
		if len(rerankedResult.Chunks) == 0 {
			t.Fatalf("qkey=%s: reranked Query returned zero chunks", q.Key)
		}
		rerankedTop1DocID := docIDForContent(rerankedResult.Chunks[0].Content)

		logf("qkey=%s: raw(unreranked-production-Pipeline.Query) top1=%s (want=%s) -> reranked(production-Pipeline.Query+TEIReranker) top1=%s",
			q.Key, rawTop1DocID, q.ExpectDocID, rerankedTop1DocID)

		if rerankedTop1DocID != q.ExpectDocID {
			t.Errorf("qkey=%s: reranked production Pipeline.Query top1=%s, want %s — reranker did not ground the correct doc",
				q.Key, rerankedTop1DocID, q.ExpectDocID)
		}

		if rawTop1DocID != q.ExpectDocID && rerankedTop1DocID == q.ExpectDocID {
			improvementDemonstrated = true
			logf("qkey=%s: RERANK-IMPROVES-ORDERING DEMONSTRATED on the REAL production Pipeline.Query "+
				"(raw ANN top-1 was %s [WRONG], real cross-encoder reranking promoted %s [CORRECT] to top-1)",
				q.Key, rawTop1DocID, q.ExpectDocID)
		}
	}

	// PRODUCTION-PATH PROOF (the deliverable's concrete case): at least one
	// query must empirically demonstrate that the wired-in reranker
	// corrected a raw-retrieval ordering mistake through the REAL,
	// unmodified Pipeline.Query — never assumed, always observed this run.
	if !improvementDemonstrated {
		logf("BLOCKED-with-reason: no query this run demonstrated raw-order-wrong -> reranked-order-correct " +
			"on the production Pipeline.Query path (see per-query lines above for the actual observed orders)")
		t.Fatal("rerank-improves-ordering was not empirically demonstrated by the production Pipeline.Query this run")
	}

	logf("RESULT: PASS — production knowledge.Pipeline.Query now performs cross-encoder reranking end-to-end " +
		"against a real Qdrant store + real TEI tei-embed/tei-rerank, and at least one adversarial query proves " +
		"the reranker corrects a raw bi-encoder ordering mistake.")
}

// docIDForContent maps a retrieved chunk's Content back to the corpus doc ID
// it came from. With ChunkSize=2000/Overlap=0 and every corpus doc well
// under 2000 chars, FixedSizeChunker.Chunk emits exactly one chunk per doc
// whose Content is byte-identical to the source doc's Content, so an exact
// match is reliable (verified against internal/knowledge/chunker.go).
func docIDForContent(content string) string {
	for _, doc := range rerankCorpus {
		if doc.Text == content {
			return doc.ID
		}
	}
	return "UNKNOWN:" + content
}

// pollHealth polls qdrant/tei-embed/tei-rerank health endpoints until all
// three report 200 or the deadline elapses.
func pollHealth(qdrantBase, teiEmbedBase, teiRerankBase string, timeout time.Duration) error {
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(timeout)
	var lastQ, lastE, lastR int
	for time.Now().Before(deadline) {
		lastQ = probeStatus(client, qdrantBase+"/collections")
		lastE = probeStatus(client, teiEmbedBase+"/health")
		lastR = probeStatus(client, teiRerankBase+"/health")
		if lastQ == http.StatusOK && lastE == http.StatusOK && lastR == http.StatusOK {
			return nil
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("health poll timed out after %s (last: qdrant=%d tei-embed=%d tei-rerank=%d)",
		timeout, lastQ, lastE, lastR)
}

func probeStatus(client *http.Client, url string) int {
	resp, err := client.Get(url) //nolint:gosec // localhost test endpoint
	if err != nil {
		return 0
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}
