package knowledge_test

import (
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func makeDoc(id, content string, meta map[string]string) knowledge.Document {
	return knowledge.Document{ID: id, Content: content, Metadata: meta}
}

func TestFixedSizeChunker_ShortDoc(t *testing.T) {
	c := knowledge.NewFixedSizeChunker(100, 0)
	doc := makeDoc("doc1", "hello world", nil)

	chunks, err := c.Chunk(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Content != "hello world" {
		t.Errorf("content = %q, want %q", chunks[0].Content, "hello world")
	}
	if chunks[0].Position != 0 {
		t.Errorf("position = %d, want 0", chunks[0].Position)
	}
	if chunks[0].ID != "doc1-0" {
		t.Errorf("ID = %q, want %q", chunks[0].ID, "doc1-0")
	}
}

func TestFixedSizeChunker_MultipleChunks(t *testing.T) {
	// 10 chars, chunk size 4, no overlap → positions 0-3, 4-7, 8-9
	c := knowledge.NewFixedSizeChunker(4, 0)
	doc := makeDoc("doc1", "0123456789", nil)

	chunks, err := c.Chunk(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d: %v", len(chunks), chunksContent(chunks))
	}
	if chunks[0].Content != "0123" {
		t.Errorf("chunk[0] = %q, want %q", chunks[0].Content, "0123")
	}
	if chunks[1].Content != "4567" {
		t.Errorf("chunk[1] = %q, want %q", chunks[1].Content, "4567")
	}
	if chunks[2].Content != "89" {
		t.Errorf("chunk[2] = %q, want %q", chunks[2].Content, "89")
	}
}

func TestFixedSizeChunker_Overlap(t *testing.T) {
	// content = "0123456789" (10 chars), size=4, overlap=2 → step=2
	// windows: [0,4), [2,6), [4,8), [6,10)
	c := knowledge.NewFixedSizeChunker(4, 2)
	doc := makeDoc("doc1", "0123456789", nil)

	chunks, err := c.Chunk(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks, got %d: %v", len(chunks), chunksContent(chunks))
	}
	if chunks[0].Content != "0123" {
		t.Errorf("chunk[0] = %q, want %q", chunks[0].Content, "0123")
	}
	if chunks[1].Content != "2345" {
		t.Errorf("chunk[1] = %q, want %q", chunks[1].Content, "2345")
	}
	if chunks[2].Content != "4567" {
		t.Errorf("chunk[2] = %q, want %q", chunks[2].Content, "4567")
	}
	if chunks[3].Content != "6789" {
		t.Errorf("chunk[3] = %q, want %q", chunks[3].Content, "6789")
	}
}

func TestFixedSizeChunker_ChunkIDs(t *testing.T) {
	c := knowledge.NewFixedSizeChunker(3, 0)
	doc := makeDoc("my-doc", "abcdefghi", nil) // 9 chars → 3 chunks
	chunks, err := c.Chunk(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []string{"my-doc-0", "my-doc-1", "my-doc-2"}
	for i, chunk := range chunks {
		if chunk.ID != expected[i] {
			t.Errorf("chunk[%d].ID = %q, want %q", i, chunk.ID, expected[i])
		}
		if chunk.Position != i {
			t.Errorf("chunk[%d].Position = %d, want %d", i, chunk.Position, i)
		}
		if chunk.DocumentID != "my-doc" {
			t.Errorf("chunk[%d].DocumentID = %q, want %q", i, chunk.DocumentID, "my-doc")
		}
	}
}

func TestFixedSizeChunker_MetadataCopied(t *testing.T) {
	meta := map[string]string{"source": "wiki", "lang": "en"}
	c := knowledge.NewFixedSizeChunker(5, 0)
	doc := makeDoc("doc1", "hello world foo", meta)

	chunks, err := c.Chunk(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, chunk := range chunks {
		if chunk.Metadata["source"] != "wiki" {
			t.Errorf("chunk[%d] missing source metadata", i)
		}
		if chunk.Metadata["lang"] != "en" {
			t.Errorf("chunk[%d] missing lang metadata", i)
		}
		// Mutating the chunk's metadata must not affect the original.
		chunk.Metadata["source"] = "mutated"
		if meta["source"] != "wiki" {
			t.Errorf("mutating chunk metadata affected original doc metadata")
		}
	}
}

func TestFixedSizeChunker_EmptyDocument(t *testing.T) {
	c := knowledge.NewFixedSizeChunker(10, 0)
	doc := makeDoc("doc1", "", nil)

	chunks, err := c.Chunk(doc)
	if err == nil && len(chunks) != 0 {
		t.Error("expected error or 0 chunks for empty document, got neither")
	}
	// Either an error is returned, or chunks is empty — both are acceptable per spec.
}

func TestFixedSizeChunker_ExactFit(t *testing.T) {
	// Content length == ChunkSize exactly → 1 chunk.
	c := knowledge.NewFixedSizeChunker(5, 0)
	doc := makeDoc("doc1", "hello", nil)
	chunks, err := c.Chunk(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Content != "hello" {
		t.Errorf("content = %q, want %q", chunks[0].Content, "hello")
	}
}

func TestFixedSizeChunker_UnicodeContent(t *testing.T) {
	// Each rune is counted, not each byte.
	content := "日本語テスト" // 6 runes
	c := knowledge.NewFixedSizeChunker(3, 0)
	doc := makeDoc("doc1", content, nil)
	chunks, err := c.Chunk(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	// Reassemble should recover the original string.
	var sb strings.Builder
	for _, ch := range chunks {
		sb.WriteString(ch.Content)
	}
	if sb.String() != content {
		t.Errorf("reassembled = %q, want %q", sb.String(), content)
	}
}

func TestFixedSizeChunker_SizeClampedToOne(t *testing.T) {
	// size < 1 is clamped to 1.
	c := knowledge.NewFixedSizeChunker(0, 0)
	if c.ChunkSize != 1 {
		t.Errorf("ChunkSize = %d, want 1 (clamped from 0)", c.ChunkSize)
	}
}

func TestFixedSizeChunker_NegativeSize(t *testing.T) {
	c := knowledge.NewFixedSizeChunker(-10, 0)
	if c.ChunkSize != 1 {
		t.Errorf("ChunkSize = %d, want 1 (clamped from -10)", c.ChunkSize)
	}
}

func TestFixedSizeChunker_NegativeOverlapClampedToZero(t *testing.T) {
	c := knowledge.NewFixedSizeChunker(10, -5)
	if c.Overlap != 0 {
		t.Errorf("Overlap = %d, want 0 (clamped from -5)", c.Overlap)
	}
}

func TestFixedSizeChunker_OverlapCappedAtSizeMinus1(t *testing.T) {
	// overlap >= size → clamped to size-1.
	c := knowledge.NewFixedSizeChunker(5, 5)
	if c.Overlap != 4 {
		t.Errorf("Overlap = %d, want 4 (capped at size-1)", c.Overlap)
	}
	c2 := knowledge.NewFixedSizeChunker(5, 10)
	if c2.Overlap != 4 {
		t.Errorf("Overlap = %d, want 4 (capped at size-1)", c2.Overlap)
	}
}

func TestFixedSizeChunker_DirectStruct_OverlapEqualsChunkSize(t *testing.T) {
	// Directly construct the struct bypassing NewFixedSizeChunker so that
	// Overlap == ChunkSize, forcing the "step < 1" defensive branch inside Chunk.
	c := &knowledge.FixedSizeChunker{ChunkSize: 4, Overlap: 4}
	doc := knowledge.Document{ID: "d1", Content: "hello world"}
	chunks, err := c.Chunk(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// step is clamped to 1, so every rune gets its own "chunk" window.
	if len(chunks) == 0 {
		t.Error("expected at least one chunk")
	}
}

// chunksContent is a helper for diagnostic output in failure messages.
func chunksContent(chunks []knowledge.Chunk) []string {
	out := make([]string, len(chunks))
	for i, c := range chunks {
		out[i] = c.Content
	}
	return out
}
