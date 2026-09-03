package fallback_test

import (
	"context"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
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
// prefix — retired or not. Which of the two it was is decided from the NAME,
// which is the behaviour under test.
type pinnerReturningNoProvider struct{}

func (pinnerReturningNoProvider) PinModel(requested string) (string, string, bool) {
	return "", requested, true
}

func completeWithPin(t *testing.T, requested string) error {
	t.Helper()
	c := fallback.NewChain(map[string]brain.Provider{}, fallback.NewRateLimitTracker(1, 1))
	c.SetModelPinner(pinnerReturningNoProvider{})
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
