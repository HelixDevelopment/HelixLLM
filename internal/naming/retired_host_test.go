package naming

import (
	"strings"
	"testing"
)

// The retired-host renderings must be DERIVED from the ruleset, not spelled
// out, and the match must be a whole segment rather than a substring.
//
// Both properties are load-bearing. A hand-written literal would stop matching
// the moment the derivation's charset or separator changed, silently reporting
// every retired identifier as merely-unavailable again. A substring match would
// permanently refuse a live model served by a machine whose own name simply
// begins with a retired rendering — turning a fix for one population into a
// defect for another.

func TestRuleset_RetiredHostIdentifierPrefixes_AreDerivedNotSpelled(t *testing.T) {
	got := ClaudeToolkit.RetiredHostIdentifierPrefixes()
	if len(got) != len(RetiredHosts) {
		t.Fatalf("got %d prefixes for %d retired hosts: %v", len(got), len(RetiredHosts), got)
	}

	// Every prefix must be exactly what Derive puts in front of an identifier
	// for that host. Building the expectation by DERIVING a real identifier —
	// rather than by re-implementing the rendering here — is what makes this a
	// check on the derivation instead of a restatement of it.
	for i, host := range RetiredHosts {
		id, err := NewIdentity(host, "llama3", "8b")
		if err != nil {
			t.Fatalf("retired host %q cannot form an identity: %v", host, err)
		}
		identifier, err := Derive(id, ClaudeToolkit)
		if err != nil {
			t.Fatalf("derive for retired host %q: %v", host, err)
		}
		if !strings.HasPrefix(identifier, got[i]) {
			t.Errorf("prefix %q is not what Derive produces for host %q (%q)",
				got[i], host, identifier)
		}
		if !strings.HasSuffix(got[i], string(ClaudeToolkit.Separator)) {
			t.Errorf("prefix %q does not end at a segment boundary; a substring match "+
				"would catch live identifiers of hosts merely NAMED like %q", got[i], host)
		}
	}
}

func TestRuleset_HasRetiredHostSegment(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want bool
	}{
		{"retired loopback ip rendering", "helixllm-127-0-0-1-qwen2-5-7b-ba85a3230a59", true},
		{"retired loopback name rendering", "helixllm-localhost-llama3-8b-0123456789ab", true},

		// The substring hazard, from both directions.
		{"host merely starting with a retired rendering", "helixllm-localhosting-llama3-8b-0123456789ab", false},
		{"retired rendering not in the host position", "helixllm-gpu-01-localhost-model-0123456789ab", false},

		// Names that are ours but carry a live host segment: unresolvable is
		// not the same as retired, and only the second may be reported as
		// permanent.
		{"machine-named host", "helixllm-gpu-01-llama3-8b-0123456789ab", false},

		// Names that are not ours at all keep their existing treatment.
		{"raw model name", "llama3:8b", false},
		{"another vendor's id", "gpt-4o", false},
		{"empty", "", false},

		// A prefix with nothing after it is not an identifier for anything.
		{"prefix only", "helixllm-", false},
		{"retired segment only, no model or digest", "helixllm-localhost-", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClaudeToolkit.HasRetiredHostSegment(tc.in); got != tc.want {
				t.Errorf("HasRetiredHostSegment(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// A retired host must be one the serving layer can no longer produce. If a
// retired rendering could still be emitted, marking it permanent would refuse
// a model that is genuinely being served.
//
// This asserts the property at the level this package can see: the retired
// hosts are the loopback spellings, and the identity host that replaced them is
// a machine name. The serving-side guarantee — that a loopback base URL now
// resolves to the machine's own name — lives in internal/brain and is guarded
// there; this is the naming half of the same claim.
func TestRetiredHosts_AreLoopbackSpellingsOnly(t *testing.T) {
	for _, host := range RetiredHosts {
		lower := strings.ToLower(host)
		if lower != "localhost" && !strings.HasPrefix(lower, "127.") {
			t.Errorf("retired host %q is not a loopback spelling; a host that could still be "+
				"published must never be reported as permanently gone", host)
		}
	}
}
