package knowledge_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func TestCodeAwareChunker_GoFile_SplitsOnFunc(t *testing.T) {
	content := `package main

import "fmt"

func hello() {
	fmt.Println("hello")
}

func world() {
	fmt.Println("world")
}
`
	doc := knowledge.Document{
		ID:      "go-doc",
		Content: content,
		Source:  "main.go",
	}

	chunker := knowledge.NewCodeAwareChunker(500, 0)
	chunks, err := chunker.Chunk(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for Go file with 2 funcs, got %d", len(chunks))
	}

	// Verify each chunk has proper fields set.
	for i, ch := range chunks {
		if ch.DocumentID != "go-doc" {
			t.Errorf("chunk[%d].DocumentID = %q, want %q", i, ch.DocumentID, "go-doc")
		}
		if ch.Content == "" {
			t.Errorf("chunk[%d] has empty content", i)
		}
		if ch.ID == "" {
			t.Errorf("chunk[%d] has empty ID", i)
		}
	}
}

func TestCodeAwareChunker_PythonFile_SplitsOnDefAndClass(t *testing.T) {
	content := `import os

class MyClass:
    pass

def hello():
    print("hello")

def world():
    print("world")
`
	doc := knowledge.Document{
		ID:      "py-doc",
		Content: content,
		Source:  "script.py",
	}

	chunker := knowledge.NewCodeAwareChunker(500, 0)
	chunks, err := chunker.Chunk(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks for Python file with class + 2 defs, got %d", len(chunks))
	}
}

func TestCodeAwareChunker_MarkdownFile_SplitsOnHeaders(t *testing.T) {
	content := `# Introduction

This is the intro.

# Getting Started

Follow these steps.

# API Reference

See the docs.
`
	doc := knowledge.Document{
		ID:      "md-doc",
		Content: content,
		Source:  "README.md",
	}

	chunker := knowledge.NewCodeAwareChunker(500, 0)
	chunks, err := chunker.Chunk(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks for Markdown with 3 headers, got %d", len(chunks))
	}
}

func TestCodeAwareChunker_UnknownFormat_FallsBackToFixed(t *testing.T) {
	content := "aaaa bbbb cccc dddd eeee ffff gggg hhhh"
	doc := knowledge.Document{
		ID:      "txt-doc",
		Content: content,
		Source:  "data.txt",
	}

	chunker := knowledge.NewCodeAwareChunker(10, 0)
	chunks, err := chunker.Chunk(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks from fixed-size fallback, got %d", len(chunks))
	}
}

func TestCodeAwareChunker_NoSource_FallsBackToFixed(t *testing.T) {
	doc := knowledge.Document{
		ID:      "no-ext",
		Content: "some content here",
		Source:  "noextension",
	}

	chunker := knowledge.NewCodeAwareChunker(5, 0)
	chunks, err := chunker.Chunk(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) == 0 {
		t.Error("expected at least one chunk")
	}
}

func TestCodeAwareChunker_EmptyContent_Error(t *testing.T) {
	doc := knowledge.Document{
		ID:      "empty",
		Content: "",
		Source:  "main.go",
	}

	chunker := knowledge.NewCodeAwareChunker(100, 0)
	_, err := chunker.Chunk(doc)
	if err == nil {
		t.Error("expected error for empty content, got nil")
	}
}

func TestCodeAwareChunker_LargeFunction_SubChunked(t *testing.T) {
	// Create a Go file with one function that exceeds maxChunkSize.
	bigBody := ""
	for i := 0; i < 50; i++ {
		bigBody += "\tfmt.Println(\"line\")\n"
	}
	content := "package main\n\nfunc big() {\n" + bigBody + "}\n"

	doc := knowledge.Document{
		ID:      "big-func",
		Content: content,
		Source:  "big.go",
	}

	chunker := knowledge.NewCodeAwareChunker(100, 10)
	chunks, err := chunker.Chunk(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The large function body should be sub-chunked into multiple pieces.
	if len(chunks) < 2 {
		t.Errorf("expected large function to be sub-chunked, got %d chunks", len(chunks))
	}
}

func TestCodeAwareChunker_MetadataPreserved(t *testing.T) {
	doc := knowledge.Document{
		ID:      "meta-doc",
		Content: "package main\n\nfunc a() {}\n\nfunc b() {}\n",
		Source:  "meta.go",
		Metadata: map[string]string{
			"author": "test",
		},
	}

	chunker := knowledge.NewCodeAwareChunker(500, 0)
	chunks, err := chunker.Chunk(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, ch := range chunks {
		if ch.Metadata == nil {
			t.Errorf("chunk[%d] has nil metadata", i)
			continue
		}
		if ch.Metadata["author"] != "test" {
			t.Errorf("chunk[%d] metadata[author] = %q, want %q", i, ch.Metadata["author"], "test")
		}
	}
}

func TestCodeAwareChunker_PositionsSequential(t *testing.T) {
	content := `package main

func a() {}

func b() {}

func c() {}
`
	doc := knowledge.Document{
		ID:      "pos-doc",
		Content: content,
		Source:  "pos.go",
	}

	chunker := knowledge.NewCodeAwareChunker(500, 0)
	chunks, err := chunker.Chunk(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, ch := range chunks {
		if ch.Position != i {
			t.Errorf("chunk[%d].Position = %d, want %d", i, ch.Position, i)
		}
	}
}

func TestCodeAwareChunker_CaseInsensitiveExt(t *testing.T) {
	doc := knowledge.Document{
		ID:      "upper-ext",
		Content: "# Title\n\nContent\n\n# Another\n\nMore content\n",
		Source:  "FILE.MD",
	}

	chunker := knowledge.NewCodeAwareChunker(500, 0)
	chunks, err := chunker.Chunk(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks for .MD file, got %d", len(chunks))
	}
}

func TestNewCodeAwareChunker_ClampValues(t *testing.T) {
	// maxSize < 1 clamped to 1
	c := knowledge.NewCodeAwareChunker(0, 0)
	_ = c // no panic

	// overlap >= maxSize clamped
	c2 := knowledge.NewCodeAwareChunker(10, 15)
	_ = c2

	// negative overlap clamped to 0
	c3 := knowledge.NewCodeAwareChunker(10, -5)
	_ = c3
}
