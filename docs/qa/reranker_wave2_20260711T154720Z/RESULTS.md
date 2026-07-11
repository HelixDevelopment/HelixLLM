# RERANKER-NOT-WIRED — Wave-2 fix, RED→GREEN, production-path proof

**Finding (prior docs-V&V stream):** the RAG cross-encoder reranker (TEI/bge)
was proven only in a standalone QA harness
(`docs/qa/phase3_rag_qdrant_rerank_20260711T142237Z/harness/main.go`);
`internal/knowledge.Pipeline.Query` never called it — production RAG queries
got NO cross-encoder reranking regardless of configuration.

**Verification (§11.4.6):** CONFIRMED TRUE by direct source read.
- `internal/knowledge/pipeline.go` `Query()` never referenced any `Reranker`.
- `internal/knowledge/hybrid.go` `HybridRetriever.SetReranker` existed but
  was never called by `NewPipeline` (dead wiring hook).
- `cmd/helixllm/main.go` never constructed a `Reranker` at all.
- No production `Reranker` implementation spoke the TEI `/rerank` protocol —
  only the standalone harness's throwaway `cmdRerank()` did.

## Fix

1. `internal/knowledge/reranker.go` — added `TEIReranker`, a production
   `Reranker` implementation calling a real TEI `/rerank` endpoint
   (`{"query":...,"texts":[...]}` → `[{"index":N,"score":F}]`), mirroring the
   protocol already proven live in the prior harness. A batch failure
   (unreachable / non-200 / malformed / out-of-range index) is returned as an
   error — never silently degrades to unranked order (§11.4.1/§11.4.6).
2. `internal/knowledge/pipeline.go` — added `Reranker` +
   `RerankFetchMultiplier` to `PipelineConfig`; `Query()` now: (a) over-fetches
   `topK * multiplier` candidates when a reranker is configured (embed →
   retrieve MORE → RERANK → trim to topK → ground), on BOTH the hybrid and
   plain vector-only retrieval paths; (b) applies `Reranker.Rerank` as the
   final ordering step before `MinScore` filtering; (c) is a **byte-for-byte
   no-op** when `Reranker` is nil — `fetchK == topK`, defaults preserved,
   zero behaviour change for every existing caller.
3. `internal/shared/config/config.go` — added
   `HELIX_RAG_RERANK_ENABLED` / `HELIX_RAG_RERANK_BASE_URL` /
   `HELIX_RAG_RERANK_FETCH_MULTIPLIER` to `KnowledgeConfig`.
4. `cmd/helixllm/main.go` — production entrypoint now constructs a
   `knowledge.NewTEIReranker(cfg.Knowledge.RerankBaseURL)` and passes it into
   `PipelineConfig` when `HELIX_RAG_RERANK_ENABLED=true`.

## RED (§11.4.115) — reproduce-first on the pre-fix artifact

Ran the exact test source below against `pipeline.go` with the wiring
neutered (struct fields present but `Query()` never consults `p.reranker`,
reproducing the historical defect at the runtime level):

```
=== RUN   TestPipeline_Query_AppliesReranker_NonHybrid
    pipeline_test.go:628: Pipeline.Query never called the configured Reranker (RERANKER-NOT-WIRED regression)
--- FAIL: TestPipeline_Query_AppliesReranker_NonHybrid (0.00s)
=== RUN   TestPipeline_Query_AppliesReranker_Hybrid
    pipeline_test.go:672: Pipeline.Query (hybrid path) never called the configured Reranker (RERANKER-NOT-WIRED regression)
--- FAIL: TestPipeline_Query_AppliesReranker_Hybrid (0.00s)
=== RUN   TestPipeline_Query_RerankError_Propagates
    pipeline_test.go:698: expected Query to return an error when the configured Reranker fails, got nil
--- FAIL: TestPipeline_Query_RerankError_Propagates (0.00s)
=== RUN   TestPipeline_Query_NoReranker_BehaviourUnchanged
--- PASS: TestPipeline_Query_NoReranker_BehaviourUnchanged (0.00s)
FAIL
FAIL	github.com/HelixDevelopment/HelixLLM/internal/knowledge	0.011s
```

(An earlier, even more direct RED signal: reverting `pipeline.go` entirely
via `git stash` produced a COMPILE failure —
`unknown field Reranker in struct literal of type knowledge.PipelineConfig`
— proving the wiring mechanism did not exist at all pre-fix.)

## GREEN — same test source, fix restored

```
=== RUN   TestPipeline_Query_AppliesReranker_NonHybrid
--- PASS: TestPipeline_Query_AppliesReranker_NonHybrid (0.00s)
=== RUN   TestPipeline_Query_AppliesReranker_Hybrid
--- PASS: TestPipeline_Query_AppliesReranker_Hybrid (0.00s)
=== RUN   TestPipeline_Query_RerankError_Propagates
--- PASS: TestPipeline_Query_RerankError_Propagates (0.00s)
=== RUN   TestPipeline_Query_NoReranker_BehaviourUnchanged
--- PASS: TestPipeline_Query_NoReranker_BehaviourUnchanged (0.00s)
PASS
ok  	github.com/HelixDevelopment/HelixLLM/internal/knowledge	0.012s
```

Regression guard (§11.4.135): these four tests are now permanently part of
`internal/knowledge/pipeline_test.go` and run on every `go test ./...`.

## §1.1 load-bearing mutation

`TestTEIReranker_OutOfRangeIndex_ReturnsError` and
`TestTEIReranker_EmptyHitsArray_ReturnsError`
(`internal/knowledge/reranker_tei_test.go`) are the analyzer-self-validation
pair (§11.4.107(10)): a mutated `TEIReranker.Rerank` that silently ignores a
malformed/out-of-range TEI response (e.g. clamping the index instead of
erroring) is caught by these tests going from PASS→FAIL. Additionally, the
runtime-RED experiment above (temporarily neutering the `Query()` wiring
while keeping the config surface intact) IS a live-executed mutation of the
fix itself, and the four reranker-wiring tests correctly went 3-FAIL/1-PASS
— proving the tests are load-bearing against the exact defect class.

## LIVE production-path proof — real Qdrant + real TEI /rerank

`tests/integration/rag_rerank_pipeline_live_test.go` ::
`TestRAGRerankPipeline_LiveEndToEnd`, run with
`HELIX_LIVE_RAG_RERANK_TEST=true`. Boots `qdrant` + `tei-embed`
(BAAI/bge-small-en-v1.5) + `tei-rerank` (BAAI/bge-reranker-base) via the
containers submodule orchestrator (§11.4.76), rootless podman (§11.4.161),
CPU-only (§11.4.119 — GPU + live coder :18434 untouched throughout, verified
below). Reuses the exact adversarial fixture corpus/queries (q3, q4)
empirically validated in `docs/qa/rag_qdrant_liveproof_20260711T142237Z`
(§11.4.74 reuse-before-reimplement). Drives the REAL, unmodified
`internal/knowledge.Pipeline.Query` — imported as a package, not
reimplemented.

Terminal output (this run):

```
=== RUN   TestRAGRerankPipeline_LiveEndToEnd
    rag_rerank_pipeline_live_test.go:113: ### TestRAGRerankPipeline_LiveEndToEnd — 2026-07-11T15:47:20Z UTC
    rag_rerank_pipeline_live_test.go:113: run_id=reranker_wave2_20260711T154720Z
    rag_rerank_pipeline_live_test.go:113: §11.4.119 single-resource-owner: coder :18434 is read-only and untouched by this test.
    rag_rerank_pipeline_live_test.go:113: booting qdrant+tei-embed(BAAI/bge-small-en-v1.5)+tei-rerank(BAAI/bge-reranker-base) via containers submodule orchestrator (§11.4.76, rootless §11.4.161)
    rag_rerank_pipeline_live_test.go:113: health OK: qdrant=http://localhost:18480 tei-embed=http://localhost:18482 tei-rerank=http://localhost:18483
    rag_rerank_pipeline_live_test.go:113: ingested 8 corpus docs into REAL Qdrant collection helixrag_wave2_20260711T154728Z (real tei-embed embeddings, real Qdrant upsert)
    rag_rerank_pipeline_live_test.go:113: qkey=q3: raw(unreranked-production-Pipeline.Query) top1=doc_fact_primary (want=doc_fact_primary) -> reranked(production-Pipeline.Query+TEIReranker) top1=doc_fact_primary
    rag_rerank_pipeline_live_test.go:113: qkey=q4: raw(unreranked-production-Pipeline.Query) top1=doc_distractor_deprecated (want=doc_fact_active) -> reranked(production-Pipeline.Query+TEIReranker) top1=doc_fact_active
    rag_rerank_pipeline_live_test.go:113: qkey=q4: RERANK-IMPROVES-ORDERING DEMONSTRATED on the REAL production Pipeline.Query (raw ANN top-1 was doc_distractor_deprecated [WRONG], real cross-encoder reranking promoted doc_fact_active [CORRECT] to top-1)
    rag_rerank_pipeline_live_test.go:113: RESULT: PASS — production knowledge.Pipeline.Query now performs cross-encoder reranking end-to-end against a real Qdrant store + real TEI tei-embed/tei-rerank, and at least one adversarial query proves the reranker corrects a raw bi-encoder ordering mistake.
    rag_rerank_pipeline_live_test.go:113: teardown OK: qdrant+tei-embed+tei-rerank removed (shared cache volumes preserved)
--- PASS: TestRAGRerankPipeline_LiveEndToEnd (10.45s)
PASS
ok  	github.com/HelixDevelopment/HelixLLM/tests/integration	10.455s
```

**The concrete case (task deliverable):** for query q4 ("Which Qdrant alias
holds HelixCode's active production embeddings registry?"), the raw
unreranked production `Pipeline.Query` ranked the WRONG document
(`doc_distractor_deprecated`) at top-1. With the fix's `TEIReranker` wired
in via the SAME production `Pipeline.Query` call, the real BAAI/bge-reranker-base
cross-encoder correctly promoted `doc_fact_active` to top-1 — an empirically
observed, not assumed, correction of raw retrieval order.

### Post-run environment integrity (§11.4.119 / §11.4.122)

```
containers: helixllm-coder Up 3 hours   (untouched)
ports 18480-18483: free (torn down cleanly)
coder :18434 /v1/models: responsive after the run
```

### Honest, out-of-scope discovery (NOT one of this wave's 3 fixes)

The first attempt at this live proof (evidence directory
`reranker_wave2_20260711T154556Z`, since removed as superseded) used
`Pipeline.Ingest` directly and hit a genuine, pre-existing, ORTHOGONAL defect:
`internal/knowledge/chunker.go`'s `FixedSizeChunker` always mints chunk IDs as
`"<docUUID>-<position>"`, which is not a valid Qdrant point ID (Qdrant
requires an unsigned integer or a bare UUID) — `Ingest()` into a real
`QdrantStore` backend fails with `"value <uuid>-0 is not a valid point ID"`.
This reproduces unconditionally, independent of the reranker fix, and is
**not** one of the three findings this stream was assigned to fix. Worked
around in the live-proof test by upserting chunks directly through the same
real, public `VectorStore.Upsert` API with valid bare-UUID chunk IDs (still
100% real Qdrant + real TEI HTTP calls). Documented here for a future,
separately tracked V&V pass — NOT fixed in this commit (scope discipline).
