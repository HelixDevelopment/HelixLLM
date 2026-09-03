package brain_test

import (
	"os"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/naming"
)

// The published identity must name a MACHINE.
//
// `helixllm/<host>/<model>` exists so a user reading a model list can say
// "that is HelixLLM-served, and THAT host is serving it" (FR-014, FR-023). A
// host segment of `127.0.0.1` or `localhost` satisfies neither half: it names
// no machine an operator could find, and — because it is the same string on
// every machine — it makes two gateways on two DIFFERENT hosts publish
// IDENTICAL identities, identical digests and identical ids. The Claude
// Toolkit then collapses them (`group_by(.provider_id) | map(.[0])` in
// claude_toolkit/scripts/claude-providers.sh), silently keeping one host's
// models and dropping the other's. That collision sits one layer ABOVE the
// digest, so no amount of hashing can fix it.
//
// This is the normal case, not an edge case: the default LocalRPCHost is
// "localhost", and cmd/helixllm/main.go rewrites it to "127.0.0.1" whenever the
// embedded llama-server is used.
//
// RED_MODE is the §11.4.115 polarity switch. RED_MODE=1 (the default) asserts
// the DEFECT IS PRESENT and is how this test was run against the pre-fix
// artifact; RED_MODE=0 is the standing regression guard asserting it is gone.
func redMode(t *testing.T) bool {
	t.Helper()
	return os.Getenv("RED_MODE") == "1"
}

// machineName is the same normalisation the fix applies, computed
// independently here so the test does not simply echo the implementation.
func machineName(t *testing.T) string {
	t.Helper()
	h, err := os.Hostname()
	if err != nil {
		t.Skipf("SKIP-OK: this host reports no hostname (%v), so there is no "+
			"machine name to compare against", err)
	}
	h = strings.ToLower(strings.TrimSpace(h))
	if i := strings.Index(h, "."); i > 0 {
		// A short name and its FQDN are the same machine; the fix keeps the
		// short one so the identity stays readable.
		h = h[:i]
	}
	if h == "" || h == "localhost" {
		t.Skipf("SKIP-OK: this host calls itself %q, which names no machine "+
			"either — there is nothing better to resolve a loopback URL to", h)
	}
	return h
}

func TestLoopbackServingHostNamesTheMachine(t *testing.T) {
	want := machineName(t)

	// Both spellings of loopback, because the two are interchangeable in
	// configuration and an operator switching between them must not re-mint
	// every identifier in their tool config.
	for _, base := range []string{
		"http://127.0.0.1:8080",
		"http://localhost:8080",
		"http://[::1]:8080",
	} {
		p := brain.NewLlamaCppProvider(base, []string{"llama3"})
		got := p.ServingHost()

		if redMode(t) {
			if got != "127.0.0.1" && got != "localhost" && got != "::1" {
				t.Fatalf("RED_MODE=1 expects the defect to be present, but %s "+
					"already resolves to %q — the defect is gone; re-run with "+
					"RED_MODE=0", base, got)
			}
			continue
		}

		if got != want {
			t.Errorf("ServingHost() for %s = %q, want %q — a loopback base URL "+
				"names no machine, so the identity must resolve to the machine "+
				"actually serving (FR-014, FR-023)", base, got, want)
		}
	}
}

// The two spellings of loopback must produce the SAME identifier.
//
// HELIX_LLM_LOCAL_RPC_HOST defaults to "localhost" and the embedded-server path
// rewrites it to "127.0.0.1". Before the fix that rewrite silently re-mints
// every published identifier — the same machine serving the same model appears
// under two different ids depending on which spelling was in effect.
func TestLoopbackSpellingsProduceOneIdentifier(t *testing.T) {
	_ = machineName(t) // skip early on a host with no usable name

	derive := func(base string) string {
		t.Helper()
		p := brain.NewLlamaCppProvider(base, []string{"llama3"})
		id, err := naming.NewIdentity(p.ServingHost(), "llama3", "")
		if err != nil {
			t.Fatalf("NewIdentity for %s: %v", base, err)
		}
		got, err := naming.Derive(id, naming.ClaudeToolkit)
		if err != nil {
			t.Fatalf("Derive for %s: %v", base, err)
		}
		return got
	}

	dotted := derive("http://127.0.0.1:8080")
	named := derive("http://localhost:8080")

	if redMode(t) {
		if dotted == named {
			t.Fatalf("RED_MODE=1 expects the defect to be present, but both "+
				"spellings already derive %q — re-run with RED_MODE=0", dotted)
		}
		return
	}

	if dotted != named {
		t.Errorf("127.0.0.1 derives %q but localhost derives %q — the same "+
			"machine serving the same model must publish ONE identifier, or "+
			"switching HELIX_LLM_LOCAL_RPC_HOST silently invalidates every "+
			"entry in a user's tool configuration", dotted, named)
	}
}

// A base URL that already names a real machine is left exactly as it is.
//
// The loopback substitution is a repair for a URL that names no machine, not a
// licence to overwrite one that does — a remote instance's host is the host, and
// rewriting it to the gateway's own name would point every identity at the
// wrong box.
func TestNonLoopbackServingHostIsUntouched(t *testing.T) {
	for base, want := range map[string]string{
		"http://gpu-01.lan:8080": "gpu-01.lan",
		"http://gpu-02:8080":     "gpu-02",
		"https://10.0.0.7:8443":  "10.0.0.7",
		"gpu-03:8080":            "gpu-03",
	} {
		if got := brain.NewLlamaCppProvider(base, nil).ServingHost(); got != want {
			t.Errorf("ServingHost() for %s = %q, want %q", base, got, want)
		}
	}
}
