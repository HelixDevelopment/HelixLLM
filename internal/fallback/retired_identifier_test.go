package fallback_test

import (
	"context"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
	"github.com/HelixDevelopment/HelixLLM/internal/naming"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// The chain has to answer two different questions about a name it cannot serve,
// and only one of them means "retry".
//
// The pin's third outcome — ok=true with an empty provider — is reached both by
// a name this deployment RETIRED and by one whose host is merely absent. They
// used to produce one error, so the HTTP boundary gave both the same
// retry-with-backoff answer, and the first of them can never succeed.
//
// The oracle here is the SENTINEL a caller can match on, not the message text:
// message-matching is exactly what the sentinels exist to replace.

// pinnerReturningNoProvider is the unit-test stand-in for the pin's third
// outcome. It reports "you named one of ours, and nothing serves it", which is
// what brain.Brain.PinModel returns for any unresolvable name carrying our
// prefix — retired or not.
//
// Which of the two it was is decided by the REGISTRY, not by the name: a live
// machine can be called `localhost.lan` and publish identifiers opening with
// the same segment a retired one does. So the stub carries a real
// naming.Registry and delegates the decision to it, exactly as brain.Brain
// does. Re-implementing the rule here would let the test agree with a
// production answer that had drifted.
type pinnerReturningNoProvider struct {
	names *naming.Registry
}

func (pinnerReturningNoProvider) PinModel(requested string) (string, string, bool) {
	return "", requested, true
}

func (p pinnerReturningNoProvider) IsRetiredIdentifier(requested string) bool {
	return p.names.IsRetiredIdentifier(naming.ClaudeToolkit, requested)
}

// pinnerWithLiveHosts returns the stub with an identity registered for each
// given host, which is what a deployment serving those hosts has in its
// registry by the time a request arrives.
func pinnerWithLiveHosts(t *testing.T, hosts ...string) pinnerReturningNoProvider {
	t.Helper()
	r := naming.NewRegistry()
	for _, h := range hosts {
		id, err := naming.NewIdentity(h, "llama3", "8b")
		if err != nil {
			t.Fatalf("build an identity on host %q: %v", h, err)
		}
		if _, err := r.Register(id, naming.ClaudeToolkit); err != nil {
			t.Fatalf("register an identity on host %q: %v", h, err)
		}
	}
	return pinnerReturningNoProvider{names: r}
}

func completeWithPin(t *testing.T, requested string, liveHosts ...string) error {
	t.Helper()
	c := fallback.NewChain(map[string]brain.Provider{}, fallback.NewRateLimitTracker(1, 1))
	c.SetModelPinner(pinnerWithLiveHosts(t, liveHosts...))
	_, err := c.Complete(context.Background(), &types.InternalChatRequest{
		Model:    requested,
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatalf("Complete(%q) returned no error; a name nothing serves must never be answered", requested)
	}
	return err
}

func TestChain_RetiredIdentifierIsPermanentNotUnservable(t *testing.T) {
	err := completeWithPin(t, "helixllm-127-0-0-1-qwen2-5-7b-ba85a3230a59")

	if !fallback.IsRetiredIdentifier(err) {
		t.Errorf("IsRetiredIdentifier(%v) = false; the host segment is one this deployment "+
			"has permanently stopped publishing, and a caller must be able to tell that "+
			"from a host that is temporarily down without reading the message", err)
	}
	if fallback.IsUnservable(err) {
		t.Errorf("IsUnservable(%v) = true; a retired identifier is not an availability "+
			"condition, and reporting it as one is what sends the client back to retry "+
			"a name that can never resolve", err)
	}
	// The remedy has to survive into the error, because the caller cannot
	// derive the replacement identifier — only the listing can hand it over.
	if !strings.Contains(err.Error(), "/v1/models") {
		t.Errorf("the retired-identifier error does not name the listing to re-fetch from: %v", err)
	}
}

func TestChain_UnresolvableIdentifierOnALiveHostStaysUnservable(t *testing.T) {
	// Our prefix, a host segment that is NOT one of the retired renderings.
	// The gateway genuinely cannot tell a machine that has gone for good from
	// one that is rebooting, so this must keep the retryable answer.
	err := completeWithPin(t, "helixllm-gpu-07-qwen2-5-7b-ba85a3230a59")

	if fallback.IsRetiredIdentifier(err) {
		t.Errorf("IsRetiredIdentifier(%v) = true for a host segment that names a real machine; "+
			"classifying an unknown host as permanently gone would stop a client retrying "+
			"a backend that is merely restarting", err)
	}
	if !fallback.IsUnservable(err) {
		t.Errorf("IsUnservable(%v) = false; nothing serves this name right now, which is an "+
			"availability condition", err)
	}
}

// A raw model name nothing serves also reaches the third outcome when a pinner
// claims it — and it must stay retryable, because a raw name carries no
// provenance and so cannot be a retired identifier of ours.
func TestChain_RawNameNothingServesStaysUnservable(t *testing.T) {
	err := completeWithPin(t, "llama3:8b")

	if fallback.IsRetiredIdentifier(err) {
		t.Errorf("IsRetiredIdentifier(%v) = true for a raw model name", err)
	}
	if !fallback.IsUnservable(err) {
		t.Errorf("IsUnservable(%v) = false", err)
	}
}

// The SAME identifier is not retired when the deployment is currently
// publishing under that host segment.
//
// "Retired" was decided from the name alone, on the stated grounds that no
// live identity could carry a retired rendering. Two things made that false: a
// machine that cannot name itself used to publish the loopback literal
// verbatim, and a machine genuinely called `localhost.lan` renders into a host
// segment that BEGINS with one. On either, a model merely missing from the
// live list was reported permanently gone — with a message blaming a rename
// that had not happened.
func TestChain_RetiredRenderingOnALiveHostStaysUnservable(t *testing.T) {
	const identifier = "helixllm-127-0-0-1-qwen2-5-7b-ba85a3230a59"

	// Same name, same chain, one difference: this deployment holds identities
	// on 127.0.0.1, so it is publishing under that segment right now.
	err := completeWithPin(t, identifier, "127.0.0.1")

	if fallback.IsRetiredIdentifier(err) {
		t.Errorf("IsRetiredIdentifier(%v) = true while this deployment is publishing "+
			"identifiers on that very host; the model is merely not in its list, and "+
			"telling the client the host was renamed is both permanent and untrue", err)
	}
	if !fallback.IsUnservable(err) {
		t.Errorf("IsUnservable(%v) = false; nothing serves this name right now, which "+
			"is an availability condition", err)
	}
}

// A machine whose name merely RENDERS like a retired one keeps both answers
// available: its own current identifiers are live, and a genuinely pre-rename
// one on the same box is still retired.
func TestChain_LiveHostNamedLikeARetiredRenderingKeepsBothAnswers(t *testing.T) {
	// `localhost.lan` sanitises to `localhost-lan`, so its identifiers open
	// with `helixllm-localhost-` — the retired prefix.
	live, err := naming.NewIdentity("localhost.lan", "qwen2.5", "7b")
	if err != nil {
		t.Fatalf("build the live identity: %v", err)
	}
	liveIdentifier, err := naming.Derive(live, naming.ClaudeToolkit)
	if err != nil {
		t.Fatalf("derive the live identifier: %v", err)
	}
	if !strings.HasPrefix(liveIdentifier, "helixllm-localhost-") {
		t.Fatalf("identifier %q does not exercise the rendering collision this test exists for",
			liveIdentifier)
	}

	if e := completeWithPin(t, liveIdentifier, "localhost.lan"); fallback.IsRetiredIdentifier(e) {
		t.Errorf("IsRetiredIdentifier(%v) = true for an identifier shaped exactly like the "+
			"ones this deployment publishes on its own live host", e)
	}

	// The round-2 answer must survive: a pre-rename identifier on the same box
	// is still permanently gone, and re-fetching is still the remedy.
	if e := completeWithPin(t, "helixllm-localhost-llama3-8b-0123456789ab", "localhost.lan"); !fallback.IsRetiredIdentifier(e) {
		t.Errorf("IsRetiredIdentifier(%v) = false; this host segment is a pre-rename "+
			"rendering that no live host publishes under, so it is gone for good and the "+
			"client needs to be told to re-fetch rather than retry", e)
	}
}
