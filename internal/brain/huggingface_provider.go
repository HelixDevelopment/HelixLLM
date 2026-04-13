package brain

import "context"

// HuggingFaceProvider is a thin wrapper around OpenAICompatProvider that
// targets the Hugging Face Inference Router (https://router.huggingface.co/v1).
//
// The HuggingFace router only surfaces models that are available for free
// serverless inference, so DiscoverFreeModels passes all discovered models
// through without additional filtering.
type HuggingFaceProvider struct {
	*OpenAICompatProvider
}

// NewHuggingFaceProvider constructs a HuggingFaceProvider.
// baseURL defaults to "https://router.huggingface.co/v1" when empty.
func NewHuggingFaceProvider(apiKey, baseURL string) *HuggingFaceProvider {
	if baseURL == "" {
		baseURL = "https://router.huggingface.co/v1"
	}
	return &HuggingFaceProvider{
		OpenAICompatProvider: NewOpenAICompat(OpenAICompatConfig{
			Name:       "huggingface",
			BaseURL:    baseURL,
			APIKey:     apiKey,
			AuthHeader: "Authorization",
			AuthPrefix: "Bearer ",
		}),
	}
}

// DiscoverFreeModels queries GET {baseURL}/models and caches all returned
// model IDs. The HuggingFace router only exposes free inference models, so no
// additional filtering is required.
func (p *HuggingFaceProvider) DiscoverFreeModels(ctx context.Context) error {
	return p.FetchModels(ctx, nil)
}
