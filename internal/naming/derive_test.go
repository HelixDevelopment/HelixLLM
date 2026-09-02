package naming_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/naming"
)

// The two Claude Toolkit validators, transcribed verbatim from
// claude_toolkit/scripts/lib.sh AS THEY STAND TODAY. This test exists to prove
// the derived identifier fits through them unchanged (FR-014a); it must never
// be "fixed" by widening either pattern here or in the toolkit.
//
//  1. cma_validate_alias — lib.sh:333-335
//     [[ "$1" =~ ^[a-zA-Z][a-zA-Z0-9_-]*$ ]] || cma_die "invalid alias name: $1"
//
//  2. the provider-id guard in cma_provider_write_alias — lib.sh:3302-3309
//     case "$id" in ”|*[!A-Za-z0-9._-]*) ... return 1 ;; esac
//
// Guard 2 is a SHELL-INJECTION CONTROL, not a style preference. Its comment in
// lib.sh states the provider id "is interpolated into the alias body and
// re-parsed when the alias is invoked", and the charset exists "so a hostile
// catalog/--id value can't inject shell commands". The shell `case` rejects the
// empty string and any string containing a character outside the class, which
// is exactly `^[A-Za-z0-9._-]+$`.
var (
	toolkitAliasRE      = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
	toolkitProviderIDRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// deriveCorpus is deliberately hostile: every entry contains something at least
// one of the two validators forbids, so a derivation that leaked any part of the
// identity through verbatim would fail.
func deriveCorpus() []naming.Identity {
	return []naming.Identity{
		{Host: "gpu-01", Model: "llama3", Variant: "8b"},
		{Host: "gpu-01", Model: "llama3"},
		// ':' and '/' — the separators, forbidden by both validators.
		{Host: "gpu-01", Model: "llama3:8b"},
		{Host: "gpu-01", Model: "org/llama3", Variant: "q4_K_M"},
		// Characters that are shell metacharacters — the injection case.
		{Host: "host;rm -rf /", Model: "m"},
		{Host: "h", Model: "$(whoami)"},
		{Host: "h", Model: "a`id`b"},
		{Host: "h", Model: "a|b&c"},
		// Leading non-letter: would break the alias rule's ^[a-zA-Z] anchor.
		{Host: "10.0.0.7", Model: "9-lives"},
		// Unicode and spaces.
		{Host: "höst-ü", Model: "модель", Variant: "большой"},
		{Host: "my host", Model: "my model", Variant: "my variant"},
		// Dots: legal for a provider id, illegal for an alias name.
		{Host: "node.local", Model: "qwen2.5", Variant: "14b"},
		// Long inputs, to exercise the length cap.
		{Host: strings.Repeat("long-host-", 12), Model: strings.Repeat("long-model-", 12)},
	}
}

// TestDeriveSatisfiesBothToolkitValidators is the T039 assertion: every derived
// identifier passes BOTH toolkit checks as they stand today.
func TestDeriveSatisfiesBothToolkitValidators(t *testing.T) {
	for _, id := range deriveCorpus() {
		got, err := naming.Derive(id, naming.ClaudeToolkit)
		if err != nil {
			t.Fatalf("Derive(%q) returned error: %v", id.String(), err)
		}
		if !toolkitAliasRE.MatchString(got) {
			t.Errorf("identifier %q for identity %q fails cma_validate_alias %s",
				got, id.String(), toolkitAliasRE)
		}
		if !toolkitProviderIDRE.MatchString(got) {
			t.Errorf("identifier %q for identity %q fails the provider-id injection guard %s",
				got, id.String(), toolkitProviderIDRE)
		}
		if naming.ClaudeToolkit.MaxLength > 0 && len(got) > naming.ClaudeToolkit.MaxLength {
			t.Errorf("identifier %q is %d bytes, over the ruleset cap %d",
				got, len(got), naming.ClaudeToolkit.MaxLength)
		}
	}
}

// TestDeriveNeverEmitsTheIdentityVerbatim guards the specific defect FR-014a
// names: shipping the human-readable identity as the identifier.
func TestDeriveNeverEmitsTheIdentityVerbatim(t *testing.T) {
	for _, id := range deriveCorpus() {
		got, err := naming.Derive(id, naming.ClaudeToolkit)
		if err != nil {
			t.Fatalf("Derive(%q): %v", id.String(), err)
		}
		if got == id.String() {
			t.Errorf("Derive returned the identity verbatim (%q) — that string is "+
				"rejected by both toolkit validators", got)
		}
	}
}

// TestDeriveIsDeterministic — the same identity must always yield the same
// identifier, or every user's configuration churns on each run (FR-015).
func TestDeriveIsDeterministic(t *testing.T) {
	for _, id := range deriveCorpus() {
		first, err := naming.Derive(id, naming.ClaudeToolkit)
		if err != nil {
			t.Fatalf("Derive(%q): %v", id.String(), err)
		}
		for i := 0; i < 8; i++ {
			again, err := naming.Derive(id, naming.ClaudeToolkit)
			if err != nil {
				t.Fatalf("Derive(%q) run %d: %v", id.String(), i, err)
			}
			if again != first {
				t.Fatalf("Derive(%q) is not deterministic: %q then %q",
					id.String(), first, again)
			}
		}
	}
}

// TestDeriveIsCollisionResistantAcrossNearMisses is the load-bearing case.
//
// The obvious implementation — "replace every forbidden character with '-'" —
// passes the charset assertions above and is still WRONG: it maps
// `llama3:8b` and `llama3-8b` onto one identifier, so two genuinely different
// models become one entry in the user's configuration. Every pair below differs
// ONLY in a character that at least one target charset forbids.
func TestDeriveIsCollisionResistantAcrossNearMisses(t *testing.T) {
	nearMisses := []naming.Identity{
		// ':' vs '-' vs '.' vs '_' in the model — same after naive substitution.
		{Host: "gpu-01", Model: "llama3:8b"},
		{Host: "gpu-01", Model: "llama3-8b"},
		{Host: "gpu-01", Model: "llama3.8b"},
		{Host: "gpu-01", Model: "llama3_8b"},
		{Host: "gpu-01", Model: "llama3 8b"},
		{Host: "gpu-01", Model: "llama3/8b"},
		// The variant boundary: model "llama3" variant "8b" is NOT the same
		// option as a model literally named "llama3:8b".
		{Host: "gpu-01", Model: "llama3", Variant: "8b"},
		// The host/model boundary, differing only in a forbidden separator.
		{Host: "gpu-01/a", Model: "b"},
		{Host: "gpu-01", Model: "a/b"},
		{Host: "gpu", Model: "01-a-b"},
		// Dots vs slashes in a namespaced model name.
		{Host: "h", Model: "org/model"},
		{Host: "h", Model: "org.model"},
		// Case: distinct model names that fold together if lowercased naively.
		{Host: "h", Model: "Llama3"},
		{Host: "h", Model: "llama3"},
	}

	seen := make(map[string]naming.Identity, len(nearMisses))
	for _, id := range nearMisses {
		got, err := naming.Derive(id, naming.ClaudeToolkit)
		if err != nil {
			t.Fatalf("Derive(%q): %v", id.String(), err)
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("COLLISION: identities %q and %q both derive to %q — two "+
				"different models would become one configuration entry",
				prev.String(), id.String(), got)
			continue
		}
		seen[got] = id
	}
}

// TestDeriveDistinguishesEveryDistinctIdentity is the broader sweep: a large
// generated corpus must produce as many identifiers as it has identities.
func TestDeriveDistinguishesEveryDistinctIdentity(t *testing.T) {
	hosts := []string{"gpu-01", "gpu.01", "gpu/01", "gpu:01", "gpu 01", "GPU-01"}
	models := []string{"llama3", "llama3:8b", "llama3-8b", "org/llama3", "qwen2.5"}
	variants := []string{"", "8b", "8-b", "8.b", "q4_K_M", "Q4_K_M"}

	seen := make(map[string]string)
	for _, h := range hosts {
		for _, m := range models {
			for _, v := range variants {
				// Built through NewIdentity, as every real caller does, so the
				// sweep exercises normalised input: "GPU-01" and "gpu-01" are
				// one host and are expected to share an identifier.
				id, err := naming.NewIdentity(h, m, v)
				if err != nil {
					t.Fatalf("NewIdentity(%q,%q,%q): %v", h, m, v, err)
				}
				got, err := naming.Derive(id, naming.ClaudeToolkit)
				if err != nil {
					t.Fatalf("Derive(%q): %v", id.String(), err)
				}
				canonical := id.String()
				if prev, dup := seen[got]; dup && prev != canonical {
					t.Errorf("COLLISION: %q and %q both derive to %q", prev, canonical, got)
				}
				seen[got] = canonical
			}
		}
	}
}

// TestDeriveRejectsAnUnusableRuleset — a ruleset whose own prefix cannot pass
// its own rules must fail loudly rather than emit an identifier the consumer
// will reject at the far end.
func TestDeriveRejectsAnUnusableRuleset(t *testing.T) {
	id := naming.Identity{Host: "h", Model: "m"}

	cases := map[string]naming.Ruleset{
		"prefix starting with a digit": {
			Name: "bad", Prefix: "9x", Separator: '-',
			Allow: naming.ClaudeToolkit.Allow, MustStartWithLetter: true,
		},
		"prefix containing a forbidden character": {
			Name: "bad", Prefix: "he/lix", Separator: '-',
			Allow: naming.ClaudeToolkit.Allow, MustStartWithLetter: true,
		},
		"separator the ruleset itself forbids": {
			Name: "bad", Prefix: "helixllm", Separator: '/',
			Allow: naming.ClaudeToolkit.Allow, MustStartWithLetter: true,
		},
		"no character predicate": {
			Name: "bad", Prefix: "helixllm", Separator: '-',
		},
		"length cap too small for prefix and digest": {
			Name: "bad", Prefix: "helixllm", Separator: '-',
			Allow: naming.ClaudeToolkit.Allow, MustStartWithLetter: true, MaxLength: 4,
		},
	}

	for name, rs := range cases {
		if got, err := naming.Derive(id, rs); err == nil {
			t.Errorf("%s: expected an error, got identifier %q", name, got)
		}
	}
}

// TestDeriveRejectsAnInvalidIdentity — an identity that could not be built by
// NewIdentity must not be derivable either.
func TestDeriveRejectsAnInvalidIdentity(t *testing.T) {
	for _, id := range []naming.Identity{
		{},
		{Host: "h"},
		{Model: "m"},
		{Host: "  ", Model: "m"},
	} {
		if got, err := naming.Derive(id, naming.ClaudeToolkit); err == nil {
			t.Errorf("Derive(%#v): expected an error, got %q", id, got)
		}
	}
}

// TestDeriveIsRulesetScoped — the consumer's rules are a parameter, so a
// different ruleset may legitimately produce a different identifier for the
// same identity.
func TestDeriveIsRulesetScoped(t *testing.T) {
	id := naming.Identity{Host: "gpu-01", Model: "llama3", Variant: "8b"}

	other := naming.Ruleset{
		Name:                "underscore-consumer",
		Prefix:              "hllm",
		Separator:           '_',
		Allow:               func(r rune) bool { return r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') },
		MustStartWithLetter: true,
		MaxLength:           64,
	}

	toolkit, err := naming.Derive(id, naming.ClaudeToolkit)
	if err != nil {
		t.Fatalf("Derive(toolkit): %v", err)
	}
	got, err := naming.Derive(id, other)
	if err != nil {
		t.Fatalf("Derive(other): %v", err)
	}
	if got == toolkit {
		t.Errorf("two rulesets produced the same identifier %q; the ruleset is "+
			"supposed to shape the output", got)
	}
	for _, r := range got {
		if !other.Allow(r) {
			t.Errorf("identifier %q contains %q, which its own ruleset forbids", got, r)
		}
	}
}

// TestRegistryRecordsTheMapping — FR-014's anti-drift requirement: the
// identifier must be resolvable back to the identity it stands for.
func TestRegistryRecordsTheMapping(t *testing.T) {
	reg := naming.NewRegistry()

	id := naming.Identity{Host: "gpu-01", Model: "llama3", Variant: "8b"}
	identifier, err := reg.Register(id, naming.ClaudeToolkit)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	back, ok := reg.IdentityFor(naming.ClaudeToolkit, identifier)
	if !ok {
		t.Fatalf("IdentityFor(%q) found nothing; the mapping was not recorded", identifier)
	}
	if back.String() != id.String() {
		t.Errorf("mapping drifted: %q resolves to %q, want %q",
			identifier, back.String(), id.String())
	}

	fwd, ok := reg.IdentifierFor(naming.ClaudeToolkit, id)
	if !ok || fwd != identifier {
		t.Errorf("IdentifierFor(%q) = (%q, %v), want (%q, true)",
			id.String(), fwd, ok, identifier)
	}
}

// TestRegistryReregistrationIsStable — re-registering an identity must return
// the identifier already recorded, never a second one.
func TestRegistryReregistrationIsStable(t *testing.T) {
	reg := naming.NewRegistry()
	id := naming.Identity{Host: "gpu-01", Model: "llama3"}

	first, err := reg.Register(id, naming.ClaudeToolkit)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	again, err := reg.Register(id, naming.ClaudeToolkit)
	if err != nil {
		t.Fatalf("re-Register: %v", err)
	}
	if again != first {
		t.Errorf("re-registration drifted: %q then %q", first, again)
	}
}

// TestRegistryRefusesAConflictingIdentifier — if two identities ever did land
// on one identifier, the registry must refuse rather than silently overwrite,
// so a drift becomes a loud failure instead of a lost model.
func TestRegistryRefusesAConflictingIdentifier(t *testing.T) {
	reg := naming.NewRegistry()

	a := naming.Identity{Host: "gpu-01", Model: "llama3"}
	identifier, err := reg.Register(a, naming.ClaudeToolkit)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	b := naming.Identity{Host: "gpu-02", Model: "mistral"}
	if err := reg.Adopt(naming.ClaudeToolkit, identifier, b); err == nil {
		t.Errorf("Adopt silently rebound %q from %q to %q; a collision must be "+
			"reported, not absorbed", identifier, a.String(), b.String())
	}
}

// TestRegistryIsRulesetScoped — the same identity registered for two consumers
// keeps a separate mapping per consumer.
func TestRegistryIsRulesetScoped(t *testing.T) {
	reg := naming.NewRegistry()
	id := naming.Identity{Host: "gpu-01", Model: "llama3"}

	other := naming.Ruleset{
		Name:                "underscore-consumer",
		Prefix:              "hllm",
		Separator:           '_',
		Allow:               func(r rune) bool { return r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') },
		MustStartWithLetter: true,
		MaxLength:           64,
	}

	toolkitID, err := reg.Register(id, naming.ClaudeToolkit)
	if err != nil {
		t.Fatalf("Register(toolkit): %v", err)
	}
	otherID, err := reg.Register(id, other)
	if err != nil {
		t.Fatalf("Register(other): %v", err)
	}

	if got, ok := reg.IdentifierFor(naming.ClaudeToolkit, id); !ok || got != toolkitID {
		t.Errorf("toolkit mapping = (%q, %v), want (%q, true)", got, ok, toolkitID)
	}
	if got, ok := reg.IdentifierFor(other, id); !ok || got != otherID {
		t.Errorf("other mapping = (%q, %v), want (%q, true)", got, ok, otherID)
	}
	if _, ok := reg.IdentityFor(other, toolkitID); ok {
		t.Errorf("a toolkit identifier resolved under a different consumer's ruleset")
	}
}
