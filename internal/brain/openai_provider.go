package brain

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// OpenAIProvider implements Provider by calling OpenAI's chat completions API.
type OpenAIProvider struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	models     []string
}

// NewOpenAI creates a new OpenAI provider. If baseURL is empty it defaults to
// "https://api.openai.com". If models is nil it defaults to the standard GPT
// model list.
func NewOpenAI(apiKey, baseURL string) *OpenAIProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	return &OpenAIProvider{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
		models: []string{"gpt-4o", "gpt-4o-mini", "gpt-3.5-turbo"},
	}
}

func (p *OpenAIProvider) Name() string     { return "openai" }
func (p *OpenAIProvider) Models() []string { return p.models }

// Available returns true when an API key is configured. The OpenAI API is a
// remote service so we do not make a network call; we just verify that a key
// has been set.
func (p *OpenAIProvider) Available() bool {
	return p.apiKey != ""
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
