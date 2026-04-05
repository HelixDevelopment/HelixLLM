package tools

import (
	"context"
	"encoding/json"
	"testing"
)

// ---------------------------------------------------------------------------
// lspError.Error
// ---------------------------------------------------------------------------

func TestLspError_Error(t *testing.T) {
	e := &lspError{Code: -32601, Message: "method not found"}
	got := e.Error()
	want := "LSP error -32601: method not found"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// fileURI
// ---------------------------------------------------------------------------

func TestFileURI_AlreadyFileScheme(t *testing.T) {
	uri := fileURI("file:///home/user/project/main.go")
	if uri != "file:///home/user/project/main.go" {
		t.Errorf("expected unchanged URI, got %q", uri)
	}
}

func TestFileURI_AbsolutePath(t *testing.T) {
	uri := fileURI("/home/user/project/main.go")
	if uri != "file:///home/user/project/main.go" {
		t.Errorf("expected file:// prefix, got %q", uri)
	}
}

func TestFileURI_RelativePath(t *testing.T) {
	uri := fileURI("src/main.go")
	if uri != "file:///src/main.go" {
		t.Errorf("expected file:/// prefix for relative path, got %q", uri)
	}
}

// ---------------------------------------------------------------------------
// parseLocations
// ---------------------------------------------------------------------------

func TestParseLocations_Null(t *testing.T) {
	locs, err := parseLocations(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(locs) != 0 {
		t.Errorf("expected empty, got %d locations", len(locs))
	}
}

func TestParseLocations_NullString(t *testing.T) {
	locs, err := parseLocations(json.RawMessage(`null`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(locs) != 0 {
		t.Errorf("expected empty, got %d locations", len(locs))
	}
}

func TestParseLocations_Array(t *testing.T) {
	raw := json.RawMessage(`[{"uri":"file:///a.go","range":{"start":{"line":1,"character":2},"end":{"line":1,"character":5}}}]`)
	locs, err := parseLocations(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location, got %d", len(locs))
	}
	if locs[0].URI != "file:///a.go" {
		t.Errorf("URI = %q", locs[0].URI)
	}
}

func TestParseLocations_SingleObject(t *testing.T) {
	raw := json.RawMessage(`{"uri":"file:///b.go","range":{"start":{"line":3,"character":0},"end":{"line":3,"character":5}}}`)
	locs, err := parseLocations(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location, got %d", len(locs))
	}
	if locs[0].URI != "file:///b.go" {
		t.Errorf("URI = %q", locs[0].URI)
	}
}

func TestParseLocations_InvalidJSON(t *testing.T) {
	raw := json.RawMessage(`"not a location"`)
	_, err := parseLocations(raw)
	if err == nil {
		t.Fatal("expected error for invalid location JSON, got nil")
	}
}

// ---------------------------------------------------------------------------
// extractHoverContents
// ---------------------------------------------------------------------------

func TestExtractHoverContents_Nil(t *testing.T) {
	result := extractHoverContents(nil)
	if result != "" {
		t.Errorf("expected empty for nil, got %q", result)
	}
}

func TestExtractHoverContents_String(t *testing.T) {
	result := extractHoverContents("func Foo() string")
	if result != "func Foo() string" {
		t.Errorf("expected string passthrough, got %q", result)
	}
}

func TestExtractHoverContents_MapWithValue(t *testing.T) {
	m := map[string]interface{}{
		"kind":  "markdown",
		"value": "func Bar() int",
	}
	result := extractHoverContents(m)
	if result != "func Bar() int" {
		t.Errorf("expected value from map, got %q", result)
	}
}

func TestExtractHoverContents_MapWithLanguageAndValue(t *testing.T) {
	m := map[string]interface{}{
		"language": "go",
		"value":    "type Foo struct{}",
	}
	result := extractHoverContents(m)
	if result != "type Foo struct{}" {
		t.Errorf("expected value from language-tagged map, got %q", result)
	}
}

func TestExtractHoverContents_Array(t *testing.T) {
	arr := []interface{}{
		"first part",
		map[string]interface{}{"value": "second part"},
	}
	result := extractHoverContents(arr)
	if result != "first part\nsecond part" {
		t.Errorf("expected joined parts, got %q", result)
	}
}

func TestExtractHoverContents_EmptyArray(t *testing.T) {
	arr := []interface{}{}
	result := extractHoverContents(arr)
	if result != "" {
		t.Errorf("expected empty for empty array, got %q", result)
	}
}

func TestExtractHoverContents_MapWithoutValue(t *testing.T) {
	m := map[string]interface{}{
		"kind": "plaintext",
	}
	// Falls through to the fmt.Sprintf fallback.
	result := extractHoverContents(m)
	if result == "" {
		t.Error("expected non-empty fallback string")
	}
}

func TestExtractHoverContents_IntFallback(t *testing.T) {
	result := extractHoverContents(42)
	if result != "42" {
		t.Errorf("expected %q, got %q", "42", result)
	}
}

func TestExtractHoverContents_MapValueNotString(t *testing.T) {
	m := map[string]interface{}{
		"value": 123, // not a string
	}
	// The "value" key exists but is not a string, so the first branch returns false.
	// Then the "language" key check also fails. Falls to fmt.Sprintf.
	result := extractHoverContents(m)
	if result == "" {
		t.Error("expected non-empty fallback")
	}
}

// ---------------------------------------------------------------------------
// severityLabel (default case)
// ---------------------------------------------------------------------------

func TestSeverityLabel_Unknown(t *testing.T) {
	label := severityLabel(99)
	if label != "unknown" {
		t.Errorf("expected %q, got %q", "unknown", label)
	}
}

func TestSeverityLabel_Zero(t *testing.T) {
	label := severityLabel(0)
	if label != "unknown" {
		t.Errorf("expected %q for 0, got %q", "unknown", label)
	}
}

// ---------------------------------------------------------------------------
// formatDiagnostics (empty source fallback)
// ---------------------------------------------------------------------------

func TestFormatDiagnostics_EmptySource(t *testing.T) {
	diags := []Diagnostic{
		{
			Range:    Range{Start: Position{Line: 10}},
			Severity: 2,
			Message:  "unused variable",
			Source:   "", // empty source should default to "lsp"
		},
	}
	result := formatDiagnostics(diags)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !contains(result, "lsp") {
		t.Errorf("expected 'lsp' as fallback source, got %q", result)
	}
	if !contains(result, "warning") {
		t.Errorf("expected 'warning' label, got %q", result)
	}
}

func TestFormatDiagnostics_MultipleDiagnostics(t *testing.T) {
	diags := []Diagnostic{
		{Severity: 1, Message: "error one", Source: "gopls"},
		{Severity: 3, Message: "info two", Source: "golint"},
	}
	result := formatDiagnostics(diags)
	if !contains(result, "error one") || !contains(result, "info two") {
		t.Errorf("expected both diagnostics in output, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// extractInt edge cases
// ---------------------------------------------------------------------------

func TestExtractInt_Int64(t *testing.T) {
	args := map[string]interface{}{"val": int64(42)}
	n, err := extractInt(args, "val")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 42 {
		t.Errorf("expected 42, got %d", n)
	}
}

func TestExtractInt_InvalidType(t *testing.T) {
	args := map[string]interface{}{"val": "not-a-number"}
	_, err := extractInt(args, "val")
	if err == nil {
		t.Fatal("expected error for string type, got nil")
	}
}

func TestExtractInt_MissingKey(t *testing.T) {
	args := map[string]interface{}{}
	_, err := extractInt(args, "missing")
	if err == nil {
		t.Fatal("expected error for missing key, got nil")
	}
}

// ---------------------------------------------------------------------------
// Named LSP tool variants
// ---------------------------------------------------------------------------

func TestNamedGotoDefinitionTool_Name(t *testing.T) {
	mock := &mockLSP{}
	tool := newNamedGotoDefinitionTool(mock, "_go")
	if tool.Name() != "goto_definition_go" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "goto_definition_go")
	}
}

func TestNamedGotoDefinitionTool_NoSuffix(t *testing.T) {
	mock := &mockLSP{}
	tool := newNamedGotoDefinitionTool(mock, "")
	if tool.Name() != "goto_definition" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "goto_definition")
	}
}

func TestNamedFindReferencesTool_Name(t *testing.T) {
	mock := &mockLSP{}
	tool := newNamedFindReferencesTool(mock, "_python")
	if tool.Name() != "find_references_python" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "find_references_python")
	}
}

func TestNamedFindReferencesTool_NoSuffix(t *testing.T) {
	mock := &mockLSP{}
	tool := newNamedFindReferencesTool(mock, "")
	if tool.Name() != "find_references" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "find_references")
	}
}

func TestNamedHoverInfoTool_Name(t *testing.T) {
	mock := &mockLSP{}
	tool := newNamedHoverInfoTool(mock, "_ts")
	if tool.Name() != "hover_info_ts" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "hover_info_ts")
	}
}

func TestNamedHoverInfoTool_NoSuffix(t *testing.T) {
	mock := &mockLSP{}
	tool := newNamedHoverInfoTool(mock, "")
	if tool.Name() != "hover_info" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "hover_info")
	}
}

func TestNamedDiagnosticsTool_Name(t *testing.T) {
	mock := &mockLSP{}
	tool := newNamedDiagnosticsTool(mock, "_rust")
	if tool.Name() != "diagnostics_rust" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "diagnostics_rust")
	}
}

func TestNamedDiagnosticsTool_NoSuffix(t *testing.T) {
	mock := &mockLSP{}
	tool := newNamedDiagnosticsTool(mock, "")
	if tool.Name() != "diagnostics" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "diagnostics")
	}
}

// helper
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// mockLSP satisfies LSPProvider for internal package tests.
type mockLSP struct{}

func (m *mockLSP) Definition(_ context.Context, _ string, _, _ int) ([]Location, error) {
	return nil, nil
}
func (m *mockLSP) References(_ context.Context, _ string, _, _ int) ([]Location, error) {
	return nil, nil
}
func (m *mockLSP) Hover(_ context.Context, _ string, _, _ int) (string, error) {
	return "", nil
}
func (m *mockLSP) Diagnostics(_ context.Context, _ string) ([]Diagnostic, error) {
	return nil, nil
}
func (m *mockLSP) Close() error { return nil }
