package brain

// CerebrasProvider is a thin wrapper around OpenAICompatProvider that targets
// Cerebras' inference API (https://api.cerebras.ai/v1).
type CerebrasProvider struct {
	*OpenAICompatProvider
}

// NewCerebrasProvider constructs a CerebrasProvider.
// baseURL defaults to "https://api.cerebras.ai/v1" when empty.
func NewCerebrasProvider(apiKey, baseURL string) *CerebrasProvider {
	if baseURL == "" {
		baseURL = "https://api.cerebras.ai/v1"
	}
	return &CerebrasProvider{
		OpenAICompatProvider: NewOpenAICompat(OpenAICompatConfig{
			Name:       "cerebras",
			BaseURL:    baseURL,
			APIKey:     apiKey,
			AuthHeader: "Authorization",
			AuthPrefix: "Bearer ",
		}),
	}
}
