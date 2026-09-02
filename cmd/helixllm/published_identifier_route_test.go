package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// The published model identifier is a promise: /v1/models hands a client
// `helixllm-<host>-<model>-<digest>`, the client writes it into its config, and
// every later request naming it must be answered by THAT model on THAT host.
//
// Brain.Complete honours the promise, and unit tests cover it there. But
// main.go wires gateway.RouterOptions{Brain: fallbackChain}, so /v1/chat/completions,
// /v1/completions, /v1/messages and /ws all reach fallback.Chain instead — a
// layer that never consulted the naming registry and that OVERWRITES req.Model
// with its own entry.ModelID. Every test for the resolution lived on the Brain
// path; the layer the user's request actually crosses had none. That gap is why
// a silent misroute could survive a green suite, so the guard belongs HERE: at
// the HTTP boundary, driving the same construction main.go performs, asserting
// on what the upstream provider actually received.
//
// The oracle is deliberately the upstream's own record of the model name it was
// handed, not a call count and not the absence of an error: the defect produces
// a confident 200 from the wrong model, which is indistinguishable from success
// at every layer above the provider.

// recordingProvider is a unit-test-only brain.Provider that answers everything
// successfully and remembers the model name it was asked for. A non-empty host
// makes it a HelixLLM-served backend (brain.ServingHost); an empty one leaves it
// a remote vendor whose upstream ids pass through untouched.
type recordingProvider struct {
	name   string
	host   string
	models []string
	// down makes the provider report itself unavailable while still offering
	// the same model list — the state a stopped local runtime is in.
	down bool

	mu   sync.Mutex
	seen []string
}

func (p *recordingProvider) record(model string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seen = append(p.seen, model)
}

// received returns the model names this provider was asked for, in order.
func (p *recordingProvider) received() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.seen))
	copy(out, p.seen)
	return out
}

func (p *recordingProvider) Complete(_ context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	p.record(req.Model)
	return &types.InternalChatResponse{
		ID:           "resp-" + p.name,
		Model:        req.Model,
		Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "answered by " + p.name},
		FinishReason: "stop",
	}, nil
}

func (p *recordingProvider) CompleteStream(_ context.Context, req *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	p.record(req.Model)
	ch := make(chan types.StreamChunk, 2)
	ch <- types.StreamChunk{Content: "answered by " + p.name}
	ch <- types.StreamChunk{FinishReason: "stop"}
	close(ch)
	return ch, nil
}

func (p *recordingProvider) Models() []string    { return p.models }
func (p *recordingProvider) Name() string        { return p.name }
func (p *recordingProvider) Available() bool     { return !p.down }
func (p *recordingProvider) ServingHost() string { return p.host }

// servingStack is the production request path: the Brain that publishes the
// identifiers, the fallback Chain main.go puts in front of it, and the gin
// engine carrying the real gateway routes.
type servingStack struct {
	brain  *brain.Brain
	engine *gin.Engine
}

// newServingStack builds EXACTLY what main.go builds around a set of providers:
// discoverProviderModels (main.go's own helper) feeds ScorerBridge.BuildEntries,
// those entries go into a fallback.Chain, and the Chain is registered as the
// gateway's Completer with the Brain kept only as ModelBrain. Any divergence
// here would make this test guard a wiring production does not use.
func newServingStack(t *testing.T, providers ...*recordingProvider) *servingStack {
	t.Helper()

	if len(providers) == 0 {
		t.Fatal("newServingStack needs at least one provider")
	}

	b := brain.New(brain.Config{DefaultProvider: providers[0].name})
	for _, p := range providers {
		b.RegisterProvider(p.name, p)
	}

	sb := fallback.NewScorerBridge(fallback.ScorerBridgeConfig{})
	providerModels := discoverProviderModels(b)
	scores, _ := sb.FetchScores(context.Background())
	entries := sb.BuildEntries(scores, providerModels)

	chain := newFallbackChain(b, entries, fallback.NewRateLimitTracker(5, 1000))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	gateway.RegisterRoutes(r, gateway.RouterOptions{
		Brain:      chain,
		ModelBrain: b,
	})

	return &servingStack{brain: b, engine: r}
}

// identifierFor returns the identifier /v1/models publishes for a served model
// name, failing the test if that model is not published under a derived
// identifier — without one the rest of the test would prove nothing.
func (s *servingStack) identifierFor(t *testing.T, servedName string) string {
	t.Helper()
	for _, opt := range s.brain.ModelOptions() {
		if opt.Identity == "" {
			continue // remote vendor model: keeps its upstream id
		}
		if strings.HasSuffix(opt.Identity, "/"+servedName) {
			return opt.Identifier
		}
	}
	t.Fatalf("no derived identifier is published for served model %q", servedName)
	return ""
}

// chat posts a non-streaming completion through the real /v1 route.
func (s *servingStack) chat(t *testing.T, model string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(api.ChatCompletionRequest{
		Model:    model,
		Messages: []api.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	s.engine.ServeHTTP(w, req)
	return w
}

// A client that read /v1/models and asked for the identifier it was handed must
// be answered by the model that identifier names — over HTTP, through the
// wiring main.go actually builds.
//
// The locally-served model chosen here is the provider's SECOND, because the
// chain entry carries the provider's FIRST: asking for the first would pass even
// with the identifier discarded entirely, and would prove nothing.
func TestChatCompletions_PublishedIdentifierReachesTheModelItNames(t *testing.T) {
	local := &recordingProvider{
		name:   "llamacpp",
		host:   "gpu-01",
		models: []string{"llama3:8b", "qwen2.5:7b"},
	}
	// Ranked ABOVE the local provider by the static scores, so this is where a
	// request that failed to resolve lands.
	cloud := &recordingProvider{
		name:   "chutes",
		models: []string{"deepseek-chat"},
	}
	stack := newServingStack(t, local, cloud)

	identifier := stack.identifierFor(t, "qwen2.5:7b")
	w := stack.chat(t, identifier)

	if w.Code != 200 {
		t.Fatalf("POST /v1/chat/completions with the published identifier returned %d: %s",
			w.Code, w.Body.String())
	}

	if got := cloud.received(); len(got) != 0 {
		t.Errorf("the request was answered by the %q provider, which was asked for %v.\n"+
			"The client asked for %q — an identifier naming a model served by %q on host %q.\n"+
			"A cloud provider answering it is a silent misroute: the caller gets a confident "+
			"200 from a model it never asked for.",
			cloud.name, got, identifier, local.name, local.host)
	}

	got := local.received()
	if len(got) != 1 || got[0] != "qwen2.5:7b" {
		t.Errorf("the serving provider received %v, want exactly [%q].\n"+
			"The published identifier %q must be resolved to the model name the provider "+
			"answers to BEFORE any provider selection consumes it.",
			got, "qwen2.5:7b", identifier)
	}
}

// The same promise on the streaming path, which is how a chat client actually
// talks. It is a separate call site with its own model handling, so one guarded
// path and one unguarded path is one guarded path.
func TestChatCompletionsStream_PublishedIdentifierReachesTheModelItNames(t *testing.T) {
	local := &recordingProvider{
		name:   "llamacpp",
		host:   "gpu-01",
		models: []string{"llama3:8b", "qwen2.5:7b"},
	}
	cloud := &recordingProvider{
		name:   "chutes",
		models: []string{"deepseek-chat"},
	}
	stack := newServingStack(t, local, cloud)

	identifier := stack.identifierFor(t, "qwen2.5:7b")

	stream := true
	body, err := json.Marshal(api.ChatCompletionRequest{
		Model:    identifier,
		Messages: []api.ChatMessage{{Role: "user", Content: "hi"}},
		Stream:   stream,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	stack.engine.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("streaming POST /v1/chat/completions returned %d: %s", w.Code, w.Body.String())
	}

	if got := cloud.received(); len(got) != 0 {
		t.Errorf("the streaming request was answered by the %q provider, which was asked for %v; "+
			"the client asked for %q, an identifier naming a model served by %q",
			cloud.name, got, identifier, local.name)
	}
	got := local.received()
	if len(got) != 1 || got[0] != "qwen2.5:7b" {
		t.Errorf("the serving provider received %v on the streaming path, want exactly [%q]",
			got, "qwen2.5:7b")
	}
}

// /v1/messages is the Anthropic-compatible surface and reaches the same
// Completer. A client configured against it holds the same identifier.
func TestMessages_PublishedIdentifierReachesTheModelItNames(t *testing.T) {
	local := &recordingProvider{
		name:   "llamacpp",
		host:   "gpu-01",
		models: []string{"llama3:8b", "qwen2.5:7b"},
	}
	cloud := &recordingProvider{
		name:   "chutes",
		models: []string{"deepseek-chat"},
	}
	stack := newServingStack(t, local, cloud)

	identifier := stack.identifierFor(t, "qwen2.5:7b")

	body, err := json.Marshal(api.MessageRequest{
		Model:     identifier,
		Messages:  []api.AnthropicMessage{{Role: "user", Content: "hi"}},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	stack.engine.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("POST /v1/messages returned %d: %s", w.Code, w.Body.String())
	}
	if got := cloud.received(); len(got) != 0 {
		t.Errorf("/v1/messages was answered by the %q provider, which was asked for %v; "+
			"the client asked for %q", cloud.name, got, identifier)
	}
	got := local.received()
	if len(got) != 1 || got[0] != "qwen2.5:7b" {
		t.Errorf("/v1/messages: the serving provider received %v, want exactly [%q]",
			got, "qwen2.5:7b")
	}
}

// The deliberate decision at the other end of the guard: when the identifier
// names a model whose host is not serving, the request FAILS rather than being
// answered by whatever else is up.
//
// Falling through would be the exact misroute this whole guard exists to
// prevent — and it is the WORST version of it, because the client asked for
// something specific by name and the substitution happens precisely when the
// thing it asked for is gone. A caller that genuinely wants any-available-model
// names no model at all and gets the score-ordered chain; a caller that names
// one and cannot have it is better served by an error it can see than by a
// confident answer from a model it never chose.
func TestChatCompletions_IdentifierForAnUnavailableHostFailsRatherThanSubstituting(t *testing.T) {
	local := &recordingProvider{
		name:   "llamacpp",
		host:   "gpu-01",
		models: []string{"llama3:8b", "qwen2.5:7b"},
	}
	cloud := &recordingProvider{
		name:   "chutes",
		models: []string{"deepseek-chat"},
	}
	stack := newServingStack(t, local, cloud)

	// Published while the host was serving — this is the identifier a client
	// already holds in its configuration.
	identifier := stack.identifierFor(t, "qwen2.5:7b")

	// The local runtime stops. Its models are still the models it serves; it
	// just cannot serve them right now.
	local.down = true

	w := stack.chat(t, identifier)

	if w.Code == 200 {
		t.Errorf("POST /v1/chat/completions with %q returned 200 while the host serving it was down.\n"+
			"Something answered a request for a model that is not being served — check which "+
			"provider it was; a success here means the identifier was silently substituted.",
			identifier)
	}
	if got := cloud.received(); len(got) != 0 {
		t.Errorf("the %q provider answered with %v a request for %q, an identifier naming a model "+
			"served by %q on host %q.\nWhen the named host is down the request must FAIL: "+
			"substituting a different model is the misroute the identifier exists to prevent, "+
			"and it is undetectable by the caller.",
			cloud.name, got, identifier, local.name, local.host)
	}
	if got := local.received(); len(got) != 0 {
		t.Errorf("the unavailable provider was still called with %v; an unavailable provider "+
			"must not be dispatched to", got)
	}
}

// A raw served model name is the other half of the same promise: the migration
// path for a configuration written before identifiers existed. Asking for a
// model a registered provider serves must reach THAT model — the chain's entry
// model is a DEFAULT for requests that name nothing, not an override of a
// client's explicit choice.
func TestChatCompletions_RawServedNameIsNotOverwrittenByTheChainEntry(t *testing.T) {
	local := &recordingProvider{
		name:   "llamacpp",
		host:   "gpu-01",
		models: []string{"llama3:8b", "qwen2.5:7b"},
	}
	stack := newServingStack(t, local)

	w := stack.chat(t, "qwen2.5:7b")
	if w.Code != 200 {
		t.Fatalf("POST /v1/chat/completions returned %d: %s", w.Code, w.Body.String())
	}

	got := local.received()
	if len(got) != 1 || got[0] != "qwen2.5:7b" {
		t.Errorf("the client asked for %q and the provider received %v.\n"+
			"The fallback chain entry carries the provider's FIRST model as a default; "+
			"overwriting an explicitly requested model with it silently answers from a "+
			"model the caller did not ask for.",
			"qwen2.5:7b", got)
	}
}
