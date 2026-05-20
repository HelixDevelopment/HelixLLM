package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// buildBenchTree creates a directory tree with dirs subdirectories each
// containing filesPerDir files, returning the root path.
func buildBenchTree(b *testing.B, dirs, filesPerDir int) string {
	b.Helper()
	root := b.TempDir()
	for d := 0; d < dirs; d++ {
		sub := filepath.Join(root, fmt.Sprintf("pkg_%d", d))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			b.Fatalf("mkdir: %v", err)
		}
		for f := 0; f < filesPerDir; f++ {
			p := filepath.Join(sub, fmt.Sprintf("file_%d.go", f))
			if err := os.WriteFile(p, []byte("package x\n\nfunc F() {}\n"), 0o644); err != nil {
				b.Fatalf("write: %v", err)
			}
		}
	}
	return root
}

// BenchmarkListDirectoryRecursive exercises the WalkDir-based recursive
// directory listing hot path migrated under speed-programme P5-T03.
func BenchmarkListDirectoryRecursive(b *testing.B) {
	root := buildBenchTree(b, 40, 20) // 800 files across 40 dirs
	s := NewSandbox(SandboxConfig{AllowedPaths: []string{root}})
	tool := NewListDirectoryTool(s)
	ctx := context.Background()
	args := map[string]interface{}{"path": root, "recursive": true}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tool.Execute(ctx, args); err != nil {
			b.Fatalf("execute: %v", err)
		}
	}
}
