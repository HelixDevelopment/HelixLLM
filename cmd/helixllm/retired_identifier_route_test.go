package main

// A stale identifier must not tell the client to retry forever.
//
// # The condition
//
// f63b96f moved the identity host off the loopback literal onto the machine's
// own name, which re-minted every locally-served identifier exactly once. A
// client whose configuration still holds an old one names something this
// deployment will never publish again.
//
// e142fce made that request fail rather than misroute, and chose 503 for it,
// deliberately and with a correct general argument: the published identifier is
// a HASH, the host cannot be recovered from it, so a stale identifier and a
// host that is merely down look the same from the gateway. That argument holds
// for the general case and this guard does not disturb it.
//
// It does not hold for ONE bounded population. The retired host renderings are
// exactly identifiable — this server KNOWS it used to publish `127-0-0-1` and
// `localhost` as the host segment and knows it no longer does — so an
// identifier carrying one of them is permanently dead, not temporarily absent.
// 503 means "retry with backoff"; a correct client obeying it against something
// that will never come back retries forever. That is the same criticism this
// project accepted for malformed requests in 834971c: a retry that cannot
// succeed is a client looping indefinitely, and the server told it to.
//
// # What is deliberately NOT generalised
//
// Only the bounded, exactly-known set is permanent. Every OTHER unresolvable
// identifier keeps 503, because for those the gateway genuinely cannot tell a
// re-minted name from a host that is down — and guessing from whether a host
// segment "looks known" would put the misclassification back with a heuristic
// wearing a certainty it does not have. The third case below is the guard on
// that: it must stay 503, or this fix could be satisfied by making everything
// permanent.
//
// # Polarity (§11.4.115)
//
//	RED_MODE=1 go test -run TestChatCompletions_RetiredLoopbackIdentifier ./cmd/helixllm/
//	           go test -run TestChatCompletions_RetiredLoopbackIdentifier ./cmd/helixllm/
//
// RED_MODE=1 asserts the pre-fix answer (503 for the retired identifier), so a
// run against the unfixed tree PASSES and proves the reproduction is real
// rather than a failure written to agree with the fix. RED_MODE=0 is the
// standing guard.

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/naming"
)

// redMode reports whether the suite runs in defect-reproduction polarity.
func redMode() bool { return os.Getenv("RED_MODE") == "1" }

// retiredLoopbackIdentifiers are identifiers of exactly the shape this server
// published before the host rename: our prefix, a retired loopback host
// segment, a model segment, a digest. Both spellings the documented setup could
// produce are covered — "localhost" was the default and cmd/helixllm rewrote it
// to "127.0.0.1" for the embedded llama-server.
var retiredLoopbackIdentifiers = []string{
	"helixllm-127-0-0-1-qwen2-5-7b-ba85a3230a59",
	"helixllm-localhost-llama3-8b-0123456789ab",
}

// TestChatCompletions_RetiredLoopbackIdentifierIsPermanent pins the three-way
// split at the HTTP boundary, driving the wiring main.go builds.
func TestChatCompletions_RetiredLoopbackIdentifierIsPermanent(t *testing.T) {
	// The status a retired identifier must carry. 404 rather than 410 because
	// it is what THIS gateway already answers for every other "the name you
	// gave identifies nothing here" condition — an unknown consumer, an
	// unknown host, a model id that is not in the listing
	// (internal/gateway/consumer_config.go, HandleGetModel). A second spelling
	// of the same answer would make a client's handling of ours depend on
	// which endpoint produced it.
	wantRetired := http.StatusNotFound
	if redMode() {
		wantRetired = http.StatusServiceUnavailable
	}

	for _, id := range retiredLoopbackIdentifiers {
		t.Run("retired "+id, func(t *testing.T) {
			local := &recordingProvider{
				name:   "llamacpp",
				host:   "gpu-01",
				models: []string{"llama3:8b", "qwen2.5:7b"},
			}
			cloud := &recordingProvider{name: "chutes", models: []string{"deepseek-chat"}}
			stack := newServingStack(t, local, cloud)

			w := stack.chat(t, id)
			if w.Code != wantRetired {
				t.Errorf("POST /v1/chat/completions with the retired identifier %q returned %d, want %d (RED_MODE=%v): %s\n"+
					"This server knows it used to publish this host segment and knows it no longer does, "+
					"so the identifier is permanently dead. Answering 503 tells a correct client to retry "+
					"with backoff against something that will never come back.",
					id, w.Code, wantRetired, redMode(), w.Body.String())
			}
			// Whatever the status, the request must never be answered by
			// another model — that is e142fce's guarantee and this change
			// must not weaken it.
			if got := cloud.received(); len(got) != 0 {
				t.Errorf("the %q provider answered %v for %q", cloud.name, got, id)
			}
			if got := local.received(); len(got) != 0 {
				t.Errorf("the %q provider answered %v for %q", local.name, got, id)
			}

			if !redMode() {
				// The answer has to be actionable, not merely permanent: a
				// client holding one of these needs to be told the names
				// changed and where to get the new ones.
				body := w.Body.String()
				if !strings.Contains(body, "/v1/models") {
					t.Errorf("the response for %q does not name the listing to re-fetch from: %s\n"+
						"A permanent status with no migration instruction leaves the holder of a "+
						"retired identifier knowing only that it failed.", id, body)
				}
			}
		})
	}

	// An identifier carrying our prefix whose host segment is NOT one of the
	// retired renderings. This is the case the gateway genuinely cannot
	// classify — it could be a machine that has gone away for good, or one that
	// is rebooting — so it must keep 503 in BOTH polarities. Without this the
	// fix would be satisfied by making every unresolvable identifier permanent,
	// which would tell clients not to retry a host that is merely restarting.
	//
	// The SECOND of these is the substring hazard in the shape that actually
	// reaches the retired branch: an identifier resolves to nothing AND its
	// host segment merely BEGINS with a retired rendering. A resolvable
	// identifier never reaches this code (its provider is named, so a down
	// backend is reported as unavailable long before), so this is the only
	// route-level case a bare-substring match could get wrong.
	for _, unknown := range []string{
		"helixllm-gpu-07-qwen2-5-7b-ba85a3230a59",
		"helixllm-localhosting-qwen2-5-7b-ba85a3230a59",
	} {
		t.Run("unresolvable identifier on a machine-named host stays temporary: "+unknown, func(t *testing.T) {
			local := &recordingProvider{
				name:   "llamacpp",
				host:   "gpu-01",
				models: []string{"llama3:8b", "qwen2.5:7b"},
			}
			cloud := &recordingProvider{name: "chutes", models: []string{"deepseek-chat"}}
			stack := newServingStack(t, local, cloud)

			w := stack.chat(t, unknown)
			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("POST /v1/chat/completions with %q returned %d, want 503: %s\n"+
					"The host segment names a real machine this gateway simply cannot see right now. "+
					"Reporting that permanently would stop a client retrying a host that is rebooting.",
					unknown, w.Code, w.Body.String())
			}
		})
	}

	// A host that is genuinely, temporarily absent: the identifier RESOLVES —
	// it is one this deployment publishes — and its provider is registered but
	// reports itself down. Nothing about it is retired, and it must stay 503 in
	// both polarities.
	t.Run("resolvable identifier whose host is temporarily down stays temporary", func(t *testing.T) {
		local := &recordingProvider{
			name:   "llamacpp",
			host:   "gpu-01",
			models: []string{"llama3:8b", "qwen2.5:7b"},
		}
		cloud := &recordingProvider{name: "chutes", models: []string{"deepseek-chat"}}
		stack := newServingStack(t, local, cloud)

		identifier := stack.identifierFor(t, "qwen2.5:7b")
		local.down = true

		w := stack.chat(t, identifier)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("POST /v1/chat/completions with %q returned %d, want 503: %s\n"+
				"The model is published by this deployment and its host is merely down. "+
				"Anything permanent here tells the client to stop retrying a backend that is warming up.",
				identifier, w.Code, w.Body.String())
		}
	})

	// A machine whose own name merely BEGINS with one of the retired renderings
	// is the only way a live identifier could be swept up by the retired-host
	// check, so it gets both of its states.
	//
	// The DOWN state is the one that carries the risk. A check matching on a
	// bare substring rather than a whole segment would tell the holder of a
	// perfectly current identifier that their model is gone for good, when its
	// host is only restarting — the exact wrong answer, aimed at a live user.
	for _, tc := range []struct {
		name string
		down bool
		want int
	}{
		{name: "serving", down: false, want: http.StatusOK},
		{name: "temporarily down", down: true, want: http.StatusServiceUnavailable},
	} {
		t.Run("live identifier on a host named like a retired rendering, "+tc.name, func(t *testing.T) {
			local := &recordingProvider{
				// "localhosting" sanitises to a host segment that STARTS WITH
				// "localhost" but is not it.
				name:   "llamacpp",
				host:   "localhosting",
				models: []string{"llama3:8b"},
			}
			stack := newServingStack(t, local)

			identifier := stack.identifierFor(t, "llama3:8b")
			if !strings.HasPrefix(identifier, "helixllm-localhost") {
				t.Fatalf("identifier %q does not exercise the substring hazard this case exists for", identifier)
			}
			local.down = tc.down

			w := stack.chat(t, identifier)
			if w.Code != tc.want {
				t.Errorf("POST /v1/chat/completions with the live identifier %q (host %s) returned %d, want %d: %s\n"+
					"The host segment merely STARTS WITH a retired rendering; the identifier is one this "+
					"deployment publishes right now, so it must never be reported as retired.",
					identifier, tc.name, w.Code, tc.want, w.Body.String())
			}
			if !tc.down {
				if got := local.received(); len(got) != 1 || got[0] != "llama3:8b" {
					t.Errorf("the serving provider received %v, want exactly [%q]", got, "llama3:8b")
				}
			}
		})
	}

	// The retired identifiers this guard uses must be exactly the shape the
	// derivation produces, or the guard would be asserting on a string the
	// system never emitted. Deriving one here from the retired host value pins
	// the connection.
	t.Run("the retired shape is the one derivation actually produced", func(t *testing.T) {
		id, err := naming.NewIdentity("127.0.0.1", "qwen2.5", "7b")
		if err != nil {
			t.Fatalf("build the pre-rename identity: %v", err)
		}
		derived, err := naming.Derive(id, naming.ClaudeToolkit)
		if err != nil {
			t.Fatalf("derive the pre-rename identifier: %v", err)
		}
		if !strings.HasPrefix(derived, "helixllm-127-0-0-1-") {
			t.Fatalf("the pre-rename identity derived %q, which is not the shape this guard asserts on", derived)
		}
	})
}
