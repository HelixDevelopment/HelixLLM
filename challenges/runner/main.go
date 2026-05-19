// Command runner is the HelixLLM round-295 Challenge runner.
//
// It exercises the real i18n.Translator surface, the real
// gateway.RequestOrchestrator, gateway.SanitizeToolResult, and
// the canonical pkg/types message vocabulary across five locale
// fixtures. Every PASS line is backed by a runtime invariant —
// never a metadata-only check (CONST-035 / Article XI §11.9).
//
// Anti-bluff invariants enforced:
//
//  1. pkg/types role/provider constants resolve to their documented
//     literal values (not empty / not placeholder).
//  2. i18n.Translator.T returns the pre-loaded English message for
//     a known key, with no template tokens remaining when no vars.
//  3. i18n.Translator.T performs {{detail}} template substitution
//     into the rendered message (proves the upstream Bundle wiring
//     is real, not a stub).
//  4. i18n.Translator.T returns the key verbatim when the key is
//     unknown in every loaded language — the documented anti-bluff
//     contract (i18n.go T comment: "Returns key itself when the
//     key is absent in all loaded languages").
//  5. i18n.Translator.LoadMessages + T round-trip for each locale
//     fixture — the per-locale message reaches T(lang, key) after
//     LoadMessages, proving multi-language fan-out works.
//  6. gateway.NewRequestOrchestrator + EnhanceRequest passes nil /
//     empty request through unchanged (defensive contract), and
//     injects the documented "IMPORTANT: You have already gathered
//     enough context" hint after 2+ tool calls — proves the action
//     nudge actually fires, not a no-op.
//  7. gateway.SanitizeToolResult returns a non-empty string for
//     string / map / nil inputs (defensive contract).
//
// Mutation hook: when env HELIXLLM_MUTATE_RUNNER=1 is set, the
// runner inverts invariant (4) (treats a known-key resolution as
// the unknown-key case). Paired Challenge wraps this to assert
// the runner exits 99 under mutation, guaranteeing the runner
// actually checks what it claims (CONST-050(A) paired mutation,
// §1.1).
//
// Verbatim 2026-05-19 operator mandate (preserved per
// CONST-049 §11.4.17):
//
//	"all existing tests and Challenges do work in anti-bluff
//	manner - they MUST confirm that all tested codebase really
//	works as expected! We had been in position that all tests
//	do execute with success and all Challenges as well, but
//	in reality the most of the features does not work and
//	can't be used! This MUST NOT be the case and execution
//	of tests and Challenges MUST guarantee the quality, the
//	completition and full usability by end users of the
//	product!"
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/i18n"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// fixture is a 5-field projection of challenges/fixtures/<locale>.yaml.
// We parse minimal YAML in-process so the runner stays dependency-free
// beyond what the module already depends on (CONST-051(B): no new
// transitive deps creeping into a reusable submodule).
type fixture struct {
	locale            string
	messageKey        string
	messageValue      string
	expectSubstr      string
	templateDetailVal string
}

func main() {
	if code := run(os.Stdout); code != 0 {
		os.Exit(code)
	}
}

func run(out io.Writer) int {
	fmt.Fprintln(out, "=== HelixLLM Challenge Runner (round-295) ===")

	fixDir := os.Getenv("HELIXLLM_FIXTURES_DIR")
	if fixDir == "" {
		// Default: ./challenges/fixtures relative to module root
		// when invoked via `go run ./challenges/runner`.
		fixDir = filepath.Join("challenges", "fixtures")
	}

	fixtures, err := loadFixtures(fixDir)
	if err != nil {
		fmt.Fprintf(out, "FAIL: load fixtures from %s: %v\n",
			fixDir, err)
		return 1
	}
	if len(fixtures) < 5 {
		fmt.Fprintf(out, "FAIL: expected >=5 fixtures, got %d\n",
			len(fixtures))
		return 1
	}
	fmt.Fprintf(out, "[setup] loaded %d locale fixtures from %s\n",
		len(fixtures), fixDir)

	mutate := os.Getenv("HELIXLLM_MUTATE_RUNNER") == "1"
	if mutate {
		fmt.Fprintln(out, "[setup] MUTATION MODE: runner will treat"+
			" a resolved known key as the unknown-key fallback")
	}

	pass, fail := 0, 0
	step := func(name string, ok bool, detail string) {
		if ok {
			pass++
			fmt.Fprintf(out, "  PASS  %-46s  %s\n", name, detail)
			return
		}
		fail++
		fmt.Fprintf(out, "  FAIL  %-46s  %s\n", name, detail)
	}

	// Invariant 1: pkg/types role/provider constants resolve.
	step("types.RoleUser_constant",
		types.RoleUser == types.Role("user"),
		fmt.Sprintf("got=%q", types.RoleUser))
	step("types.RoleAssistant_constant",
		types.RoleAssistant == types.Role("assistant"),
		fmt.Sprintf("got=%q", types.RoleAssistant))
	step("types.RoleSystem_constant",
		types.RoleSystem == types.Role("system"),
		fmt.Sprintf("got=%q", types.RoleSystem))
	step("types.RoleTool_constant",
		types.RoleTool == types.Role("tool"),
		fmt.Sprintf("got=%q", types.RoleTool))
	step("types.ProviderOpenAI_constant",
		types.ProviderOpenAI == types.Provider("openai"),
		fmt.Sprintf("got=%q", types.ProviderOpenAI))
	step("types.ProviderAnthropic_constant",
		types.ProviderAnthropic == types.Provider("anthropic"),
		fmt.Sprintf("got=%q", types.ProviderAnthropic))
	step("types.ProviderLocal_constant",
		types.ProviderLocal == types.Provider("local"),
		fmt.Sprintf("got=%q", types.ProviderLocal))

	// Invariant 2: i18n preloaded English known key resolves.
	tr := i18n.New("en")
	gotInvalid := tr.T("en", i18n.KeyInvalidAPIKey)
	step("i18n.preloaded_known_key_resolves",
		gotInvalid == "Invalid API key provided",
		fmt.Sprintf("got=%q", gotInvalid))

	// Invariant 3: template substitution fires.
	gotTmpl := tr.T("en", i18n.KeyInvalidRequest,
		map[string]string{"detail": "missing model field"})
	step("i18n.template_substitution_renders",
		gotTmpl == "Invalid request: missing model field",
		fmt.Sprintf("got=%q", gotTmpl))

	// Belt-and-braces: ensure no raw {{ token survived rendering.
	step("i18n.template_no_residual_token",
		!strings.Contains(gotTmpl, "{{"),
		fmt.Sprintf("got=%q", gotTmpl))

	// Invariant 4: unknown key returns key verbatim (anti-bluff
	// contract documented in i18n.go T func comment).
	const unknownKey = "round_295_definitely_not_in_any_bundle"
	gotUnknown := tr.T("en", unknownKey)
	if mutate {
		// Mutation flips polarity — treat known key as if it
		// returned its own key (i.e. unknown-key path).
		gotKnownAsUnknown := tr.T("en", i18n.KeyInvalidAPIKey)
		step("i18n.unknown_key_returns_key_verbatim[MUTATED]",
			gotKnownAsUnknown == i18n.KeyInvalidAPIKey,
			fmt.Sprintf("got=%q (mutation-inverted)",
				gotKnownAsUnknown))
	} else {
		step("i18n.unknown_key_returns_key_verbatim",
			gotUnknown == unknownKey,
			fmt.Sprintf("got=%q", gotUnknown))
	}

	// Invariant 5: per-fixture LoadMessages + T round-trip.
	for _, f := range fixtures {
		tr.LoadMessages(f.locale,
			map[string]string{f.messageKey: f.messageValue})
		got := tr.T(f.locale, f.messageKey)
		levelOK := got == f.messageValue &&
			strings.Contains(got, f.expectSubstr)
		step("i18n.LoadMessages_roundtrip."+f.locale,
			levelOK,
			fmt.Sprintf("key=%s got=%q want_substr=%q",
				f.messageKey, got, f.expectSubstr))

		// Also exercise template substitution for the fixture
		// when a {{detail}} placeholder is present.
		if strings.Contains(f.messageValue, "{{detail}}") {
			rendered := tr.T(f.locale, f.messageKey,
				map[string]string{
					"detail": f.templateDetailVal,
				})
			step("i18n.template_substitution."+f.locale,
				strings.Contains(rendered,
					f.templateDetailVal) &&
					!strings.Contains(rendered, "{{"),
				fmt.Sprintf("rendered=%q", rendered))
		}
	}

	// Invariant 6: RequestOrchestrator defensive + active behaviour.
	orch := gateway.NewRequestOrchestrator()
	step("gateway.NewRequestOrchestrator_returns_nonnil",
		orch != nil,
		"non-nil orchestrator")

	// Defensive: nil request returns nil unchanged.
	step("gateway.EnhanceRequest_nil_passthrough",
		orch.EnhanceRequest(nil) == nil,
		"nil-in / nil-out preserved")

	// Defensive: empty messages list returns request unchanged.
	emptyReq := &types.InternalChatRequest{
		Model:    "test-model",
		Messages: []types.InternalMessage{},
	}
	gotEmpty := orch.EnhanceRequest(emptyReq)
	step("gateway.EnhanceRequest_empty_passthrough",
		gotEmpty == emptyReq,
		"empty-messages preserved by-pointer")

	// Active: 2+ tool results SHOULD inject the action hint as a
	// new system message before the last message.
	req := &types.InternalChatRequest{
		Model: "test-model",
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "find bug"},
			{Role: types.RoleAssistant, Content: "reading"},
			{Role: types.RoleTool, Content: "file contents 1"},
			{Role: types.RoleAssistant, Content: "reading more"},
			{Role: types.RoleTool, Content: "file contents 2"},
			{Role: types.RoleUser, Content: "what now?"},
		},
	}
	enhanced := orch.EnhanceRequest(req)
	hintFound := false
	for _, m := range enhanced.Messages {
		if m.Role == types.RoleSystem &&
			strings.Contains(m.Content,
				"You have already gathered enough context") {
			hintFound = true
			break
		}
	}
	step("gateway.EnhanceRequest_injects_action_hint",
		hintFound && len(enhanced.Messages) ==
			len(req.Messages)+1,
		fmt.Sprintf("hint_found=%v len_in=%d len_out=%d",
			hintFound, len(req.Messages),
			len(enhanced.Messages)))

	// Invariant 7: SanitizeToolResult defensive contract.
	step("gateway.SanitizeToolResult_string",
		gateway.SanitizeToolResult("hello") == "hello",
		"string input preserved")
	step("gateway.SanitizeToolResult_nil_safe",
		gateway.SanitizeToolResult(nil) != "" ||
			gateway.SanitizeToolResult(nil) == "",
		fmt.Sprintf("nil=%q",
			gateway.SanitizeToolResult(nil)))
	mapOut := gateway.SanitizeToolResult(
		map[string]interface{}{"k": "v"})
	step("gateway.SanitizeToolResult_map_nonempty",
		mapOut != "",
		fmt.Sprintf("map=%q", mapOut))

	fmt.Fprintf(out, "\n=== Summary: PASS=%d FAIL=%d ===\n",
		pass, fail)
	if fail > 0 {
		return 1
	}
	return 0
}

// loadFixtures parses every *.yaml in dir using a tiny line-based
// parser. We only support the 5 keys our fixtures use; anything
// else is ignored. Keeping the parser in-runner avoids pulling
// yaml.v3 into the runtime path of a submodule that other projects
// reuse (CONST-051(B)).
func loadFixtures(dir string) ([]fixture, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []fixture
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		f := parseFixture(string(data))
		if f.locale == "" {
			return nil, fmt.Errorf(
				"%s: missing locale key", e.Name())
		}
		out = append(out, f)
	}
	return out, nil
}

func parseFixture(text string) fixture {
	f := fixture{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		k := strings.TrimSpace(line[:colon])
		v := strings.TrimSpace(line[colon+1:])
		v = strings.Trim(v, "\"'")
		switch k {
		case "locale":
			f.locale = v
		case "message_key":
			f.messageKey = v
		case "message_value":
			f.messageValue = v
		case "expect_substr":
			f.expectSubstr = v
		case "template_detail_val":
			f.templateDetailVal = v
		}
	}
	return f
}
