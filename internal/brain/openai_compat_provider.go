package brain

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// ProviderError is returned by OpenAICompatProvider whenever the upstream API
// responds with a non-2xx status code. The FallbackChain uses errors.As to
// distinguish 429 (rate limit) from 5xx (transient failure) and route
// accordingly.
type ProviderError struct {
	Provider   string
	StatusCode int
	Body       string
	Headers    http.Header
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("%s: HTTP %d: %s", e.Provider, e.StatusCode, e.Body)
}

// AsProviderError is a convenience wrapper around errors.As for *ProviderError.
// It reports whether err (or anything in its chain) is a *ProviderError and
// sets target to that value when found.
func AsProviderError(err error, target **ProviderError) bool {
	return errors.As(err, target)
}

// OpenAICompatConfig holds the configuration for an OpenAI-compatible provider.
// It is intentionally minimal — each concrete provider (Chutes, OpenRouter, …)
// fills in the fields that differ and embeds the shared OpenAICompatProvider.
type OpenAICompatConfig struct {
	// Name is the human-readable provider name returned by Name().
	Name string

	// BaseURL is the root URL of the API, WITHOUT a trailing slash.
	// Example: "https://llm.chutes.ai/v1"
	BaseURL string

	// APIKey is the credential used for authentication.
	// Available() returns false when this is empty.
	APIKey string

	// AuthHeader is the HTTP header name used to send the API key.
	// Example: "Authorization" or "x-api-key".
	AuthHeader string

	// AuthPrefix is prepended to the APIKey value when setting AuthHeader.
	// Example: "Bearer " (note the trailing space) or "" (key sent as-is).
	AuthPrefix string

	// Timeout overrides the default HTTP client timeout (5 minutes).
	// A zero value uses the default.
	Timeout time.Duration
}

// OpenAICompatProvider is a shared base provider for all OpenAI-compatible
// free-tier cloud APIs.  Embed it in a concrete provider struct and set
// cfg.Name/BaseURL/APIKey/AuthHeader/AuthPrefix to get a fully functional
// Provider with model discovery and error propagation.
type OpenAICompatProvider struct {
	cfg        OpenAICompatConfig
	httpClient *http.Client

	mu     sync.RWMutex
	models []string
}

// NewOpenAICompat constructs an OpenAICompatProvider from cfg.
// The returned provider is ready to use; call FetchModels or SetModels to
// populate the model list before routing requests to it.
func NewOpenAICompat(cfg OpenAICompatConfig) *OpenAICompatProvider {
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	return &OpenAICompatProvider{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Name returns the provider's configured name.
func (p *OpenAICompatProvider) Name() string { return p.cfg.Name }

// Available returns true when an API key has been configured.
func (p *OpenAICompatProvider) Available() bool { return p.cfg.APIKey != "" }

// Models returns the currently cached model list.
func (p *OpenAICompatProvider) Models() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]string, len(p.models))
	copy(out, p.models)
	return out
}

// SetModels replaces the model list directly without a network call.
// Useful for providers whose model list is static and known at compile time.
func (p *OpenAICompatProvider) SetModels(models []string) {
	cp := make([]string, len(models))
	copy(cp, models)
	p.mu.Lock()
	p.models = cp
	p.mu.Unlock()
}

// FetchModels queries GET {baseURL}/models and caches the result.
// If filterFn is non-nil, only model IDs for which filterFn returns true are
// kept.  Returns an error if the HTTP request fails or the server responds
// with a non-200 status.
func (p *OpenAICompatProvider) FetchModels(ctx context.Context, filterFn func(id string) bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.BaseURL+"/models", nil)
	if err != nil {
		return fmt.Errorf("%s: FetchModels create request: %w", p.cfg.Name, err)
	}
	p.setAuthHeader(req)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s: FetchModels send request: %w", p.cfg.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: FetchModels unexpected status %d: %s",
			p.cfg.Name, resp.StatusCode, truncate(string(body), 200))
	}

	var listing modelsListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		return fmt.Errorf("%s: FetchModels decode response: %w", p.cfg.Name, err)
	}

	ids := make([]string, 0, len(listing.Data))
	for _, m := range listing.Data {
		if m.ID == "" {
			continue
		}
		if filterFn != nil && !filterFn(m.ID) {
			continue
		}
		ids = append(ids, m.ID)
	}

	p.mu.Lock()
	p.models = ids
	p.mu.Unlock()

	return nil
}

// Complete sends a non-streaming chat completion request to the upstream API
// and returns the parsed response. Non-2xx status codes are returned as
// *ProviderError so callers can distinguish 429 from 5xx.
func (p *OpenAICompatProvider) Complete(
	ctx context.Context,
	req *types.InternalChatRequest,
) (*types.InternalChatResponse, error) {
	apiReq := p.toAPIRequest(req)
	apiReq.Stream = false

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal request: %w", p.cfg.Name, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.cfg.BaseURL+"/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("%s: create request: %w", p.cfg.Name, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	p.setAuthHeader(httpReq)

	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%s: send request: %w", p.cfg.Name, err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, &ProviderError{
			Provider:   p.cfg.Name,
			StatusCode: httpResp.StatusCode,
			Body:       truncate(string(respBody), 500),
			Headers:    httpResp.Header.Clone(),
		}
	}

	var apiResp api.ChatCompletionResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("%s: decode response: %w", p.cfg.Name, err)
	}

	return p.fromAPIResponse(&apiResp), nil
}

// CompleteStream sends a streaming chat completion request and returns a
// channel of StreamChunks. The channel is closed when the stream ends or the
// context is cancelled. Non-2xx status codes are returned as errors before the
// channel is opened.
func (p *OpenAICompatProvider) CompleteStream(
	ctx context.Context,
	req *types.InternalChatRequest,
) (<-chan types.StreamChunk, error) {
	apiReq := p.toAPIRequest(req)
	apiReq.Stream = true

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal stream request: %w", p.cfg.Name, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.cfg.BaseURL+"/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("%s: create stream request: %w", p.cfg.Name, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	p.setAuthHeader(httpReq)

	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%s: send stream request: %w", p.cfg.Name, err)
	}

	if httpResp.StatusCode != http.StatusOK {
		httpResp.Body.Close()
		return nil, fmt.Errorf("%s: stream unexpected status %d", p.cfg.Name, httpResp.StatusCode)
	}

	ch := make(chan types.StreamChunk, 64)
	go func() {
		defer close(ch)
		defer httpResp.Body.Close()
		p.readSSEStream(ctx, httpResp, ch)
	}()

	return ch, nil
}

// --- internal helpers -------------------------------------------------------

// setAuthHeader applies the configured authentication header to req.
func (p *OpenAICompatProvider) setAuthHeader(req *http.Request) {
	if p.cfg.AuthHeader != "" && p.cfg.APIKey != "" {
		req.Header.Set(p.cfg.AuthHeader, p.cfg.AuthPrefix+p.cfg.APIKey)
	}
}

// readSSEStream reads SSE lines from the HTTP response body and sends
// StreamChunks on ch. It stops on [DONE], scanner EOF, or ctx cancellation.
func (p *OpenAICompatProvider) readSSEStream(
	ctx context.Context,
	resp *http.Response,
	ch chan<- types.StreamChunk,
) {
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
			continue // skip malformed lines
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

// toAPIRequest converts an InternalChatRequest to an api.ChatCompletionRequest.
func (p *OpenAICompatProvider) toAPIRequest(req *types.InternalChatRequest) api.ChatCompletionRequest {
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
	for _, t := range req.Tools {
		apiReq.Tools = append(apiReq.Tools, api.Tool{
			Type: t.Type,
			Function: func() api.ToolFunction {
				if fn, ok := t.Function.(api.ToolFunction); ok {
					return fn
				}
				data, _ := json.Marshal(t.Function)
				var fn api.ToolFunction
				json.Unmarshal(data, &fn) //nolint:errcheck
				return fn
			}(),
		})
	}
	return apiReq
}

// fromAPIResponse converts an api.ChatCompletionResponse to an
// InternalChatResponse. It sets Provider to an empty string — the caller
// (concrete provider) should override it if a dedicated types.Provider
// constant exists.
func (p *OpenAICompatProvider) fromAPIResponse(resp *api.ChatCompletionResponse) *types.InternalChatResponse {
	result := &types.InternalChatResponse{
		ID:    resp.ID,
		Model: resp.Model,
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

// truncate returns s truncated to at most n bytes (rune-safe approximation).
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
