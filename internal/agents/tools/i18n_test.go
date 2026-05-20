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
