package brain_test

import (
	"context"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// EX-6, streaming half.
//
// Publishing a derived identifier is only safe because ResolveModelName maps it
// back to the name the serving provider answers to. Routing matches a request's
// model against provider model names, so WITHOUT that translation a client that
// listed an identifier and then asked for it misses every exact match and falls
// through to whichever provider the router reaches anyway. Not an error — a
// SILENT MISROUTE: a different model answers, confidently, and the caller has no
// way to tell.
//
// Complete is guarded by TestComplete_AcceptsPublishedIdentifierAndTheRawNameAlike.
// CompleteStream performs the SAME translation (brain.go), on its own line, and
// nothing asserted it: deleting that one line left every test green while every
// streaming request — which is how a chat client actually talks — misrouted.
// One guarded call site and one unguarded one is one guarded call site.
//
// The oracle is the CONTENT of the stream, not the absence of an error, because
// the defect produces a perfectly successful response from the wrong model.
func TestCompleteStream_AcceptsPublishedIdentifierAndTheRawNameAlike(t *testing.T) {
	served := &hostedProvider{
		mockProvider: mockProvider{
			name:      "llamacpp",
			available: true,
			models:    []string{"llama3:8b"},
			chunks: []types.StreamChunk{
				{Content: "served"},
				{Content: "", FinishReason: "stop"},
			},
		},
		host: "gpu-01",
	}
	// The provider a failed resolution would fall through to. Its chunks say so
	// in words, so a failure reads as what it is rather than as a diff.
	other := &mockProvider{
		name: "openai", available: true, models: []string{"gpt-4o"},
		chunks: []types.StreamChunk{
			{Content: "WRONG PROVIDER"},
			{Content: "", FinishReason: "stop"},
		},
	}
	b := newBrainWithMocks(map[string]brain.Provider{"llamacpp": served, "openai": other}, "openai")

	published := b.Models()
	if len(published) != 2 {
		t.Fatalf("Models() returned %d entries, want 2", len(published))
	}
	var identifier string
	for _, m := range published {
		if m.ID != "gpt-4o" {
			identifier = m.ID
		}
	}
	if identifier == "" {
		t.Fatal("no derived identifier was published for the locally-served model; " +
			"the rest of this test would prove nothing")
	}

	// Both vocabularies must reach the same provider: the identifier this
	// listing just handed out, and the raw provider name a pre-existing
	// configuration already holds.
	for _, name := range []string{identifier, "llama3:8b"} {
		ch, err := b.CompleteStream(context.Background(), &types.InternalChatRequest{
			Model:    name,
			Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
		})
		if err != nil {
			t.Fatalf("CompleteStream(%q) failed: %v", name, err)
		}
		var got strings.Builder
		for c := range ch {
			got.WriteString(c.Content)
		}
		if got.String() != "served" {
			t.Errorf("CompleteStream(%q) was answered by the wrong provider: streamed %q, want %q.\n"+
				"The published identifier was not resolved to the provider's own model name, so "+
				"routing missed every exact match and fell through — a silent misroute, not an error.",
				name, got.String(), "served")
		}
	}
}

// The migration guarantee has a second half worth pinning separately: a name
// that is neither a published identifier nor a served model must NOT be quietly
// rewritten into one. ResolveModelName returns unknown names unchanged, which is
// what keeps an existing configuration working; if it ever started guessing, a
// typo would become a successful call to something the caller did not ask for.
func TestResolveModelName_LeavesAnUnknownNameAlone(t *testing.T) {
	served := &hostedProvider{
		mockProvider: mockProvider{
			name: "llamacpp", available: true, models: []string{"llama3:8b"},
		},
		host: "gpu-01",
	}
	b := newBrainWithMocks(map[string]brain.Provider{"llamacpp": served}, "llamacpp")

	got, ok := b.ResolveModelName("a-model-nobody-published")
	if ok {
		t.Errorf("ResolveModelName resolved a name it has never published (to %q): "+
			"an unrecognised model must stay unrecognised, or a typo silently becomes "+
			"a successful call to a model the caller did not ask for", got)
	}
	if got != "a-model-nobody-published" {
		t.Errorf("ResolveModelName rewrote an unknown name to %q; unknown names must pass through "+
			"unchanged so pre-existing configurations keep working", got)
	}
}
