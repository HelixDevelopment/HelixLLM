package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/internal/naming"
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
// It takes brain.Provider rather than *recordingProvider so a test can drive
// the stack with a REAL provider. That matters: recordingProvider.Available()
// is a bool, so it cannot observe whether a code path performs the network
// probe a real provider's availability check performs (F3).
func newServingStack(t *testing.T, providers ...brain.Provider) *servingStack {
	t.Helper()

	if len(providers) == 0 {
		t.Fatal("newServingStack needs at least one provider")
	}

	b := brain.New(brain.Config{DefaultProvider: providers[0].Name()})
	for _, p := range providers {
		b.RegisterProvider(p.Name(), p)
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

// A client holding an identifier this deployment no longer publishes must be
// told so, not answered by something else.
//
// Identifiers are re-minted whenever the host segment of the identity changes,
// and one such change has already shipped: the serving host moved from the
// loopback literal to the machine name. Every identifier published before that
// change is now stale — the population is not hypothetical, it is one the
// codebase created. A stale identifier resolves to no identity, is served by no
// provider, and so used to leave PinModel reporting "nothing pinned", which is
// the signal the chain reads as "this caller named no model" and answers from
// its own top-ranked entry.
//
// That is precisely the misroute the identifier exists to prevent, aimed at the
// one population most likely to hit it: a caller whose configuration was
// written from a real /v1/models listing. A string carrying our own provenance
// prefix is unambiguously a request for one specific thing, so "the caller
// wants any available model" is not an available reading of it.
func TestChatCompletions_StaleIdentifierFailsRatherThanFallingThrough(t *testing.T) {
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

	// An identifier of exactly the shape published before the serving host
	// moved off the loopback literal: our prefix, a host segment, a model
	// segment, a digest. Nothing registered stands for it any more.
	const stale = "helixllm-127-0-0-1-qwen2-5-7b-ba85a3230a59"

	w := stack.chat(t, stale)

	if w.Code == 200 {
		t.Errorf("POST /v1/chat/completions with the stale identifier %q returned 200: %s\n"+
			"An identifier this deployment does not publish was answered by SOMETHING. "+
			"A caller holding a re-minted identifier gets a confident reply from a model "+
			"it never named and no way to detect the substitution.", stale, w.Body.String())
	}
	if got := cloud.received(); len(got) != 0 {
		t.Errorf("the %q provider answered %v for the stale identifier %q; "+
			"a request naming one specific model must fail rather than reach another one",
			cloud.name, got, stale)
	}
	if got := local.received(); len(got) != 0 {
		t.Errorf("the %q provider received %v for the stale identifier %q; "+
			"the identifier resolves to nothing, so nothing may be dispatched for it",
			local.name, got, stale)
	}
}

// countingLlamaCpp is a real llama.cpp endpoint: it counts /health probes and
// answers completions. It exists so a test can observe the NETWORK COST of a
// request, which a bool-valued stub provider cannot show.
type countingLlamaCpp struct {
	server *httptest.Server

	probes atomic.Int64
	// hold, when non-nil, blocks every /health probe until it is closed —
	// the state a wedged local runtime is in.
	hold chan struct{}
}

func newCountingLlamaCpp(t *testing.T) *countingLlamaCpp {
	t.Helper()
	c := &countingLlamaCpp{}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		c.probes.Add(1)
		if c.hold != nil {
			select {
			case <-c.hold:
			case <-r.Context().Done():
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req api.ChatCompletionRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(api.ChatCompletionResponse{
			ID:     "resp-llamacpp",
			Model:  req.Model,
			Object: "chat.completion",
			Choices: []api.ChatCompletionChoice{{
				Message:      api.ChatMessage{Role: "assistant", Content: "answered by llamacpp"},
				FinishReason: "stop",
			}},
		})
	})
	c.server = httptest.NewServer(mux)
	t.Cleanup(c.server.Close)
	return c
}

// A request for a CLOUD model must not touch the local backend at all.
//
// Resolving a model name used to fall back to re-deriving the whole option
// list on a registry miss, and deriving that list asks EVERY provider whether
// it is available — which for llama.cpp is an HTTP GET /health with a
// two-second timeout. So every request that named anything not already in the
// registry — every cloud-routed request included — paid for a probe of a
// backend it was never going to use, and a wedged local runtime added its full
// timeout to requests that had nothing to do with it.
//
// The oracle is the probe count taken from the real endpoint, because the
// defect is invisible in the response: the request succeeds either way.
func TestChatCompletions_CloudRequestDoesNotProbeTheLocalBackend(t *testing.T) {
	endpoint := newCountingLlamaCpp(t)
	local := brain.NewLlamaCppProvider(endpoint.server.URL, []string{"llama3:8b", "qwen2.5:7b"})
	cloud := &recordingProvider{name: "chutes", models: []string{"deepseek-chat"}}

	stack := newServingStack(t, local, cloud)

	// Startup wiring is allowed to probe; this test is about the REQUEST path.
	endpoint.probes.Store(0)

	w := stack.chat(t, "deepseek-chat")
	if w.Code != 200 {
		t.Fatalf("POST /v1/chat/completions for a cloud model returned %d: %s", w.Code, w.Body.String())
	}
	if got := endpoint.probes.Load(); got != 0 {
		t.Errorf("a request for the cloud model %q probed the local llama.cpp /health endpoint %d time(s), want 0.\n"+
			"Resolving a model name must not perform network I/O against providers the request "+
			"does not name: a slow or wedged local backend then delays every unrelated request.",
			"deepseek-chat", got)
	}
}

// The same defect, measured as the latency a user actually feels: a wedged
// local backend must not delay a cloud request.
func TestChatCompletions_WedgedLocalBackendDoesNotDelayACloudRequest(t *testing.T) {
	endpoint := newCountingLlamaCpp(t)
	endpoint.hold = make(chan struct{})
	t.Cleanup(func() { close(endpoint.hold) })

	local := brain.NewLlamaCppProvider(endpoint.server.URL, []string{"llama3:8b", "qwen2.5:7b"})
	cloud := &recordingProvider{name: "chutes", models: []string{"deepseek-chat"}}
	stack := newServingStack(t, local, cloud)
	endpoint.probes.Store(0)

	start := time.Now()
	w := stack.chat(t, "deepseek-chat")
	elapsed := time.Since(start)

	if w.Code != 200 {
		t.Fatalf("POST /v1/chat/completions for a cloud model returned %d: %s", w.Code, w.Body.String())
	}
	// The provider's own health probe gives up after two seconds, so a request
	// that waits on it lands at ~2s while one that never probes lands in
	// microseconds. One second separates them with room to spare.
	if elapsed > time.Second {
		t.Errorf("a cloud request took %v while the local llama.cpp /health endpoint was wedged.\n"+
			"It waited on a backend it never uses; the local health probe's own timeout is "+
			"being charged to every unrelated request.", elapsed)
	}
}

// A model name two providers serve must reach one that is actually serving it.
//
// With no host named there is nothing to disambiguate on, so provider selection
// falls back to sorted order for determinism. Taking the first serving provider
// WITHOUT consulting availability turns that tie-break into a failure: the same
// model is up on another provider and is never tried, and the caller is told the
// model cannot be served when it can.
//
// This is a different case from substitution. Refusing to answer with a
// DIFFERENT model is the contract; refusing to answer with the SAME model from
// a provider that has it is just a lost request.
func TestChatCompletions_RawNameServedByTwoProvidersReachesTheAvailableOne(t *testing.T) {
	down := &recordingProvider{
		name:   "aaa-ollama",
		host:   "gpu-01",
		models: []string{"llama3:8b"},
		down:   true,
	}
	up := &recordingProvider{
		name:   "zzz-llamacpp",
		host:   "gpu-02",
		models: []string{"llama3:8b"},
	}
	stack := newServingStack(t, down, up)

	w := stack.chat(t, "llama3:8b")

	if w.Code != 200 {
		t.Fatalf("POST /v1/chat/completions for %q returned %d: %s\n"+
			"Two providers serve that model and %q is available. Sorted order is a tie-break "+
			"for determinism, not a reason to fail a request the deployment can serve.",
			"llama3:8b", w.Code, w.Body.String(), up.name)
	}
	if got := up.received(); len(got) != 1 || got[0] != "llama3:8b" {
		t.Errorf("the available provider %q received %v, want exactly [%q]", up.name, got, "llama3:8b")
	}
	if got := down.received(); len(got) != 0 {
		t.Errorf("the unavailable provider %q was dispatched to with %v", down.name, got)
	}
}

// A named model that cannot be served right now is an AVAILABILITY condition,
// and must be reported as one.
//
// The chain's own exhausted-providers failure already answers 503, which is
// what tells a client, a load balancer and a readiness probe to retry with
// backoff. The pinned path reaches the identical situation — nothing can serve
// this request now — so answering 500 there tells all three that the build is
// broken and that retrying is pointless.
//
// SCOPE, narrowed once (§11.4.120). This case originally also covered
// `helixllm-127-0-0-1-…`, an identifier carrying a RETIRED loopback host
// segment. That one is no longer an availability condition: the retired
// renderings are an exactly-known set this deployment has permanently stopped
// publishing, so it now answers 404 with a re-fetch instruction — see
// retired_identifier_route_test.go, which owns that case and asserts the same
// no-substitution guarantee. The case is replaced here rather than dropped, by
// an identifier carrying a host segment that is NOT retired: that is the
// unresolvable name the gateway genuinely cannot classify, and it is the reason
// 503 must remain the answer for everything outside the bounded set.
func TestChatCompletions_PinnedButUnservableIsServiceUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model func(*servingStack) string
		down  bool
	}{
		{
			name:  "identifier whose host stopped serving",
			model: func(s *servingStack) string { return s.identifierFor(t, "qwen2.5:7b") },
			down:  true,
		},
		{
			name:  "identifier for a machine-named host this deployment cannot see",
			model: func(*servingStack) string { return "helixllm-gpu-07-qwen2-5-7b-ba85a3230a59" },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			local := &recordingProvider{
				name:   "llamacpp",
				host:   "gpu-01",
				models: []string{"llama3:8b", "qwen2.5:7b"},
			}
			cloud := &recordingProvider{name: "chutes", models: []string{"deepseek-chat"}}
			stack := newServingStack(t, local, cloud)

			model := tc.model(stack)
			local.down = tc.down

			w := stack.chat(t, model)
			if w.Code != 503 {
				t.Errorf("POST /v1/chat/completions with %q returned %d, want 503: %s\n"+
					"Nothing can serve the model the caller named. 503 says \"retry with backoff\"; "+
					"500 says \"this build is broken\" and stops clients, load balancers and "+
					"readiness probes from doing the right thing.", model, w.Code, w.Body.String())
			}
		})
	}
}

// A client's FIRST request may name an identifier it read from a PREVIOUS run,
// so resolution must work with no listing in this process.
//
// Resolution is registry-only and does no I/O, which means an identifier is
// resolvable only once something has registered it. Listing models registers as
// a side effect — so every test that fetches its identifier from
// Brain.ModelOptions() populates the registry itself and would pass even if the
// startup registration were removed entirely. This test therefore derives the
// expected identifier INDEPENDENTLY, from the identity, without touching the
// Brain: the only thing that can have filled the registry is the serving stack's
// own construction.
//
// Without that registration the request would not merely misroute — it would be
// REFUSED, because a name carrying our prefix that resolves to nothing is now an
// explicit error. Registering is what keeps that strictness from turning a valid
// identifier into a 503.
func TestChatCompletions_IdentifierResolvesWithoutAnyPriorModelListing(t *testing.T) {
	local := &recordingProvider{
		name:   "llamacpp",
		host:   "gpu-01",
		models: []string{"llama3:8b", "qwen2.5:7b"},
	}
	cloud := &recordingProvider{name: "chutes", models: []string{"deepseek-chat"}}
	stack := newServingStack(t, local, cloud)

	id, err := naming.NewIdentity(local.host, "qwen2.5", "7b")
	if err != nil {
		t.Fatalf("build identity: %v", err)
	}
	identifier, err := naming.Derive(id, naming.ClaudeToolkit)
	if err != nil {
		t.Fatalf("derive identifier: %v", err)
	}

	w := stack.chat(t, identifier)
	if w.Code != 200 {
		t.Fatalf("POST /v1/chat/completions with %q returned %d before any /v1/models listing: %s\n"+
			"The identifier is one this deployment publishes, so it must resolve. Nothing had "+
			"listed models in this process, which means the naming registry was never populated "+
			"at startup — and an unregistered identifier is now refused rather than misrouted.",
			identifier, w.Code, w.Body.String())
	}
	if got := local.received(); len(got) != 1 || got[0] != "qwen2.5:7b" {
		t.Errorf("the serving provider received %v, want exactly [%q]", got, "qwen2.5:7b")
	}
	if got := cloud.received(); len(got) != 0 {
		t.Errorf("the %q provider answered %v", cloud.name, got)
	}
}
