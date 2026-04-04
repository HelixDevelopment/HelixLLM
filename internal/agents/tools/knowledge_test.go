package tools_test

import (
	"context"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
	"github.com/HelixDevelopment/HelixLLM/internal/agents/tools"
	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func TestKnowledgeQueryToolInterface(t *testing.T) {
	var _ agents.Tool = (*tools.KnowledgeQueryTool)(nil)
}

func TestKnowledgeQueryToolName(t *testing.T) {
	tool := tools.NewKnowledgeQueryTool(nil, "default")
	if tool.Name() != "knowledge_query" {
		t.Errorf("expected name 'knowledge_query', got %s", tool.Name())
	}
}

func TestKnowledgeQueryToolDescription(t *testing.T) {
	tool := tools.NewKnowledgeQueryTool(nil, "default")
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestKnowledgeQueryToolParameters(t *testing.T) {
	tool := tools.NewKnowledgeQueryTool(nil, "default")
	params := tool.Parameters()
	if params == nil {
		t.Fatal("expected non-nil parameters")
	}
	if _, ok := params["query"]; !ok {
		t.Error("expected 'query' parameter in schema")
	}
}

func TestKnowledgeQueryToolExecuteNilPipeline(t *testing.T) {
	tool := tools.NewKnowledgeQueryTool(nil, "default")
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"query": "test query",
	})
	if err == nil {
		t.Error("expected error when pipeline is nil, got nil")
	}
}

func TestKnowledgeQueryToolExecuteMissingQuery(t *testing.T) {
	tool := tools.NewKnowledgeQueryTool(nil, "default")
	_, err := tool.Execute(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Error("expected error for missing query, got nil")
	}
}

func TestKnowledgeQueryToolExecuteWithPipeline(t *testing.T) {
	// Set up an in-memory pipeline with some data.
	embedder := knowledge.NewHashEmbedder(64)
	store := knowledge.NewMemoryStore()
	chunker := knowledge.NewFixedSizeChunker(500, 50)
	pipeline := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          embedder,
		Store:             store,
		Chunker:           chunker,
		DefaultCollection: "test",
		DefaultTopK:       3,
	})

	// Ingest a document.
	_, err := pipeline.Ingest(context.Background(), knowledge.IngestRequest{
		Title:      "Go Guide",
		Content:    "Go is a statically typed, compiled language designed at Google.",
		Source:     "test",
		Collection: "test",
	})
	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}

	tool := tools.NewKnowledgeQueryTool(pipeline, "test")
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"query": "Go language",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result from knowledge query")
	}
}

func TestKnowledgeQueryToolExecuteNilArgs(t *testing.T) {
	tool := tools.NewKnowledgeQueryTool(nil, "default")
	_, err := tool.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil args, got nil")
	}
}

func TestKnowledgeQueryToolExecuteNonStringQuery(t *testing.T) {
	tool := tools.NewKnowledgeQueryTool(nil, "default")
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"query": 42, // not a string
	})
	if err == nil {
		t.Error("expected error for non-string query, got nil")
	}
}

func TestKnowledgeQueryToolExecuteEmptyStringQuery(t *testing.T) {
	tool := tools.NewKnowledgeQueryTool(nil, "default")
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"query": "", // empty string
	})
	if err == nil {
		t.Error("expected error for empty string query, got nil")
	}
}

func TestKnowledgeQueryToolExecute_PipelineError(t *testing.T) {
	// Pipeline with no data in the queried collection → Query returns error for empty collection name.
	pipeline := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          knowledge.NewHashEmbedder(64),
		Store:             knowledge.NewMemoryStore(),
		Chunker:           knowledge.NewFixedSizeChunker(500, 0),
		DefaultCollection: "default",
		DefaultTopK:       3,
	})

	// Query with empty collection triggers validation error from pipeline.
	tool := tools.NewKnowledgeQueryTool(pipeline, "") // empty default collection
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"query": "something",
		// no "collection" arg → uses empty defaultCollection → pipeline error
	})
	if err == nil {
		t.Error("expected error when pipeline query fails, got nil")
	}
}

func TestKnowledgeQueryToolExecute_EmptyContext(t *testing.T) {
	// Ingest content and query for something totally unrelated so that
	// all chunks are filtered out by MinScore, returning an empty context.
	pipeline := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          knowledge.NewHashEmbedder(64),
		Store:             knowledge.NewMemoryStore(),
		Chunker:           knowledge.NewFixedSizeChunker(500, 0),
		DefaultCollection: "empty-ctx",
		DefaultTopK:       3,
	})

	_, err := pipeline.Ingest(context.Background(), knowledge.IngestRequest{
		Content:    "Go programming language",
		Collection: "empty-ctx",
	})
	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}

	// Use a very high MinScore via a custom query request; the tool itself
	// doesn't expose MinScore, but an empty collection with a mismatch query
	// can still return results (hash embedder). Instead, use a collection
	// override argument that points to an empty (nonexistent) collection so
	// Search returns no chunks → context is "".
	tool := tools.NewKnowledgeQueryTool(pipeline, "empty-ctx")
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"query":      "query against empty collection",
		"collection": "nonexistent-empty-col",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result != "No relevant information found." {
		t.Errorf("expected empty-context message, got %q", result)
	}
}

func TestKnowledgeQueryToolExecuteWithCollectionOverride(t *testing.T) {
	embedder := knowledge.NewHashEmbedder(64)
	store := knowledge.NewMemoryStore()
	chunker := knowledge.NewFixedSizeChunker(500, 50)
	pipeline := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          embedder,
		Store:             store,
		Chunker:           chunker,
		DefaultCollection: "default",
		DefaultTopK:       3,
	})

	_, err := pipeline.Ingest(context.Background(), knowledge.IngestRequest{
		Title:      "Custom",
		Content:    "Custom collection content for testing.",
		Source:     "test",
		Collection: "custom",
	})
	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}

	tool := tools.NewKnowledgeQueryTool(pipeline, "default")
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"query":      "custom content",
		"collection": "custom",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result from custom collection query")
	}
}
