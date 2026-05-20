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

	// Echo tool.
	keyEchoDesc     = "tool_echo_description"
	keyEchoParamMsg = "tool_echo_param_message"

	// Code-execution tool descriptions and parameter descriptions.
	keyExecPythonDesc      = "tool_exec_python_description"
	keyExecPythonParamCode = "tool_exec_python_param_code"
	keyExecShellDesc       = "tool_exec_shell_description"
	keyExecShellParamCmd   = "tool_exec_shell_param_command"

	// Code-analysis tool descriptions, parameter descriptions, results.
	keyAnalyzeCodeDesc        = "tool_analyze_code_description"
	keyAnalyzeCodeParamPath   = "tool_analyze_code_param_path"
	keyRunTestsDesc           = "tool_run_tests_description"
	keyRunTestsParamPath      = "tool_run_tests_param_path"
	keyRunTestsParamFramework = "tool_run_tests_param_framework"
	keyDepsDesc               = "tool_dependencies_description"
	keyDepsParamPath          = "tool_dependencies_param_path"
	keyDepsResultNoManifest   = "tool_dependencies_result_no_manifest"
	keyComplexityDesc         = "tool_complexity_description"
	keyComplexityParamPath    = "tool_complexity_param_path"
	keyComplexityResultNoFns  = "tool_complexity_result_no_functions"

	// Filesystem tool descriptions, parameter descriptions, results.
	keyReadFileDesc        = "tool_read_file_description"
	keyReadFileParamPath   = "tool_read_file_param_path"
	keyReadFileParamOffset = "tool_read_file_param_offset"
	keyReadFileParamLimit  = "tool_read_file_param_limit"
	keyWriteFileDesc       = "tool_write_file_description"
	keyWriteFileParamPath  = "tool_write_file_param_path"
	keyWriteFileParamData  = "tool_write_file_param_content"
	keyWriteFileResult     = "tool_write_file_result"
	keyListDirDesc         = "tool_list_directory_description"
	keyListDirParamPath    = "tool_list_directory_param_path"
	keyListDirParamRecurse = "tool_list_directory_param_recursive"
	keySearchFilesDesc     = "tool_search_files_description"
	keySearchFilesParamQ   = "tool_search_files_param_query"
	keySearchFilesParamDir = "tool_search_files_param_dir"
	keySearchFilesNoMatch  = "tool_search_files_result_no_matches"

	// LSP tool descriptions.
	keyGotoDefinitionDesc = "tool_goto_definition_description"
	keyFindReferencesDesc = "tool_find_references_description"
	keyHoverInfoDesc      = "tool_hover_info_description"
	keyDiagnosticsDesc    = "tool_diagnostics_description"

	// LSP tool parameter descriptions.
	keyLSPParamFile   = "tool_lsp_param_file"
	keyLSPParamLine   = "tool_lsp_param_line"
	keyLSPParamColumn = "tool_lsp_param_column"

	// LSP tool execution result messages.
	keyLSPResultNoDefinition  = "tool_lsp_result_no_definition"
	keyLSPResultNoReferences  = "tool_lsp_result_no_references"
	keyLSPResultNoHover       = "tool_lsp_result_no_hover"
	keyLSPResultNoDiagnostics = "tool_lsp_result_no_diagnostics"
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

	keyEchoDesc:     "Returns the input message unchanged. Useful for testing.",
	keyEchoParamMsg: "The message to echo back",

	keyExecPythonDesc:      "Execute Python code. The code is written to a temporary file and run with python3. Returns stdout/stderr.",
	keyExecPythonParamCode: "Python code to execute",
	keyExecShellDesc:       "Execute a shell command. The command is validated against the security sandbox before execution.",
	keyExecShellParamCmd:   "Shell command to execute",

	keyAnalyzeCodeDesc:        "Analyze code in a directory or file: count files, lines, and functions.",
	keyAnalyzeCodeParamPath:   "Path to a file or directory to analyze",
	keyRunTestsDesc:           "Detect and run tests in a project. Auto-detects Go, Python (pytest), and Node.js (jest/npm test).",
	keyRunTestsParamPath:      "Path to the project directory (default: current directory)",
	keyRunTestsParamFramework: "Test framework to use: 'go', 'pytest', 'jest', 'npm'. Auto-detected if omitted.",
	keyDepsDesc:               "List project dependencies by parsing go.mod, package.json, or requirements.txt.",
	keyDepsParamPath:          "Path to the project directory",
	keyDepsResultNoManifest:   "No dependency manifest found (go.mod, package.json, or requirements.txt).",
	keyComplexityDesc:         "Estimate cyclomatic complexity of Go source files by counting branching statements (if, for, switch, case, &&, ||) per function.",
	keyComplexityParamPath:    "Path to a Go source file or directory",
	keyComplexityResultNoFns:  "No functions found.",

	keyReadFileDesc:        "Read the contents of a file. Supports optional line offset and limit.",
	keyReadFileParamPath:   "Absolute path to the file to read",
	keyReadFileParamOffset: "Line number to start reading from (0-based, default 0)",
	keyReadFileParamLimit:  "Maximum number of lines to return (default: all)",
	keyWriteFileDesc:       "Write content to a file. Creates the file and parent directories if they do not exist.",
	keyWriteFileParamPath:  "Absolute path to the file to write",
	keyWriteFileParamData:  "Content to write to the file",
	keyWriteFileResult:     "Wrote {{bytes}} bytes to {{path}}",
	keyListDirDesc:         "List the contents of a directory. Optionally recurse into subdirectories.",
	keyListDirParamPath:    "Absolute path to the directory to list",
	keyListDirParamRecurse: "Whether to recurse into subdirectories (default false)",
	keySearchFilesDesc:     "Search for files by glob pattern or grep for content within files under a directory.",
	keySearchFilesParamQ:   "Glob pattern (e.g. '*.go') or text to search for in file contents",
	keySearchFilesParamDir: "Directory to search in (default: current directory)",
	keySearchFilesNoMatch:  "No matches found.",

	keyGotoDefinitionDesc: "Go to the definition of a symbol at a given file location",
	keyFindReferencesDesc: "Find all references to the symbol at a given file location",
	keyHoverInfoDesc:      "Get hover documentation for the symbol at a given file location",
	keyDiagnosticsDesc:    "Get compiler and linter diagnostics for a file reported by the language server",

	keyLSPParamFile:   "Absolute file path",
	keyLSPParamLine:   "Line number (0-based)",
	keyLSPParamColumn: "Column number (0-based)",

	keyLSPResultNoDefinition:  "No definition found.",
	keyLSPResultNoReferences:  "No references found.",
	keyLSPResultNoHover:       "No hover information available.",
	keyLSPResultNoDiagnostics: "No diagnostics reported.",
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
