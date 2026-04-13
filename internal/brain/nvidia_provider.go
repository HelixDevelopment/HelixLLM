package brain

// NvidiaProvider is a thin wrapper around OpenAICompatProvider that targets
// NVIDIA's NIM inference API (https://integrate.api.nvidia.com/v1).
type NvidiaProvider struct {
	*OpenAICompatProvider
}

// NewNvidiaProvider constructs a NvidiaProvider.
// baseURL defaults to "https://integrate.api.nvidia.com/v1" when empty.
func NewNvidiaProvider(apiKey, baseURL string) *NvidiaProvider {
	if baseURL == "" {
		baseURL = "https://integrate.api.nvidia.com/v1"
	}
	return &NvidiaProvider{
		OpenAICompatProvider: NewOpenAICompat(OpenAICompatConfig{
			Name:       "nvidia",
			BaseURL:    baseURL,
			APIKey:     apiKey,
			AuthHeader: "Authorization",
			AuthPrefix: "Bearer ",
		}),
	}
}
