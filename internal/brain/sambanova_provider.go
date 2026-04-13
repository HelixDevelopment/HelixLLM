package brain

// SambaNovaProvider is a thin wrapper around OpenAICompatProvider that targets
// SambaNova's inference API (https://api.sambanova.ai/v1).
type SambaNovaProvider struct {
	*OpenAICompatProvider
}

// NewSambaNovaProvider constructs a SambaNovaProvider.
// baseURL defaults to "https://api.sambanova.ai/v1" when empty.
func NewSambaNovaProvider(apiKey, baseURL string) *SambaNovaProvider {
	if baseURL == "" {
		baseURL = "https://api.sambanova.ai/v1"
	}
	return &SambaNovaProvider{
		OpenAICompatProvider: NewOpenAICompat(OpenAICompatConfig{
			Name:       "sambanova",
			BaseURL:    baseURL,
			APIKey:     apiKey,
			AuthHeader: "Authorization",
			AuthPrefix: "Bearer ",
		}),
	}
}
