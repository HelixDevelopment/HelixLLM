package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tmpSandbox(t *testing.T) (*Sandbox, string) {
	t.Helper()
	dir := t.TempDir()
	s := NewSandbox(SandboxConfig{
		AllowedPaths: []string{dir},
	})
	return s, dir
}

// ---------------------------------------------------------------------------
// ReadFileTool
// ---------------------------------------------------------------------------

func TestReadFileTool_Name(t *testing.T) {
	s := DefaultSandbox()
	tool := NewReadFileTool(s)
	if tool.Name() != "read_file" {
		t.Errorf("expected name read_file, got %s", tool.Name())
	}
}

func TestReadFileTool_Execute(t *testing.T) {
	s, dir := tmpSandbox(t)
	tool := NewReadFileTool(s)

	content := "line1\nline2\nline3\nline4\nline5"
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte(content), 0o644)

	ctx := context.Background()

	// Read all lines.
	out, err := tool.Execute(ctx, map[string]interface{}{"path": path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "line1") || !strings.Contains(out, "line5") {
		t.Errorf("expected all lines, got: %s", out)
	}

	// Read with offset.
	out, err = tool.Execute(ctx, map[string]interface{}{
		"path":   path,
		"offset": 2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "line1") {
		t.Error("expected line1 to be skipped with offset=2")
	}
	if !strings.Contains(out, "line3") {
		t.Error("expected line3 to be present")
	}

	// Read with limit.
	out, err = tool.Execute(ctx, map[string]interface{}{
		"path":  path,
		"limit": 2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "line1") || !strings.Contains(out, "line2") {
		t.Error("expected line1 and line2")
	}
	if strings.Contains(out, "line3") {
		t.Error("expected line3 to be excluded with limit=2")
	}
}

func TestReadFileTool_PathBlocked(t *testing.T) {
	s, _ := tmpSandbox(t)
	tool := NewReadFileTool(s)

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"path": "/etc/passwd",
	})
	if err == nil {
		t.Error("expected error for blocked path")
	}
}

func TestReadFileTool_NilArgs(t *testing.T) {
	s := DefaultSandbox()
	tool := NewReadFileTool(s)

	_, err := tool.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil args")
	}
}

func TestReadFileTool_FileNotFound(t *testing.T) {
	s, dir := tmpSandbox(t)
	tool := NewReadFileTool(s)

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"path": filepath.Join(dir, "nonexistent.txt"),
	})
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

// ---------------------------------------------------------------------------
// WriteFileTool
// ---------------------------------------------------------------------------

func TestWriteFileTool_Execute(t *testing.T) {
	s, dir := tmpSandbox(t)
	tool := NewWriteFileTool(s)

	path := filepath.Join(dir, "output.txt")
	ctx := context.Background()

	out, err := tool.Execute(ctx, map[string]interface{}{
		"path":    path,
		"content": "hello world",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "11 bytes") {
		t.Errorf("expected byte count in output, got: %s", out)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(data))
	}
}

func TestWriteFileTool_CreatesSubdirs(t *testing.T) {
	s, dir := tmpSandbox(t)
	tool := NewWriteFileTool(s)

	path := filepath.Join(dir, "sub", "dir", "file.txt")
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"path":    path,
		"content": "nested",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "nested" {
		t.Errorf("expected 'nested', got %q", string(data))
	}
}

func TestWriteFileTool_PathBlocked(t *testing.T) {
	s, _ := tmpSandbox(t)
	tool := NewWriteFileTool(s)

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"path":    "/etc/evil.txt",
		"content": "bad",
	})
	if err == nil {
		t.Error("expected error for blocked path")
	}
}

func TestWriteFileTool_NilArgs(t *testing.T) {
	s := DefaultSandbox()
	tool := NewWriteFileTool(s)

	_, err := tool.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil args")
	}
}

// ---------------------------------------------------------------------------
// ListDirectoryTool
// ---------------------------------------------------------------------------

func TestListDirectoryTool_Execute(t *testing.T) {
	s, dir := tmpSandbox(t)
	tool := NewListDirectoryTool(s)

	// Create some files and a subdir.
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0o755)

	ctx := context.Background()

	out, err := tool.Execute(ctx, map[string]interface{}{"path": dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "a.txt") {
		t.Error("expected a.txt in listing")
	}
	if !strings.Contains(out, "subdir/") {
		t.Error("expected subdir/ in listing")
	}
}

func TestListDirectoryTool_Recursive(t *testing.T) {
	s, dir := tmpSandbox(t)
	tool := NewListDirectoryTool(s)

	sub := filepath.Join(dir, "sub")
	os.Mkdir(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "deep.txt"), []byte("deep"), 0o644)

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"path":      dir,
		"recursive": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "deep.txt") {
		t.Error("expected deep.txt in recursive listing")
	}
}

func TestListDirectoryTool_EmptyDir(t *testing.T) {
	s, dir := tmpSandbox(t)
	tool := NewListDirectoryTool(s)

	empty := filepath.Join(dir, "empty")
	os.Mkdir(empty, 0o755)

	out, err := tool.Execute(context.Background(), map[string]interface{}{"path": empty})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "(empty directory)" {
		t.Errorf("expected '(empty directory)', got %q", out)
	}
}

// ---------------------------------------------------------------------------
// SearchFilesTool
// ---------------------------------------------------------------------------

func TestSearchFilesTool_GlobMatch(t *testing.T) {
	s, dir := tmpSandbox(t)
	tool := NewSearchFilesTool(s)

	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(dir, "util.go"), []byte("package util"), 0o644)
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# readme"), 0o644)

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"pattern": "*.go",
		"path":    dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "main.go") || !strings.Contains(out, "util.go") {
		t.Errorf("expected .go files in result, got: %s", out)
	}
	if strings.Contains(out, "readme.md") {
		t.Error("did not expect readme.md in glob *.go results")
	}
}

func TestSearchFilesTool_ContentSearch(t *testing.T) {
	s, dir := tmpSandbox(t)
	tool := NewSearchFilesTool(s)

	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello world\nfoo bar"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("no match here"), 0o644)

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"pattern": "hello world",
		"path":    dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "a.txt") {
		t.Errorf("expected a.txt in content search, got: %s", out)
	}
}

func TestSearchFilesTool_NoMatch(t *testing.T) {
	s, dir := tmpSandbox(t)
	tool := NewSearchFilesTool(s)

	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("something"), 0o644)

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"pattern": "nonexistent_pattern_xyz",
		"path":    dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "No matches found." {
		t.Errorf("expected 'No matches found.', got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// FileInfoTool
// ---------------------------------------------------------------------------

func TestFileInfoTool_Execute(t *testing.T) {
	s, dir := tmpSandbox(t)
	tool := NewFileInfoTool(s)

	path := filepath.Join(dir, "info.txt")
	os.WriteFile(path, []byte("test content"), 0o644)

	out, err := tool.Execute(context.Background(), map[string]interface{}{"path": path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Name: info.txt") {
		t.Error("expected file name in output")
	}
	if !strings.Contains(out, "Type: file") {
		t.Error("expected type=file in output")
	}
	if !strings.Contains(out, "Size: 12 bytes") {
		t.Error("expected size in output")
	}
}

func TestFileInfoTool_Directory(t *testing.T) {
	s, dir := tmpSandbox(t)
	tool := NewFileInfoTool(s)

	sub := filepath.Join(dir, "mydir")
	os.Mkdir(sub, 0o755)

	out, err := tool.Execute(context.Background(), map[string]interface{}{"path": sub})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Type: directory") {
		t.Error("expected type=directory in output")
	}
}

func TestFileInfoTool_NotFound(t *testing.T) {
	s, dir := tmpSandbox(t)
	tool := NewFileInfoTool(s)

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"path": filepath.Join(dir, "nope.txt"),
	})
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

// ---------------------------------------------------------------------------
// Argument helpers
// ---------------------------------------------------------------------------

func TestRequireString(t *testing.T) {
	args := map[string]interface{}{"key": "value"}

	val, err := requireString(args, "key")
	if err != nil || val != "value" {
		t.Errorf("expected 'value', got %q, err=%v", val, err)
	}

	_, err = requireString(args, "missing")
	if err == nil {
		t.Error("expected error for missing key")
	}

	_, err = requireString(map[string]interface{}{"key": ""}, "key")
	if err == nil {
		t.Error("expected error for empty string")
	}
}

func TestOptionalInt(t *testing.T) {
	args := map[string]interface{}{
		"int_val":   42,
		"float_val": float64(3.14),
		"str_val":   "hello",
	}

	if v := optionalInt(args, "int_val", 0); v != 42 {
		t.Errorf("expected 42, got %d", v)
	}
	if v := optionalInt(args, "float_val", 0); v != 3 {
		t.Errorf("expected 3, got %d", v)
	}
	if v := optionalInt(args, "str_val", 99); v != 99 {
		t.Errorf("expected default 99, got %d", v)
	}
	if v := optionalInt(args, "missing", 7); v != 7 {
		t.Errorf("expected default 7, got %d", v)
	}
}

func TestOptionalBool(t *testing.T) {
	args := map[string]interface{}{
		"flag":   true,
		"notbool": "yes",
	}

	if v := optionalBool(args, "flag", false); !v {
		t.Error("expected true")
	}
	if v := optionalBool(args, "notbool", false); v {
		t.Error("expected false for non-bool type")
	}
	if v := optionalBool(args, "missing", true); !v {
		t.Error("expected default true")
	}
}
