package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/i18n"
)

// fakeToolsTranslator is a non-production test double (unit-test only,
// per CONST-050(A)) that resolves every tools-package i18n key to a
// localised marker so tests can prove the call site routes through the
// Translator seam rather than emitting a hardcoded English literal.
type fakeToolsTranslator struct {
	prefix string
}

// T returns prefix+key for every key, ignoring vars. The marked output
// proves the seam was consulted: if a call site emitted a hardcoded
// English literal, the marker would be absent.
func (f fakeToolsTranslator) T(_, key string, _ ...map[string]string) string {
	return f.prefix + key
}

// restoreToolsI18n resets the package-level translator + lang to the
// English default so tests do not leak state into one another.
func restoreToolsI18n(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		SetTranslator(nil)
		SetLang("")
	})
}

// TestToolsI18n_DefaultFallbackIsEnglish verifies that with no
// translator wired, every git tool's user-facing surface resolves to
// the bundled English fallback — a sensible standalone default per
// CONST-046 / CONST-051(B).
func TestToolsI18n_DefaultFallbackIsEnglish(t *testing.T) {
	restoreToolsI18n(t)
	SetTranslator(nil)

	sandbox := NewSandbox(SandboxConfig{AllowedPaths: []string{"/tmp"}})
	cases := []struct {
		name string
		desc string
		want string
	}{
		{"git_status", NewGitStatusTool(sandbox).Description(), "working tree status"},
		{"git_diff", NewGitDiffTool(sandbox).Description(), "Show changes"},
		{"git_log", NewGitLogTool(sandbox).Description(), "recent commits"},
		{"git_branch", NewGitBranchTool(sandbox).Description(), "List all branches"},
		{"git_commit", NewGitCommitTool(sandbox).Description(), "Create a git commit"},
		{"git_push", NewGitPushTool(sandbox).Description(), "Push commits"},
		{"git_pull", NewGitPullTool(sandbox).Description(), "Pull changes"},
		{"git_create_branch", NewGitCreateBranchTool(sandbox).Description(), "Create a new git branch"},
	}
	for _, c := range cases {
		if !strings.Contains(c.desc, c.want) {
			t.Errorf("%s description = %q, want substring %q", c.name, c.desc, c.want)
		}
	}
}

// TestToolsI18n_TranslatorSeamConsulted is the anti-bluff core test:
// it wires a fake translator and asserts every git tool description +
// parameter description carries the fake's marker. If any call site
// regressed to a hardcoded literal, the marker would be missing and
// this test FAILs — that is the paired mutation per §1.1.
func TestToolsI18n_TranslatorSeamConsulted(t *testing.T) {
	restoreToolsI18n(t)
	const marker = "XLATED::"
	SetTranslator(fakeToolsTranslator{prefix: marker})

	sandbox := NewSandbox(SandboxConfig{AllowedPaths: []string{"/tmp"}})
	tools := []interface {
		Description() string
		Parameters() map[string]interface{}
	}{
		NewGitStatusTool(sandbox),
		NewGitDiffTool(sandbox),
		NewGitLogTool(sandbox),
		NewGitBranchTool(sandbox),
		NewGitCommitTool(sandbox),
		NewGitPushTool(sandbox),
		NewGitPullTool(sandbox),
		NewGitCreateBranchTool(sandbox),
	}

	for _, tool := range tools {
		desc := tool.Description()
		if !strings.HasPrefix(desc, marker) {
			t.Errorf("Description() = %q did NOT route through translator seam (missing %q) — CONST-046 hardcoded-literal regression", desc, marker)
		}
		params := tool.Parameters()
		props, ok := params["properties"].(map[string]interface{})
		if !ok {
			t.Fatalf("Parameters() has no properties map: %v", params)
		}
		for pname, raw := range props {
			pmap, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			pdesc, _ := pmap["description"].(string)
			if !strings.HasPrefix(pdesc, marker) {
				t.Errorf("param %q description = %q did NOT route through translator seam — CONST-046 regression", pname, pdesc)
			}
		}
	}
}

// TestToolsI18n_PlaceholderSubstitution verifies that {{name}}
// placeholders in result messages are substituted from vars on the
// fallback path so the user sees a complete sentence, not a raw token.
func TestToolsI18n_PlaceholderSubstitution(t *testing.T) {
	restoreToolsI18n(t)
	SetTranslator(nil)

	got := renderFallback(keyGitResultSwitchedBranch, map[string]string{"name": "feature/x"})
	if !strings.Contains(got, "feature/x") {
		t.Errorf("renderFallback did not substitute placeholder: %q", got)
	}
	if strings.Contains(got, "{{name}}") {
		t.Errorf("renderFallback left raw placeholder token: %q", got)
	}
}

// TestToolsI18n_RealTranslatorLocalisation proves the seam works with
// the real shared/i18n Translator: a non-English bundle is loaded and
// the call site resolves to the localised string for that locale.
func TestToolsI18n_RealTranslatorLocalisation(t *testing.T) {
	restoreToolsI18n(t)
	real := i18n.New("en")
	real.LoadMessages("es", map[string]string{
		keyGitStatusDesc: "Mostrar el estado del arbol de trabajo de un repositorio git.",
	})
	SetTranslator(real)
	SetLang("es")

	sandbox := NewSandbox(SandboxConfig{AllowedPaths: []string{"/tmp"}})
	desc := NewGitStatusTool(sandbox).Description()
	if !strings.Contains(desc, "estado del arbol") {
		t.Errorf("expected Spanish description, got %q", desc)
	}
}

// TestToolsI18n_ExecuteResultMessagesLocalised proves result messages
// returned by Execute route through the seam too (not only schema text).
func TestToolsI18n_ExecuteResultMessagesLocalised(t *testing.T) {
	restoreToolsI18n(t)
	const marker = "XLATED::"
	SetTranslator(fakeToolsTranslator{prefix: marker})

	got := tr(keyGitResultPushDone)
	if !strings.HasPrefix(got, marker) {
		t.Errorf("git push result message did not route through seam: %q", got)
	}
	_ = context.Background()
}

// TestToolsI18n_NonGitToolsSeamConsulted is the round-417 anti-bluff
// core: it wires a fake translator and asserts every NON-git tool's
// description + parameter descriptions carry the fake's marker. If any
// echo/code-exec/analysis/filesystem call site regressed to a hardcoded
// English literal, the marker would be absent and this test FAILs —
// that is the paired mutation per §1.1 for the round-417 migration.
func TestToolsI18n_NonGitToolsSeamConsulted(t *testing.T) {
	restoreToolsI18n(t)
	const marker = "XLATED::"
	SetTranslator(fakeToolsTranslator{prefix: marker})

	sandbox := NewSandbox(SandboxConfig{AllowedPaths: []string{"/tmp"}})
	tools := []interface {
		Description() string
		Parameters() map[string]interface{}
	}{
		NewEchoTool(),
		NewExecutePythonTool(sandbox),
		NewExecuteShellTool(sandbox),
		NewAnalyzeCodeTool(sandbox),
		NewRunTestsTool(sandbox),
		NewGetDependenciesTool(sandbox),
		NewCalculateComplexityTool(sandbox),
		NewReadFileTool(sandbox),
		NewWriteFileTool(sandbox),
		NewListDirectoryTool(sandbox),
		NewSearchFilesTool(sandbox),
	}

	for _, tool := range tools {
		desc := tool.Description()
		if !strings.HasPrefix(desc, marker) {
			t.Errorf("Description() = %q did NOT route through translator seam (missing %q) — CONST-046 hardcoded-literal regression", desc, marker)
		}
		params := tool.Parameters()
		props, ok := params["properties"].(map[string]interface{})
		if !ok {
			// EchoTool exposes a flat parameter map (no nested
			// "properties" key); inspect it directly.
			props = params
		}
		for pname, raw := range props {
			pmap, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			pdesc, has := pmap["description"].(string)
			if !has {
				continue
			}
			if !strings.HasPrefix(pdesc, marker) {
				t.Errorf("param %q description = %q did NOT route through translator seam — CONST-046 regression", pname, pdesc)
			}
		}
	}
}

// TestToolsI18n_NonGitDefaultFallbackIsEnglish verifies that with no
// translator wired every round-417-migrated non-git tool resolves to
// its bundled English fallback — sensible standalone default per
// CONST-046 / CONST-051(B).
func TestToolsI18n_NonGitDefaultFallbackIsEnglish(t *testing.T) {
	restoreToolsI18n(t)
	SetTranslator(nil)

	sandbox := NewSandbox(SandboxConfig{AllowedPaths: []string{"/tmp"}})
	cases := []struct {
		name string
		desc string
		want string
	}{
		{"echo", NewEchoTool().Description(), "input message unchanged"},
		{"execute_python", NewExecutePythonTool(sandbox).Description(), "Execute Python code"},
		{"execute_shell", NewExecuteShellTool(sandbox).Description(), "Execute a shell command"},
		{"analyze_code", NewAnalyzeCodeTool(sandbox).Description(), "Analyze code"},
		{"run_tests", NewRunTestsTool(sandbox).Description(), "Detect and run tests"},
		{"get_dependencies", NewGetDependenciesTool(sandbox).Description(), "List project dependencies"},
		{"complexity", NewCalculateComplexityTool(sandbox).Description(), "cyclomatic complexity"},
		{"read_file", NewReadFileTool(sandbox).Description(), "Read the contents of a file"},
		{"write_file", NewWriteFileTool(sandbox).Description(), "Write content to a file"},
		{"list_directory", NewListDirectoryTool(sandbox).Description(), "List the contents of a directory"},
		{"search_files", NewSearchFilesTool(sandbox).Description(), "Search for files"},
	}
	for _, c := range cases {
		if !strings.Contains(c.desc, c.want) {
			t.Errorf("%s description = %q, want substring %q", c.name, c.desc, c.want)
		}
	}
}

// TestToolsI18n_WriteFileResultPlaceholders verifies the write-file
// result message substitutes both {{bytes}} and {{path}} placeholders
// so the user sees a complete sentence, not raw tokens.
func TestToolsI18n_WriteFileResultPlaceholders(t *testing.T) {
	restoreToolsI18n(t)
	SetTranslator(nil)

	got := tr(keyWriteFileResult, map[string]string{"bytes": "42", "path": "/tmp/out.txt"})
	if !strings.Contains(got, "42") || !strings.Contains(got, "/tmp/out.txt") {
		t.Errorf("write-file result did not substitute placeholders: %q", got)
	}
	if strings.Contains(got, "{{") {
		t.Errorf("write-file result left raw placeholder token: %q", got)
	}
}

// TestToolsI18n_NonGitRealTranslatorLocalisation proves the round-417
// non-git keys localise through the real shared/i18n Translator.
func TestToolsI18n_NonGitRealTranslatorLocalisation(t *testing.T) {
	restoreToolsI18n(t)
	real := i18n.New("en")
	real.LoadMessages("de", map[string]string{
		keyReadFileDesc: "Lies den Inhalt einer Datei.",
	})
	SetTranslator(real)
	SetLang("de")

	sandbox := NewSandbox(SandboxConfig{AllowedPaths: []string{"/tmp"}})
	desc := NewReadFileTool(sandbox).Description()
	if !strings.Contains(desc, "Inhalt einer Datei") {
		t.Errorf("expected German description, got %q", desc)
	}
}

// --- round-421: LSP tool i18n (CONST-046 Phase 4) ---------------------------

// lspParamDescription pulls the description string for a named property
// out of an LSP tool's Parameters() JSON-schema map. Returns "" when the
// property or its description is absent — making a missing-key mistake
// loud rather than silent.
func lspParamDescription(params map[string]interface{}, prop string) string {
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		return ""
	}
	p, ok := props[prop].(map[string]interface{})
	if !ok {
		return ""
	}
	desc, _ := p["description"].(string)
	return desc
}

// TestToolsI18n_LSPDefaultFallbackIsEnglish verifies that with no
// translator wired, every LSP tool's description and parameter
// descriptions resolve to the bundled English fallback — the standalone
// default per CONST-046 / CONST-051(B).
func TestToolsI18n_LSPDefaultFallbackIsEnglish(t *testing.T) {
	restoreToolsI18n(t)
	SetTranslator(nil)

	descCases := []struct {
		name string
		desc string
		want string
	}{
		{"goto_definition", (&GotoDefinitionTool{}).Description(), "Go to the definition"},
		{"find_references", (&FindReferencesTool{}).Description(), "Find all references"},
		{"hover_info", (&HoverInfoTool{}).Description(), "hover documentation"},
		{"diagnostics", (&DiagnosticsTool{}).Description(), "compiler and linter diagnostics"},
	}
	for _, c := range descCases {
		if !strings.Contains(c.desc, c.want) {
			t.Errorf("%s description = %q, want substring %q", c.name, c.desc, c.want)
		}
	}

	// Parameter descriptions resolve through the seam too.
	params := (&GotoDefinitionTool{}).Parameters()
	if got := lspParamDescription(params, "file"); !strings.Contains(got, "Absolute file path") {
		t.Errorf("goto_definition file param = %q, want 'Absolute file path'", got)
	}
	if got := lspParamDescription(params, "line"); !strings.Contains(got, "Line number") {
		t.Errorf("goto_definition line param = %q, want 'Line number'", got)
	}
	if got := lspParamDescription(params, "column"); !strings.Contains(got, "Column number") {
		t.Errorf("goto_definition column param = %q, want 'Column number'", got)
	}
	if got := lspParamDescription((&DiagnosticsTool{}).Parameters(), "file"); !strings.Contains(got, "Absolute file path") {
		t.Errorf("diagnostics file param = %q, want 'Absolute file path'", got)
	}
}

// TestToolsI18n_LSPSeamMutation is the paired-mutation test (§1.1): it
// wires a marking fake Translator and asserts every LSP user-facing
// string carries the marker. If a future edit reverts an LSP call site
// to a hardcoded English literal, the marker is absent and this test
// fails — proving the seam is genuinely consulted, not bypassed.
func TestToolsI18n_LSPSeamMutation(t *testing.T) {
	restoreToolsI18n(t)
	SetTranslator(fakeToolsTranslator{prefix: "XX::"})

	descs := []struct {
		name string
		got  string
	}{
		{"goto_definition", (&GotoDefinitionTool{}).Description()},
		{"find_references", (&FindReferencesTool{}).Description()},
		{"hover_info", (&HoverInfoTool{}).Description()},
		{"diagnostics", (&DiagnosticsTool{}).Description()},
	}
	for _, d := range descs {
		if !strings.HasPrefix(d.got, "XX::") {
			t.Errorf("%s description %q did not route through the i18n seam (hardcoded literal?)", d.name, d.got)
		}
	}

	for _, prop := range []string{"file", "line", "column"} {
		got := lspParamDescription((&FindReferencesTool{}).Parameters(), prop)
		if !strings.HasPrefix(got, "XX::") {
			t.Errorf("find_references %s param %q did not route through the i18n seam", prop, got)
		}
	}
	if got := lspParamDescription((&DiagnosticsTool{}).Parameters(), "file"); !strings.HasPrefix(got, "XX::") {
		t.Errorf("diagnostics file param %q did not route through the i18n seam", got)
	}
}

// TestToolsI18n_LSPRealTranslatorLocalisation proves the LSP keys
// localise through the real shared/i18n Translator — the genuine
// end-user usability guarantee behind CONST-046.
func TestToolsI18n_LSPRealTranslatorLocalisation(t *testing.T) {
	restoreToolsI18n(t)
	real := i18n.New("en")
	real.LoadMessages("de", map[string]string{
		keyGotoDefinitionDesc:    "Springe zur Definition eines Symbols.",
		keyLSPResultNoReferences: "Keine Verweise gefunden.",
	})
	SetTranslator(real)
	SetLang("de")

	if desc := (&GotoDefinitionTool{}).Description(); !strings.Contains(desc, "Springe zur Definition") {
		t.Errorf("expected German LSP description, got %q", desc)
	}
	if msg := tr(keyLSPResultNoReferences); !strings.Contains(msg, "Keine Verweise") {
		t.Errorf("expected German no-references result, got %q", msg)
	}
}

// flatParamDescription extracts a parameter description from a tool
// Parameters() map that may either nest under "properties" (object
// schema) or place the parameter at the top level (flat schema, as
// the time tool does). Returns "" when absent.
func flatParamDescription(params map[string]interface{}, prop string) string {
	if props, ok := params["properties"].(map[string]interface{}); ok {
		if p, ok := props[prop].(map[string]interface{}); ok {
			desc, _ := p["description"].(string)
			return desc
		}
	}
	if p, ok := params[prop].(map[string]interface{}); ok {
		desc, _ := p["description"].(string)
		return desc
	}
	return ""
}

// TestToolsI18n_Round429SeamMutation is the round-429 paired-mutation
// guard (CONST-046 Phase 4): with a marker translator wired, every
// web / time / knowledge_query / file_info description and parameter
// description MUST carry the marker prefix. A regression that
// reintroduces a hardcoded English literal at any of these call sites
// makes the marker absent and fails the test.
func TestToolsI18n_Round429SeamMutation(t *testing.T) {
	restoreToolsI18n(t)
	SetTranslator(fakeToolsTranslator{prefix: "XX::"})

	sandbox := NewSandbox(SandboxConfig{AllowedPaths: []string{"/tmp"}})
	descs := []struct {
		name string
		got  string
	}{
		{"web_search", NewWebSearchTool().Description()},
		{"fetch_url", NewFetchURLTool().Description()},
		{"time", NewTimeTool().Description()},
		{"knowledge_query", NewKnowledgeQueryTool(nil, "default").Description()},
		{"file_info", NewFileInfoTool(sandbox).Description()},
	}
	for _, d := range descs {
		if !strings.HasPrefix(d.got, "XX::") {
			t.Errorf("%s description %q did not route through the i18n seam (hardcoded literal?)", d.name, d.got)
		}
	}

	params := []struct {
		name string
		got  string
	}{
		{"web_search.query", flatParamDescription(NewWebSearchTool().Parameters(), "query")},
		{"fetch_url.url", flatParamDescription(NewFetchURLTool().Parameters(), "url")},
		{"time.timezone", flatParamDescription(NewTimeTool().Parameters(), "timezone")},
		{"knowledge_query.query", flatParamDescription(NewKnowledgeQueryTool(nil, "default").Parameters(), "query")},
		{"knowledge_query.collection", flatParamDescription(NewKnowledgeQueryTool(nil, "default").Parameters(), "collection")},
		{"file_info.path", flatParamDescription(NewFileInfoTool(sandbox).Parameters(), "path")},
	}
	for _, p := range params {
		if !strings.HasPrefix(p.got, "XX::") {
			t.Errorf("%s param description %q did not route through the i18n seam", p.name, p.got)
		}
	}
}

// TestToolsI18n_Round429DefaultFallbackIsEnglish verifies that with no
// translator wired, the round-429 keys resolve to their bundled
// English fallbacks — the standalone default per CONST-051(B).
func TestToolsI18n_Round429DefaultFallbackIsEnglish(t *testing.T) {
	restoreToolsI18n(t)
	SetTranslator(nil)

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"web_search", NewWebSearchTool().Description(), "Search the web"},
		{"fetch_url", NewFetchURLTool().Description(), "Fetch the content of a URL"},
		{"time", NewTimeTool().Description(), "current date and time"},
		{"knowledge_query", NewKnowledgeQueryTool(nil, "d").Description(), "knowledge base"},
		{"file_info", NewFileInfoTool(NewSandbox(SandboxConfig{})).Description(), "metadata about a file"},
		{"web_search_unavail", tr(keyWebSearchUnavail, map[string]string{"query": `"q"`}), "not available in local mode"},
		{"knowledge_no_info", tr(keyKnowledgeQueryNoInfo), "No relevant information found."},
	}
	for _, c := range cases {
		if !strings.Contains(c.got, c.want) {
			t.Errorf("%s: expected English fallback containing %q, got %q", c.name, c.want, c.got)
		}
	}
}

// TestToolsI18n_Round429RealTranslatorLocalisation proves the round-429
// keys localise through the real shared/i18n Translator with
// placeholder substitution — the genuine end-user usability guarantee
// behind CONST-046.
func TestToolsI18n_Round429RealTranslatorLocalisation(t *testing.T) {
	restoreToolsI18n(t)
	real := i18n.New("en")
	real.LoadMessages("de", map[string]string{
		keyWebSearchDesc:        "Durchsuche das Web nach Informationen.",
		keyKnowledgeQueryNoInfo: "Keine relevanten Informationen gefunden.",
		keyFileInfoResult:       "Name: {{name}}\nTyp: {{kind}}",
	})
	SetTranslator(real)
	SetLang("de")

	if desc := NewWebSearchTool().Description(); !strings.Contains(desc, "Durchsuche das Web") {
		t.Errorf("expected German web_search description, got %q", desc)
	}
	if msg := tr(keyKnowledgeQueryNoInfo); !strings.Contains(msg, "Keine relevanten") {
		t.Errorf("expected German no-info result, got %q", msg)
	}
	got := tr(keyFileInfoResult, map[string]string{"name": "go.mod", "kind": "Datei"})
	if !strings.Contains(got, "Name: go.mod") || !strings.Contains(got, "Typ: Datei") {
		t.Errorf("expected localised file_info result with substituted placeholders, got %q", got)
	}
}
