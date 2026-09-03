package brain_test

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/naming"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

// Contract: specs/002-adaptive-local-model-serving/contracts/model-listing.md
//
// This file covers the clauses of that contract that nothing else in this
// package asserts. It deliberately does NOT restate what is already guarded,
// because a second test asserting an invariant that already has a guard buys no
// safety and costs a maintenance edit every time the first one moves:
//
//	Shape `id` + `model_identity` on the wire ... wire_identity_test.go
//	Invariant 1 (identity names HelixLLM + host) ... naming_test.go, tests 1-2
//	Invariant 2 (id passes the consumer's own rules) . naming_test.go, test 2
//	                                                   naming/derive_test.go
//	Invariant 3 (identifier -> identity is recorded) . naming_test.go, test 5
//	Invariant 5 (unserved is never listed available)  naming_test.go, test 4
//
// What is NOT covered there, and is covered here:
//
//	Shape `owned_by` ................................ TestModelOptions_OwnedBy…
//	Shape "id appropriate to the REQUESTING consumer" TestModelOptionsFor_…
//	Invariant 2+5, the withhold path for a name that
//	  cannot form an identity at all .................. TestModelOptions_Unnameable…
//	Invariant 3, the ENFORCEMENT side: what happens
//	  when a derived identifier is already taken ...... TestModelOptions_Conflicting…
//	Invariant 4, this layer's half of scheme stability
//	  (the served-name -> identity split) ............. TestModelOptions_VariantBoundary…

// --- Shape: `owned_by` (provenance) -----------------------------------------
//
// The contract lists `owned_by` as one of the four things every listed model
// carries, and it is the only one of the four with no assertion anywhere:
// naming_test.go and wire_identity_test.go both SET OwnedBy on their fixtures
// and then assert other fields. Provenance answers "who is offering me this?",
// which is the question a user asks when two hosts serve a model of the same
// name — so a listing that dropped it, or that stamped every option with the
// default provider's name, would be wrong in a way currently invisible.
func TestModelOptions_OwnedByNamesTheOfferingProvider(t *testing.T) {
	b := newBrainWithMocks(map[string]brain.Provider{
		"llamacpp": &hostedProvider{
			mockProvider: mockProvider{name: "llamacpp", available: true, models: []string{"llama3"}},
			host:         "gpu-01",
		},
		"openai": &mockProvider{name: "openai", available: true, models: []string{"gpt-4o"}},
	}, "openai")

	// Two providers, so a bug that reports one name for everything — the
	// default provider's, say — cannot pass.
	byIdentifier := map[string]brain.ModelOption{}
	for _, o := range b.ModelOptions() {
		byIdentifier[o.Identifier] = o
	}
	if len(byIdentifier) != 2 {
		t.Fatalf("ModelOptions() returned %d distinct options, want 2: %+v", len(byIdentifier), byIdentifier)
	}

	local := optionByIdentity(t, b.ModelOptions(), "helixllm/gpu-01/llama3")
	if local.OwnedBy != "llamacpp" {
		t.Errorf("locally-served option OwnedBy = %q, want the offering provider %q",
			local.OwnedBy, "llamacpp")
	}
	if remote := byIdentifier["gpt-4o"]; remote.OwnedBy != "openai" {
		t.Errorf("remote option OwnedBy = %q, want the offering provider %q", remote.OwnedBy, "openai")
	}

	// And it survives onto the wire, where the consumer actually reads it.
	models := b.Models()
	if len(models) != 2 {
		t.Fatalf("Models() returned %d entries, want 2: %+v", len(models), models)
	}
	for _, m := range models {
		if m.OwnedBy == "" {
			t.Errorf("listed model %q carries no owned_by; provenance was dropped", m.ID)
		}
		blob, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal %q: %v", m.ID, err)
		}
		if !strings.Contains(string(blob), `"owned_by":"`+m.OwnedBy+`"`) {
			t.Errorf("owned_by does not reach the wire for %q:\n  %s", m.ID, string(blob))
		}
	}
}

// --- Invariants 2 and 5: the un-nameable model ------------------------------
//
// A served name can be so hostile that no valid identity can be formed from it
// at all — a control character, which would corrupt any line-oriented
// configuration or listing the value were written into. The listing has a
// branch for exactly this (brain.ReasonUnnameable), and nothing exercised it.
//
// The failure this guards is not "an ugly name appears". It is that the branch
// could be removed, or turned into a pass-through, and every existing test would
// stay green while a newline-bearing id landed in a user's config file and split
// one entry into two. So the oracle is the PUBLISHED bytes: no listed id may
// carry a control character, whatever else changes.
func TestModelOptions_UnnameableModelIsWithheldRatherThanPublishedMalformed(t *testing.T) {
	const rogue = "llama3\nrogue" // interior newline: TrimSpace cannot rescue it

	b := newBrainWithMocks(map[string]brain.Provider{
		"llamacpp": &hostedProvider{
			mockProvider: mockProvider{
				name:      "llamacpp",
				available: true,
				models:    []string{"llama3", rogue},
			},
			host: "gpu-01",
		},
	}, "llamacpp")

	opts := b.ModelOptions()
	if len(opts) != 2 {
		t.Fatalf("ModelOptions() returned %d options, want both the good and the rogue one: %+v",
			len(opts), opts)
	}

	var withheld *brain.ModelOption
	for i := range opts {
		if opts[i].Identifier == rogue {
			withheld = &opts[i]
		}
	}
	if withheld == nil {
		t.Fatalf("the un-nameable model is missing from ModelOptions() entirely; it must be "+
			"reported as withheld, not silently dropped (FR-019): %+v", opts)
	}
	if withheld.Available {
		t.Error("a model whose name cannot form a valid identity is reported as available")
	}
	if withheld.Reason != brain.ReasonUnnameable {
		t.Errorf("withheld reason = %q, want %q", withheld.Reason, brain.ReasonUnnameable)
	}
	if withheld.Identity != "" {
		t.Errorf("a model that has no valid identity carries one anyway: %q", withheld.Identity)
	}

	// The load-bearing assertion: nothing corrupting reaches the listing.
	models := b.Models()
	if len(models) != 1 {
		t.Fatalf("Models() published %d entries, want only the 1 nameable model: %+v", len(models), models)
	}
	for _, m := range models {
		for _, r := range m.ID {
			if unicode.IsControl(r) {
				t.Fatalf("published id %q contains control character %q; written into a "+
					"line-oriented configuration this corrupts the file", m.ID, r)
			}
		}
	}
	// And the good model beside it is unharmed — a withhold that took out the
	// whole provider would also satisfy the assertions above.
	if models[0].ModelIdentity != "helixllm/gpu-01/llama3" {
		t.Errorf("surviving model identity = %q, want %q",
			models[0].ModelIdentity, "helixllm/gpu-01/llama3")
	}
}

// --- Invariant 3, enforcement side: the taken identifier --------------------
//
// Invariant 3 says id and model_identity "cannot drift". naming_test.go test 5
// shows the mapping is RECORDED; this shows what happens when recording it
// fails, which is the half that actually protects a user's configuration.
//
// If a derived identifier is already bound to a different identity, the only
// two available behaviours are: withhold the option, or overwrite the binding.
// Overwriting is the dangerous one and it is silent — the identifier in the
// user's config keeps working and starts answering with a DIFFERENT model. So
// the oracle here is that the conflicting identifier is never published as this
// model's own.
//
// The registry is pre-loaded rather than waiting for a natural 48-bit collision,
// which is the only way to reach this branch deliberately.
func TestModelOptions_ConflictingIdentifierIsWithheldNotSilentlyRebound(t *testing.T) {
	b := newBrainWithMocks(map[string]brain.Provider{
		"llamacpp": &hostedProvider{
			mockProvider: mockProvider{name: "llamacpp", available: true, models: []string{"llama3"}},
			host:         "gpu-01",
		},
	}, "llamacpp")

	// The identifier this listing is about to derive...
	wanted, err := naming.NewIdentity("gpu-01", "llama3", "")
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	identifier, err := naming.Derive(wanted, naming.ClaudeToolkit)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}

	// ...is already spoken for by a different model.
	incumbent, err := naming.NewIdentity("gpu-02", "mistral", "")
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	if err := b.Names().Adopt(naming.ClaudeToolkit, identifier, incumbent); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	opts := b.ModelOptions()
	if len(opts) != 1 {
		t.Fatalf("ModelOptions() returned %d options, want 1: %+v", len(opts), opts)
	}
	got := opts[0]

	if got.Available {
		t.Error("an option whose identifier is already bound to a different model is reported " +
			"as available; the user would select it and be answered by the other model")
	}
	if got.Reason != brain.ReasonIdentifierConflict {
		t.Errorf("withheld reason = %q, want %q", got.Reason, brain.ReasonIdentifierConflict)
	}
	if got.Identifier == identifier {
		t.Errorf("the contested identifier %q was published for %q anyway, even though it "+
			"stands for %q — the incumbent model just disappeared from the user's configuration",
			identifier, got.Identity, incumbent.String())
	}
	// The withheld option still says which model it is, or the consumer cannot
	// report what it lost (FR-019).
	if got.Identity != wanted.String() {
		t.Errorf("withheld option identity = %q, want %q", got.Identity, wanted.String())
	}

	// The registry is unchanged: the incumbent still owns the identifier.
	back, ok := b.Names().IdentityFor(naming.ClaudeToolkit, identifier)
	if !ok || back != incumbent {
		t.Errorf("identifier %q now resolves to (%q, %v); the conflicting registration "+
			"overwrote the incumbent binding", identifier, back.String(), ok)
	}
	// The conflicted option is LISTED — a consumer that cannot see it cannot
	// report what it lost — but it is never offered as usable, and it says why.
	published := b.Models()
	if len(published) != 1 {
		t.Fatalf("Models() published %d entries, want the 1 conflicted option "+
			"reported as withheld: %+v", len(published), published)
	}
	if published[0].Availability != api.AvailabilityWithheld {
		t.Errorf("a conflicted option was published as %q; a consumer routes to exactly "+
			"%q, and this model is answered by a DIFFERENT one",
			published[0].Availability, api.AvailabilityServing)
	}
	if published[0].WithheldReason != api.WithheldIdentifierConflict {
		t.Errorf("withheld_reason = %q, want %q — the collision is the part the "+
			"operator can act on", published[0].WithheldReason, api.WithheldIdentifierConflict)
	}
	if published[0].ID == identifier {
		t.Errorf("the contested identifier %q reached the listing anyway", identifier)
	}
}

// --- Shape: "the identifier appropriate to the REQUESTING consumer" ---------
//
// The contract's first shape clause makes `id` consumer-dependent, and
// ModelOptionsFor takes the ruleset as a parameter to honour that. Every test
// in this package calls ModelOptions(), which hardcodes ClaudeToolkit — so a
// change that ignored the parameter and always derived for the toolkit would be
// invisible: every assertion in the package would still pass while a consumer
// with a different charset received identifiers it rejects.
//
// naming/derive_test.go proves Derive and Registry are ruleset-scoped. This
// proves Brain actually passes the caller's ruleset down to them.
func TestModelOptionsFor_DerivesForTheRequestingConsumerNotTheDefaultOne(t *testing.T) {
	// A consumer whose charset has NOTHING in common with the toolkit's beyond
	// alphanumerics: '-' is forbidden, '_' is the separator.
	underscore := naming.Ruleset{
		Name:      "underscore-consumer",
		Prefix:    "hllm",
		Separator: '_',
		Allow: func(r rune) bool {
			return r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z')
		},
		MustStartWithLetter: true,
		MaxLength:           64,
	}

	b := newBrainWithMocks(map[string]brain.Provider{
		"llamacpp": &hostedProvider{
			mockProvider: mockProvider{name: "llamacpp", available: true, models: []string{"llama3:8b"}},
			host:         "gpu-01",
		},
	}, "llamacpp")

	toolkit := b.ModelOptionsFor(naming.ClaudeToolkit)
	custom := b.ModelOptionsFor(underscore)
	if len(toolkit) != 1 || len(custom) != 1 {
		t.Fatalf("want 1 option per ruleset, got %d and %d", len(toolkit), len(custom))
	}

	// Same option, same identity — only the identifier is consumer-specific.
	if custom[0].Identity != toolkit[0].Identity {
		t.Errorf("the identity changed with the consumer: %q vs %q; the identity is a VALUE "+
			"describing the option, not a per-consumer rendering",
			custom[0].Identity, toolkit[0].Identity)
	}
	if custom[0].Identifier == toolkit[0].Identifier {
		t.Fatalf("both consumers received the identifier %q, but their charsets differ; "+
			"the requested ruleset was ignored", custom[0].Identifier)
	}
	if !strings.HasPrefix(custom[0].Identifier, "hllm_") {
		t.Errorf("identifier %q was not derived under the requested ruleset (prefix %q, separator %q)",
			custom[0].Identifier, underscore.Prefix, string(underscore.Separator))
	}
	for _, r := range custom[0].Identifier {
		if !underscore.Allow(r) {
			t.Errorf("identifier %q contains %q, which the requesting consumer forbids",
				custom[0].Identifier, r)
		}
	}

	// And it is recorded under THAT consumer, so resolving it later works.
	back, ok := b.Names().IdentityFor(underscore, custom[0].Identifier)
	if !ok {
		t.Fatalf("identifier %q was not recorded for consumer %q", custom[0].Identifier, underscore.Name)
	}
	if back.String() != custom[0].Identity {
		t.Errorf("recorded identity %q != published identity %q", back.String(), custom[0].Identity)
	}
}

// --- Invariant 4, this layer's half: the served-name -> identity split ------
//
// FR-015 stability is enforced at the derivation layer by the golden file in
// internal/naming. But the derivation is only half the pipeline: this layer
// decides WHICH identity a served name maps to, by splitting "llama3:8b" into
// model "llama3" and variant "8b". That split feeds the canonical identity that
// gets hashed, so changing it changes every derived identifier for every
// variant-bearing model — silently breaking exactly the configurations FR-015
// protects, while a golden file over Identity values stays green.
//
// Nothing pins it: naming_test.go serves "llama3:8b" in three tests and never
// asserts the identity it produces. So the literal is pinned here, and the
// reverse direction — identifier back to the name the provider answers to — is
// asserted directly rather than only through a routing side-effect.
func TestModelOptions_VariantBoundaryIsPinnedAndReversible(t *testing.T) {
	b := newBrainWithMocks(map[string]brain.Provider{
		"llamacpp": &hostedProvider{
			mockProvider: mockProvider{
				name:      "llamacpp",
				available: true,
				models:    []string{"llama3:8b", "library/qwen2.5-coder:7b-instruct-q4_K_M"},
			},
			host: "gpu-01",
		},
	}, "llamacpp")

	opts := b.ModelOptions()
	if len(opts) != 2 {
		t.Fatalf("ModelOptions() returned %d options, want 2: %+v", len(opts), opts)
	}

	// The exact canonical values. A model named "llama3:8b" splits on its LAST
	// colon; it must NOT arrive as a model literally named "llama3:8b", which
	// renders with an escaped colon and hashes to something else entirely.
	want := map[string]string{
		"llama3:8b": "helixllm/gpu-01/llama3:8b",
		"library/qwen2.5-coder:7b-instruct-q4_K_M": `helixllm/gpu-01/library\/qwen2.5-coder:7b-instruct-q4_K_M`,
	}
	for served, identity := range want {
		opt := optionByIdentity(t, opts, identity)
		if opt.Identity != identity {
			t.Errorf("served %q produced identity %q, want %q", served, opt.Identity, identity)
		}

		// Reverse: the identifier we just published resolves back to the name
		// the provider actually answers to, character for character.
		got, ok := b.ResolveModelName(opt.Identifier)
		if !ok {
			t.Errorf("published identifier %q does not resolve back to a served name", opt.Identifier)
			continue
		}
		if got != served {
			t.Errorf("identifier %q resolves to %q, want the served name %q; the split and the "+
				"join disagree, so a request for this option reaches no provider by exact match",
				opt.Identifier, got, served)
		}
	}
}
