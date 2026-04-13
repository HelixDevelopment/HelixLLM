package brain

// ChutesProvider is a thin wrapper around OpenAICompatProvider that targets
// Chutes' hosted inference API (https://llm.chutes.ai/v1).
//
// Usage:
//
//	p := brain.NewChutesProvider(os.Getenv("CHUTES_API_KEY"), "")
//	resp, err := p.Complete(ctx, req)
type ChutesProvider struct {
	*OpenAICompatProvider
}

// NewChutesProvider constructs a ChutesProvider.
// baseURL defaults to "https://llm.chutes.ai/v1" when empty.
func NewChutesProvider(apiKey, baseURL string) *ChutesProvider {
	if baseURL == "" {
		baseURL = "https://llm.chutes.ai/v1"
	}
	return &ChutesProvider{
		OpenAICompatProvider: NewOpenAICompat(OpenAICompatConfig{
			Name:       "chutes",
			BaseURL:    baseURL,
			APIKey:     apiKey,
			AuthHeader: "Authorization",
			AuthPrefix: "Bearer ",
		}),
	}
}
