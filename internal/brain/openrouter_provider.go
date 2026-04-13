package brain

import (
	"context"
	"strings"
)

// OpenRouterProvider is a thin wrapper around OpenAICompatProvider that targets
// OpenRouter's unified LLM gateway (https://openrouter.ai/api/v1).
//
// It adds DiscoverFreeModels which filters the full model catalog to only the
// models that carry the ":free" suffix — OpenRouter's convention for models
// available without per-token billing.
type OpenRouterProvider struct {
	*OpenAICompatProvider
}

// NewOpenRouterProvider constructs an OpenRouterProvider.
// baseURL defaults to "https://openrouter.ai/api/v1" when empty.
func NewOpenRouterProvider(apiKey, baseURL string) *OpenRouterProvider {
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	return &OpenRouterProvider{
		OpenAICompatProvider: NewOpenAICompat(OpenAICompatConfig{
			Name:       "openrouter",
			BaseURL:    baseURL,
			APIKey:     apiKey,
			AuthHeader: "Authorization",
			AuthPrefix: "Bearer ",
		}),
	}
}

// DiscoverFreeModels queries GET {baseURL}/models and retains only models whose
// ID ends with the ":free" suffix. The result is cached and accessible via
// Models().
func (p *OpenRouterProvider) DiscoverFreeModels(ctx context.Context) error {
	return p.FetchModels(ctx, func(id string) bool {
		return strings.HasSuffix(id, ":free")
	})
}
