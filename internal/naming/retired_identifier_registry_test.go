package naming

// A retired host RENDERING is not a retired identifier.
//
// HasRetiredHostSegment answers a question about the NAME: does this identifier
// open with one of the host renderings this project has stopped publishing? For
// a while that was treated as the whole answer, on the stated grounds that no
// live identity could carry such a rendering. Two things made that false:
//
//   - a machine that could not say what it is called published the loopback
//     literal verbatim, so `helixllm-127-0-0-1-…` was a CURRENT identifier
//     there (fixed in brain.LlamaCppProvider.ServingHost);
//   - a machine genuinely called `localhost.lan` or `localhost-2` renders into
//     a host segment that BEGINS with a retired one — and that is not a defect
//     to fix, it is a real machine name we must keep.
//
// So the decision needs the registry: what is this deployment publishing right
// now? These cases pin both directions of that — a live host must not have its
// own identifiers called permanently gone, and a genuinely pre-rename
// identifier must still be called permanently gone even on such a host.

import "testing"

// registryServing returns a registry holding one identity per given host, which
// is what a deployment serving those hosts has by the time a request arrives.
func registryServing(t *testing.T, hosts ...string) *Registry {
	t.Helper()
	r := NewRegistry()
	for _, h := range hosts {
		id, err := NewIdentity(h, "llama3", "8b")
		if err != nil {
			t.Fatalf("build an identity on host %q: %v", h, err)
		}
		if _, err := r.Register(id, ClaudeToolkit); err != nil {
			t.Fatalf("register an identity on host %q: %v", h, err)
		}
	}
	return r
}

func TestRegistry_IsRetiredIdentifier(t *testing.T) {
	for _, tc := range []struct {
		name       string
		liveHosts  []string
		identifier string
		want       bool
		why        string
	}{
		{
			name:       "retired rendering, nothing live under it",
			liveHosts:  []string{"gpu-01"},
			identifier: "helixllm-127-0-0-1-qwen2-5-7b-ba85a3230a59",
			want:       true,
			why:        "this is the pre-rename identifier the migration message is for",
		},
		{
			name:       "retired rendering, but that very host is live",
			liveHosts:  []string{"127.0.0.1"},
			identifier: "helixllm-127-0-0-1-qwen2-5-7b-ba85a3230a59",
			want:       false,
			why:        "the deployment is publishing on that host now; the model is merely not in its list",
		},
		{
			name:       "live host renders into the retired segment",
			liveHosts:  []string{"localhost.lan"},
			identifier: "helixllm-localhost-lan-qwen2-5-7b-142ed7235c94",
			want:       false,
			why:        "`localhost.lan` is a real machine and these are its current identifiers",
		},
		{
			name:       "pre-rename identifier on a host that renders alike",
			liveHosts:  []string{"localhost.lan"},
			identifier: "helixllm-localhost-llama3-8b-0123456789ab",
			want:       true,
			why: "the live host publishes `helixllm-localhost-lan-…`; this shorter segment is " +
				"the pre-rename one and is still gone for good",
		},
		{
			name:       "host merely starting with a retired rendering",
			liveHosts:  []string{"gpu-01"},
			identifier: "helixllm-localhosting-qwen2-5-7b-ba85a3230a59",
			want:       false,
			why:        "the segment separator makes the match whole-segment, so `localhosting` is untouched",
		},
		{
			name:       "empty registry keeps the name-level answer",
			identifier: "helixllm-localhost-llama3-8b-0123456789ab",
			want:       true,
			why:        "nothing is published at all, so nothing accounts for the segment",
		},
		{
			name:       "not one of ours",
			liveHosts:  []string{"gpu-01"},
			identifier: "llama3:8b",
			want:       false,
			why:        "a raw model name carries no provenance and cannot be a retired identifier",
		},
		{
			name:       "surrounding whitespace is not a way past the check",
			liveHosts:  []string{"gpu-01"},
			identifier: "  helixllm-127-0-0-1-qwen2-5-7b-ba85a3230a59\t",
			want:       true,
			why:        "the name is trimmed before it is classified",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := registryServing(t, tc.liveHosts...)
			if got := r.IsRetiredIdentifier(ClaudeToolkit, tc.identifier); got != tc.want {
				t.Errorf("IsRetiredIdentifier(%q) = %v, want %v with live hosts %v.\n%s",
					tc.identifier, got, tc.want, tc.liveHosts, tc.why)
			}
		})
	}
}

// The live-host prefix and the retired-host prefix have to be built the same
// way, or the comparison between them is meaningless. Deriving both from
// HostIdentifierPrefix is what guarantees that; this pins the connection to
// what Derive actually emits.
func TestRuleset_HostIdentifierPrefix_MatchesWhatDeriveEmits(t *testing.T) {
	for _, host := range []string{"gpu-01", "localhost.lan", "127.0.0.1", "localhost"} {
		id, err := NewIdentity(host, "llama3", "8b")
		if err != nil {
			t.Fatalf("build an identity on host %q: %v", host, err)
		}
		identifier, err := Derive(id, ClaudeToolkit)
		if err != nil {
			t.Fatalf("derive on host %q: %v", host, err)
		}
		prefix := ClaudeToolkit.HostIdentifierPrefix(host)
		if prefix == "" {
			t.Errorf("HostIdentifierPrefix(%q) is empty, but %q was derived for it", host, identifier)
			continue
		}
		if got := identifier[:min(len(identifier), len(prefix))]; got != prefix {
			t.Errorf("Derive produced %q for host %q, which does not open with the prefix %q "+
				"computed for the same host — the two spellings have drifted apart",
				identifier, host, prefix)
		}
	}

	// The retired prefixes are the same computation applied to the retired
	// hosts, so they must agree element for element.
	got := ClaudeToolkit.RetiredHostIdentifierPrefixes()
	if len(got) != len(RetiredHosts) {
		t.Fatalf("got %d retired prefixes for %d retired hosts: %v", len(got), len(RetiredHosts), got)
	}
	for i, host := range RetiredHosts {
		if want := ClaudeToolkit.HostIdentifierPrefix(host); got[i] != want {
			t.Errorf("retired prefix %d = %q, want %q", i, got[i], want)
		}
	}
}
