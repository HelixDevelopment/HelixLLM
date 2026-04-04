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

const anthropicVersion = "2023-06-01"

// AnthropicProvider implements Provider by calling Anthropic's Messages API.
type AnthropicProvider struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	models     []string
}

// NewAnthropic creates a new Anthropic provider. If baseURL is empty it
// defaults to "https://api.anthropic.com". Models default to the standard
// Claude model list.
func NewAnthropic(apiKey, baseURL string) *AnthropicProvider {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return &AnthropicProvider{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
		models: []string{
			"claude-opus-4-5",
			"claude-sonnet-4-5",
			"claude-haiku-4-5",
		},
	}
}

func (p *AnthropicProvider) Name() string     { return "anthropic" }
func (p *AnthropicProvider) Models() []string { return p.models }

// Available returns true when an API key is configured.
func (p *AnthropicProvider) Available() bool {
	return p.apiKey != ""
}

// Complete sends a non-streaming request to Anthropic's Messages API and
// returns the response translated to HelixLLM's internal types.
func (p *AnthropicProvider) Complete(ctx context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	apiReq := p.toMessageRequest(req)
	apiReq.Stream = false

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/messages", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("anthropic: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: send request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic: unexpected status %d", httpResp.StatusCode)
	}

	var msgResp api.MessageResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&msgResp); err != nil {
		return nil, fmt.Errorf("anthropic: decode response: %w", err)
	}

	return p.fromMessageResponse(&msgResp), nil
}

// CompleteStream sends a streaming request to Anthropic's Messages API and
// returns a channel of StreamChunks. The channel is closed when the stream ends.
func (p *AnthropicProvider) CompleteStream(ctx context.Context, req *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	apiReq := p.toMessageRequest(req)
	apiReq.Stream = true

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/messages", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("anthropic: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("Accept", "text/event-stream")

	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: send request: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		httpResp.Body.Close()
		return nil, fmt.Errorf("anthropic: unexpected status %d", httpResp.StatusCode)
	}

	ch := make(chan types.StreamChunk, 64)
	go func() {
		defer close(ch)
		defer httpResp.Body.Close()
		p.readAnthropicSSEStream(ctx, httpResp, ch)
	}()

	return ch, nil
}

// readAnthropicSSEStream reads Anthropic's named-event SSE format.
// Anthropic uses paired "event: <type>" and "data: <json>" lines.
// Only content_block_delta events carry actual text tokens.
func (p *AnthropicProvider) readAnthropicSSEStream(ctx context.Context, resp *http.Response, ch chan<- types.StreamChunk) {
	scanner := bufio.NewScanner(resp.Body)
	var currentEvent string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			// Blank line or other line — reset event type on blank.
			if line == "" {
				currentEvent = ""
			}
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		// message_stop signals end of stream.
		if currentEvent == "message_stop" {
			return
		}

		// Only content_block_delta carries text we care about.
		if currentEvent != "content_block_delta" {
			continue
		}

		var event api.MessageStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		if event.Delta != nil && event.Delta.Type == "text_delta" && event.Delta.Text != "" {
			ch <- types.StreamChunk{Content: event.Delta.Text}
		}
	}
}

// toMessageRequest converts an InternalChatRequest to an Anthropic MessageRequest.
// System messages are extracted and concatenated into the top-level system field.
func (p *AnthropicProvider) toMessageRequest(req *types.InternalChatRequest) api.MessageRequest {
	var systemParts []string
	var messages []api.AnthropicMessage

	for _, m := range req.Messages {
		if m.Role == types.RoleSystem {
			systemParts = append(systemParts, m.Content)
			continue
		}
		messages = append(messages, api.AnthropicMessage{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}

	// Anthropic requires max_tokens; default to a sensible value.
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	msgReq := api.MessageRequest{
		Model:     req.Model,
		Messages:  messages,
		MaxTokens: maxTokens,
		System:    strings.Join(systemParts, "\n"),
	}

	if req.Temperature > 0 {
		temp := req.Temperature
		msgReq.Temperature = &temp
	}

	return msgReq
}

// fromMessageResponse converts an Anthropic MessageResponse to an InternalChatResponse.
func (p *AnthropicProvider) fromMessageResponse(resp *api.MessageResponse) *types.InternalChatResponse {
	result := &types.InternalChatResponse{
		ID:       resp.ID,
		Model:    resp.Model,
		Provider: types.ProviderAnthropic,
	}

	if len(resp.Content) > 0 {
		result.Message = types.InternalMessage{
			Role:    types.RoleAssistant,
			Content: resp.Content[0].Text,
		}
	}

	if resp.StopReason != nil {
		result.FinishReason = *resp.StopReason
	}

	result.Usage = types.InternalUsage{
		PromptTokens:     resp.Usage.InputTokens,
		CompletionTokens: resp.Usage.OutputTokens,
		TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
	}

	return result
}
