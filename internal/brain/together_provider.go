package brain

// TogetherProvider is a thin wrapper around OpenAICompatProvider that targets
// Together AI's inference API (https://api.together.xyz/v1).
type TogetherProvider struct {
	*OpenAICompatProvider
}

// NewTogetherProvider constructs a TogetherProvider.
// baseURL defaults to "https://api.together.xyz/v1" when empty.
func NewTogetherProvider(apiKey, baseURL string) *TogetherProvider {
	if baseURL == "" {
		baseURL = "https://api.together.xyz/v1"
	}
	return &TogetherProvider{
		OpenAICompatProvider: NewOpenAICompat(OpenAICompatConfig{
			Name:       "together",
			BaseURL:    baseURL,
			APIKey:     apiKey,
			AuthHeader: "Authorization",
			AuthPrefix: "Bearer ",
		}),
	}
}
