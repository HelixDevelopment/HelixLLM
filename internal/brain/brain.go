package brain

import (
	"context"
	"fmt"

	"golang.org/x/sync/semaphore"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// Brain ties providers and the router together, exposing a single surface for
// the Gateway to call without knowing which backend handles each request.
type Brain struct {
	router    *Router
	providers map[string]Provider
	sem       *semaphore.Weighted
}

// Config holds the provider credentials and URLs needed to build a Brain.
// Fields are optional: providers are only registered when their key/URL is set.
type Config struct {
	LlamaCppURL      string
	LlamaCppModels   []string
	OpenAIKey        string
	OpenAIBaseURL    string
	AnthropicKey     string
	AnthropicBaseURL string
	DefaultProvider  string
	MaxConcurrent    int // 0 means unlimited
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

	// Register OpenAI if an API key is provided.
	if cfg.OpenAIKey != "" {
		p := NewOpenAI(cfg.OpenAIKey, cfg.OpenAIBaseURL)
		b.providers["openai"] = p
		b.router.Register("openai", p)
	}

	// Register Anthropic if an API key is provided.
	if cfg.AnthropicKey != "" {
		p := NewAnthropic(cfg.AnthropicKey, cfg.AnthropicBaseURL)
		b.providers["anthropic"] = p
		b.router.Register("anthropic", p)
	}

	return b
}

// RegisterProvider adds or replaces a provider by name. It is used by tests to
// inject mock providers without going through the real constructors.
func (b *Brain) RegisterProvider(name string, p Provider) {
	b.providers[name] = p
	b.router.Register(name, p)
}

// Complete selects the best provider for the request and returns a full
// (non-streaming) completion response.
func (b *Brain) Complete(ctx context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	if b.sem != nil {
		if err := b.sem.Acquire(ctx, 1); err != nil {
			return nil, fmt.Errorf("brain: acquire semaphore: %w", err)
		}
		defer b.sem.Release(1)
	}

	provider, err := b.router.Route(req)
	if err != nil {
		return nil, err
	}
	return provider.Complete(ctx, req)
}

// CompleteStream selects the best provider for the request and returns a
// channel of streaming chunks. The channel is closed when the stream ends.
func (b *Brain) CompleteStream(ctx context.Context, req *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	if b.sem != nil {
		if err := b.sem.Acquire(ctx, 1); err != nil {
			return nil, fmt.Errorf("brain: acquire semaphore: %w", err)
		}
	}

	provider, err := b.router.Route(req)
	if err != nil {
		if b.sem != nil {
			b.sem.Release(1)
		}
		return nil, err
	}

	ch, err := provider.CompleteStream(ctx, req)
	if err != nil {
		if b.sem != nil {
			b.sem.Release(1)
		}
		return nil, err
	}

	// Wrap the channel to release the semaphore when the stream completes.
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
