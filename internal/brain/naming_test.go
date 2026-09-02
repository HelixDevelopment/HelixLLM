package brain_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/naming"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// The Claude Toolkit applies two INDEPENDENT checks to a value used as both an
// alias name and a provider id (claude_toolkit/scripts/lib.sh). They are
// reproduced here verbatim rather than referenced, because the point of the
// test is that a published identifier passes them AS THEY STAND: if a future
// change could only be made green by editing one of these patterns, that is the
// FR-014a violation the test exists to catch. The second is a shell-injection
// guard — the provider id is interpolated into an alias body that is re-parsed
// on invocation — so widening it trades naming convenience for a security hole.
var (
	toolkitAliasName  = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
	toolkitProviderID = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// hostedProvider is a provider that knows which machine serves its models, as a
// locally-served HelixLLM backend does.
type hostedProvider struct {
	mockProvider
	host   string
	reason string
}

func (h *hostedProvider) ServingHost() string { return h.host }

func (h *hostedProvider) UnavailableReason() string { return h.reason }

func optionByIdentity(t *testing.T, opts []brain.ModelOption, identity string) brain.ModelOption {
	t.Helper()
	for _, o := range opts {
		if o.Identity == identity {
			return o
		}
	}
	t.Fatalf("no option with identity %q; got %+v", identity, opts)
	return brain.ModelOption{}
}

// --- Test 1 (FR-014, SC-015) -----------------------------------------------
//
// The identity VALUE alone must say "HelixLLM-served" and name the serving
// host, with nothing else consulted.
func TestModelOptions_IdentityNamesHelixLLMAndHost(t *testing.T) {
	b := newBrainWithMocks(map[string]brain.Provider{
		"llamacpp": &hostedProvider{
			mockProvider: mockProvider{name: "llamacpp", available: true, models: []string{"llama3"}},
			host:         "gpu-01",
		},
	}, "llamacpp")

	opts := b.ModelOptions()
	if len(opts) != 1 {
		t.Fatalf("ModelOptions() returned %d options, want 1: %+v", len(opts), opts)
	}

	const want = "helixllm/gpu-01/llama3"
	if opts[0].Identity != want {
		t.Errorf("Identity = %q, want %q", opts[0].Identity, want)
	}

	// The three facts SC-015 requires a user to read off the name alone.
	id, err := naming.ParseIdentity(opts[0].Identity)
	if err != nil {
		t.Fatalf("published identity does not parse: %v", err)
	}
	if id.Host != "gpu-01" {
		t.Errorf("identity host = %q, want %q", id.Host, "gpu-01")
	}
	if id.Model != "llama3" {
		t.Errorf("identity model = %q, want %q", id.Model, "llama3")
	}
	if !strings.HasPrefix(opts[0].Identity, naming.IdentityPrefix+"/") {
		t.Errorf("identity %q does not open with the HelixLLM provenance prefix", opts[0].Identity)
	}
}

// A remote provider's model must NOT wear the helixllm identity — claiming it
// would destroy the very distinction FR-014 asks the identity to draw.
func TestModelOptions_RemoteProviderModelsAreNotClaimedAsHelixLLMServed(t *testing.T) {
	b := newBrainWithMocks(map[string]brain.Provider{
		"openai": &mockProvider{name: "openai", available: true, models: []string{"gpt-4o"}},
	}, "openai")

	opts := b.ModelOptions()
	if len(opts) != 1 {
		t.Fatalf("ModelOptions() returned %d options, want 1: %+v", len(opts), opts)
	}
	if opts[0].Identity != "" {
		t.Errorf("remote model carries HelixLLM identity %q; it is not HelixLLM-served", opts[0].Identity)
	}
	if opts[0].Identifier != "gpt-4o" {
		t.Errorf("remote model identifier = %q, want the unchanged upstream id %q", opts[0].Identifier, "gpt-4o")
	}
}

// --- Test 2 (FR-014a) -------------------------------------------------------
//
// The published identifier satisfies BOTH toolkit validators as they stand.
func TestModels_PublishedIdentifierSatisfiesBothToolkitValidators(t *testing.T) {
	// Every one of these model names is illegal in at least one of the two
	// charsets, so a raw pass-through cannot make this test pass.
	b := newBrainWithMocks(map[string]brain.Provider{
		"llamacpp": &hostedProvider{
			mockProvider: mockProvider{
				name:      "llamacpp",
				available: true,
				models: []string{
					"llama3:8b",
					"library/qwen2.5-coder:7b-instruct-q4_K_M",
					"3-leading-digit",
					"model with spaces",
					"$(touch /tmp/pwned)",
					"日本語モデル",
				},
			},
			host: "GPU-01.local",
		},
	}, "llamacpp")

	models := b.Models()
	if len(models) != 6 {
		t.Fatalf("Models() returned %d models, want 6", len(models))
	}
	for _, m := range models {
		if !toolkitAliasName.MatchString(m.ID) {
			t.Errorf("published id %q fails the toolkit alias-name check %s", m.ID, toolkitAliasName)
		}
		if !toolkitProviderID.MatchString(m.ID) {
			t.Errorf("published id %q fails the toolkit provider-id injection guard %s", m.ID, toolkitProviderID)
		}
		if strings.ContainsAny(m.ID, "/: ") {
			t.Errorf("published id %q contains a structural character no consumer accepts", m.ID)
		}
	}
}

// --- Test 3 (FR-014, collision) ---------------------------------------------
//
// Two models that differ ONLY in a character the target charset forbids must
// still receive different identifiers. A naive sanitiser merges them, which
// would silently collapse two models into one config entry — one of them simply
// vanishes from the user's tool.
func TestModels_ModelsDifferingOnlyInAForbiddenCharacterStayDistinct(t *testing.T) {
	b := newBrainWithMocks(map[string]brain.Provider{
		"llamacpp": &hostedProvider{
			mockProvider: mockProvider{
				name:      "llamacpp",
				available: true,
				models:    []string{"llama3:8b", "llama3-8b"},
			},
			host: "gpu-01",
		},
	}, "llamacpp")

	models := b.Models()
	if len(models) != 2 {
		t.Fatalf("Models() returned %d models, want 2 — two distinct models collapsed into one", len(models))
	}
	if models[0].ID == models[1].ID {
		t.Fatalf("both models published the same identifier %q; one of them is unreachable", models[0].ID)
	}

	// And the identities they stand for are themselves distinct.
	opts := b.ModelOptions()
	if len(opts) != 2 || opts[0].Identity == opts[1].Identity {
		t.Fatalf("identities are not distinct: %+v", opts)
	}

	// Show that the hazard is real rather than hypothetical: a purely lossy
	// mapping into the target charset — the obvious implementation — sends both
	// names to the SAME string. Only something carried over the full identity
	// can keep them apart, so this test fails the moment that is dropped.
	naive := func(s string) string {
		return strings.Map(func(r rune) rune {
			if toolkitProviderID.MatchString(string(r)) && r != '.' {
				return r
			}
			return '-'
		}, s)
	}
	if naive("llama3:8b") != naive("llama3-8b") {
		t.Fatal("test premise broken: the two names were expected to sanitise identically")
	}
}

// --- Test 4 (FR-019, contract invariant 5) ----------------------------------
//
// An unavailable option carries its withheld reason, and is never presented as
// available. A model shown as usable that is not being served is worse than one
// omitted: the user picks it and the request fails.
func TestModelOptions_UnavailableCarriesReasonAndIsNotListedAsAvailable(t *testing.T) {
	b := newBrainWithMocks(map[string]brain.Provider{
		"serving": &hostedProvider{
			mockProvider: mockProvider{name: "serving", available: true, models: []string{"llama3"}},
			host:         "gpu-01",
		},
		"stopped": &hostedProvider{
			mockProvider: mockProvider{name: "stopped", available: false, models: []string{"mistral"}},
			host:         "gpu-02",
			reason:       "vram-reclaimed",
		},
		"silent": &hostedProvider{
			mockProvider: mockProvider{name: "silent", available: false, models: []string{"phi3"}},
			host:         "gpu-03",
		},
	}, "serving")

	opts := b.ModelOptions()
	if len(opts) != 3 {
		t.Fatalf("ModelOptions() returned %d options, want all 3 including the unavailable ones", len(opts))
	}

	stopped := optionByIdentity(t, opts, "helixllm/gpu-02/mistral")
	if stopped.Available {
		t.Error("a stopped model is reported as available")
	}
	if stopped.Reason != "vram-reclaimed" {
		t.Errorf("withheld reason = %q, want the provider's own reason %q", stopped.Reason, "vram-reclaimed")
	}

	// A provider that reports no reason must still not be silent about it.
	silent := optionByIdentity(t, opts, "helixllm/gpu-03/phi3")
	if silent.Available {
		t.Error("an unavailable model is reported as available")
	}
	if silent.Reason == "" {
		t.Error("unavailable option carries no withheld reason at all")
	}

	serving := optionByIdentity(t, opts, "helixllm/gpu-01/llama3")
	if !serving.Available {
		t.Error("a served model is reported as unavailable")
	}
	if serving.Reason != "" {
		t.Errorf("available option carries a withheld reason %q", serving.Reason)
	}

	// The OpenAI-shaped listing must contain only what is actually served.
	models := b.Models()
	if len(models) != 1 {
		t.Fatalf("Models() returned %d entries, want only the 1 actually being served: %+v", len(models), models)
	}
	if models[0].ID != serving.Identifier {
		t.Errorf("Models()[0].ID = %q, want the served option's identifier %q", models[0].ID, serving.Identifier)
	}
}

// --- Test 5 (contract invariant 3, compatibility) ---------------------------
//
// The id <-> identity mapping is recorded, so the two cannot drift.
func TestModelOptions_IdentifierAndIdentityMappingIsRecorded(t *testing.T) {
	b := newBrainWithMocks(map[string]brain.Provider{
		"llamacpp": &hostedProvider{
			mockProvider: mockProvider{name: "llamacpp", available: true, models: []string{"llama3:8b"}},
			host:         "gpu-01",
		},
	}, "llamacpp")

	opts := b.ModelOptions()
	if len(opts) != 1 {
		t.Fatalf("ModelOptions() returned %d options, want 1", len(opts))
	}

	got, ok := b.Names().IdentityFor(naming.ClaudeToolkit, opts[0].Identifier)
	if !ok {
		t.Fatalf("identifier %q is not recorded against any identity", opts[0].Identifier)
	}
	if got.String() != opts[0].Identity {
		t.Errorf("recorded identity %q != published identity %q", got.String(), opts[0].Identity)
	}
}

// A published identifier must be usable as the request's model — otherwise
// listing it and then acting on it are two different vocabularies, and a
// request would silently land on whichever provider the router fell through to.
func TestComplete_AcceptsPublishedIdentifierAndTheRawNameAlike(t *testing.T) {
	served := &hostedProvider{
		mockProvider: mockProvider{
			name:      "llamacpp",
			available: true,
			models:    []string{"llama3:8b"},
			response: &types.InternalChatResponse{
				ID: "c1", Model: "llama3:8b",
				Message: types.InternalMessage{Role: types.RoleAssistant, Content: "served"},
			},
		},
		host: "gpu-01",
	}
	// A second, unrelated provider the router would fall through to if the
	// published identifier failed to resolve.
	other := &mockProvider{
		name: "openai", available: true, models: []string{"gpt-4o"},
		response: &types.InternalChatResponse{
			ID: "c2", Model: "gpt-4o",
			Message: types.InternalMessage{Role: types.RoleAssistant, Content: "WRONG PROVIDER"},
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
		t.Fatal("no derived identifier was published for the locally-served model")
	}

	// The migration path: BOTH the newly published identifier and the raw name
	// a pre-existing configuration already holds must reach the same provider.
	for _, name := range []string{identifier, "llama3:8b"} {
		resp, err := b.Complete(context.Background(), &types.InternalChatRequest{
			Model:    name,
			Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
		})
		if err != nil {
			t.Fatalf("Complete(%q) failed: %v", name, err)
		}
		if resp.Message.Content != "served" {
			t.Errorf("Complete(%q) reached the wrong provider: %q", name, resp.Message.Content)
		}
	}
}
