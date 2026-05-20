package tools

import (
	"context"
	"fmt"
	"strings"
)

// GotoDefinitionTool wraps LSP textDocument/definition as a Tool.
type GotoDefinitionTool struct {
	client LSPProvider
}

// NewGotoDefinitionTool creates a GotoDefinitionTool backed by the given LSPProvider.
func NewGotoDefinitionTool(client LSPProvider) *GotoDefinitionTool {
	return &GotoDefinitionTool{client: client}
}

func (t *GotoDefinitionTool) Name() string { return "goto_definition" }
func (t *GotoDefinitionTool) Description() string {
	return tr(keyGotoDefinitionDesc)
}

func (t *GotoDefinitionTool) Parameters() map[string]interface{} {
	return filePositionParameters()
}

func (t *GotoDefinitionTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	file, line, col, err := extractFilePosition(args)
	if err != nil {
		return "", fmt.Errorf("goto_definition: %w", err)
	}

	locs, err := t.client.Definition(ctx, file, line, col)
	if err != nil {
		return "", fmt.Errorf("goto_definition: %w", err)
	}

	if len(locs) == 0 {
		return tr(keyLSPResultNoDefinition), nil
	}
	return formatLocations(locs), nil
}

// FindReferencesTool wraps LSP textDocument/references as a Tool.
type FindReferencesTool struct {
	client LSPProvider
}

// NewFindReferencesTool creates a FindReferencesTool backed by the given LSPProvider.
func NewFindReferencesTool(client LSPProvider) *FindReferencesTool {
	return &FindReferencesTool{client: client}
}

func (t *FindReferencesTool) Name() string { return "find_references" }
func (t *FindReferencesTool) Description() string {
	return tr(keyFindReferencesDesc)
}

func (t *FindReferencesTool) Parameters() map[string]interface{} {
	return filePositionParameters()
}

func (t *FindReferencesTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	file, line, col, err := extractFilePosition(args)
	if err != nil {
		return "", fmt.Errorf("find_references: %w", err)
	}

	locs, err := t.client.References(ctx, file, line, col)
	if err != nil {
		return "", fmt.Errorf("find_references: %w", err)
	}

	if len(locs) == 0 {
		return tr(keyLSPResultNoReferences), nil
	}
	return formatLocations(locs), nil
}

// HoverInfoTool wraps LSP textDocument/hover as a Tool.
type HoverInfoTool struct {
	client LSPProvider
}

// NewHoverInfoTool creates a HoverInfoTool backed by the given LSPProvider.
func NewHoverInfoTool(client LSPProvider) *HoverInfoTool {
	return &HoverInfoTool{client: client}
}

func (t *HoverInfoTool) Name() string { return "hover_info" }
func (t *HoverInfoTool) Description() string {
	return tr(keyHoverInfoDesc)
}

func (t *HoverInfoTool) Parameters() map[string]interface{} {
	return filePositionParameters()
}

func (t *HoverInfoTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	file, line, col, err := extractFilePosition(args)
	if err != nil {
		return "", fmt.Errorf("hover_info: %w", err)
	}

	text, err := t.client.Hover(ctx, file, line, col)
	if err != nil {
		return "", fmt.Errorf("hover_info: %w", err)
	}

	if text == "" {
		return tr(keyLSPResultNoHover), nil
	}
	return text, nil
}

// DiagnosticsTool wraps LSP textDocument/publishDiagnostics as a Tool.
// Diagnostics are pushed by the server; this tool returns the cached values.
type DiagnosticsTool struct {
	client LSPProvider
}

// NewDiagnosticsTool creates a DiagnosticsTool backed by the given LSPProvider.
func NewDiagnosticsTool(client LSPProvider) *DiagnosticsTool {
	return &DiagnosticsTool{client: client}
}

func (t *DiagnosticsTool) Name() string { return "diagnostics" }
func (t *DiagnosticsTool) Description() string {
	return tr(keyDiagnosticsDesc)
}

func (t *DiagnosticsTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"file": map[string]interface{}{"type": "string", "description": tr(keyLSPParamFile)},
		},
		"required": []string{"file"},
	}
}

func (t *DiagnosticsTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if args == nil {
		return "", fmt.Errorf("diagnostics: arguments must not be nil")
	}

	fileVal, ok := args["file"]
	if !ok {
		return "", fmt.Errorf("diagnostics: missing required parameter 'file'")
	}
	file, ok := fileVal.(string)
	if !ok || file == "" {
		return "", fmt.Errorf("diagnostics: 'file' must be a non-empty string")
	}

	diags, err := t.client.Diagnostics(ctx, file)
	if err != nil {
		return "", fmt.Errorf("diagnostics: %w", err)
	}

	if len(diags) == 0 {
		return tr(keyLSPResultNoDiagnostics), nil
	}
	return formatDiagnostics(diags), nil
}

// --- shared helpers -----------------------------------------------------------

// filePositionParameters returns the JSON-schema parameter map shared by
// the goto_definition, find_references, and hover_info LSP tools. The
// file/line/column parameter descriptions resolve through the package
// i18n seam (tr) so they localise per CONST-046 rather than living as
// hardcoded English literals.
func filePositionParameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"file":   map[string]interface{}{"type": "string", "description": tr(keyLSPParamFile)},
			"line":   map[string]interface{}{"type": "integer", "description": tr(keyLSPParamLine)},
			"column": map[string]interface{}{"type": "integer", "description": tr(keyLSPParamColumn)},
		},
		"required": []string{"file", "line", "column"},
	}
}

// extractFilePosition pulls file, line, and column out of an args map with
// consistent validation and error messages.
func extractFilePosition(args map[string]interface{}) (file string, line, col int, err error) {
	if args == nil {
		return "", 0, 0, fmt.Errorf("arguments must not be nil")
	}

	fileVal, ok := args["file"]
	if !ok {
		return "", 0, 0, fmt.Errorf("missing required parameter 'file'")
	}
	file, ok = fileVal.(string)
	if !ok || file == "" {
		return "", 0, 0, fmt.Errorf("'file' must be a non-empty string")
	}

	line, err = extractInt(args, "line")
	if err != nil {
		return "", 0, 0, err
	}
	col, err = extractInt(args, "column")
	if err != nil {
		return "", 0, 0, err
	}
	return file, line, col, nil
}

// extractInt retrieves an integer value from an args map. JSON numbers
// unmarshalled into interface{} arrive as float64, so we handle that case.
func extractInt(args map[string]interface{}, key string) (int, error) {
	v, ok := args[key]
	if !ok {
		return 0, fmt.Errorf("missing required parameter %q", key)
	}
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	default:
		return 0, fmt.Errorf("parameter %q must be an integer, got %T", key, v)
	}
}

// formatLocations renders a slice of Location values as a human-readable string.
func formatLocations(locs []Location) string {
	var sb strings.Builder
	for i, loc := range locs {
		if i > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "%s:%d:%d",
			loc.URI,
			loc.Range.Start.Line,
			loc.Range.Start.Character,
		)
	}
	return sb.String()
}

// severityLabel converts a numeric LSP diagnostic severity to a label.
func severityLabel(s int) string {
	switch s {
	case 1:
		return "error"
	case 2:
		return "warning"
	case 3:
		return "info"
	case 4:
		return "hint"
	default:
		return "unknown"
	}
}

// formatDiagnostics renders a slice of Diagnostic values as a human-readable string.
func formatDiagnostics(diags []Diagnostic) string {
	var sb strings.Builder
	for i, d := range diags {
		if i > 0 {
			sb.WriteString("\n")
		}
		source := d.Source
		if source == "" {
			source = "lsp"
		}
		fmt.Fprintf(&sb, "[%s] %s line %d: %s",
			severityLabel(d.Severity),
			source,
			d.Range.Start.Line,
			d.Message,
		)
	}
	return sb.String()
}
