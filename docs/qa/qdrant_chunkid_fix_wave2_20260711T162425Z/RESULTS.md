# FIX 1 — Qdrant chunk-ID / point-ID mapping (Wave-2, highest value: completes RAG-Qdrant end-to-end)

**Assignment:** `FixedSizeChunker` mints chunk IDs like `"<uuid>-<n>"`, which
`QdrantStore.Upsert` sends verbatim as the Qdrant point `id`. Qdrant REST
requires a point ID to be an unsigned 64-bit integer or a UUID
(https://qdrant.tech/documentation/concepts/points/#point-ids) — the
`"<uuid>-<n>"` shape is neither, so the whole batch upsert is rejected and a
production `Pipeline.Ingest → Qdrant` call silently loses every chunk.

**Fix chosen (boundary-mapping, not chunker-widening):**
`internal/knowledge/qdrant.go` now maps every chunk ID to a Qdrant-valid
point ID AT THE STORE BOUNDARY: `qdrantPointID(chunkID)` passes already-valid
IDs (unsigned integer or real UUID) through unchanged, and derives a
deterministic UUIDv5 (`uuid.NewSHA1(uuid.NameSpaceOID, []byte(chunkID))`) for
everything else — same chunk ID always maps to the same point ID (idempotent
re-ingestion, no duplicate leakage on document edits). The original semantic
chunk ID is preserved in the point payload (`chunk_id` field) so
`Search`/`chunkFromMetadata` hands back the ID the chunk was ingested with,
not Qdrant's internal point ID. `Delete` was fixed identically (it shared the
same root cause: deleting by the un-mapped semantic ID would silently no-op
against Qdrant). Boundary-mapping was chosen over widening `FixedSizeChunker`'s
ID format because MemoryStore and every other `VectorStore` backend have no
such ID-shape constraint and already rely on the current human-readable
`"<documentID>-<position>"` shape — changing the chunker would be a needless,
wider-blast-radius change for backends that were never broken.

**New finding fixed alongside (same file/function, surfaced while verifying
the fix end-to-end):** `chunkFromMetadata`'s `Position` field read used a bare
`.(int)` type assertion, which ALWAYS silently fails on a real Qdrant
`Search()` round-trip (`encoding/json` decodes JSON numbers into `map[string]any`
as `float64`, never `int`) — `Position` was zero on every real retrieval.
Fixed to handle both `float64` and `int`.

## Live RED → GREEN proof

Run: `HELIX_LIVE_RAG_QDRANT_INGEST_TEST=true go test ./tests/integration/ -run TestRAGQdrantChunkIDFix_LiveEndToEnd -v`
(`tests/integration/rag_qdrant_chunkid_fix_live_test.go`). Boots real
`qdrant` + `tei-embed` (BAAI/bge-small-en-v1.5) via the containers-submodule
orchestrator (§11.4.76), rootless podman (§11.4.161), on ports
18490-18492 (distinct from the reranker-wave harness's 18480-18483 and from
the live coder :18434, which is read-only and untouched throughout — verified
via `Available()`/`/health` before and after). RED phase physically reverts
`internal/knowledge/qdrant.go` to its exact pre-fix content (verified
byte-identical to `git show HEAD:internal/knowledge/qdrant.go` before this
run) and drives `Pipeline.Ingest` in a fresh `go run` subprocess against the
live Qdrant server; GREEN phase restores the fix and re-runs the same
Ingest+Query sequence against a fresh collection on the same server. The real
terminal transcript below (this run) is the proof:

### TestRAGQdrantChunkIDFix_LiveEndToEnd — 2026-07-11T16:24:25Z UTC
run_id=qdrant_chunkid_fix_wave2_20260711T162425Z
§11.4.119 single-resource-owner: coder :18434 is read-only and untouched by this test.
booting qdrant+tei-embed(BAAI/bge-small-en-v1.5) via containers submodule orchestrator (§11.4.76, rootless §11.4.161)
health OK: qdrant=http://localhost:18490 tei-embed=http://localhost:18492
RED phase: internal/knowledge/qdrant.go reverted to its pre-fix content on disk
RED result: subprocess exit=exit status 1
RED captured output:
INGEST_FAILED: doc=doc_alpha err=ingest: store upsert: qdrant upsert: failed to upsert points: request failed with status 400: {"status":{"error":"Format error in JSON body: value d6cdd29d-f893-4b8d-be1c-4094bfe15350-0 is not a valid point ID, valid values are either an unsigned integer or a UUID"},"time":0.0}
exit status 1

RED CONFIRMED: real Pipeline.Ingest into real Qdrant rejected the pre-fix chunk-ID shape, exactly as documented in docs/qa/reranker_wave2_20260711T154720Z/RESULTS.md
GREEN phase: internal/knowledge/qdrant.go restored to the fixed content on disk
GREEN result: subprocess exit=<nil>
GREEN captured output:
INGEST_OK: ingested 5 docs into collection helixrag_chunkidfix_green_20260711T162431Z
QUERY_OK: retrieved 5 chunks
QUERY_DOC_FOUND: doc_alpha
QUERY_DOC_FOUND: doc_beta
QUERY_DOC_FOUND: doc_gamma
QUERY_DOC_FOUND: doc_delta
QUERY_DOC_FOUND: doc_epsilon
QUERY_RETRIEVED_ALL_DOCS

GREEN CONFIRMED: real Pipeline.Ingest of a 5-doc corpus succeeded against real Qdrant (no rejection), and real Pipeline.Query retrieved the ingested chunks end-to-end.
RESULT: PASS — chunk-ID -> Qdrant-point-ID mapping fix proven RED (pre-fix rejection reproduced on live Qdrant) -> GREEN (post-fix production Pipeline.Ingest -> Qdrant upsert -> Pipeline.Query works end-to-end).

## Post-run environment integrity (§11.4.119/§11.4.122)

`podman ps -a` shows no residual `qdrant`/`tei-embed` containers after
teardown; `GET http://127.0.0.1:18434/health` returned `200` both before and
after this run — the live coder was never touched.

## Fast, always-on unit coverage (complementing the opt-in live test)

`internal/knowledge/qdrant_pointid_test.go` — five hermetic, no-network unit
tests covering `qdrantPointID`/`isValidQdrantPointID`/`chunkFromMetadata`
directly, run on every `go test ./...` (not gated behind an opt-in env var):
`TestQdrantPointID_AlreadyValidIDs_PassThroughUnchanged`,
`TestQdrantPointID_ChunkerShapedIDs_MappedToValidUUID` (proves determinism +
no collisions across sample IDs),
`TestQdrantPointID_EmptyID_TreatedAsInvalid`,
`TestChunkFromMetadata_RoundTripsSemanticChunkID` (also covers the adjacent
`Position` float64/int fix), `TestChunkFromMetadata_NoStoredChunkID_FallsBackToPointID`
(backward-compat with pre-fix-upserted points). All PASS (`-count=1` fresh run):

```
=== RUN   TestQdrantPointID_AlreadyValidIDs_PassThroughUnchanged
--- PASS: TestQdrantPointID_AlreadyValidIDs_PassThroughUnchanged (0.00s)
=== RUN   TestQdrantPointID_ChunkerShapedIDs_MappedToValidUUID
--- PASS: TestQdrantPointID_ChunkerShapedIDs_MappedToValidUUID (0.00s)
=== RUN   TestQdrantPointID_EmptyID_TreatedAsInvalid
--- PASS: TestQdrantPointID_EmptyID_TreatedAsInvalid (0.00s)
=== RUN   TestChunkFromMetadata_RoundTripsSemanticChunkID
--- PASS: TestChunkFromMetadata_RoundTripsSemanticChunkID (0.00s)
=== RUN   TestChunkFromMetadata_NoStoredChunkID_FallsBackToPointID
--- PASS: TestChunkFromMetadata_NoStoredChunkID_FallsBackToPointID (0.00s)
PASS
ok  	github.com/HelixDevelopment/HelixLLM/internal/knowledge	0.007s
```

## §1.1 load-bearing mutations (this session)

1. **Unit-level**: mutated `qdrantPointID` to `if true { // MUTATED ...
   return chunkID }` (always pass through unmapped, i.e. simulate the
   pre-fix behaviour). Re-ran `TestQdrantPointID_ChunkerShapedIDs_MappedToValidUUID`:

   ```
   qdrant_pointid_test.go:50: qdrantPointID("19bd9769-950a-43c5-9a41-e3cc875385dc-0") = "19bd9769-950a-43c5-9a41-e3cc875385dc-0", not a valid UUID: invalid UUID format
   ...
   --- FAIL: TestQdrantPointID_ChunkerShapedIDs_MappedToValidUUID (0.00s)
   FAIL
   ```

   Restored (`grep -c MUTATED` → `0`), re-ran → PASS. Load-bearing confirmed.

2. **System-level**: the RED phase above (physically reverting
   `internal/knowledge/qdrant.go` to its exact pre-fix content, byte-verified
   against `git show HEAD:...`) IS itself a live-executed full-file mutation
   of the fix, run against a REAL Qdrant server — the strongest possible
   §1.1 pairing for this defect class, since it reproduces the ACTUAL
   historical HTTP 400 from the real upstream, not a synthetic assertion.
