package brain

// A machine that cannot say what it is called must publish NO host, never the
// loopback literal it was configured with.
//
// # The condition
//
// f63b96f resolved a loopback base URL to the machine's own name, and
// TestLoopbackServingHostNamesTheMachine guards that. But that guard SKIPS on
// exactly the machines this test is about: its machineName() helper skips when
// os.Hostname() answers "localhost", because there is then nothing better to
// resolve to. The branch it skips over is `return host` — the loopback literal,
// published verbatim.
//
// That is not a rare machine. os.Hostname() answers "localhost" (or
// "localhost.localdomain") on stock cloud images, live images and many
// containers, and thisMachineName rejects both. On such a machine the embedded
// llama-server path sets LocalRPCHost to "127.0.0.1" (cmd/helixllm/main.go), so
// the deployment publishes `helixllm-127-0-0-1-<model>-<digest>` — which is:
//
//   - the identity that names no machine FR-023 exists to make findable, and
//   - the SAME string on every such machine, so two gateways on two different
//     hosts publish identical ids and the consuming toolkit de-duplicates one
//     of them away. That is precisely the collision f63b96f was written to fix,
//     still live on this class of machine.
//
// It also falsified a premise two later changes were built on:
// naming.RetiredHosts documents that "the serving host resolver rejects
// anything that names no machine … so no future identity can carry any of
// them". That was an assertion, not a fact, and this test is what makes it a
// fact — the resolver now cannot emit one.
//
// # Polarity (§11.4.115)
//
//	RED_MODE=1 go test -run TestServingHost_ ./internal/brain/
//	           go test -run TestServingHost_ ./internal/brain/
//
// RED_MODE=1 asserts the pre-fix answer (the loopback literal, published), so a
// run against the unfixed tree PASSES and proves the reproduction is real.
// RED_MODE=0 is the standing guard.

import (
	"os"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/naming"
)

// noMachineName is the resolver a machine that calls itself "localhost"
// produces: thisMachineName rejects such a name and answers "".
func noMachineName() string { return "" }

// loopbackBaseURLs are the base URLs a HelixLLM deployment actually runs with.
// "localhost" is the documented default for HELIX_LLM_LOCAL_RPC_HOST and
// "127.0.0.1" is what cmd/helixllm rewrites it to for the embedded server; the
// wildcard binds and the IPv6 spelling reach the same branch.
var loopbackBaseURLs = map[string]string{
	"http://127.0.0.1:8080": "127.0.0.1",
	"http://localhost:8080": "localhost",
	"http://0.0.0.0:8080":   "0.0.0.0",
	"http://[::1]:8080":     "::1",
}

func redModeNoMachine() bool { return os.Getenv("RED_MODE") == "1" }

func TestServingHost_LoopbackOnAMachineWithNoNameIsNotPublished(t *testing.T) {
	for base, loopbackLiteral := range loopbackBaseURLs {
		p := NewLlamaCppProvider(base, []string{"llama3:8b"})
		p.machineName = noMachineName

		got := p.ServingHost()

		if redModeNoMachine() {
			if got != loopbackLiteral {
				t.Fatalf("RED_MODE=1 expects the defect to be present, but %s already "+
					"resolves to %q instead of the loopback literal %q — the defect is "+
					"gone; re-run with RED_MODE=0", base, got, loopbackLiteral)
			}
			continue
		}

		if got != "" {
			t.Errorf("ServingHost() for %s on a machine that cannot name itself = %q, want %q.\n"+
				"A loopback or wildcard literal names no machine and is the same string on "+
				"every host, so publishing it re-creates the cross-host identity collision "+
				"f63b96f fixed. Reporting no host instead leaves the models un-prefixed, "+
				"which is what a remote provider already does.", base, got, "")
		}
	}
}

// The constructive form of the same statement, and the one the retired-host
// reasoning in naming.RetiredHosts leans on: whatever ServingHost returns, it
// is never a value that names no machine.
func TestServingHost_NeverReturnsAValueThatNamesNoMachine(t *testing.T) {
	if redModeNoMachine() {
		t.Skip("SKIP-OK: this is the post-fix invariant; RED polarity is covered by " +
			"TestServingHost_LoopbackOnAMachineWithNoNameIsNotPublished")
	}
	for _, resolver := range []func() string{noMachineName, thisMachineName} {
		for base := range loopbackBaseURLs {
			p := NewLlamaCppProvider(base, nil)
			p.machineName = resolver
			got := p.ServingHost()
			if got != "" && namesNoMachine(got) {
				t.Errorf("ServingHost() for %s = %q, which names no machine.\n"+
					"naming.RetiredHosts states as fact that the resolver rejects such "+
					"values so no future identity can carry one; that is only true if this "+
					"holds.", base, got)
			}
		}
	}
}

// The user-visible consequence, asserted where the user sees it: the option
// list such a deployment publishes carries no `helixllm-` identifier at all,
// because there is no host to name in one.
func TestModelOptions_NoIdentifierIsPublishedWithoutAMachineName(t *testing.T) {
	p := NewLlamaCppProvider("http://127.0.0.1:8080", []string{"llama3:8b"})
	p.machineName = noMachineName

	b := New(Config{DefaultProvider: p.Name()})
	b.RegisterProvider(p.Name(), p)

	opts := b.ModelOptionsFor(naming.ClaudeToolkit)
	if len(opts) == 0 {
		t.Fatal("no options published at all; this test would then prove nothing")
	}
	for _, opt := range opts {
		if redModeNoMachine() {
			if strings.HasPrefix(opt.Identifier, naming.ClaudeToolkit.IdentifierPrefix()) {
				return // defect present: an identifier WAS minted from the loopback literal
			}
			continue
		}
		if strings.HasPrefix(opt.Identifier, naming.ClaudeToolkit.IdentifierPrefix()) {
			t.Errorf("published identifier %q (identity %q, host %q) on a machine that "+
				"cannot name itself.\nEvery such machine publishes this same string, so a "+
				"second gateway would collide with it id-for-id.",
				opt.Identifier, opt.Identity, opt.Host)
		}
	}
	if redModeNoMachine() {
		t.Fatalf("RED_MODE=1 expects the defect to be present, but no identifier was "+
			"minted from the loopback literal: %+v — re-run with RED_MODE=0", opts)
	}
}
