package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// AnalyzeCodeTool
// ---------------------------------------------------------------------------

func TestAnalyzeCodeTool_Name(t *testing.T) {
	s := DefaultSandbox()
	tool := NewAnalyzeCodeTool(s)
	if tool.Name() != "analyze_code" {
		t.Errorf("expected analyze_code, got %s", tool.Name())
	}
}

func TestAnalyzeCodeTool_AnalyzeToolsDir(t *testing.T) {
	// Analyze the tools/ directory itself -- it has Go files with functions.
	toolsDir := "/run/media/milosvasic/DATA4TB/Projects/helix_agent/HelixLLM/internal/agents/tools"
	s := NewSandbox(SandboxConfig{
		AllowedPaths: []string{toolsDir},
	})
	tool := NewAnalyzeCodeTool(s)

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"path": toolsDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "Files:") {
		t.Error("expected 'Files:' in output")
	}
	if !strings.Contains(out, "Lines:") {
		t.Error("expected 'Lines:' in output")
	}
	if !strings.Contains(out, "Functions:") {
		t.Error("expected 'Functions:' in output")
	}
	if !strings.Contains(out, ".go:") {
		t.Error("expected .go extension in output")
	}
}

func TestAnalyzeCodeTool_SingleFile(t *testing.T) {
	s, dir := tmpSandbox(t)
	tool := NewAnalyzeCodeTool(s)

	code := "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n\nfunc helper() {\n}\n"
	path := filepath.Join(dir, "main.go")
	os.WriteFile(path, []byte(code), 0o644)

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"path": path,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Files: 1") {
		t.Errorf("expected 'Files: 1', got: %s", out)
	}
	if !strings.Contains(out, "Functions: 2") {
		t.Errorf("expected 'Functions: 2', got: %s", out)
	}
}

func TestAnalyzeCodeTool_NilArgs(t *testing.T) {
	s := DefaultSandbox()
	tool := NewAnalyzeCodeTool(s)

	_, err := tool.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil args")
	}
}

// ---------------------------------------------------------------------------
// RunTestsTool
// ---------------------------------------------------------------------------

func TestRunTestsTool_Name(t *testing.T) {
	s := DefaultSandbox()
	tool := NewRunTestsTool(s)
	if tool.Name() != "run_tests" {
		t.Errorf("expected run_tests, got %s", tool.Name())
	}
}

func TestDetectTestFramework_Go(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\ngo 1.21\n"), 0o644)

	fw := detectTestFramework(dir)
	if fw != "go" {
		t.Errorf("expected 'go', got %q", fw)
	}
}

func TestDetectTestFramework_Python(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[tool.pytest]\n"), 0o644)

	fw := detectTestFramework(dir)
	if fw != "pytest" {
		t.Errorf("expected 'pytest', got %q", fw)
	}
}

func TestDetectTestFramework_NPM(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644)

	fw := detectTestFramework(dir)
	if fw != "npm" {
		t.Errorf("expected 'npm', got %q", fw)
	}
}

func TestDetectTestFramework_None(t *testing.T) {
	dir := t.TempDir()
	fw := detectTestFramework(dir)
	if fw != "" {
		t.Errorf("expected empty string, got %q", fw)
	}
}

func TestRunTestsTool_UnknownFramework(t *testing.T) {
	s := DefaultSandbox()
	tool := NewRunTestsTool(s)

	dir := t.TempDir()
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"path": dir,
	})
	if err == nil {
		t.Error("expected error for unknown framework")
	}
}

// ---------------------------------------------------------------------------
// GetDependenciesTool
// ---------------------------------------------------------------------------

func TestGetDependenciesTool_Name(t *testing.T) {
	s := DefaultSandbox()
	tool := NewGetDependenciesTool(s)
	if tool.Name() != "get_dependencies" {
		t.Errorf("expected get_dependencies, got %s", tool.Name())
	}
}

func TestGetDependenciesTool_GoMod(t *testing.T) {
	s, dir := tmpSandbox(t)
	tool := NewGetDependenciesTool(s)

	gomod := `module example.com/test

go 1.21

require (
	github.com/gin-gonic/gin v1.9.0
	github.com/sirupsen/logrus v1.9.3
)
`
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644)

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"path": dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "gin-gonic/gin") {
		t.Errorf("expected gin in output, got: %s", out)
	}
	if !strings.Contains(out, "logrus") {
		t.Errorf("expected logrus in output, got: %s", out)
	}
}

func TestGetDependenciesTool_NoDeps(t *testing.T) {
	s, dir := tmpSandbox(t)
	tool := NewGetDependenciesTool(s)

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"path": dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No dependency manifest found") {
		t.Errorf("expected no manifest message, got: %s", out)
	}
}

func TestGetDependenciesTool_NilArgs(t *testing.T) {
	s := DefaultSandbox()
	tool := NewGetDependenciesTool(s)

	_, err := tool.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil args")
	}
}

// ---------------------------------------------------------------------------
// CalculateComplexityTool
// ---------------------------------------------------------------------------

func TestCalculateComplexityTool_Name(t *testing.T) {
	s := DefaultSandbox()
	tool := NewCalculateComplexityTool(s)
	if tool.Name() != "calculate_complexity" {
		t.Errorf("expected calculate_complexity, got %s", tool.Name())
	}
}

func TestCalculateComplexityTool_Execute(t *testing.T) {
	s, dir := tmpSandbox(t)
	tool := NewCalculateComplexityTool(s)

	code := `package main

func simple() {
	fmt.Println("hello")
}

func complex() {
	if a > 0 {
		for i := 0; i < 10; i++ {
			if i%2 == 0 {
				switch i {
				case 2:
					fmt.Println(2)
				case 4:
					fmt.Println(4)
				}
			}
		}
	}
}
`
	path := filepath.Join(dir, "main.go")
	os.WriteFile(path, []byte(code), 0o644)

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"path": path,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "simple") {
		t.Error("expected 'simple' function in output")
	}
	if !strings.Contains(out, "complex") {
		t.Error("expected 'complex' function in output")
	}
	// The complex function should have higher complexity.
	if !strings.Contains(out, "MEDIUM") && !strings.Contains(out, "HIGH") {
		t.Error("expected MEDIUM or HIGH complexity for complex function")
	}
}

func TestCalculateComplexityTool_NoFunctions(t *testing.T) {
	s, dir := tmpSandbox(t)
	tool := NewCalculateComplexityTool(s)

	os.WriteFile(filepath.Join(dir, "empty.go"), []byte("package main\n"), 0o644)

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"path": filepath.Join(dir, "empty.go"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "No functions found." {
		t.Errorf("expected 'No functions found.', got: %s", out)
	}
}

func TestCalculateComplexityTool_NilArgs(t *testing.T) {
	s := DefaultSandbox()
	tool := NewCalculateComplexityTool(s)

	_, err := tool.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil args")
	}
}

// ---------------------------------------------------------------------------
// extractFuncName
// ---------------------------------------------------------------------------

func TestExtractFuncName(t *testing.T) {
	tests := []struct {
		line     string
		expected string
	}{
		{"func main() {", "main"},
		{"func (s *Server) Start() error {", "Start"},
		{"func helper(a int) string {", "helper"},
	}

	for _, tt := range tests {
		got := extractFuncName(tt.line)
		if got != tt.expected {
			t.Errorf("extractFuncName(%q) = %q, want %q", tt.line, got, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// parseDeps
// ---------------------------------------------------------------------------

func TestParseDeps_Go(t *testing.T) {
	content := `module test
go 1.21
require (
	github.com/foo/bar v1.0.0
	github.com/baz/qux v2.0.0
)
`
	result := parseDeps(content, "go")
	if !strings.Contains(result, "foo/bar") || !strings.Contains(result, "baz/qux") {
		t.Errorf("expected deps in output, got: %s", result)
	}
}

func TestParseDeps_NoDeps(t *testing.T) {
	content := `module test
go 1.21
`
	result := parseDeps(content, "go")
	if result != "(no dependencies)" {
		t.Errorf("expected '(no dependencies)', got: %s", result)
	}
}
