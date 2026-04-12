package brain

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
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
	if p.apiKey != "" {
		if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	}

	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: send request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("openai: unexpected status %d: %s", httpResp.StatusCode, string(respBody)[:min(len(respBody), 200)])
	}

	var apiResp api.ChatCompletionResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("openai: decode response: %w", err)
	}

	return p.fromAPIResponse(&apiResp), nil

}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
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
		// Pass tool calls from upstream response (OpenAI native format)
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

		// If no native tool_calls but content contains XML <function> tags
		// (Qwen2.5/llama.cpp format), parse them and convert to tool_calls.
		// This bridges the gap between llama.cpp's output format and the
		// OpenAI tool_calls array that CLI agents (OpenCode, etc.) expect.
		if len(msg.ToolCalls) == 0 && strings.Contains(content, "<function>") {
			if tc := parseXMLToolCall(content); tc != nil {
				msg.ToolCalls = append(msg.ToolCalls, *tc)
				msg.Content = "" // Clear content since it was a tool call
				result.FinishReason = "tool_calls"
			}
		}

		// Also handle JSON-in-content format ({"name": "...", "arguments": {...}})
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

// parseXMLToolCall extracts a tool call from Qwen-style XML format:
// <function><name>tool_name</name><arguments>{"key":"value"}</arguments></function>
func parseXMLToolCall(content string) *types.InternalToolCall {
	fnStart := strings.Index(content, "<function>")
	fnEnd := strings.Index(content, "</function>")
	if fnStart < 0 || fnEnd < 0 {
		return nil
	}
	inner := content[fnStart+len("<function>") : fnEnd]

	nameStart := strings.Index(inner, "<name>")
	nameEnd := strings.Index(inner, "</name>")
	if nameStart < 0 || nameEnd < 0 {
		return nil
	}
	name := strings.TrimSpace(inner[nameStart+len("<name>") : nameEnd])

	argsStart := strings.Index(inner, "<arguments>")
	argsEnd := strings.Index(inner, "</arguments>")
	args := "{}"
	if argsStart >= 0 && argsEnd > argsStart {
		args = strings.TrimSpace(inner[argsStart+len("<arguments>") : argsEnd])
	}

	return &types.InternalToolCall{
		ID:   "call_" + name,
		Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: name, Arguments: args},
	}
}

// parseJSONToolCall extracts a tool call from JSON-in-content format:
// {"name": "tool_name", "arguments": {"key": "value"}}
// May be wrapped in markdown code fences.
func parseJSONToolCall(content string) *types.InternalToolCall {
	// Strip markdown fences
	cleaned := content
	if idx := strings.Index(cleaned, "```"); idx >= 0 {
		start := idx + 3
		if nl := strings.IndexByte(cleaned[start:], '\n'); nl >= 0 {
			start += nl + 1
		}
		if end := strings.Index(cleaned[start:], "```"); end >= 0 {
			cleaned = strings.TrimSpace(cleaned[start : start+end])
		}
	}
	// Find JSON object
	if !strings.HasPrefix(cleaned, "{") {
		if brace := strings.Index(cleaned, "{"); brace >= 0 {
			if end := strings.LastIndex(cleaned, "}"); end > brace {
				cleaned = cleaned[brace : end+1]
			}
		}
	}

	var tc struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(cleaned), &tc); err != nil || tc.Name == "" {
		return nil
	}

	// Sanitize arguments: CLI agents (OpenCode) require specific fields
	// with specific types. The model often omits optional fields or
	// sends null/undefined values that fail schema validation.
	sanitizeToolArgs(tc.Name, tc.Arguments)

	argsJSON, _ := json.Marshal(tc.Arguments)
	return &types.InternalToolCall{
		ID:   "call_" + tc.Name,
		Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: tc.Name, Arguments: string(argsJSON)},
	}
}

// sanitizeToolArgs ensures tool arguments match CLI agent schemas.
// OpenCode requires specific fields (description, timeout) that the
// model often omits. This adds sensible defaults for missing fields.
func sanitizeToolArgs(toolName string, args map[string]interface{}) {
	if args == nil {
		return
	}

	// Remove null/nil values first — schema validators reject them
	for k, v := range args {
		if v == nil {
			delete(args, k)
		}
	}

	switch toolName {
	case "bash", "shell", "execute_shell":
		// Requires: command (string), description (string), timeout (number)
		if _, ok := args["description"]; !ok {
			if cmd, ok := args["command"].(string); ok {
				args["description"] = "Running: " + cmd
			} else {
				args["description"] = "Executing command"
			}
		}
		if _, ok := args["timeout"]; !ok {
			args["timeout"] = 30000
		}

	case "read", "read_file":
		// Normalize path → filePath
		if _, ok := args["filePath"]; !ok {
			if p, ok := args["path"].(string); ok {
				args["filePath"] = p
				delete(args, "path")
			}
		}

	case "write", "write_file":
		if _, ok := args["filePath"]; !ok {
			if p, ok := args["path"].(string); ok {
				args["filePath"] = p
				delete(args, "path")
			}
		}

	case "edit":
		// Requires: filePath (string), old (string), new (string)
		if _, ok := args["filePath"]; !ok {
			if p, ok := args["path"].(string); ok {
				args["filePath"] = p
				delete(args, "path")
			}
		}

	case "question", "ask":
		// Requires: questions (array of {question: string})
		// Model often sends flat question string instead of array
		if _, ok := args["questions"]; !ok {
			if q, ok := args["question"].(string); ok {
				args["questions"] = []map[string]interface{}{
					{"question": q},
				}
				delete(args, "question")
			} else if q, ok := args["text"].(string); ok {
				args["questions"] = []map[string]interface{}{
					{"question": q},
				}
				delete(args, "text")
			}
		}
		// Ensure questions is an array, not a string
		if q, ok := args["questions"].(string); ok {
			args["questions"] = []map[string]interface{}{
				{"question": q},
			}
		}

	case "glob", "search", "grep":
		// Normalize common field names
		if _, ok := args["pattern"]; !ok {
			if p, ok := args["query"].(string); ok {
				args["pattern"] = p
				delete(args, "query")
			}
		}

	case "list_directory", "ls":
		if _, ok := args["path"]; !ok {
			args["path"] = "."
		}
	}
}
