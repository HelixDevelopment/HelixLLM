package knowledge_test

import (
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func TestDocumentFields(t *testing.T) {
	doc := knowledge.Document{
		ID: "doc-1", Title: "Test", Content: "Hello world",
		Source: "test.md", Format: "markdown",
		CreatedAt: time.Now(),
	}
	if doc.ID != "doc-1" {
		t.Error("ID wrong")
	}
	if doc.Content != "Hello world" {
		t.Error("Content wrong")
	}
}

func TestChunkFields(t *testing.T) {
	chunk := knowledge.Chunk{
		ID: "chunk-1", DocumentID: "doc-1",
		Content: "Hello", Position: 0,
	}
	if chunk.DocumentID != "doc-1" {
		t.Error("DocumentID wrong")
	}
}

func TestScoredChunkScore(t *testing.T) {
	sc := knowledge.ScoredChunk{
		Chunk: knowledge.Chunk{ID: "c1", Content: "test"},
		Score: 0.95,
	}
	if sc.Score != 0.95 {
		t.Errorf("Score = %f, want 0.95", sc.Score)
	}
}

func TestQueryRequestDefaults(t *testing.T) {
	qr := knowledge.QueryRequest{Query: "test", Collection: "default"}
	if qr.TopK != 0 {
		t.Error("TopK should default to 0")
	}
}

func TestIngestResultFields(t *testing.T) {
	ir := knowledge.IngestResult{DocumentID: "doc-1", Chunks: 5, Collection: "default"}
	if ir.Chunks != 5 {
		t.Error("Chunks wrong")
	}
}
