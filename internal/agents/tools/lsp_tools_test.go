package tools_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
	"github.com/HelixDevelopment/HelixLLM/internal/agents/tools"
)

// mockLSPProvider is a test double for LSPProvider that returns pre-configured
// canned values without requiring a real language server.
type mockLSPProvider struct {
	definitionLocs []tools.Location
	definitionErr  error

	referenceLocs []tools.Location
	referenceErr  error

	hoverText string
	hoverErr  error

	diagnosticsList []tools.Diagnostic
	diagnosticsErr  error
}

func (m *mockLSPProvider) Definition(_ context.Context, _ string, _, _ int) ([]tools.Location, error) {
	return m.definitionLocs, m.definitionErr
}

func (m *mockLSPProvider) References(_ context.Context, _ string, _, _ int) ([]tools.Location, error) {
	return m.referenceLocs, m.referenceErr
}

func (m *mockLSPProvider) Hover(_ context.Context, _ string, _, _ int) (string, error) {
	return m.hoverText, m.hoverErr
}

func (m *mockLSPProvider) Diagnostics(_ context.Context, _ string) ([]tools.Diagnostic, error) {
	return m.diagnosticsList, m.diagnosticsErr
}

func (m *mockLSPProvider) Close() error { return nil }

// ---------------------------------------------------------------------------
// GotoDefinitionTool
// ---------------------------------------------------------------------------

func TestGotoDefinitionToolInterface(t *testing.T) {
	var _ agents.Tool = (*tools.GotoDefinitionTool)(nil)
}

func TestGotoDefinitionToolMeta(t *testing.T) {
	tool := tools.NewGotoDefinitionTool(&mockLSPProvider{})
	if tool.Name() != "goto_definition" {
		t.Errorf("unexpected name: %s", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
	params := tool.Parameters()
	if params == nil {
		t.Fatal("expected non-nil parameters")
	}
}

func TestGotoDefinitionToolExecute_Found(t *testing.T) {
	mock := &mockLSPProvider{
		definitionLocs: []tools.Location{
			{
				URI: "file:///pkg/foo.go",
				Range: tools.Range{
					Start: tools.Position{Line: 10, Character: 4},
					End:   tools.Position{Line: 10, Character: 7},
				},
			},
		},
	}
	tool := tools.NewGotoDefinitionTool(mock)

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"file":   "/src/main.go",
		"line":   5,
		"column": 12,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "file:///pkg/foo.go") {
		t.Errorf("expected URI in result, got: %s", result)
	}
}

func TestGotoDefinitionToolExecute_NotFound(t *testing.T) {
	mock := &mockLSPProvider{definitionLocs: []tools.Location{}}
	tool := tools.NewGotoDefinitionTool(mock)

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"file":   "/src/main.go",
		"line":   0,
		"column": 0,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result message")
	}
}

func TestGotoDefinitionToolExecute_ProviderError(t *testing.T) {
	mock := &mockLSPProvider{definitionErr: errors.New("server crashed")}
	tool := tools.NewGotoDefinitionTool(mock)

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"file":   "/src/main.go",
		"line":   0,
		"column": 0,
	})
	if err == nil {
		t.Error("expected error from provider, got nil")
	}
}

func TestGotoDefinitionToolExecute_MissingArgs(t *testing.T) {
	tool := tools.NewGotoDefinitionTool(&mockLSPProvider{})

	cases := []map[string]interface{}{
		nil,
		{"line": 0, "column": 0},               // missing file
		{"file": "/f.go", "column": 0},          // missing line
		{"file": "/f.go", "line": 0},            // missing column
	}
	for _, args := range cases {
		_, err := tool.Execute(context.Background(), args)
		if err == nil {
			t.Errorf("expected error for args %v, got nil", args)
		}
	}
}

func TestGotoDefinitionToolExecute_Float64Args(t *testing.T) {
	// JSON numbers unmarshal as float64 — the tool must accept them.
	mock := &mockLSPProvider{definitionLocs: []tools.Location{}}
	tool := tools.NewGotoDefinitionTool(mock)

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"file":   "/src/main.go",
		"line":   float64(3),
		"column": float64(7),
	})
	if err != nil {
		t.Fatalf("Execute with float64 args: %v", err)
	}
}

// ---------------------------------------------------------------------------
// FindReferencesTool
// ---------------------------------------------------------------------------

func TestFindReferencesToolInterface(t *testing.T) {
	var _ agents.Tool = (*tools.FindReferencesTool)(nil)
}

func TestFindReferencesToolMeta(t *testing.T) {
	tool := tools.NewFindReferencesTool(&mockLSPProvider{})
	if tool.Name() != "find_references" {
		t.Errorf("unexpected name: %s", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestFindReferencesToolExecute_Found(t *testing.T) {
	mock := &mockLSPProvider{
		referenceLocs: []tools.Location{
			{URI: "file:///a.go", Range: tools.Range{Start: tools.Position{Line: 1}}},
			{URI: "file:///b.go", Range: tools.Range{Start: tools.Position{Line: 2}}},
		},
	}
	tool := tools.NewFindReferencesTool(mock)

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"file":   "/src/main.go",
		"line":   1,
		"column": 0,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "file:///a.go") || !strings.Contains(result, "file:///b.go") {
		t.Errorf("expected both URIs in result, got: %s", result)
	}
}

func TestFindReferencesToolExecute_Empty(t *testing.T) {
	tool := tools.NewFindReferencesTool(&mockLSPProvider{referenceLocs: []tools.Location{}})

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"file":   "/src/main.go",
		"line":   0,
		"column": 0,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty no-results message")
	}
}

func TestFindReferencesToolExecute_Error(t *testing.T) {
	mock := &mockLSPProvider{referenceErr: errors.New("timeout")}
	tool := tools.NewFindReferencesTool(mock)

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"file":   "/src/main.go",
		"line":   0,
		"column": 0,
	})
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// HoverInfoTool
// ---------------------------------------------------------------------------

func TestHoverInfoToolInterface(t *testing.T) {
	var _ agents.Tool = (*tools.HoverInfoTool)(nil)
}

func TestHoverInfoToolMeta(t *testing.T) {
	tool := tools.NewHoverInfoTool(&mockLSPProvider{})
	if tool.Name() != "hover_info" {
		t.Errorf("unexpected name: %s", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestHoverInfoToolExecute_WithText(t *testing.T) {
	mock := &mockLSPProvider{hoverText: "func Foo() string"}
	tool := tools.NewHoverInfoTool(mock)

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"file":   "/src/main.go",
		"line":   2,
		"column": 6,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "func Foo() string" {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestHoverInfoToolExecute_Empty(t *testing.T) {
	tool := tools.NewHoverInfoTool(&mockLSPProvider{hoverText: ""})

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"file":   "/src/main.go",
		"line":   0,
		"column": 0,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == "" {
		t.Error("expected a non-empty no-info message")
	}
}

func TestHoverInfoToolExecute_Error(t *testing.T) {
	mock := &mockLSPProvider{hoverErr: errors.New("lsp error")}
	tool := tools.NewHoverInfoTool(mock)

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"file":   "/src/main.go",
		"line":   0,
		"column": 0,
	})
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// DiagnosticsTool
// ---------------------------------------------------------------------------

func TestDiagnosticsToolInterface(t *testing.T) {
	var _ agents.Tool = (*tools.DiagnosticsTool)(nil)
}

func TestDiagnosticsToolMeta(t *testing.T) {
	tool := tools.NewDiagnosticsTool(&mockLSPProvider{})
	if tool.Name() != "diagnostics" {
		t.Errorf("unexpected name: %s", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestDiagnosticsToolExecute_WithDiagnostics(t *testing.T) {
	mock := &mockLSPProvider{
		diagnosticsList: []tools.Diagnostic{
			{
				Range:    tools.Range{Start: tools.Position{Line: 5}},
				Severity: 1,
				Message:  "undefined: Bar",
				Source:   "gopls",
			},
		},
	}
	tool := tools.NewDiagnosticsTool(mock)

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"file": "/src/main.go",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "undefined: Bar") {
		t.Errorf("expected diagnostic message in result, got: %s", result)
	}
	if !strings.Contains(result, "error") {
		t.Errorf("expected severity label in result, got: %s", result)
	}
}

func TestDiagnosticsToolExecute_Empty(t *testing.T) {
	tool := tools.NewDiagnosticsTool(&mockLSPProvider{diagnosticsList: []tools.Diagnostic{}})

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"file": "/src/main.go",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty no-diagnostics message")
	}
}

func TestDiagnosticsToolExecute_MissingFile(t *testing.T) {
	tool := tools.NewDiagnosticsTool(&mockLSPProvider{})

	cases := []map[string]interface{}{
		nil,
		{},
		{"file": ""},
		{"file": 42},
	}
	for _, args := range cases {
		_, err := tool.Execute(context.Background(), args)
		if err == nil {
			t.Errorf("expected error for args %v, got nil", args)
		}
	}
}

func TestDiagnosticsToolExecute_Error(t *testing.T) {
	mock := &mockLSPProvider{diagnosticsErr: errors.New("provider failure")}
	tool := tools.NewDiagnosticsTool(mock)

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"file": "/src/main.go",
	})
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestDiagnosticsToolSeverityLabels(t *testing.T) {
	severities := []struct {
		sev   int
		label string
	}{
		{1, "error"},
		{2, "warning"},
		{3, "info"},
		{4, "hint"},
	}

	for _, tc := range severities {
		mock := &mockLSPProvider{
			diagnosticsList: []tools.Diagnostic{
				{Severity: tc.sev, Message: "msg", Source: "test"},
			},
		}
		tool := tools.NewDiagnosticsTool(mock)
		result, err := tool.Execute(context.Background(), map[string]interface{}{"file": "/f.go"})
		if err != nil {
			t.Fatalf("severity %d: %v", tc.sev, err)
		}
		if !strings.Contains(result, tc.label) {
			t.Errorf("severity %d: expected label %q in %q", tc.sev, tc.label, result)
		}
	}
}
