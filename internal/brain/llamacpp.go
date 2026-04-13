package brain

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/brain/models"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// LlamaCppProvider implements Provider by calling llama.cpp's OpenAI-compatible
// API at the configured base URL.
type LlamaCppProvider struct {
	baseURL  string
	models   []string
	client   *http.Client
	registry *models.Registry
}

// NewLlamaCppProvider creates a new llama.cpp provider pointing at the given
// base URL (e.g. "http://localhost:8080"). The models slice lists model IDs
// that this llama.cpp instance serves.
func NewLlamaCppProvider(baseURL string, models []string) *LlamaCppProvider {
	return &LlamaCppProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		models:  models,
		client: &http.Client{
			Timeout: 5 * time.Minute, // LLM completions can be slow.
		},
	}
}

func (p *LlamaCppProvider) Name() string { return "llamacpp" }

func (p *LlamaCppProvider) Models() []string {
	if p.registry != nil {
		return p.registry.ModelNames()
	}
	return p.models
}

// Available checks whether the llama.cpp server is reachable by hitting /health.
func (p *LlamaCppProvider) Available() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Complete sends a non-streaming chat completion to llama.cpp and returns the
// response translated to HelixLLM's internal types.
func (p *LlamaCppProvider) Complete(ctx context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	apiReq := p.toAPIRequest(req)
	apiReq.Stream = false

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("llamacpp: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("llamacpp: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llamacpp: send request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llamacpp: unexpected status %d", httpResp.StatusCode)
	}

	var apiResp api.ChatCompletionResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("llamacpp: decode response: %w", err)
	}

	return p.fromAPIResponse(&apiResp), nil
}

// CompleteStream sends a streaming chat completion to llama.cpp and returns a
// channel of StreamChunks. The channel is closed when the stream ends.
func (p *LlamaCppProvider) CompleteStream(ctx context.Context, req *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	apiReq := p.toAPIRequest(req)
	apiReq.Stream = true

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("llamacpp: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("llamacpp: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llamacpp: send request: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		httpResp.Body.Close()
		return nil, fmt.Errorf("llamacpp: unexpected status %d", httpResp.StatusCode)
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
func (p *LlamaCppProvider) readSSEStream(ctx context.Context, resp *http.Response, ch chan<- types.StreamChunk) {
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

// toAPIRequest converts an InternalChatRequest to an OpenAI ChatCompletionRequest
// suitable for llama.cpp's API.
func (p *LlamaCppProvider) toAPIRequest(req *types.InternalChatRequest) api.ChatCompletionRequest {
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
	// Pass tools to llama.cpp (requires --jinja flag for tool support)
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

// fromAPIResponse converts an OpenAI ChatCompletionResponse to an InternalChatResponse.
func (p *LlamaCppProvider) fromAPIResponse(resp *api.ChatCompletionResponse) *types.InternalChatResponse {
	result := &types.InternalChatResponse{
		ID:       resp.ID,
		Model:    resp.Model,
		Provider: types.ProviderLocal,
	}
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		// Extract content as string. The Content field is interface{} in the API
		// types; for llama.cpp it is always a string.
		content := ""
		switch v := choice.Message.Content.(type) {
		case string:
			content = v
		}
		msg := types.InternalMessage{
			Role:    types.Role(choice.Message.Role),
			Content: content,
		}
		// Pass tool calls from llama.cpp response (native format)
		// Apply sanitizeToolArgs to fix common issues (offset=0, missing fields)
		for _, tc := range choice.Message.ToolCalls {
			args := tc.Function.Arguments
			// Sanitize native tool call arguments
			var argsMap map[string]interface{}
			if err := json.Unmarshal([]byte(args), &argsMap); err == nil {
				sanitizeToolArgs(tc.Function.Name, argsMap)
				if sanitized, err := json.Marshal(argsMap); err == nil {
					args = string(sanitized)
				}
			}
			msg.ToolCalls = append(msg.ToolCalls, types.InternalToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      tc.Function.Name,
					Arguments: args,
				},
			})
		}

		// Bridge: llama.cpp with Qwen models often returns tool calls
		// as XML or JSON in content instead of the tool_calls array.
		// Parse these and convert to proper tool_calls format.
		if len(msg.ToolCalls) == 0 && strings.Contains(content, "<function>") {
			if tc := parseXMLToolCall(content); tc != nil {
				msg.ToolCalls = append(msg.ToolCalls, *tc)
				msg.Content = ""
				result.FinishReason = "tool_calls"
			}
		}
		if len(msg.ToolCalls) == 0 && strings.Contains(content, `"name"`) && strings.Contains(content, `"arguments"`) {
			if tc := parseJSONToolCall(content); tc != nil {
				msg.ToolCalls = append(msg.ToolCalls, *tc)
				msg.Content = ""
				result.FinishReason = "tool_calls"
			}
		}

		result.Message = msg
		if result.FinishReason == "" {
			result.FinishReason = choice.FinishReason
		}
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
