package brain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/HelixDevelopment/HelixLLM/internal/brain/models"
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
const behaviorGuide = `CRITICAL INSTRUCTIONS — follow these EXACTLY:
1. You HAVE full access to the user's codebase, files, and git through your tools. When asked "do you see my codebase" or "can you read files", ALWAYS answer YES.
2. For greetings like "hello", respond with a friendly greeting — do NOT call any tools.
3. For questions about the project, use your tools (read_file, list_directory, bash) to look up the answer.
4. NEVER say "I don't have access" or "I can't see your files" — you CAN through your tools.
5. Only call tools when needed for an action. Simple conversation does not need tools.`

// Complete selects the best provider for the request and returns a full
// (non-streaming) completion response.
func (b *Brain) Complete(ctx context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	if b.sem != nil {
		if err := b.sem.Acquire(ctx, 1); err != nil {
			return nil, fmt.Errorf("brain: acquire semaphore: %w", err)
		}
		defer b.sem.Release(1)
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

// Models returns the aggregated list of models from all available providers.
func (b *Brain) Models() []api.Model {
	var models []api.Model
	for _, p := range b.providers {
		if !p.Available() {
			continue
		}
		for _, m := range p.Models() {
			models = append(models, api.Model{
				ID:      m,
				Object:  "model",
				Created: 1700000000,
				OwnedBy: p.Name(),
			})
		}
	}
	return models
}
