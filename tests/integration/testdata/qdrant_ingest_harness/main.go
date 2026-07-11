// Package main is a throwaway harness invoked via `go run` by
// tests/integration/rag_qdrant_chunkid_fix_live_test.go. It performs a REAL
// knowledge.Pipeline.Ingest of a multi-doc corpus into a REAL Qdrant server,
// then a REAL knowledge.Pipeline.Query, printing machine-checkable markers
// (INGEST_OK / INGEST_FAILED / QUERY_RETRIEVED_ALL_DOCS / ...) that the
// parent test asserts on.
//
// Running as a SEPARATE OS process (rather than an in-process import)
// matters here: the parent test physically rewrites
// internal/knowledge/qdrant.go between the RED and GREEN phases, and only a
// fresh `go run` genuinely (re)compiles and exercises whatever content is
// currently on disk for that file.
//
// Lives under testdata/ so it is excluded from `go build ./...` /
// `go vet ./...` / `go test ./...` package discovery (Go's standard
// testdata-directory exclusion) — it is a script, not a package under this
// module's normal build graph.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

type corpusDoc struct {
	ID    string
	Title string
	Text  string
}

// MUST stay content-identical to qdrantIngestCorpus in
// ../../rag_qdrant_chunkid_fix_live_test.go (duplicated here because this
// is a separate `package main` under testdata/, which cannot import the
// integration_test package).
var corpus = []corpusDoc{
	{"doc_alpha", "Alpha", "The Cindervale telemetry collector batches events every thirty seconds before flushing to storage."},
	{"doc_beta", "Beta", "Emberkiln's active production embeddings registry is rebuilt nightly from the canonical document store."},
	{"doc_gamma", "Gamma", "The HelixCode release pipeline tags every artifact with a monotonically increasing build number."},
	{"doc_delta", "Delta", "Qdrant point IDs must be an unsigned 64-bit integer or a UUID; any other shape is rejected by the server."},
	{"doc_epsilon", "Epsilon", "Coffee brewing techniques improve flavor extraction using controlled water temperature."},
}

func main() {
	teiEmbedBase := os.Getenv("HARNESS_TEI_EMBED_BASE")
	embedModel := os.Getenv("HARNESS_TEI_EMBED_MODEL")
	qdrantHostPort := os.Getenv("HARNESS_QDRANT_HOST_PORT")
	collection := os.Getenv("HARNESS_QDRANT_COLLECTION")
	if teiEmbedBase == "" || embedModel == "" || qdrantHostPort == "" || collection == "" {
		fmt.Println("HARNESS_CONFIG_MISSING: all of HARNESS_TEI_EMBED_BASE/HARNESS_TEI_EMBED_MODEL/HARNESS_QDRANT_HOST_PORT/HARNESS_QDRANT_COLLECTION are required")
		os.Exit(2)
	}
	qdrantPort, err := strconv.Atoi(qdrantHostPort)
	if err != nil {
		fmt.Printf("HARNESS_CONFIG_INVALID: HARNESS_QDRANT_HOST_PORT=%q: %v\n", qdrantHostPort, err)
		os.Exit(2)
	}

	embedder := knowledge.NewLlamaEmbedder(teiEmbedBase, embedModel, 384)
	store, err := knowledge.NewQdrantStore("localhost", qdrantPort)
	if err != nil {
		fmt.Printf("HARNESS_STORE_CONNECT_FAILED: %v\n", err)
		os.Exit(2)
	}
	chunker := knowledge.NewFixedSizeChunker(2000, 0)

	pipeline := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          embedder,
		Store:             store,
		Chunker:           chunker,
		DefaultCollection: collection,
		DefaultTopK:       len(corpus),
	})

	ctx := context.Background()

	for _, doc := range corpus {
		_, err := pipeline.Ingest(ctx, knowledge.IngestRequest{
			Title:      doc.Title,
			Content:    doc.Text,
			Collection: collection,
		})
		if err != nil {
			fmt.Printf("INGEST_FAILED: doc=%s err=%v\n", doc.ID, err)
			os.Exit(1)
		}
	}
	fmt.Printf("INGEST_OK: ingested %d docs into collection %s\n", len(corpus), collection)

	result, err := pipeline.Query(ctx, knowledge.QueryRequest{
		Query:      "Cindervale Emberkiln HelixCode Qdrant coffee brewing telemetry embeddings registry release pipeline point IDs",
		Collection: collection,
		TopK:       len(corpus),
	})
	if err != nil {
		fmt.Printf("QUERY_FAILED: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("QUERY_OK: retrieved %d chunks\n", len(result.Chunks))

	missing := 0
	for _, doc := range corpus {
		found := false
		for _, ch := range result.Chunks {
			if ch.Content == doc.Text {
				found = true
				break
			}
		}
		if found {
			fmt.Printf("QUERY_DOC_FOUND: %s\n", doc.ID)
		} else {
			fmt.Printf("QUERY_DOC_MISSING: %s\n", doc.ID)
			missing++
		}
	}
	if missing > 0 {
		fmt.Printf("QUERY_RETRIEVAL_INCOMPLETE: %d/%d docs missing\n", missing, len(corpus))
		os.Exit(1)
	}
	fmt.Println("QUERY_RETRIEVED_ALL_DOCS")
}
