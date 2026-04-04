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
