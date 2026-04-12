package knowledge_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func newAutoIngestPipeline() *knowledge.Pipeline {
	return knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          knowledge.NewHashEmbedder(64),
		Store:             knowledge.NewMemoryStore(),
		Chunker:           knowledge.NewFixedSizeChunker(500, 50),
		DefaultCollection: "default",
		DefaultTopK:       5,
	})
}

func TestAutoIngest_IngestsSourceFiles(t *testing.T) {
	dir := t.TempDir()

	// Create sample source files.
	files := map[string]string{
		"main.go":          "package main\n\nfunc main() {}\n",
		"utils.py":         "def hello():\n    print('hello')\n",
		"app.js":           "console.log('hello');\n",
		"config.yaml":      "key: value\n",
		"schema.sql":       "CREATE TABLE test (id INT);\n",
		"build.sh":         "#!/bin/bash\necho build\n",
		"sub/nested.ts":    "export const x: number = 42;\n",
		"data.json":        `{"key": "value"}` + "\n",
		"notes.md":         "# Notes\n\nSome notes here.\n",
		"settings.toml":    "[server]\nport = 8080\n",
		"lib.rs":           "fn main() {}\n",
		"App.java":         "public class App { public static void main(String[] args) {} }\n",
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("failed to create %s: %v", name, err)
		}
	}

	pipeline := newAutoIngestPipeline()
	err := knowledge.AutoIngest(pipeline, dir, "codebase")
	if err != nil {
		t.Fatalf("AutoIngest failed: %v", err)
	}

	// Verify files were ingested by querying the collection.
	stats, err := pipeline.Stats(nil)
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	if stats.TotalDocs < len(files) {
		t.Errorf("expected at least %d documents, got %d", len(files), stats.TotalDocs)
	}
}

func TestAutoIngest_SkipsVendorAndHiddenDirs(t *testing.T) {
	dir := t.TempDir()

	// Create files in skippable directories.
	skipped := []string{
		"vendor/lib.go",
		"node_modules/pkg.js",
		".git/config",
		"bin/tool.go",
		"__pycache__/mod.py",
		".hidden/secret.go",
	}

	for _, name := range skipped {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte("package skip\n"), 0o644)
	}

	// Create one valid file.
	os.WriteFile(filepath.Join(dir, "valid.go"), []byte("package main\n\nfunc valid() {}\n"), 0o644)

	pipeline := newAutoIngestPipeline()
	err := knowledge.AutoIngest(pipeline, dir, "test")
	if err != nil {
		t.Fatalf("AutoIngest failed: %v", err)
	}

	stats, err := pipeline.Stats(nil)
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	// Only the valid.go file should be ingested.
	if stats.TotalDocs != 1 {
		t.Errorf("expected 1 document (only valid.go), got %d", stats.TotalDocs)
	}
}

func TestAutoIngest_SkipsLargeFiles(t *testing.T) {
	dir := t.TempDir()

	// Create a file that exceeds 100KB.
	largeContent := make([]byte, 101*1024)
	for i := range largeContent {
		largeContent[i] = 'x'
	}
	os.WriteFile(filepath.Join(dir, "large.go"), largeContent, 0o644)

	// Create one small file.
	os.WriteFile(filepath.Join(dir, "small.go"), []byte("package main\n"), 0o644)

	pipeline := newAutoIngestPipeline()
	err := knowledge.AutoIngest(pipeline, dir, "test")
	if err != nil {
		t.Fatalf("AutoIngest failed: %v", err)
	}

	stats, err := pipeline.Stats(nil)
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	if stats.TotalDocs != 1 {
		t.Errorf("expected 1 document (only small.go), got %d", stats.TotalDocs)
	}
}

func TestAutoIngest_SkipsUnsupportedExtensions(t *testing.T) {
	dir := t.TempDir()

	// Create files with unsupported extensions.
	unsupported := []string{"image.png", "data.csv", "archive.tar.gz", "readme.txt"}
	for _, name := range unsupported {
		os.WriteFile(filepath.Join(dir, name), []byte("content\n"), 0o644)
	}

	// Create one supported file.
	os.WriteFile(filepath.Join(dir, "code.go"), []byte("package main\n"), 0o644)

	pipeline := newAutoIngestPipeline()
	err := knowledge.AutoIngest(pipeline, dir, "test")
	if err != nil {
		t.Fatalf("AutoIngest failed: %v", err)
	}

	stats, err := pipeline.Stats(nil)
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	if stats.TotalDocs != 1 {
		t.Errorf("expected 1 document (only code.go), got %d", stats.TotalDocs)
	}
}

func TestAutoIngest_NilPipeline(t *testing.T) {
	err := knowledge.AutoIngest(nil, "/tmp", "col")
	if err == nil {
		t.Error("expected error for nil pipeline")
	}
}

func TestAutoIngest_EmptyDir(t *testing.T) {
	pipeline := newAutoIngestPipeline()
	err := knowledge.AutoIngest(pipeline, "", "col")
	if err == nil {
		t.Error("expected error for empty dir")
	}
}

func TestAutoIngest_EmptyCollection(t *testing.T) {
	pipeline := newAutoIngestPipeline()
	err := knowledge.AutoIngest(pipeline, "/tmp", "")
	if err == nil {
		t.Error("expected error for empty collection")
	}
}

func TestAutoIngest_EmptyDir_NoFiles(t *testing.T) {
	dir := t.TempDir()

	pipeline := newAutoIngestPipeline()
	err := knowledge.AutoIngest(pipeline, dir, "empty")
	if err != nil {
		t.Fatalf("AutoIngest should not error on empty dir: %v", err)
	}

	stats, err := pipeline.Stats(nil)
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	if stats.TotalDocs != 0 {
		t.Errorf("expected 0 documents, got %d", stats.TotalDocs)
	}
}

func TestAutoIngest_SkipsEmptyFiles(t *testing.T) {
	dir := t.TempDir()

	// Create an empty source file.
	os.WriteFile(filepath.Join(dir, "empty.go"), []byte(""), 0o644)
	// And a whitespace-only file.
	os.WriteFile(filepath.Join(dir, "spaces.py"), []byte("   \n\t\n  "), 0o644)
	// And one valid file.
	os.WriteFile(filepath.Join(dir, "valid.go"), []byte("package main\n"), 0o644)

	pipeline := newAutoIngestPipeline()
	err := knowledge.AutoIngest(pipeline, dir, "test")
	if err != nil {
		t.Fatalf("AutoIngest failed: %v", err)
	}

	stats, err := pipeline.Stats(nil)
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	if stats.TotalDocs != 1 {
		t.Errorf("expected 1 document (only valid.go), got %d", stats.TotalDocs)
	}
}
