package naming

import (
	"fmt"
	"regexp"
	"strings"
)

// HelixCode consumer export.
//
// WHAT HELIXCODE ACTUALLY ACCEPTS, and why this file targets what it does.
//
// There are two candidate integration points and only one of them is live.
//
//  1. `helix_code/config/config.yaml` declares `helix-llm` and `helix-debate`
//     provider types. NO Go code loads them: `internal/config.LLMConfig` has no
//     `providers` field at all. That block is dead configuration. It is left
//     alone here — it is separately tracked for an operator decision (§11.4.122
//     forbids removing a declared component without asking), and building
//     against it would produce an artefact that changes nothing.
//
//  2. The live path is a hand-written special case in HelixCode's HTTP handler:
//     `resolveLLMProvider` routes a request naming provider "helixllm" or
//     "local" to `resolveHelixLLMLocalProvider`
//     (`helix_code/internal/server/llm_generate.go`), which builds an
//     OpenAI-compatible client whose base URL comes from the environment
//     variable HELIX_LLM_LOCAL_OPENAI_ENDPOINT and whose model is the request's
//     own `model` field, passed through verbatim.
//
// This file targets (2), because that is the path a user's request actually
// travels. The artefact is therefore an environment-file fragment — the one
// thing that path reads from configuration — plus the roster of identifiers the
// user names in the `model` field.
//
// WHICH IDENTIFIER, and why it is not a HelixCode-specific one.
//
// HelixCode imposes no character restriction on `model`: the string is copied
// into the upstream request body untouched, and HelixCode's own regression
// guards exercise a served model literally named
// "/models/Qwen3-Coder-30B-A3B-Instruct-Q4_K_M.gguf". So a HelixCode-only
// ruleset could be arbitrarily permissive.
//
// It is not, because the binding constraint sits on the far side of the wire.
// HelixLLM maps a published identifier back to the model name its providers
// answer to (`Brain.ResolveModelName`), and that lookup is keyed on the
// ClaudeToolkit ruleset. An identifier derived under any other ruleset would
// arrive unresolvable and fall through to whichever provider the router reached
// last — a silent misroute, not an error. So the export publishes exactly the
// identifier that resolver can map. That ruleset is also the strictest in play,
// so this is a self-restriction; nothing is relaxed to fit a name (FR-014a).

// HelixCodeEndpointEnv is the environment variable HelixCode's live HelixLLM
// route reads its base URL from. The value is an origin with NO "/v1" suffix —
// HelixCode's OpenAI-compatible client already carries that prefix in its own
// endpoint defaults, so appending one here would produce "/v1/v1".
const HelixCodeEndpointEnv = "HELIX_LLM_LOCAL_OPENAI_ENDPOINT"

// The managed block markers. A merge replaces what is between them and leaves
// every other line of the operator's file exactly as it was.
const (
	helixCodeBlockOpen  = "# >>> " + IdentityPrefix + " managed block"
	helixCodeBlockClose = "# <<< " + IdentityPrefix + " managed block"
)

// HelixCodeConfig is the artefact a user applies to HelixCode.
type HelixCodeConfig struct {
	// EnvFile is the environment-file fragment, ready to write or merge.
	EnvFile string

	// Models are the options that can actually be served, each carrying the
	// identifier to put in a request's `model` field, the human-readable
	// identity it stands for, and the name the instance itself answers to.
	Models []Exported

	// Withheld are the options deliberately not offered, each with its reason.
	Withheld []WithheldOption
}

// ExportHelixCode produces the HelixCode configuration for one serving
// instance.
//
// One instance, not several: HelixCode's live route reads a single endpoint
// variable, so there is exactly one instance it can be pointed at at a time.
// Pretending otherwise — emitting several endpoints into one file — would
// produce a fragment whose last line silently wins.
func ExportHelixCode(inst Instance) (HelixCodeConfig, error) {
	exported, withheld, err := partition(inst, ClaudeToolkit)
	if err != nil {
		return HelixCodeConfig{}, err
	}
	endpoint, err := safeEndpoint(inst.BaseURL)
	if err != nil {
		return HelixCodeConfig{}, err
	}

	var b strings.Builder
	b.WriteString(helixCodeBlockOpen)
	b.WriteByte('\n')
	b.WriteString("# " + IdentityPrefix + "/" + inst.Host)
	b.WriteByte('\n')
	for _, m := range exported {
		// The roster travels as comments because the live route has no
		// configuration slot for a model list — the user names one per request.
		b.WriteString("# " + m.Identifier + " = " + m.Identity)
		b.WriteByte('\n')
	}
	for _, w := range withheld {
		b.WriteString("# withheld: " + w.Identity + " = " + w.Reason)
		b.WriteByte('\n')
	}
	// Quoted because this file is sourced by a shell. safeEndpoint has already
	// refused every character that would end the quoting or be re-evaluated
	// inside it.
	b.WriteString(HelixCodeEndpointEnv + "=\"" + endpoint.String() + "\"")
	b.WriteByte('\n')
	b.WriteString(helixCodeBlockClose)
	b.WriteByte('\n')

	return HelixCodeConfig{EnvFile: b.String(), Models: exported, Withheld: withheld}, nil
}

// helixCodeAssignment matches any line that assigns the managed variable,
// with or without an `export` prefix.
var helixCodeAssignment = regexp.MustCompile(
	`^[[:space:]]*(export[[:space:]]+)?` + regexp.QuoteMeta(HelixCodeEndpointEnv) + `[[:space:]]*=`)

// MergeHelixCodeEnv returns the operator's environment file with the managed
// block updated in place — replaced if it is already there, appended if it is
// not — and every other line untouched.
//
// Re-running it is a no-op: the block is delimited, so the second run replaces
// exactly what the first one wrote (contract invariant 3).
//
// It refuses when the file assigns the same variable OUTSIDE the managed block.
// Whether that assignment or ours takes effect depends on the order the file is
// read, so neither silently winning nor silently losing is honest; the conflict
// is surfaced for the operator to resolve. This function returns the merged
// text — it never writes to the user's file (FR-018).
func MergeHelixCodeEnv(existing string, cfg HelixCodeConfig) (string, error) {
	lines := strings.Split(existing, "\n")

	var (
		kept    []string
		inBlock bool
		found   []int
	)
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, helixCodeBlockOpen):
			inBlock = true
			continue
		case strings.HasPrefix(line, helixCodeBlockClose):
			inBlock = false
			continue
		case inBlock:
			continue
		}
		if helixCodeAssignment.MatchString(line) {
			found = append(found, i+1)
		}
		kept = append(kept, line)
	}
	if inBlock {
		return "", fmt.Errorf("%w: managed block is not closed", ErrMalformed)
	}
	if len(found) > 0 {
		return "", fmt.Errorf("%w: %s is assigned at line(s) %v outside the managed block",
			ErrForeignAssignment, HelixCodeEndpointEnv, found)
	}

	body := strings.TrimRight(strings.Join(kept, "\n"), "\n")
	if body == "" {
		return cfg.EnvFile, nil
	}
	return body + "\n" + cfg.EnvFile, nil
}
