package brain

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// modelCacheTTL is how long discovered models are considered fresh.
const modelCacheTTL = 1 * time.Hour

// defaultOpenAIModels are used as fallback when dynamic discovery fails.
var defaultOpenAIModels = []string{"gpt-4o", "gpt-4o-mini", "gpt-3.5-turbo"}

// OpenAIProvider implements Provider by calling OpenAI's chat completions API.
// On creation it attempts to discover the real model list from the upstream
// /v1/models endpoint and caches the result with a 1-hour TTL. If discovery
// fails it falls back to defaultOpenAIModels.
type OpenAIProvider struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client

	mu           sync.RWMutex
	models       []string
	modelsFetched time.Time
}

// NewOpenAI creates a new OpenAI provider. If baseURL is empty it defaults to
// "https://api.openai.com". The provider immediately attempts to discover
// models from the upstream API; on failure it falls back to a default list.
// Set insecureSkipVerify to true to skip TLS certificate verification for
// self-signed certificates.
func NewOpenAI(apiKey, baseURL string, opts ...OpenAIOption) *OpenAIProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	cfg := openAIConfig{}
	for _, o := range opts {
		o(&cfg)
	}

	transport := &http.Transport{}
	if cfg.insecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // user-configurable for self-signed certs
		}
	}

	p := &OpenAIProvider{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout:   5 * time.Minute,
			Transport: transport,
		},
		models: append([]string(nil), defaultOpenAIModels...),
	}

	return p
}

// OpenAIOption configures optional behaviour of the OpenAI provider.
type OpenAIOption func(*openAIConfig)

type openAIConfig struct {
	insecureSkipVerify bool
}

// WithInsecureSkipVerify makes the HTTP client skip TLS certificate
// verification. Use this when the upstream API uses self-signed certificates.
func WithInsecureSkipVerify(skip bool) OpenAIOption {
	return func(c *openAIConfig) {
		c.insecureSkipVerify = skip
	}
}

func (p *OpenAIProvider) Name() string { return "openai" }

// Models returns the dynamically discovered model list. If the cache has
// expired it re-fetches in the background; stale data is returned immediately
// so callers are never blocked.
func (p *OpenAIProvider) Models() []string {
	p.mu.RLock()
	models := p.models
	fetched := p.modelsFetched
	p.mu.RUnlock()

	if time.Since(fetched) > modelCacheTTL {
		go p.refreshModelsIfStale()
	}

	return models
}

// RefreshModels forces a synchronous model list refresh from the upstream API.
func (p *OpenAIProvider) RefreshModels() {
	p.discoverModels()
}

// refreshModelsIfStale re-discovers models only when the cache has expired.
func (p *OpenAIProvider) refreshModelsIfStale() {
	p.mu.RLock()
	fresh := time.Since(p.modelsFetched) <= modelCacheTTL
	p.mu.RUnlock()
	if fresh {
		return
	}
	p.discoverModels()
}

// modelsListResponse is the envelope returned by /v1/models.
type modelsListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// discoverModels queries the upstream /v1/models endpoint and updates the
// cached model list. On failure the existing list (or defaults) is kept.
func (p *OpenAIProvider) discoverModels() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		p.baseURL+"/v1/models", nil)
	if err != nil {
		return
	}
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	var listing modelsListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		return
	}

	if len(listing.Data) == 0 {
		return
	}

	ids := make([]string, 0, len(listing.Data))
	for _, m := range listing.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}

	if len(ids) == 0 {
		return
	}

	p.mu.Lock()
	p.models = ids
	p.modelsFetched = time.Now()
	p.mu.Unlock()
}

// Available returns true when an API key is configured. The OpenAI API is a
// remote service so we do not make a network call; we just verify that a key
// has been set.
func (p *OpenAIProvider) Available() bool {
	// A non-default base URL (e.g. Ollama at thinker.local:11434) means the
	// provider is intentionally configured even without an API key — Ollama
	// and other open-access endpoints don't require authentication.
	return p.apiKey != "" || (p.baseURL != "" && p.baseURL != "https://api.openai.com")
}

// Complete sends a non-streaming chat completion to OpenAI and returns the
// response translated to HelixLLM's internal types.
func (p *OpenAIProvider) Complete(ctx context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	apiReq := p.toAPIRequest(req)
	apiReq.Stream = false

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("openai: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: send request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai: unexpected status %d", httpResp.StatusCode)
	}

	var apiResp api.ChatCompletionResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("openai: decode response: %w", err)
	}

	return p.fromAPIResponse(&apiResp), nil
}

// CompleteStream sends a streaming chat completion to OpenAI and returns a
// channel of StreamChunks. The channel is closed when the stream ends.
func (p *OpenAIProvider) CompleteStream(ctx context.Context, req *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	apiReq := p.toAPIRequest(req)
	apiReq.Stream = true

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("openai: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: send request: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		httpResp.Body.Close()
		return nil, fmt.Errorf("openai: unexpected status %d", httpResp.StatusCode)
	}

	ch := make(chan types.StreamChunk, 64)
	go func() {
		defer close(ch)
		defer httpResp.Body.Close()
		p.readSSEStream(ctx, httpResp, ch)
	}()

	return ch, nil
}

// readSSEStream reads SSE lines from an HTTP response and sends StreamChunks
// on the channel.
func (p *OpenAIProvider) readSSEStream(ctx context.Context, resp *http.Response, ch chan<- types.StreamChunk) {
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return
		}

		var chunk api.ChatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) > 0 {
			sc := types.StreamChunk{
				Content: chunk.Choices[0].Delta.Content,
			}
			if chunk.Choices[0].FinishReason != nil {
				sc.FinishReason = *chunk.Choices[0].FinishReason
			}
			ch <- sc
		}
	}
}

// toAPIRequest converts an InternalChatRequest to an OpenAI ChatCompletionRequest.
func (p *OpenAIProvider) toAPIRequest(req *types.InternalChatRequest) api.ChatCompletionRequest {
	messages := make([]api.ChatMessage, len(req.Messages))
	for i, m := range req.Messages {
		msg := api.ChatMessage{
			Role:       string(m.Role),
			Content:    m.Content,
			Name:       m.Name,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, api.ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: api.ToolCallFunction{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
		messages[i] = msg
	}
	apiReq := api.ChatCompletionRequest{
		Model:      req.Model,
		Messages:   messages,
		ToolChoice: req.ToolChoice,
	}
	if req.MaxTokens > 0 {
		mt := req.MaxTokens
		apiReq.MaxTokens = &mt
	}
	if req.Temperature > 0 {
		temp := req.Temperature
		apiReq.Temperature = &temp
	}
	// Pass tools through to the upstream API
	for _, t := range req.Tools {
		apiReq.Tools = append(apiReq.Tools, api.Tool{
			Type: t.Type,
			Function: func() api.ToolFunction {
				if fn, ok := t.Function.(api.ToolFunction); ok {
					return fn
				}
				// Re-marshal/unmarshal for interface conversion
				data, _ := json.Marshal(t.Function)
				var fn api.ToolFunction
				json.Unmarshal(data, &fn) //nolint:errcheck
				return fn
			}(),
		})
	}
	return apiReq
}

// fromAPIResponse converts an OpenAI ChatCompletionResponse to an InternalChatResponse.
func (p *OpenAIProvider) fromAPIResponse(resp *api.ChatCompletionResponse) *types.InternalChatResponse {
	result := &types.InternalChatResponse{
		ID:       resp.ID,
		Model:    resp.Model,
		Provider: types.ProviderOpenAI,
	}
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		content := ""
		switch v := choice.Message.Content.(type) {
		case string:
			content = v
		}
		msg := types.InternalMessage{
			Role:    types.Role(choice.Message.Role),
			Content: content,
		}
		// Pass tool calls from upstream response
		for _, tc := range choice.Message.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, types.InternalToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
		result.Message = msg
		result.FinishReason = choice.FinishReason
	}
	if resp.Usage != nil {
		result.Usage = types.InternalUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
	}
	return result
}
