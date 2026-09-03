package naming

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// OpenCode consumer export.
//
// This is net-new: no OpenCode provider-configuration integration existed
// anywhere in this project, only Skills/MCP/instructions sync. Every claim
// below about OpenCode's format was read out of OpenCode itself rather than
// assumed, because a config shape guessed from memory is only discovered to be
// wrong when a user's file fails to load.
//
// WHAT WAS ESTABLISHED, AND FROM WHERE (verified 2026-09-02):
//
//   - The document shape. `Config.provider` is an object keyed by provider id
//     whose values are `ProviderConfig` — official JSON Schema at
//     https://opencode.ai/config.json, corroborated by the generated TypeScript
//     types shipped in the installed `@opencode-ai/sdk` package. The fields
//     used here (`npm`, `name`, `options.baseURL`, `models.<key>.{id,name}`)
//     are all present in both.
//
//   - The adapter id. OpenCode dispatches on the `npm` value; the branch
//     `l.api.npm === "@ai-sdk/openai-compatible"` is what configures an
//     OpenAI-compatible client, so that literal is the adapter for a HelixLLM
//     endpoint. Read from the installed opencode binary (v1.18.x).
//
//   - The base URL needs its version segment. That adapter builds requests as
//     `${config.baseURL}/chat/completions` — it appends the path itself and
//     adds no "/v1". So `options.baseURL` must carry it, which is why this
//     export appends one to the instance's bare origin.
//
//   - The wire model is `models.<key>.id`, not the key. OpenCode resolves it as
//     `api.id = api.id ?? id ?? <key>` and hands `api.id` to the adapter's
//     `.model(...)`. The key is only the fallback — which is exactly what lets
//     the key be a safe derived identifier while the served name (a gguf path,
//     say) travels as a value.
//
//   - A "/" in a provider key breaks reference resolution. A model reference is
//     `provider/model`, and the parse is `indexOf("/")` then
//     `slice(0, i)` / `slice(i+1)` — split on the FIRST separator. A "/" inside
//     the provider key would therefore be read as the start of the model name.
//     (Model ids legitimately contain "/" — 4243 of the 7490 references in
//     models.dev's schema do — but a key with one also collides with OpenCode's
//     `model/variant` lookup, so both keys are kept free of it here.)
//
// WHAT COULD NOT BE ESTABLISHED, stated rather than invented: the schema places
// NO pattern constraint on either key — `additionalProperties` with no
// `propertyNames` — and documents no length limit. The ruleset below is
// therefore tighter than anything OpenCode enforces. That direction is the safe
// one: it is a self-restriction, never a relaxation of a consumer's check
// (FR-014a).

// OpenCode is the ruleset for OpenCode configuration keys.
//
// The one rule that is load-bearing rather than conservative is the exclusion
// of "/": OpenCode splits a model reference on the first one, so a key
// containing it silently re-points the reference. The rest of the charset is
// deliberately narrow — a key is typed on a command line, shown in a picker,
// and passed through as the adapter's provider name — and "." is admitted
// because real model ids carry it. Unlike [ClaudeToolkit] there is no anchored
// leading-letter rule and no documented length cap, so none is claimed here;
// the prefix supplies a leading letter anyway.
var OpenCode = Ruleset{
	Name:      "opencode",
	Prefix:    IdentityPrefix,
	Separator: '-',
	Allow: func(r rune) bool {
		return r == '_' || r == '-' || r == '.' ||
			(r >= '0' && r <= '9') ||
			(r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z')
	},
	MustStartWithLetter: false,
	MaxLength:           0,
}

// openCodeAdapterNPM is the adapter OpenCode dispatches to for an
// OpenAI-compatible endpoint.
const openCodeAdapterNPM = "@ai-sdk/openai-compatible"

// openCodeAPIVersionPath is the segment that adapter does NOT add for itself.
const openCodeAPIVersionPath = "/v1"

// OpenCodeConfig is the artefact a user applies to OpenCode.
type OpenCodeConfig struct {
	// ProviderID is the key this instance occupies under `provider`, and the
	// left half of every `provider/model` reference naming one of its models.
	ProviderID string

	// Document is the configuration fragment: a JSON object carrying just
	// `provider.<ProviderID>`, ready to merge into a user's own file.
	Document []byte

	// Models are the options written into the entry.
	Models []Exported

	// Withheld are the options deliberately left out, each with its reason.
	// They are absent from Document entirely — an unavailable model listed
	// under `models` would appear in OpenCode's picker as selectable, which is
	// precisely what FR-019 forbids.
	Withheld []WithheldOption
}

// openCodeModel is one entry under `models`.
type openCodeModel struct {
	// ID is the model name the instance answers to. It is what reaches the
	// wire, and it may contain characters the key may not.
	ID string `json:"id"`

	// Name is the human-readable identity, carried as a displayed VALUE
	// (contract invariant 2).
	Name string `json:"name"`
}

// openCodeOptions is the `options` object. It has no apiKey field on purpose:
// this type cannot express one, so no credential can reach the document
// through it (contract invariant 5).
type openCodeOptions struct {
	BaseURL string `json:"baseURL"`
}

// openCodeProvider is one `provider.<id>` entry.
type openCodeProvider struct {
	NPM     string                   `json:"npm"`
	Name    string                   `json:"name"`
	Options openCodeOptions          `json:"options"`
	Models  map[string]openCodeModel `json:"models"`
}

// ExportOpenCode produces the OpenCode provider entry for one serving instance.
//
// One instance is one provider entry, because `options.baseURL` is per-entry:
// two hosts are two entries, not two models under one.
func ExportOpenCode(inst Instance) (OpenCodeConfig, error) {
	exported, withheld, err := partition(inst, OpenCode)
	if err != nil {
		return OpenCodeConfig{}, err
	}
	endpoint, err := safeEndpoint(inst.BaseURL)
	if err != nil {
		return OpenCodeConfig{}, err
	}
	providerID, err := hostIdentifier(inst.Host, OpenCode)
	if err != nil {
		return OpenCodeConfig{}, err
	}
	if err := conforms(providerID, OpenCode); err != nil {
		return OpenCodeConfig{}, err
	}

	entry := openCodeProvider{
		NPM: openCodeAdapterNPM,
		// Composed from the instance's own metadata rather than being a fixed
		// English label (CONST-046).
		Name:    IdentityPrefix + "/" + inst.Host,
		Options: openCodeOptions{BaseURL: openCodeBaseURL(endpoint.String())},
		Models:  make(map[string]openCodeModel, len(exported)),
	}
	for _, m := range exported {
		entry.Models[m.Identifier] = openCodeModel{ID: m.WireModel, Name: m.Identity}
	}

	doc := map[string]map[string]openCodeProvider{
		"provider": {providerID: entry},
	}
	// Go marshals map keys in sorted order, so the same instance always
	// produces the same bytes (contract invariant 3).
	document, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return OpenCodeConfig{}, fmt.Errorf("naming: rendering the opencode document: %w", err)
	}

	return OpenCodeConfig{
		ProviderID: providerID,
		Document:   append(document, '\n'),
		Models:     exported,
		Withheld:   withheld,
	}, nil
}

// openCodeBaseURL gives the adapter the versioned base it concatenates onto,
// without doubling a version segment an operator already supplied.
func openCodeBaseURL(raw string) string {
	trimmed := strings.TrimRight(raw, "/")
	if strings.HasSuffix(trimmed, openCodeAPIVersionPath) {
		return trimmed
	}
	return trimmed + openCodeAPIVersionPath
}

// MergeOpenCode returns the operator's opencode.json with this instance's
// provider entry added or replaced, and everything else preserved.
//
// Additive by key: a pre-existing provider the operator configured themselves
// is copied through byte-for-byte, as is every other top-level key. Replacing
// our own entry wholesale rather than merging into it is deliberate — a model
// that stopped being offered must disappear, and a field-wise merge would leave
// it behind. Re-running is a no-op (contract invariant 3).
//
// WHAT "WHOLESALE" COSTS, AND THE ONE CASE IT IS REFUSED IN.
//
// That replacement is scoped to OUR key: the operator's own providers and every
// other top-level field survive it. But within our key it is total, and an
// export produced while the backend was LOADING carries `models: {}` — so a
// merge run during a restart used to answer 200 with the user's working entry
// replaced by an empty one. Their models stopped appearing in OpenCode's picker
// and nothing in the returned document said why.
//
// The replacement rule assumed withheld meant WITHDRAWN. Since withheld options
// carry a reason, that assumption is no longer necessary: an export that can
// name nothing servable while holding reasons for every absence is refused with
// [ErrNothingServable] rather than written. It is not a description of the
// instance's models — it is the export saying it cannot describe them yet.
//
// An instance that genuinely offers nothing still converges. It has no offers,
// therefore no withheld reasons, therefore does not meet this condition: the
// empty entry merges through and clears whatever was there. The refusal costs a
// retry and only in the state where proceeding costs the user their entry.
//
// The narrower case — SOME options servable, others withheld — is deliberately
// left as it was. Replacement there still drops the withheld ones, so a user
// mid-restart can temporarily lose the options that have not come back. That is
// a degraded entry rather than an unusable one, and separating a transient
// withholding from a permanent one would need a reason taxonomy this package
// does not have: [withheldReason] passes an instance's or an offer's own reason
// string through untouched, so the set is open. Guessing which reasons are
// temporary is the kind of invention that put the empty document here.
//
// It returns the merged document; it never writes to the user's file (FR-018).
func MergeOpenCode(existing []byte, cfg OpenCodeConfig) ([]byte, error) {
	if cfg.ProviderID == "" {
		return nil, fmt.Errorf("%w: configuration names no provider", ErrMalformed)
	}
	if len(cfg.Models) == 0 && len(cfg.Withheld) > 0 {
		return nil, fmt.Errorf("%w: %d offered, none servable (%s)",
			ErrNothingServable, len(cfg.Withheld), WithheldReasons(cfg.Withheld))
	}

	root := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(existing)) > 0 {
		if err := json.Unmarshal(existing, &root); err != nil {
			return nil, fmt.Errorf("naming: existing opencode configuration does not parse: %w", err)
		}
	}

	providers := map[string]json.RawMessage{}
	if raw, ok := root["provider"]; ok && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &providers); err != nil {
			return nil, fmt.Errorf("naming: existing `provider` is not an object: %w", err)
		}
	}

	var ours struct {
		Provider map[string]json.RawMessage `json:"provider"`
	}
	if err := json.Unmarshal(cfg.Document, &ours); err != nil {
		return nil, fmt.Errorf("naming: exported document does not parse: %w", err)
	}
	entry, ok := ours.Provider[cfg.ProviderID]
	if !ok {
		return nil, fmt.Errorf("%w: exported document has no entry for %q", ErrMalformed, cfg.ProviderID)
	}
	providers[cfg.ProviderID] = entry

	merged, err := json.Marshal(providers)
	if err != nil {
		return nil, fmt.Errorf("naming: rendering merged providers: %w", err)
	}
	root["provider"] = merged

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("naming: rendering merged configuration: %w", err)
	}
	return append(out, '\n'), nil
}

// WithheldReasons is the distinct reasons in a withheld set, sorted and
// comma-joined, for a caller that has to SAY why nothing could be exported.
//
// Distinct rather than one-per-option because the reason is the actionable
// part: five options withheld for one loading backend is one thing to wait for,
// and repeating the token five times says nothing more. Sorted so the same
// state always renders the same string.
func WithheldReasons(withheld []WithheldOption) string {
	seen := make(map[string]struct{}, len(withheld))
	reasons := make([]string, 0, len(withheld))
	for _, w := range withheld {
		r := strings.TrimSpace(w.Reason)
		if r == "" {
			continue
		}
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		reasons = append(reasons, r)
	}
	sort.Strings(reasons)
	return strings.Join(reasons, ", ")
}
