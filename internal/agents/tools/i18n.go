package tools

import (
	"github.com/HelixDevelopment/HelixLLM/internal/shared/i18n"
)

// Package-level i18n seam for the agent-tools package.
//
// Per CONST-046, every user-facing string in this package — tool
// descriptions surfaced to the LLM and to UI tool catalogs, parameter
// descriptions shown in tool-schema panels, and execution result
// messages returned to the user — MUST resolve through a Translator
// rather than living as a hardcoded English literal.
//
// Tool descriptions and parameter schemas are produced by value-
// receiver methods on plain structs that carry neither a translator
// field nor a context.Context (Name/Description/Parameters are part
// of the agents.Tool interface and cannot change shape). The minimal
// honest seam for that constraint is a package-level Translator that
// a consuming project wires once at init time. Tests override it via
// SetTranslator. When no translator is wired the call site falls
// back to the bundled English string supplied alongside the key.
//
// Decoupling per CONST-051(B): the seam binds only to HelixLLM's own
// shared/i18n package — no consumer-project type is referenced, so
// the tools package remains reusable as a standalone unit.

// i18n message keys for the agent-tools package.
const (
	// Git tool descriptions.
	keyGitStatusDesc       = "tool_git_status_description"
	keyGitDiffDesc         = "tool_git_diff_description"
	keyGitLogDesc          = "tool_git_log_description"
	keyGitBranchDesc       = "tool_git_branch_description"
	keyGitCommitDesc       = "tool_git_commit_description"
	keyGitPushDesc         = "tool_git_push_description"
	keyGitPullDesc         = "tool_git_pull_description"
	keyGitCreateBranchDesc = "tool_git_create_branch_description"

	// Git tool parameter descriptions.
	keyGitParamRepoPath   = "tool_git_param_repo_path"
	keyGitParamDiffStaged = "tool_git_param_diff_staged"
	keyGitParamLogCount   = "tool_git_param_log_count"
	keyGitParamCommitMsg  = "tool_git_param_commit_message"
	keyGitParamCommitAll  = "tool_git_param_commit_all"
	keyGitParamRemote     = "tool_git_param_remote"
	keyGitParamBranch     = "tool_git_param_branch"
	keyGitParamNewBranch  = "tool_git_param_new_branch_name"
	keyGitParamCheckout   = "tool_git_param_checkout"

	// Git tool execution result messages.
	keyGitResultNoChanges      = "tool_git_result_no_changes"
	keyGitResultNoCommits      = "tool_git_result_no_commits"
	keyGitResultNoBranches     = "tool_git_result_no_branches"
	keyGitResultPushDone       = "tool_git_result_push_done"
	keyGitResultPullDone       = "tool_git_result_pull_done"
	keyGitResultSwitchedBranch = "tool_git_result_switched_branch"
	keyGitResultBranchCreated  = "tool_git_result_branch_created"
)

// englishFallbacks maps every tools-package i18n key to its bundled
// English text. Per CONST-046 this is NOT the canonical localisation
// source — a consuming project's translator owns per-locale bundles —
// but it guarantees a sensible default on the no-translator path so
// the package stays standalone-usable.
var englishFallbacks = map[string]string{
	keyGitStatusDesc:       "Show the working tree status of a git repository (short format with branch info).",
	keyGitDiffDesc:         "Show changes in a git repository. Use staged=true to see staged changes.",
	keyGitLogDesc:          "Show recent commits in a git repository (oneline format).",
	keyGitBranchDesc:       "List all branches (local and remote) in a git repository.",
	keyGitCommitDesc:       "Create a git commit with the given message. Optionally stage all changes first.",
	keyGitPushDesc:         "Push commits to a remote repository. Defaults to 'origin' if no remote is specified.",
	keyGitPullDesc:         "Pull changes from a remote repository into the current branch.",
	keyGitCreateBranchDesc: "Create a new git branch. Optionally switch to it immediately.",

	keyGitParamRepoPath:   "Path to the git repository (default: current directory)",
	keyGitParamDiffStaged: "Show staged changes instead of unstaged (default false)",
	keyGitParamLogCount:   "Number of commits to show (default 10)",
	keyGitParamCommitMsg:  "Commit message (required)",
	keyGitParamCommitAll:  "Stage all changes before committing (git add -A) (default false)",
	keyGitParamRemote:     "Remote name (default: origin)",
	keyGitParamBranch:     "Branch to push (default: current branch)",
	keyGitParamNewBranch:  "Name of the new branch (required)",
	keyGitParamCheckout:   "Switch to the new branch after creating it (default true)",

	keyGitResultNoChanges:      "No changes.",
	keyGitResultNoCommits:      "No commits found.",
	keyGitResultNoBranches:     "No branches found.",
	keyGitResultPushDone:       "Push completed successfully.",
	keyGitResultPullDone:       "Pull completed successfully.",
	keyGitResultSwitchedBranch: "Switched to a new branch '{{name}}'.",
	keyGitResultBranchCreated:  "Branch '{{name}}' created.",
}

// pkgTranslator is the package-level Translator used by tool
// Description/Parameters methods (value receivers, no translator
// field) and by Execute result messages. Production wires a real
// Translator at init time via SetTranslator; tests inject a fake.
// The default is an English-only Translator pre-loaded with the
// bundled fallbacks so the no-wiring path still localises to English.
var pkgTranslator i18n.TranslatorAPI = newDefaultTranslator()

// pkgLang is the language tag passed to the package-level Translator.
// Consuming projects override it via SetLang when serving a non-English
// locale; the default is English.
var pkgLang = "en"

// newDefaultTranslator builds an English-only Translator pre-loaded
// with this package's bundled fallback strings.
func newDefaultTranslator() i18n.TranslatorAPI {
	tr := i18n.New("en")
	tr.LoadMessages("en", englishFallbacks)
	return tr
}

// SetTranslator wires a package-level Translator for the agent-tools
// package. Consuming projects call this at init time; tests use it to
// inject a fake. A nil argument resets to the English-only default.
func SetTranslator(tr i18n.TranslatorAPI) {
	if tr == nil {
		pkgTranslator = newDefaultTranslator()
		return
	}
	pkgTranslator = tr
}

// SetLang sets the language tag used when resolving tools-package
// strings. An empty argument resets to English.
func SetLang(lang string) {
	if lang == "" {
		pkgLang = "en"
		return
	}
	pkgLang = lang
}

// tr resolves a tools-package i18n key to its locale-appropriate text.
// On a translator miss (key absent in every loaded language) the
// upstream Translator returns the key verbatim; tr then substitutes
// the bundled English fallback so the user never sees a raw key.
func tr(key string, vars ...map[string]string) string {
	got := pkgTranslator.T(pkgLang, key, vars...)
	if got == key {
		return renderFallback(key, vars...)
	}
	return got
}

// renderFallback returns the bundled English text for key with
// {{placeholder}} tokens substituted from vars. It is the last-resort
// path when no translator (not even the English default) resolves the
// key — guaranteeing a human-readable string at the call site.
func renderFallback(key string, vars ...map[string]string) string {
	msg, ok := englishFallbacks[key]
	if !ok {
		return key
	}
	if len(vars) == 0 || vars[0] == nil {
		return msg
	}
	for name, val := range vars[0] {
		msg = substitutePlaceholder(msg, name, val)
	}
	return msg
}

// substitutePlaceholder replaces every "{{name}}" occurrence in msg
// with val. A tiny dependency-free helper so the fallback path needs
// no template engine.
func substitutePlaceholder(msg, name, val string) string {
	token := "{{" + name + "}}"
	out := ""
	for {
		idx := indexOf(msg, token)
		if idx < 0 {
			return out + msg
		}
		out += msg[:idx] + val
		msg = msg[idx+len(token):]
	}
}

// indexOf returns the first index of sub in s, or -1 when absent.
func indexOf(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
