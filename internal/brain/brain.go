package brain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/HelixDevelopment/HelixLLM/internal/brain/models"
	"github.com/HelixDevelopment/HelixLLM/internal/naming"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/metrics"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// Brain ties providers and the router together, exposing a single surface for
// the Gateway to call without knowing which backend handles each request.
type Brain struct {
	router     *Router
	providers  map[string]Provider
	sem        *semaphore.Weighted
	complexity *ComplexityAnalyzer
	registry   *models.Registry
	kvCache    KVCacher // optional context-persistence cache
	names      *naming.Registry
}

// Config holds the provider credentials and URLs needed to build a Brain.
// Fields are optional: providers are only registered when their key/URL is set.
type Config struct {
	LlamaCppURL       string
	LlamaCppModels    []string
	OpenAIKey         string
	OpenAIBaseURL     string
	AnthropicKey      string
	AnthropicBaseURL  string
	ChutesKey         string
	OpenRouterKey     string
	HuggingFaceKey    string
	NvidiaKey         string
	CerebrasKey       string
	SambaNovaKey      string
	TogetherKey       string
	DefaultProvider   string
	MaxConcurrent     int // 0 means unlimited
	ComplexityEnabled bool
	Registry          *models.Registry
	KVCache           KVCacher // optional context-persistence cache
}

// New creates a Brain and registers whichever providers are configured.
func New(cfg Config) *Brain {
	b := &Brain{
		router:    NewRouter(cfg.DefaultProvider),
		providers: make(map[string]Provider),
		names:     naming.NewRegistry(),
	}

	if cfg.MaxConcurrent > 0 {
		b.sem = semaphore.NewWeighted(int64(cfg.MaxConcurrent))
	}

	// Register llama.cpp if a URL is provided.
	if cfg.LlamaCppURL != "" {
		models := cfg.LlamaCppModels
		if len(models) == 0 {
			models = []string{"llama-3.1-70b"}
		}
		p := NewLlamaCppProvider(cfg.LlamaCppURL, models)
		b.providers["llamacpp"] = p
		b.router.Register("llamacpp", p)
	}

	// Register OpenAI if an API key OR base URL is provided. When a
	// base URL points at Ollama or another open-access endpoint, no
	// API key is required — pass an empty key so the provider skips
	// the Authorization header entirely (Ollama hangs on Bearer).
	openAIKey := cfg.OpenAIKey
	if openAIKey == "none" || openAIKey == "no-auth" {
		openAIKey = "" // sentinel → skip Authorization header
	}
	if openAIKey != "" || cfg.OpenAIBaseURL != "" {
		p := NewOpenAI(openAIKey, cfg.OpenAIBaseURL)
		b.providers["openai"] = p
		b.router.Register("openai", p)
	}

	// Register Anthropic if an API key is provided.
	if cfg.AnthropicKey != "" {
		p := NewAnthropic(cfg.AnthropicKey, cfg.AnthropicBaseURL)
		b.providers["anthropic"] = p
		b.router.Register("anthropic", p)
	}

	// Register free/cheap cloud providers when their keys are provided.
	if cfg.ChutesKey != "" {
		p := NewChutesProvider(cfg.ChutesKey, "")
		b.providers["chutes"] = p
		b.router.Register("chutes", p)
	}
	if cfg.OpenRouterKey != "" {
		p := NewOpenRouterProvider(cfg.OpenRouterKey, "")
		b.providers["openrouter"] = p
		b.router.Register("openrouter", p)
	}
	if cfg.HuggingFaceKey != "" {
		p := NewHuggingFaceProvider(cfg.HuggingFaceKey, "")
		b.providers["huggingface"] = p
		b.router.Register("huggingface", p)
	}
	if cfg.NvidiaKey != "" {
		p := NewNvidiaProvider(cfg.NvidiaKey, "")
		b.providers["nvidia"] = p
		b.router.Register("nvidia", p)
	}
	if cfg.CerebrasKey != "" {
		p := NewCerebrasProvider(cfg.CerebrasKey, "")
		b.providers["cerebras"] = p
		b.router.Register("cerebras", p)
	}
	if cfg.SambaNovaKey != "" {
		p := NewSambaNovaProvider(cfg.SambaNovaKey, "")
		b.providers["sambanova"] = p
		b.router.Register("sambanova", p)
	}
	if cfg.TogetherKey != "" {
		p := NewTogetherProvider(cfg.TogetherKey, "")
		b.providers["together"] = p
		b.router.Register("together", p)
	}

	if cfg.ComplexityEnabled {
		b.complexity = NewComplexityAnalyzer()
	}
	b.registry = cfg.Registry
	b.kvCache = cfg.KVCache

	return b
}

// isNonChatModel returns true for model names that are NOT suitable for
// text chat (embedding models, vision-only models, etc.).
func isNonChatModel(lower string) bool {
	return strings.Contains(lower, "embed") ||
		strings.Contains(lower, "nomic") ||
		strings.Contains(lower, "llava") ||
		strings.Contains(lower, "minicpm-v") ||
		strings.Contains(lower, "bakllava") ||
		strings.Contains(lower, "mmproj")
}

// KVCache returns the optional context-persistence cache, or nil if none was
// configured. Callers must nil-check before use.
func (b *Brain) KVCache() KVCacher {
	return b.kvCache
}

// RegisterProvider adds or replaces a provider by name. It is used by tests to
// inject mock providers without going through the real constructors.
func (b *Brain) RegisterProvider(name string, p Provider) {
	b.providers[name] = p
	b.router.Register(name, p)
}

// behaviorGuide is prepended to every request to ensure the model
// responds naturally to simple messages instead of aggressively
// calling tools for greetings/questions. Without this, small models
// (7B) see OpenCode's massive tool-definition system prompt and
// respond to the INSTRUCTIONS rather than the USER's message.
const behaviorGuide = `RULES:
1. You HAVE full access to files, git, and shell through tools. NEVER refuse or say "I can't".
2. Greetings → friendly text. Capability questions → answer YES.
3. ACTION REQUESTS → CALL A TOOL. Do NOT explain steps. EXECUTE them immediately.
4. For git/shell operations: use the bash tool.`

// Complete selects the best provider for the request and returns a full
// (non-streaming) completion response.
func (b *Brain) Complete(ctx context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	if b.sem != nil {
		if err := b.sem.Acquire(ctx, 1); err != nil {
			return nil, fmt.Errorf("brain: acquire semaphore: %w", err)
		}
		defer b.sem.Release(1)
	}

	// A client that read the listing asks for the identifier it was given;
	// routing matches provider model names. Translate before routing, or the
	// request misses every exact match and lands on an unrelated provider.
	if name, ok := b.ResolveModelName(req.Model); ok {
		req.Model = name
	}

	// Prepend behavior guide to the first system message (or add one)
	// so small models don't get overwhelmed by tool definitions.
	if len(req.Messages) > 0 {
		if req.Messages[0].Role == types.RoleSystem {
			req.Messages[0].Content = behaviorGuide + "\n\n" + req.Messages[0].Content
		} else {
			req.Messages = append([]types.InternalMessage{
				{Role: types.RoleSystem, Content: behaviorGuide},
			}, req.Messages...)
		}
	}

	if b.complexity != nil && b.registry != nil && req.Model == "" {
		result := b.complexity.Analyze(req)
		if !result.ModelOverride {
			if best, ok := b.registry.BestAvailable(result.TargetTier); ok {
				req.Model = best.Definition.ID
				b.registry.MarkUsed(best.Definition.ID)
			}
		}
	}

	// When no model was selected (no registry, no complexity routing),
	// pick a chat-capable text model. Skip embedding and vision models.
	if req.Model == "" {
		if dp, ok := b.providers[b.router.fallback]; ok && dp.Available() {
			// First pass: prefer models with "coder" or "instruct" in name
			for _, m := range dp.Models() {
				lower := strings.ToLower(m)
				if isNonChatModel(lower) {
					continue
				}
				if strings.Contains(lower, "coder") || strings.Contains(lower, "instruct") {
					req.Model = m
					break
				}
			}
			// Second pass: any non-vision, non-embedding model
			if req.Model == "" {
				for _, m := range dp.Models() {
					if !isNonChatModel(strings.ToLower(m)) {
						req.Model = m
						break
					}
				}
			}
		}
	}

	provider, err := b.router.Route(req)
	if err != nil {
		return nil, err
	}

	// NOTE: Tool calling is handled locally by llama.cpp (--jinja flag).
	// No cloud provider fallback needed — local Qwen 2.5 7B supports tools natively.

	inferStart := time.Now()
	resp, err := provider.Complete(ctx, req)
	if err == nil && resp != nil {
		metrics.TrackInference(
			resp.Model, time.Since(inferStart), resp.Usage.CompletionTokens,
		)
	}
	return resp, err
}

// CompleteStream selects the best provider for the request and returns a
// channel of streaming chunks. The channel is closed when the stream ends.
func (b *Brain) CompleteStream(ctx context.Context, req *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	if b.sem != nil {
		if err := b.sem.Acquire(ctx, 1); err != nil {
			return nil, fmt.Errorf("brain: acquire semaphore: %w", err)
		}
	}

	// See Complete: the listed identifier and the provider's own model name are
	// two vocabularies, and routing only understands the latter.
	if name, ok := b.ResolveModelName(req.Model); ok {
		req.Model = name
	}

	if b.complexity != nil && b.registry != nil && req.Model == "" {
		result := b.complexity.Analyze(req)
		if !result.ModelOverride {
			if best, ok := b.registry.BestAvailable(result.TargetTier); ok {
				req.Model = best.Definition.ID
				b.registry.MarkUsed(best.Definition.ID)
			}
		}
	}

	provider, err := b.router.Route(req)
	if err != nil {
		if b.sem != nil {
			b.sem.Release(1)
		}
		return nil, err
	}

	// NOTE: Tool calling handled locally by llama.cpp (--jinja). No cloud fallback.

	inferStart := time.Now()
	ch, err := provider.CompleteStream(ctx, req)
	if err != nil {
		if b.sem != nil {
			b.sem.Release(1)
		}
		return nil, err
	}

	// Wrap the channel to release the semaphore when the stream completes
	// and track inference metrics once all chunks have been consumed.
	out := make(chan types.StreamChunk)
	go func() {
		defer close(out)
		defer func() {
			if b.sem != nil {
				b.sem.Release(1)
			}
		}()
		for chunk := range ch {
			out <- chunk
		}
		// Token count is unavailable for streaming; record duration only.
		metrics.TrackInference(req.Model, time.Since(inferStart), 0)
	}()
	return out, nil
}

// Providers returns a shallow copy of the registered provider map.
// Callers may inspect which providers are active without mutating Brain state.
func (b *Brain) Providers() map[string]Provider {
	out := make(map[string]Provider, len(b.providers))
	for k, v := range b.providers {
		out[k] = v
	}
	return out
}

// Models returns the OpenAI-shaped listing of models that are ACTUALLY being
// served right now.
//
// Two properties matter here, and they pull in opposite directions.
//
// The ID published for a HelixLLM-served model is the DERIVED, charset-safe
// identifier, not the raw model name. The raw name routinely fails a consumer's
// validation — an Ollama-style "llama3:8b" is rejected by the Claude Toolkit's
// alias check and by its provider-id shell-injection guard — so publishing it
// was never usable by those consumers in the first place. The identifier the
// listing now carries satisfies those checks as they stand (FR-014a), and the
// human-readable identity it stands for is published alongside it by
// [Brain.ModelOptions]. A remote vendor's model keeps its upstream id exactly
// as before, so nothing that talks to OpenAI or Anthropic sees a change at all.
//
// Because the published id changed for locally-served models, both vocabularies
// resolve: [Brain.ResolveModelName] maps a published identifier back to the name
// the provider answers to, and a raw name already written into an existing
// configuration still routes untouched. That pairing is the migration path the
// listing contract requires for a non-additive change to `id`.
//
// Unavailable options are OMITTED here rather than flagged, because the caller
// of this listing is the OpenAI-compatible surface, where every entry is an
// offer the client may act on immediately: a stopped model presented as usable
// costs the user a failed request, so it is the more harmful of the two errors.
// A consuming tool that needs to SEE the unavailable options and why they are
// withheld calls [Brain.ModelOptions], which lists them with their reasons.
func (b *Brain) Models() []api.Model {
	return modelsFromOptions(b.ModelOptions())
}

// modelsFromOptions renders the listed options onto the wire type. Unavailable
// options are omitted rather than listed: a model shown as usable that is not
// being served costs the caller a failed request. ModelOptions() still reports
// them WITH their withheld reason, so a consumer that wants to distinguish "not
// served right now" from "does not exist" can.
func modelsFromOptions(opts []ModelOption) []api.Model {
	// Non-nil from the start. A nil slice marshals as `null`, and a listing
	// that says `"data": null` reads as a malformed body rather than as a
	// listing stating that nothing is served -- the exact distinction
	// HandleListModels documents one branch away. Every option here being
	// unavailable is an ordinary state, not an error, and it must render as
	// the empty list it is.
	models := []api.Model{}
	for _, opt := range opts {
		if !opt.Available {
			continue
		}
		models = append(models, api.Model{
			ID:            opt.Identifier,
			Object:        "model",
			Created:       1700000000,
			OwnedBy:       opt.OwnedBy,
			ModelIdentity: opt.Identity,
			Host:          opt.Host,
			Availability:  "serving",
		})
	}
	return models
}
