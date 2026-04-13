package brain

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// defaultOpenAIModels are used as fallback when dynamic discovery fails.
var defaultOpenAIModels = []string{"gpt-4o", "gpt-4o-mini", "gpt-3.5-turbo"}

// OpenAIProvider implements Provider by calling OpenAI's chat completions API.
// It embeds OpenAICompatProvider for all shared HTTP/SSE logic and adds
// OpenAI-specific behaviour:
//
//   - Base URL defaults to "https://api.openai.com"; endpoints are rooted at /v1/
//   - Available() returns true for non-default base URLs even without an API key
//     (Ollama and other open-access endpoints don't require authentication)
//   - fromAPIResponse stamps Provider = types.ProviderOpenAI on every response
//   - XML/JSON tool-call parsing bridges Qwen2.5/llama.cpp output to the
//     OpenAI tool_calls array that CLI agents (OpenCode, etc.) expect
type OpenAIProvider struct {
	*OpenAICompatProvider
	rawBaseURL string // the base URL before the /v1 suffix is appended
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

// NewOpenAI creates a new OpenAI provider. If baseURL is empty it defaults to
// "https://api.openai.com". The underlying OpenAICompatProvider is configured
// with baseURL+"/v1" so that all shared HTTP helpers target the correct paths.
//
// Set insecureSkipVerify via WithInsecureSkipVerify to skip TLS certificate
// verification for self-signed certificates.
func NewOpenAI(apiKey, baseURL string, opts ...OpenAIOption) *OpenAIProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	rawBase := strings.TrimRight(baseURL, "/")

	cfg := openAIConfig{}
	for _, o := range opts {
		o(&cfg)
	}

	compatCfg := OpenAICompatConfig{
		Name:       "openai",
		BaseURL:    rawBase + "/v1",
		APIKey:     apiKey,
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	}

	compat := NewOpenAICompat(compatCfg)

	// Apply TLS override when requested.
	if cfg.insecureSkipVerify {
		compat.httpClient = &http.Client{
			Timeout: 5 * time.Minute,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, //nolint:gosec // user-configurable for self-signed certs
				},
			},
		}
	}

	// Seed the model list with known defaults so Models() is never empty even
	// before the first successful discovery call.
	compat.SetModels(defaultOpenAIModels)

	return &OpenAIProvider{
		OpenAICompatProvider: compat,
		rawBaseURL:           rawBase,
	}
}

// Name returns the provider name.
func (p *OpenAIProvider) Name() string { return "openai" }

// Available returns true when an API key is configured, OR when a non-default
// base URL is set (Ollama and other open-access endpoints don't require auth).
func (p *OpenAIProvider) Available() bool {
	return p.cfg.APIKey != "" ||
		(p.rawBaseURL != "" && p.rawBaseURL != "https://api.openai.com")
}

// RefreshModels forces a synchronous model list refresh from the upstream API.
func (p *OpenAIProvider) RefreshModels() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Ignore the error — on failure the cached list is retained.
	_ = p.OpenAICompatProvider.FetchModels(ctx, nil)
}

// Complete sends a non-streaming chat completion to OpenAI and returns the
// response. It delegates to the embedded provider for the HTTP round-trip and
// then applies OpenAI-specific post-processing:
//   - stamps Provider = types.ProviderOpenAI
//   - converts XML/JSON tool-call content to native tool_calls entries
func (p *OpenAIProvider) Complete(ctx context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	resp, err := p.OpenAICompatProvider.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	p.postProcess(resp)
	return resp, nil
}

// CompleteStream sends a streaming chat completion to OpenAI. The underlying
// SSE transport is handled by the embedded provider; this method just delegates.
func (p *OpenAIProvider) CompleteStream(ctx context.Context, req *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	return p.OpenAICompatProvider.CompleteStream(ctx, req)
}

// postProcess applies OpenAI-specific transformations to resp after it has
// been decoded by the shared fromAPIResponse helper:
//
//  1. Stamps Provider = types.ProviderOpenAI.
//  2. Parses XML <function> tool calls produced by Qwen2.5/llama.cpp.
//  3. Parses JSON-in-content tool calls produced by some fine-tuned models.
func (p *OpenAIProvider) postProcess(resp *types.InternalChatResponse) {
	resp.Provider = types.ProviderOpenAI

	// Already has native tool calls — nothing to do.
	if len(resp.Message.ToolCalls) > 0 {
		return
	}

	content := resp.Message.Content

	// If content contains XML <function> tags (Qwen2.5/llama.cpp format),
	// parse them and convert to native tool_calls. This bridges the gap
	// between llama.cpp's output format and the OpenAI tool_calls array that
	// CLI agents (OpenCode, etc.) expect.
	if strings.Contains(content, "<function>") {
		if tc := parseXMLToolCall(content); tc != nil {
			resp.Message.ToolCalls = append(resp.Message.ToolCalls, *tc)
			resp.Message.Content = "" // content was a tool call, not prose
			resp.FinishReason = "tool_calls"
			return
		}
	}

	// Also handle JSON-in-content format: {"name": "...", "arguments": {...}}
	if strings.Contains(content, `"name"`) && strings.Contains(content, `"arguments"`) {
		if tc := parseJSONToolCall(content); tc != nil {
			resp.Message.ToolCalls = append(resp.Message.ToolCalls, *tc)
			resp.Message.Content = ""
			resp.FinishReason = "tool_calls"
		}
	}
}

// parseXMLToolCall extracts a tool call from Qwen-style XML format:
//
//	<function><name>tool_name</name><arguments>{"key":"value"}</arguments></function>
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
//
//	{"name": "tool_name", "arguments": {"key": "value"}}
//
// May be wrapped in markdown code fences.
func parseJSONToolCall(content string) *types.InternalToolCall {
	// Strip markdown fences.
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
	// Find JSON object.
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

	// Sanitize arguments: CLI agents (OpenCode) require specific fields with
	// specific types. The model often omits optional fields or sends null values
	// that fail schema validation.
	sanitizeToolArgs(tc.Name, tc.Arguments)

	// Validate: if after sanitization the args are still invalid (empty or
	// missing critical fields), don't convert — let the natural language
	// fallback pass through as text.
	if !isValidToolCall(tc.Name, tc.Arguments) {
		return nil
	}

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

	// Remove null/nil values first — schema validators reject them.
	for k, v := range args {
		if v == nil {
			delete(args, k)
		}
	}

	switch toolName {
	case "bash", "shell", "execute_shell", "Bash":
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

	case "read", "read_file", "Read":
		// Normalize path → filePath
		if _, ok := args["filePath"]; !ok {
			if p, ok := args["path"].(string); ok {
				args["filePath"] = p
				delete(args, "path")
			}
		}
		// Fix offset: OpenCode requires offset >= 1 (1-based line numbers)
		if off, ok := args["offset"].(float64); ok && off < 1 {
			args["offset"] = float64(1)
		}

	case "write", "write_file", "Write":
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
		// Model often sends flat question string instead of array.
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
		// Ensure questions is an array, not a string.
		if q, ok := args["questions"].(string); ok {
			args["questions"] = []map[string]interface{}{
				{"question": q},
			}
		}

	case "glob", "search", "grep":
		// Normalize common field names.
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

// isValidToolCall checks if tool arguments have the minimum required fields
// after sanitization. Returns false if the call would cause schema validation
// errors in the CLI agent.
func isValidToolCall(toolName string, args map[string]interface{}) bool {
	switch toolName {
	case "bash", "shell", "execute_shell", "Bash":
		cmd, ok := args["command"].(string)
		return ok && cmd != ""
	case "read", "read_file", "Read", "write", "write_file", "Write", "edit", "Edit":
		fp, ok := args["filePath"].(string)
		return ok && fp != ""
	case "question", "ask":
		questions, ok := args["questions"].([]map[string]interface{})
		if !ok || len(questions) == 0 {
			return false
		}
		q, ok := questions[0]["question"].(string)
		return ok && q != ""
	case "glob", "search", "grep":
		pattern, ok := args["pattern"].(string)
		return ok && pattern != ""
	}
	// Unknown tools: allow if args are non-empty.
	return len(args) > 0
}
