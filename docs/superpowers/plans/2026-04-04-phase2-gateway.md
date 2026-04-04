# Phase 2: Gateway Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Gateway API layer with OpenAI and Anthropic compatible endpoints, auth middleware, rate limiting, SSE streaming, content negotiation, and security headers. All endpoints are stubs that return properly formatted responses -- the Brain layer (Phase 3) will provide real LLM routing.

**Architecture:** Gateway endpoints are registered on the existing Gin engine via a gateway router. New vasic-digital submodules (Auth, RateLimiter, Security, Streaming, I18n, Formatters) provide middleware and streaming infrastructure. API types in `pkg/api/` and `pkg/types/` define the request/response contracts. All code is TDD -- tests first.

**Tech Stack:** Go 1.26+, Gin Gonic, vasic-digital modules (Auth, RateLimiter, Security, Streaming, I18n, Formatters), toon-format/toon-go

**Spec Reference:** `docs/superpowers/specs/2026-04-04-helixllm-master-design.md` -- Section 5 (Gateway Layer Design)

---

## File Structure

```
helixllm/
  cmd/helixllm/
    main.go                              Updated to wire gateway router
  internal/
    gateway/
      router.go                          Gateway router: wires endpoints + middleware
      router_test.go
      openai.go                          OpenAI-compatible endpoint handlers (stubs)
      openai_test.go
      anthropic.go                       Anthropic-compatible endpoint handler (stub)
      anthropic_test.go
      streaming.go                       SSE streaming in OpenAI text/event-stream format
      streaming_test.go
      middleware/
        auth.go                          API key auth middleware wrapping digital.vasic.auth
        auth_test.go
        ratelimit.go                     Rate limiting middleware wrapping digital.vasic.ratelimiter
        ratelimit_test.go
        negotiation.go                   Content negotiation (TOON/JSON) via Accept header
        negotiation_test.go
        security.go                      HTTP security headers wrapping digital.vasic.security
        security_test.go
  pkg/
    api/
      openai.go                          OpenAI API request/response types
      openai_test.go
      anthropic.go                       Anthropic API request/response types
      anthropic_test.go
    types/
      types.go                           Internal HelixLLM types (InternalChatRequest, etc.)
      types_test.go
  submodules/
    Auth/                                digital.vasic.auth
    RateLimiter/                         digital.vasic.ratelimiter
    Security/                            digital.vasic.security
    Streaming/                           digital.vasic.streaming
    I18n/                                digital.vasic.i18n
    Formatters/                          digital.vasic.formatters
  go.mod                                 Updated with new submodules + replace directives
  go.sum
```

---

### Task 1: Add Phase 2 Git Submodules

**Files:**
- Modify: `.gitmodules`
- Modify: `go.mod`
- Create: `submodules/` entries for Auth, RateLimiter, Security, Streaming, I18n, Formatters

- [ ] **Step 1: Add Gateway layer submodules from vasic-digital (GitHub)**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
git submodule add git@github.com:vasic-digital/Auth.git submodules/Auth
git submodule add git@github.com:vasic-digital/RateLimiter.git submodules/RateLimiter
git submodule add git@github.com:vasic-digital/Security.git submodules/Security
git submodule add git@github.com:vasic-digital/Streaming.git submodules/Streaming
git submodule add git@github.com:vasic-digital/I18n.git submodules/I18n
git submodule add git@github.com:vasic-digital/Formatters.git submodules/Formatters
```

Expected: each submodule cloned into `submodules/`, `.gitmodules` updated with 6 new entries.

- [ ] **Step 2: Add replace directives to go.mod**

Add these `replace` directives to the existing `replace` block in `go.mod`:

```
replace (
	// ... existing Phase 1 replacements ...
	digital.vasic.auth => ./submodules/Auth
	digital.vasic.ratelimiter => ./submodules/RateLimiter
	digital.vasic.security => ./submodules/Security
	digital.vasic.streaming => ./submodules/Streaming
	digital.vasic.i18n => ./submodules/I18n
	digital.vasic.formatters => ./submodules/Formatters
)
```

Also add to the `require` block:

```
require (
	// ... existing Phase 1 requirements ...
	digital.vasic.auth v0.0.0
	digital.vasic.ratelimiter v0.0.0
	digital.vasic.security v0.0.0
	digital.vasic.streaming v0.0.0
	digital.vasic.i18n v0.0.0
	digital.vasic.formatters v0.0.0
	github.com/toon-format/toon-go v0.0.0
)
```

- [ ] **Step 3: Add toon-go dependency**

```bash
go get github.com/toon-format/toon-go@latest
```

If `toon-format/toon-go` is not yet published, add a placeholder `replace` directive:

```
replace github.com/toon-format/toon-go => ./submodules/toon-go
```

And clone or create the placeholder:

```bash
git submodule add git@github.com:toon-format/toon-go.git submodules/toon-go
```

- [ ] **Step 4: Tidy modules**

```bash
go mod tidy
```

Expected: `go.sum` updated, all new dependencies resolved.

- [ ] **Step 5: Verify build**

```bash
go build ./...
```

Expected: builds successfully with all new submodules resolved.

- [ ] **Step 6: Commit**

```bash
git add .gitmodules submodules/ go.mod go.sum
git commit -m "feat: add Phase 2 Gateway submodules (Auth, RateLimiter, Security, Streaming, I18n, Formatters)"
```

---

### Task 2: OpenAI API Types

**Files:**
- Create: `pkg/api/openai.go`
- Create: `pkg/api/openai_test.go`

- [ ] **Step 1: Write failing tests for OpenAI types**

Create `pkg/api/openai_test.go`:

```go
package api_test

import (
	"encoding/json"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

func TestChatCompletionRequestJSON(t *testing.T) {
	req := api.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []api.ChatMessage{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Hello"},
		},
		Temperature: floatPtr(0.7),
		MaxTokens:   intPtr(100),
		Stream:      false,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded api.ChatCompletionRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if decoded.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", decoded.Model, "gpt-4o")
	}
	if len(decoded.Messages) != 2 {
		t.Errorf("Messages count = %d, want 2", len(decoded.Messages))
	}
	if decoded.Messages[0].Role != "system" {
		t.Errorf("Messages[0].Role = %q, want %q", decoded.Messages[0].Role, "system")
	}
}

func TestChatCompletionResponseJSON(t *testing.T) {
	resp := api.ChatCompletionResponse{
		ID:      "chatcmpl-abc123",
		Object:  "chat.completion",
		Created: 1700000000,
		Model:   "gpt-4o",
		Choices: []api.ChatChoice{
			{
				Index: 0,
				Message: api.ChatMessage{
					Role:    "assistant",
					Content: "Hello! How can I help?",
				},
				FinishReason: "stop",
			},
		},
		Usage: &api.Usage{
			PromptTokens:     10,
			CompletionTokens: 8,
			TotalTokens:      18,
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded api.ChatCompletionResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if decoded.ID != "chatcmpl-abc123" {
		t.Errorf("ID = %q, want %q", decoded.ID, "chatcmpl-abc123")
	}
	if decoded.Object != "chat.completion" {
		t.Errorf("Object = %q, want %q", decoded.Object, "chat.completion")
	}
	if len(decoded.Choices) != 1 {
		t.Errorf("Choices count = %d, want 1", len(decoded.Choices))
	}
	if decoded.Usage.TotalTokens != 18 {
		t.Errorf("Usage.TotalTokens = %d, want 18", decoded.Usage.TotalTokens)
	}
}

func TestChatCompletionChunkJSON(t *testing.T) {
	chunk := api.ChatCompletionChunk{
		ID:      "chatcmpl-abc123",
		Object:  "chat.completion.chunk",
		Created: 1700000000,
		Model:   "gpt-4o",
		Choices: []api.ChatChunkChoice{
			{
				Index: 0,
				Delta: api.ChatDelta{
					Role:    "assistant",
					Content: "Hello",
				},
				FinishReason: nil,
			},
		},
	}

	data, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded api.ChatCompletionChunk
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if decoded.Object != "chat.completion.chunk" {
		t.Errorf("Object = %q, want %q", decoded.Object, "chat.completion.chunk")
	}
	if decoded.Choices[0].Delta.Content != "Hello" {
		t.Errorf("Delta.Content = %q, want %q", decoded.Choices[0].Delta.Content, "Hello")
	}
}

func TestModelJSON(t *testing.T) {
	model := api.Model{
		ID:      "gpt-4o",
		Object:  "model",
		Created: 1700000000,
		OwnedBy: "openai",
	}

	data, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded api.Model
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if decoded.ID != "gpt-4o" {
		t.Errorf("ID = %q, want %q", decoded.ID, "gpt-4o")
	}
	if decoded.OwnedBy != "openai" {
		t.Errorf("OwnedBy = %q, want %q", decoded.OwnedBy, "openai")
	}
}

func TestModelListJSON(t *testing.T) {
	list := api.ModelList{
		Object: "list",
		Data: []api.Model{
			{ID: "gpt-4o", Object: "model", Created: 1700000000, OwnedBy: "openai"},
			{ID: "llama-3.1-70b", Object: "model", Created: 1700000000, OwnedBy: "helixllm"},
		},
	}

	data, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded api.ModelList
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if decoded.Object != "list" {
		t.Errorf("Object = %q, want %q", decoded.Object, "list")
	}
	if len(decoded.Data) != 2 {
		t.Errorf("Data count = %d, want 2", len(decoded.Data))
	}
}

func TestCompletionRequestJSON(t *testing.T) {
	req := api.CompletionRequest{
		Model:       "gpt-3.5-turbo-instruct",
		Prompt:      "Say hello",
		MaxTokens:   intPtr(50),
		Temperature: floatPtr(0.5),
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded api.CompletionRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if decoded.Model != "gpt-3.5-turbo-instruct" {
		t.Errorf("Model = %q, want %q", decoded.Model, "gpt-3.5-turbo-instruct")
	}
}

func TestCompletionResponseJSON(t *testing.T) {
	resp := api.CompletionResponse{
		ID:      "cmpl-abc123",
		Object:  "text_completion",
		Created: 1700000000,
		Model:   "gpt-3.5-turbo-instruct",
		Choices: []api.CompletionChoice{
			{Index: 0, Text: "Hello!", FinishReason: "stop"},
		},
		Usage: &api.Usage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded api.CompletionResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if decoded.Object != "text_completion" {
		t.Errorf("Object = %q, want %q", decoded.Object, "text_completion")
	}
}

func TestEmbeddingRequestJSON(t *testing.T) {
	req := api.EmbeddingRequest{
		Model: "text-embedding-ada-002",
		Input: "Hello world",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded api.EmbeddingRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if decoded.Model != "text-embedding-ada-002" {
		t.Errorf("Model = %q, want %q", decoded.Model, "text-embedding-ada-002")
	}
}

func TestEmbeddingResponseJSON(t *testing.T) {
	resp := api.EmbeddingResponse{
		Object: "list",
		Data: []api.Embedding{
			{Object: "embedding", Index: 0, Embedding: []float64{0.1, 0.2, 0.3}},
		},
		Model: "text-embedding-ada-002",
		Usage: &api.EmbeddingUsage{PromptTokens: 3, TotalTokens: 3},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded api.EmbeddingResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if len(decoded.Data) != 1 {
		t.Errorf("Data count = %d, want 1", len(decoded.Data))
	}
	if len(decoded.Data[0].Embedding) != 3 {
		t.Errorf("Embedding length = %d, want 3", len(decoded.Data[0].Embedding))
	}
}

func floatPtr(f float64) *float64 { return &f }
func intPtr(i int) *int           { return &i }
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/api/ -v
```

Expected: FAIL -- package does not exist yet.

- [ ] **Step 3: Implement OpenAI API types**

Create `pkg/api/openai.go`:

```go
// Package api defines request/response types for OpenAI and Anthropic compatible APIs.
package api

// ---------------------------------------------------------------------------
// Chat Completions
// ---------------------------------------------------------------------------

// ChatCompletionRequest matches the OpenAI chat completion request schema.
type ChatCompletionRequest struct {
	Model            string            `json:"model"`
	Messages         []ChatMessage     `json:"messages"`
	Temperature      *float64          `json:"temperature,omitempty"`
	TopP             *float64          `json:"top_p,omitempty"`
	N                *int              `json:"n,omitempty"`
	Stream           bool              `json:"stream,omitempty"`
	Stop             interface{}       `json:"stop,omitempty"`
	MaxTokens        *int              `json:"max_tokens,omitempty"`
	PresencePenalty  *float64          `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64          `json:"frequency_penalty,omitempty"`
	LogitBias        map[string]int    `json:"logit_bias,omitempty"`
	User             string            `json:"user,omitempty"`
	Tools            []Tool            `json:"tools,omitempty"`
	ToolChoice       interface{}       `json:"tool_choice,omitempty"`
	ResponseFormat   *ResponseFormat   `json:"response_format,omitempty"`
	Seed             *int              `json:"seed,omitempty"`
}

// ChatMessage represents a single message in a chat conversation.
type ChatMessage struct {
	Role       string      `json:"role"`
	Content    string      `json:"content"`
	Name       string      `json:"name,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
}

// Tool represents a tool available for the model.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction defines a function tool.
type ToolFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

// ToolCall represents a tool invocation by the model.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction is the function details within a tool call.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ResponseFormat specifies the output format for the model.
type ResponseFormat struct {
	Type string `json:"type"`
}

// ChatCompletionResponse matches the OpenAI chat completion response schema.
type ChatCompletionResponse struct {
	ID                string       `json:"id"`
	Object            string       `json:"object"`
	Created           int64        `json:"created"`
	Model             string       `json:"model"`
	Choices           []ChatChoice `json:"choices"`
	Usage             *Usage       `json:"usage,omitempty"`
	SystemFingerprint string       `json:"system_fingerprint,omitempty"`
}

// ChatChoice is one completion choice in the response.
type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// Usage tracks token counts.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ---------------------------------------------------------------------------
// Chat Completion Streaming (Chunks)
// ---------------------------------------------------------------------------

// ChatCompletionChunk is a single streamed chunk matching OpenAI's format.
type ChatCompletionChunk struct {
	ID                string            `json:"id"`
	Object            string            `json:"object"`
	Created           int64             `json:"created"`
	Model             string            `json:"model"`
	Choices           []ChatChunkChoice `json:"choices"`
	SystemFingerprint string            `json:"system_fingerprint,omitempty"`
}

// ChatChunkChoice is one choice within a streamed chunk.
type ChatChunkChoice struct {
	Index        int       `json:"index"`
	Delta        ChatDelta `json:"delta"`
	FinishReason *string   `json:"finish_reason"`
}

// ChatDelta is the incremental content in a streaming chunk.
type ChatDelta struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ---------------------------------------------------------------------------
// Models
// ---------------------------------------------------------------------------

// Model represents an available model.
type Model struct {
	ID         string `json:"id"`
	Object     string `json:"object"`
	Created    int64  `json:"created"`
	OwnedBy   string `json:"owned_by"`
	Permission []interface{} `json:"permission,omitempty"`
}

// ModelList is the response for GET /v1/models.
type ModelList struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

// ---------------------------------------------------------------------------
// Completions (Legacy)
// ---------------------------------------------------------------------------

// CompletionRequest matches the OpenAI completions request schema.
type CompletionRequest struct {
	Model            string      `json:"model"`
	Prompt           interface{} `json:"prompt"`
	MaxTokens        *int        `json:"max_tokens,omitempty"`
	Temperature      *float64    `json:"temperature,omitempty"`
	TopP             *float64    `json:"top_p,omitempty"`
	N                *int        `json:"n,omitempty"`
	Stream           bool        `json:"stream,omitempty"`
	Stop             interface{} `json:"stop,omitempty"`
	PresencePenalty  *float64    `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64    `json:"frequency_penalty,omitempty"`
	LogitBias        map[string]int `json:"logit_bias,omitempty"`
	User             string      `json:"user,omitempty"`
	Suffix           string      `json:"suffix,omitempty"`
	Echo             bool        `json:"echo,omitempty"`
}

// CompletionResponse matches the OpenAI completions response schema.
type CompletionResponse struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []CompletionChoice `json:"choices"`
	Usage   *Usage             `json:"usage,omitempty"`
}

// CompletionChoice is one choice in a completion response.
type CompletionChoice struct {
	Index        int    `json:"index"`
	Text         string `json:"text"`
	FinishReason string `json:"finish_reason"`
}

// ---------------------------------------------------------------------------
// Embeddings
// ---------------------------------------------------------------------------

// EmbeddingRequest matches the OpenAI embeddings request schema.
type EmbeddingRequest struct {
	Model          string      `json:"model"`
	Input          interface{} `json:"input"`
	EncodingFormat string      `json:"encoding_format,omitempty"`
	User           string      `json:"user,omitempty"`
}

// EmbeddingResponse matches the OpenAI embeddings response schema.
type EmbeddingResponse struct {
	Object string          `json:"object"`
	Data   []Embedding     `json:"data"`
	Model  string          `json:"model"`
	Usage  *EmbeddingUsage `json:"usage,omitempty"`
}

// Embedding is a single embedding vector.
type Embedding struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

// EmbeddingUsage tracks token counts for embedding requests.
type EmbeddingUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

// ErrorResponse matches the OpenAI error response format.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail contains the error information.
type ErrorDetail struct {
	Message string  `json:"message"`
	Type    string  `json:"type"`
	Param   *string `json:"param"`
	Code    *string `json:"code"`
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./pkg/api/ -v -count=1
```

Expected: all 9 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/api/openai.go pkg/api/openai_test.go
git commit -m "feat: add OpenAI API request/response types with full spec coverage"
```

---

### Task 3: Anthropic API Types

**Files:**
- Create: `pkg/api/anthropic.go`
- Create: `pkg/api/anthropic_test.go`

- [ ] **Step 1: Write failing tests for Anthropic types**

Create `pkg/api/anthropic_test.go`:

```go
package api_test

import (
	"encoding/json"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

func TestMessageRequestJSON(t *testing.T) {
	req := api.MessageRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []api.AnthropicMessage{
			{Role: "user", Content: "Hello, Claude"},
		},
		System:      "You are a helpful assistant.",
		Temperature: floatPtr(0.7),
		Stream:      false,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded api.MessageRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if decoded.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want %q", decoded.Model, "claude-sonnet-4-20250514")
	}
	if decoded.MaxTokens != 1024 {
		t.Errorf("MaxTokens = %d, want 1024", decoded.MaxTokens)
	}
	if len(decoded.Messages) != 1 {
		t.Errorf("Messages count = %d, want 1", len(decoded.Messages))
	}
}

func TestMessageResponseJSON(t *testing.T) {
	resp := api.MessageResponse{
		ID:           "msg_abc123",
		Type:         "message",
		Role:         "assistant",
		Content:      []api.ContentBlock{{Type: "text", Text: "Hello! How can I help?"}},
		Model:        "claude-sonnet-4-20250514",
		StopReason:   stringPtr("end_turn"),
		StopSequence: nil,
		Usage: api.AnthropicUsage{
			InputTokens:  10,
			OutputTokens: 8,
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded api.MessageResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if decoded.ID != "msg_abc123" {
		t.Errorf("ID = %q, want %q", decoded.ID, "msg_abc123")
	}
	if decoded.Type != "message" {
		t.Errorf("Type = %q, want %q", decoded.Type, "message")
	}
	if len(decoded.Content) != 1 {
		t.Errorf("Content count = %d, want 1", len(decoded.Content))
	}
	if decoded.Content[0].Text != "Hello! How can I help?" {
		t.Errorf("Content[0].Text = %q, want %q", decoded.Content[0].Text, "Hello! How can I help?")
	}
	if decoded.Usage.InputTokens != 10 {
		t.Errorf("Usage.InputTokens = %d, want 10", decoded.Usage.InputTokens)
	}
}

func TestContentBlockJSON(t *testing.T) {
	blocks := []api.ContentBlock{
		{Type: "text", Text: "Hello world"},
		{Type: "tool_use", ID: "tool_1", Name: "search", Input: map[string]interface{}{"query": "test"}},
	}

	data, err := json.Marshal(blocks)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded []api.ContentBlock
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("decoded count = %d, want 2", len(decoded))
	}
	if decoded[0].Type != "text" {
		t.Errorf("decoded[0].Type = %q, want %q", decoded[0].Type, "text")
	}
	if decoded[1].Type != "tool_use" {
		t.Errorf("decoded[1].Type = %q, want %q", decoded[1].Type, "tool_use")
	}
}

func TestMessageStreamEventJSON(t *testing.T) {
	events := []api.MessageStreamEvent{
		{Type: "message_start", Message: &api.MessageResponse{ID: "msg_1", Type: "message", Role: "assistant", Model: "claude-sonnet-4-20250514"}},
		{Type: "content_block_start", Index: intPtr(0), ContentBlock: &api.ContentBlock{Type: "text"}},
		{Type: "content_block_delta", Index: intPtr(0), Delta: &api.StreamDelta{Type: "text_delta", Text: "Hello"}},
		{Type: "content_block_stop", Index: intPtr(0)},
		{Type: "message_delta", Delta: &api.StreamDelta{Type: "message_delta", StopReason: stringPtr("end_turn")}, Usage: &api.AnthropicUsage{OutputTokens: 5}},
		{Type: "message_stop"},
	}

	for i, evt := range events {
		data, err := json.Marshal(evt)
		if err != nil {
			t.Fatalf("event[%d] Marshal error: %v", i, err)
		}
		var decoded api.MessageStreamEvent
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("event[%d] Unmarshal error: %v", i, err)
		}
		if decoded.Type != evt.Type {
			t.Errorf("event[%d].Type = %q, want %q", i, decoded.Type, evt.Type)
		}
	}
}

func stringPtr(s string) *string { return &s }
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/api/ -v -run TestMessage
```

Expected: FAIL -- types not defined.

- [ ] **Step 3: Implement Anthropic API types**

Create `pkg/api/anthropic.go`:

```go
package api

// ---------------------------------------------------------------------------
// Messages API (Anthropic)
// ---------------------------------------------------------------------------

// MessageRequest matches the Anthropic Messages API request schema.
type MessageRequest struct {
	Model         string             `json:"model"`
	MaxTokens     int                `json:"max_tokens"`
	Messages      []AnthropicMessage `json:"messages"`
	System        string             `json:"system,omitempty"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	TopK          *int               `json:"top_k,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Metadata      *MessageMetadata   `json:"metadata,omitempty"`
	Tools         []AnthropicTool    `json:"tools,omitempty"`
	ToolChoice    interface{}        `json:"tool_choice,omitempty"`
}

// AnthropicMessage is a single message in the Anthropic conversation format.
type AnthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// MessageMetadata contains optional request metadata.
type MessageMetadata struct {
	UserID string `json:"user_id,omitempty"`
}

// AnthropicTool defines a tool in the Anthropic format.
type AnthropicTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema interface{} `json:"input_schema"`
}

// MessageResponse matches the Anthropic Messages API response schema.
type MessageResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Content      []ContentBlock `json:"content"`
	Model        string         `json:"model"`
	StopReason   *string        `json:"stop_reason"`
	StopSequence *string        `json:"stop_sequence"`
	Usage        AnthropicUsage `json:"usage"`
}

// ContentBlock represents a content block in an Anthropic response.
type ContentBlock struct {
	Type  string      `json:"type"`
	Text  string      `json:"text,omitempty"`
	ID    string      `json:"id,omitempty"`
	Name  string      `json:"name,omitempty"`
	Input interface{} `json:"input,omitempty"`
}

// AnthropicUsage tracks token usage for Anthropic responses.
type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ---------------------------------------------------------------------------
// Anthropic Streaming Events
// ---------------------------------------------------------------------------

// MessageStreamEvent represents a single SSE event in the Anthropic stream.
type MessageStreamEvent struct {
	Type         string           `json:"type"`
	Message      *MessageResponse `json:"message,omitempty"`
	Index        *int             `json:"index,omitempty"`
	ContentBlock *ContentBlock    `json:"content_block,omitempty"`
	Delta        *StreamDelta     `json:"delta,omitempty"`
	Usage        *AnthropicUsage  `json:"usage,omitempty"`
}

// StreamDelta holds incremental content in a streaming event.
type StreamDelta struct {
	Type       string  `json:"type"`
	Text       string  `json:"text,omitempty"`
	StopReason *string `json:"stop_reason,omitempty"`
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./pkg/api/ -v -count=1
```

Expected: all tests in `pkg/api/` PASS (both OpenAI and Anthropic).

- [ ] **Step 5: Commit**

```bash
git add pkg/api/anthropic.go pkg/api/anthropic_test.go
git commit -m "feat: add Anthropic Messages API request/response types"
```

---

### Task 4: Internal Types

**Files:**
- Create: `pkg/types/types.go`
- Create: `pkg/types/types_test.go`

- [ ] **Step 1: Write failing tests for internal types**

Create `pkg/types/types_test.go`:

```go
package types_test

import (
	"encoding/json"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

func TestRoleString(t *testing.T) {
	tests := []struct {
		role types.Role
		want string
	}{
		{types.RoleSystem, "system"},
		{types.RoleUser, "user"},
		{types.RoleAssistant, "assistant"},
		{types.RoleTool, "tool"},
	}
	for _, tt := range tests {
		if got := string(tt.role); got != tt.want {
			t.Errorf("Role = %q, want %q", got, tt.want)
		}
	}
}

func TestInternalMessageJSON(t *testing.T) {
	msg := types.InternalMessage{
		Role:    types.RoleUser,
		Content: "Hello",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded types.InternalMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if decoded.Role != types.RoleUser {
		t.Errorf("Role = %q, want %q", decoded.Role, types.RoleUser)
	}
	if decoded.Content != "Hello" {
		t.Errorf("Content = %q, want %q", decoded.Content, "Hello")
	}
}

func TestInternalChatRequestJSON(t *testing.T) {
	temp := 0.7
	maxTok := 100
	req := types.InternalChatRequest{
		Model: "llama-3.1-70b",
		Messages: []types.InternalMessage{
			{Role: types.RoleSystem, Content: "You are helpful."},
			{Role: types.RoleUser, Content: "Hi"},
		},
		Temperature: &temp,
		MaxTokens:   &maxTok,
		Stream:      true,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded types.InternalChatRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if decoded.Model != "llama-3.1-70b" {
		t.Errorf("Model = %q, want %q", decoded.Model, "llama-3.1-70b")
	}
	if len(decoded.Messages) != 2 {
		t.Errorf("Messages count = %d, want 2", len(decoded.Messages))
	}
	if !decoded.Stream {
		t.Error("Stream should be true")
	}
}

func TestInternalChatResponseJSON(t *testing.T) {
	resp := types.InternalChatResponse{
		ID:    "helix-abc123",
		Model: "llama-3.1-70b",
		Message: types.InternalMessage{
			Role:    types.RoleAssistant,
			Content: "Hello! How can I help?",
		},
		FinishReason: "stop",
		Usage: types.InternalUsage{
			PromptTokens:     10,
			CompletionTokens: 8,
			TotalTokens:      18,
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded types.InternalChatResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if decoded.ID != "helix-abc123" {
		t.Errorf("ID = %q, want %q", decoded.ID, "helix-abc123")
	}
	if decoded.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", decoded.FinishReason, "stop")
	}
}

func TestProviderConstants(t *testing.T) {
	providers := []types.Provider{
		types.ProviderLocal,
		types.ProviderOpenAI,
		types.ProviderAnthropic,
	}
	for _, p := range providers {
		if p == "" {
			t.Error("provider constant is empty")
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/types/ -v
```

Expected: FAIL -- package does not exist yet.

- [ ] **Step 3: Implement internal types**

Create `pkg/types/types.go`:

```go
// Package types defines the internal HelixLLM types that gateway handlers
// convert to/from when interacting with the Brain layer.
package types

// ---------------------------------------------------------------------------
// Role
// ---------------------------------------------------------------------------

// Role represents the role of a message sender.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

// Provider identifies an LLM provider backend.
type Provider string

const (
	ProviderLocal     Provider = "local"
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
)

// ---------------------------------------------------------------------------
// Internal Message
// ---------------------------------------------------------------------------

// InternalMessage is the canonical message type used across all HelixLLM layers.
type InternalMessage struct {
	Role       Role        `json:"role"`
	Content    string      `json:"content"`
	Name       string      `json:"name,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
}

// ToolCall represents an internal tool invocation.
type ToolCall struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Arguments string `json:"arguments"`
}

// ---------------------------------------------------------------------------
// Internal Chat Request / Response
// ---------------------------------------------------------------------------

// InternalChatRequest is the canonical chat request passed from the Gateway
// to the Brain layer.
type InternalChatRequest struct {
	Model       string            `json:"model"`
	Messages    []InternalMessage `json:"messages"`
	Temperature *float64          `json:"temperature,omitempty"`
	TopP        *float64          `json:"top_p,omitempty"`
	MaxTokens   *int              `json:"max_tokens,omitempty"`
	Stream      bool              `json:"stream,omitempty"`
	Stop        []string          `json:"stop,omitempty"`
	User        string            `json:"user,omitempty"`
	Provider    Provider          `json:"provider,omitempty"`
}

// InternalChatResponse is the canonical chat response from the Brain layer
// back to the Gateway.
type InternalChatResponse struct {
	ID           string          `json:"id"`
	Model        string          `json:"model"`
	Message      InternalMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
	Usage        InternalUsage   `json:"usage"`
	Provider     Provider        `json:"provider,omitempty"`
}

// InternalUsage tracks token counts in the internal format.
type InternalUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ---------------------------------------------------------------------------
// Internal Streaming Chunk
// ---------------------------------------------------------------------------

// InternalStreamChunk is one piece of a streamed response from the Brain.
type InternalStreamChunk struct {
	ID           string `json:"id"`
	Model        string `json:"model"`
	Delta        string `json:"delta"`
	Role         Role   `json:"role,omitempty"`
	FinishReason string `json:"finish_reason,omitempty"`
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./pkg/types/ -v -count=1
```

Expected: all 5 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/types/
git commit -m "feat: add internal HelixLLM types for cross-layer communication"
```

---

### Task 5: Auth Middleware

**Files:**
- Create: `internal/gateway/middleware/auth.go`
- Create: `internal/gateway/middleware/auth_test.go`

- [ ] **Step 1: Write failing tests for auth middleware**

Create `internal/gateway/middleware/auth_test.go`:

```go
package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway/middleware"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestAuthMiddleware_NoKeysConfigured_AllowsAll(t *testing.T) {
	router := gin.New()
	router.Use(middleware.Auth(""))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAuthMiddleware_ValidKey(t *testing.T) {
	router := gin.New()
	router.Use(middleware.Auth("sk-test-key-1,sk-test-key-2"))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer sk-test-key-1")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAuthMiddleware_InvalidKey(t *testing.T) {
	router := gin.New()
	router.Use(middleware.Auth("sk-test-key-1"))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer sk-wrong-key")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	router := gin.New()
	router.Use(middleware.Auth("sk-test-key-1"))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_MalformedHeader(t *testing.T) {
	router := gin.New()
	router.Use(middleware.Auth("sk-test-key-1"))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Token sk-test-key-1")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_SecondValidKey(t *testing.T) {
	router := gin.New()
	router.Use(middleware.Auth("sk-key-1,sk-key-2"))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer sk-key-2")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/gateway/middleware/ -v
```

Expected: FAIL -- package does not exist yet.

- [ ] **Step 3: Implement auth middleware**

Create `internal/gateway/middleware/auth.go`:

```go
// Package middleware provides Gateway-specific Gin middleware for
// authentication, rate limiting, content negotiation, and security.
package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

// Auth returns a Gin middleware that validates API keys from the
// Authorization header. The apiKeys parameter is a comma-separated list
// of valid keys (from HELIX_AUTH_API_KEYS). If apiKeys is empty, all
// requests are allowed through (open mode).
func Auth(apiKeys string) gin.HandlerFunc {
	keySet := make(map[string]struct{})
	if apiKeys != "" {
		for _, k := range strings.Split(apiKeys, ",") {
			k = strings.TrimSpace(k)
			if k != "" {
				keySet[k] = struct{}{}
			}
		}
	}

	return func(c *gin.Context) {
		// If no keys configured, allow all requests.
		if len(keySet) == 0 {
			c.Next()
			return
		}

		header := c.GetHeader("Authorization")
		if header == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.ErrorResponse{
				Error: api.ErrorDetail{
					Message: "Missing Authorization header. Expected: Authorization: Bearer <api-key>",
					Type:    "invalid_request_error",
				},
			})
			return
		}

		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.ErrorResponse{
				Error: api.ErrorDetail{
					Message: "Invalid Authorization header format. Expected: Bearer <api-key>",
					Type:    "invalid_request_error",
				},
			})
			return
		}

		key := strings.TrimPrefix(header, "Bearer ")
		if _, ok := keySet[key]; !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.ErrorResponse{
				Error: api.ErrorDetail{
					Message: "Invalid API key.",
					Type:    "authentication_error",
				},
			})
			return
		}

		c.Next()
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/gateway/middleware/ -v -count=1
```

Expected: all 6 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/middleware/auth.go internal/gateway/middleware/auth_test.go
git commit -m "feat: add API key auth middleware for Gateway endpoints"
```

---

### Task 6: Rate Limiting Middleware

**Files:**
- Create: `internal/gateway/middleware/ratelimit.go`
- Create: `internal/gateway/middleware/ratelimit_test.go`

- [ ] **Step 1: Write failing tests for rate limiting middleware**

Create `internal/gateway/middleware/ratelimit_test.go`:

```go
package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway/middleware"
)

func TestRateLimiter_AllowsWithinLimit(t *testing.T) {
	router := gin.New()
	router.Use(middleware.RateLimit(10, 10)) // 10 req/s, burst 10
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRateLimiter_BlocksOverLimit(t *testing.T) {
	router := gin.New()
	router.Use(middleware.RateLimit(1, 1)) // 1 req/s, burst 1
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// First request should pass.
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "192.168.1.2:12345"
	router.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("first request: status = %d, want %d", w1.Code, http.StatusOK)
	}

	// Second request should be rate limited.
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.2:12345"
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("second request: status = %d, want %d", w2.Code, http.StatusTooManyRequests)
	}
}

func TestRateLimiter_DifferentIPsIndependent(t *testing.T) {
	router := gin.New()
	router.Use(middleware.RateLimit(1, 1)) // 1 req/s, burst 1
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// IP 1 uses its quota.
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "10.0.0.1:12345"
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Errorf("IP1: status = %d, want %d", w1.Code, http.StatusOK)
	}

	// IP 2 should still be allowed.
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "10.0.0.2:12345"
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("IP2: status = %d, want %d", w2.Code, http.StatusOK)
	}
}

func TestRateLimiter_ReturnRetryAfterHeader(t *testing.T) {
	router := gin.New()
	router.Use(middleware.RateLimit(1, 1))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Exhaust quota.
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "10.0.0.3:12345"
	router.ServeHTTP(w1, req1)

	// Over limit.
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "10.0.0.3:12345"
	router.ServeHTTP(w2, req2)

	if w2.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header on rate limited response")
	}
}

func TestRateLimiter_ZeroDisablesLimiting(t *testing.T) {
	router := gin.New()
	router.Use(middleware.RateLimit(0, 0)) // disabled
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	for i := 0; i < 100; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.4:12345"
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d: status = %d, want %d", i, w.Code, http.StatusOK)
			break
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/gateway/middleware/ -v -run TestRateLimiter
```

Expected: FAIL -- RateLimit function not defined.

- [ ] **Step 3: Implement rate limiting middleware**

Create `internal/gateway/middleware/ratelimit.go`:

```go
package middleware

import (
	"net"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

// rateLimiterEntry holds a per-IP token bucket.
type rateLimiterEntry struct {
	tokens float64
	max    float64
}

// RateLimit returns a Gin middleware that rate limits requests per client IP
// using a simple token bucket. rate is requests per second, burst is the
// maximum burst size. If rate is 0, rate limiting is disabled.
func RateLimit(rate float64, burst int) gin.HandlerFunc {
	if rate <= 0 {
		return func(c *gin.Context) { c.Next() }
	}

	var mu sync.Mutex
	buckets := make(map[string]*rateLimiterEntry)
	burstF := float64(burst)

	return func(c *gin.Context) {
		ip := clientIP(c)

		mu.Lock()
		entry, exists := buckets[ip]
		if !exists {
			entry = &rateLimiterEntry{tokens: burstF, max: burstF}
			buckets[ip] = entry
		}

		if entry.tokens >= 1 {
			entry.tokens--
			mu.Unlock()
			c.Next()
			return
		}
		mu.Unlock()

		c.Header("Retry-After", "1")
		c.AbortWithStatusJSON(http.StatusTooManyRequests, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: "Rate limit exceeded. Please retry after a brief wait.",
				Type:    "rate_limit_error",
			},
		})
	}
}

// clientIP extracts the IP address from the request, stripping the port.
func clientIP(c *gin.Context) string {
	ip := c.ClientIP()
	if ip == "" {
		ip = c.Request.RemoteAddr
	}
	// Strip port if present.
	if host, _, err := net.SplitHostPort(ip); err == nil {
		return host
	}
	return ip
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/gateway/middleware/ -v -count=1 -run TestRateLimiter
```

Expected: all 5 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/middleware/ratelimit.go internal/gateway/middleware/ratelimit_test.go
git commit -m "feat: add per-IP rate limiting middleware for Gateway"
```

---

### Task 7: SSE Streaming Handler

**Files:**
- Create: `internal/gateway/streaming.go`
- Create: `internal/gateway/streaming_test.go`

- [ ] **Step 1: Write failing tests for SSE streaming**

Create `internal/gateway/streaming_test.go`:

```go
package gateway_test

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestSSEWriter_WriteEvent(t *testing.T) {
	router := gin.New()
	router.GET("/stream", func(c *gin.Context) {
		w := gateway.NewSSEWriter(c)
		w.WriteEvent(`{"id":"1","choices":[{"delta":{"content":"Hi"}}]}`)
		w.WriteEvent(`{"id":"1","choices":[{"delta":{"content":"!"}}]}`)
		w.WriteDone()
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/stream", nil)
	router.ServeHTTP(w, req)

	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/event-stream")
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-cache")
	}
	if conn := w.Header().Get("Connection"); conn != "keep-alive" {
		t.Errorf("Connection = %q, want %q", conn, "keep-alive")
	}

	body := w.Body.String()
	scanner := bufio.NewScanner(strings.NewReader(body))
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			lines = append(lines, line)
		}
	}

	// Expect: data: {json1}, data: {json2}, data: [DONE]
	if len(lines) != 3 {
		t.Fatalf("expected 3 data lines, got %d: %v", len(lines), lines)
	}
	if !strings.HasPrefix(lines[0], "data: {") {
		t.Errorf("line[0] = %q, want prefix 'data: {'", lines[0])
	}
	if !strings.HasPrefix(lines[1], "data: {") {
		t.Errorf("line[1] = %q, want prefix 'data: {'", lines[1])
	}
	if lines[2] != "data: [DONE]" {
		t.Errorf("line[2] = %q, want %q", lines[2], "data: [DONE]")
	}
}

func TestSSEWriter_Format(t *testing.T) {
	router := gin.New()
	router.GET("/stream", func(c *gin.Context) {
		w := gateway.NewSSEWriter(c)
		w.WriteEvent(`{"test":true}`)
		w.WriteDone()
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/stream", nil)
	router.ServeHTTP(w, req)

	body := w.Body.String()

	// Each event must be "data: {json}\n\n" (double newline).
	if !strings.Contains(body, "data: {\"test\":true}\n\n") {
		t.Errorf("body missing expected format, got:\n%s", body)
	}
	if !strings.Contains(body, "data: [DONE]\n\n") {
		t.Errorf("body missing [DONE] terminator, got:\n%s", body)
	}
}

func TestSSEWriter_EmptyStream(t *testing.T) {
	router := gin.New()
	router.GET("/stream", func(c *gin.Context) {
		w := gateway.NewSSEWriter(c)
		w.WriteDone()
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/stream", nil)
	router.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "data: [DONE]\n\n") {
		t.Errorf("body should contain [DONE], got:\n%s", body)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/gateway/ -v -run TestSSEWriter
```

Expected: FAIL -- package does not exist yet.

- [ ] **Step 3: Implement SSE streaming**

Create `internal/gateway/streaming.go`:

```go
// Package gateway implements the Gateway API layer for HelixLLM, providing
// OpenAI and Anthropic compatible endpoints, SSE streaming, and middleware.
package gateway

import (
	"fmt"

	"github.com/gin-gonic/gin"
)

// SSEWriter writes Server-Sent Events in the OpenAI text/event-stream format.
// Each event is: "data: {json}\n\n"
// The stream terminates with: "data: [DONE]\n\n"
type SSEWriter struct {
	c *gin.Context
}

// NewSSEWriter creates an SSEWriter and sets the appropriate response headers.
func NewSSEWriter(c *gin.Context) *SSEWriter {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Transfer-Encoding", "chunked")
	return &SSEWriter{c: c}
}

// WriteEvent writes a single SSE data event. The data parameter should be
// a JSON string (already serialized).
func (w *SSEWriter) WriteEvent(data string) {
	fmt.Fprintf(w.c.Writer, "data: %s\n\n", data)
	w.c.Writer.Flush()
}

// WriteDone writes the OpenAI-standard [DONE] termination event.
func (w *SSEWriter) WriteDone() {
	fmt.Fprintf(w.c.Writer, "data: [DONE]\n\n")
	w.c.Writer.Flush()
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/gateway/ -v -count=1 -run TestSSEWriter
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/streaming.go internal/gateway/streaming_test.go
git commit -m "feat: add SSE streaming writer matching OpenAI text/event-stream format"
```

---

### Task 8: OpenAI Endpoint Handlers

**Files:**
- Create: `internal/gateway/openai.go`
- Create: `internal/gateway/openai_test.go`

- [ ] **Step 1: Write failing tests for OpenAI endpoints**

Create `internal/gateway/openai_test.go`:

```go
package gateway_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

func setupOpenAIRouter() *gin.Engine {
	router := gin.New()
	h := gateway.NewOpenAIHandler()
	router.POST("/v1/chat/completions", h.ChatCompletions)
	router.POST("/v1/completions", h.Completions)
	router.GET("/v1/models", h.ListModels)
	router.GET("/v1/models/:id", h.GetModel)
	router.POST("/v1/embeddings", h.Embeddings)
	return router
}

func TestOpenAI_ChatCompletions(t *testing.T) {
	router := setupOpenAIRouter()

	reqBody := api.ChatCompletionRequest{
		Model: "llama-3.1-70b",
		Messages: []api.ChatMessage{
			{Role: "user", Content: "Hello"},
		},
	}
	body, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp api.ChatCompletionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if resp.Object != "chat.completion" {
		t.Errorf("Object = %q, want %q", resp.Object, "chat.completion")
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	if resp.Choices[0].Message.Role != "assistant" {
		t.Errorf("Choices[0].Message.Role = %q, want %q", resp.Choices[0].Message.Role, "assistant")
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", resp.Choices[0].FinishReason, "stop")
	}
}

func TestOpenAI_ChatCompletions_Stream(t *testing.T) {
	router := setupOpenAIRouter()

	reqBody := api.ChatCompletionRequest{
		Model: "llama-3.1-70b",
		Messages: []api.ChatMessage{
			{Role: "user", Content: "Hello"},
		},
		Stream: true,
	}
	body, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/event-stream")
	}

	sseBody := w.Body.String()
	if !strings.Contains(sseBody, "data: [DONE]") {
		t.Error("stream missing [DONE] terminator")
	}
	if !strings.Contains(sseBody, `"object":"chat.completion.chunk"`) {
		t.Error("stream missing chat.completion.chunk objects")
	}
}

func TestOpenAI_ChatCompletions_MissingModel(t *testing.T) {
	router := setupOpenAIRouter()

	reqBody := api.ChatCompletionRequest{
		Messages: []api.ChatMessage{
			{Role: "user", Content: "Hello"},
		},
	}
	body, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestOpenAI_Completions(t *testing.T) {
	router := setupOpenAIRouter()

	reqBody := api.CompletionRequest{
		Model:  "llama-3.1-70b",
		Prompt: "Hello",
	}
	body, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp api.CompletionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if resp.Object != "text_completion" {
		t.Errorf("Object = %q, want %q", resp.Object, "text_completion")
	}
}

func TestOpenAI_ListModels(t *testing.T) {
	router := setupOpenAIRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/models", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp api.ModelList
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if resp.Object != "list" {
		t.Errorf("Object = %q, want %q", resp.Object, "list")
	}
	if len(resp.Data) == 0 {
		t.Error("expected at least one model in list")
	}
}

func TestOpenAI_GetModel(t *testing.T) {
	router := setupOpenAIRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/models/llama-3.1-70b", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp api.Model
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if resp.ID != "llama-3.1-70b" {
		t.Errorf("ID = %q, want %q", resp.ID, "llama-3.1-70b")
	}
}

func TestOpenAI_GetModel_NotFound(t *testing.T) {
	router := setupOpenAIRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/models/nonexistent-model", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestOpenAI_Embeddings(t *testing.T) {
	router := setupOpenAIRouter()

	reqBody := api.EmbeddingRequest{
		Model: "text-embedding-ada-002",
		Input: "Hello world",
	}
	body, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/embeddings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp api.EmbeddingResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if resp.Object != "list" {
		t.Errorf("Object = %q, want %q", resp.Object, "list")
	}
	if len(resp.Data) == 0 {
		t.Fatal("expected at least one embedding")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/gateway/ -v -run TestOpenAI
```

Expected: FAIL -- OpenAIHandler not defined.

- [ ] **Step 3: Implement OpenAI endpoint handlers**

Create `internal/gateway/openai.go`:

```go
package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

// stubModels is the hardcoded model list returned until the Brain layer is wired.
var stubModels = map[string]api.Model{
	"llama-3.1-70b": {
		ID: "llama-3.1-70b", Object: "model",
		Created: 1700000000, OwnedBy: "helixllm",
	},
	"llama-3.1-8b": {
		ID: "llama-3.1-8b", Object: "model",
		Created: 1700000000, OwnedBy: "helixllm",
	},
	"gpt-4o": {
		ID: "gpt-4o", Object: "model",
		Created: 1700000000, OwnedBy: "openai",
	},
	"claude-sonnet-4-20250514": {
		ID: "claude-sonnet-4-20250514", Object: "model",
		Created: 1700000000, OwnedBy: "anthropic",
	},
}

// OpenAIHandler implements OpenAI-compatible API endpoints.
type OpenAIHandler struct{}

// NewOpenAIHandler creates a new OpenAIHandler.
func NewOpenAIHandler() *OpenAIHandler {
	return &OpenAIHandler{}
}

// ChatCompletions handles POST /v1/chat/completions.
func (h *OpenAIHandler) ChatCompletions(c *gin.Context) {
	var req api.ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: fmt.Sprintf("Invalid request body: %v", err),
				Type:    "invalid_request_error",
			},
		})
		return
	}

	if req.Model == "" {
		c.JSON(http.StatusBadRequest, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: "model is required",
				Type:    "invalid_request_error",
			},
		})
		return
	}

	id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	now := time.Now().Unix()

	if req.Stream {
		h.streamChatCompletion(c, id, now, req.Model)
		return
	}

	resp := api.ChatCompletionResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: now,
		Model:   req.Model,
		Choices: []api.ChatChoice{
			{
				Index: 0,
				Message: api.ChatMessage{
					Role:    "assistant",
					Content: "This is a stub response from HelixLLM. The Brain layer (Phase 3) will provide real LLM completions.",
				},
				FinishReason: "stop",
			},
		},
		Usage: &api.Usage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}

	c.JSON(http.StatusOK, resp)
}

func (h *OpenAIHandler) streamChatCompletion(c *gin.Context, id string, created int64, model string) {
	w := NewSSEWriter(c)

	words := []string{"This ", "is ", "a ", "stub ", "streaming ", "response ", "from ", "HelixLLM."}
	for i, word := range words {
		chunk := api.ChatCompletionChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []api.ChatChunkChoice{
				{
					Index: 0,
					Delta: api.ChatDelta{Content: word},
				},
			},
		}
		if i == 0 {
			chunk.Choices[0].Delta.Role = "assistant"
		}
		data, _ := json.Marshal(chunk)
		w.WriteEvent(string(data))
	}

	// Final chunk with finish_reason.
	stop := "stop"
	finalChunk := api.ChatCompletionChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []api.ChatChunkChoice{
			{
				Index:        0,
				Delta:        api.ChatDelta{},
				FinishReason: &stop,
			},
		},
	}
	data, _ := json.Marshal(finalChunk)
	w.WriteEvent(string(data))

	w.WriteDone()
}

// Completions handles POST /v1/completions.
func (h *OpenAIHandler) Completions(c *gin.Context) {
	var req api.CompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: fmt.Sprintf("Invalid request body: %v", err),
				Type:    "invalid_request_error",
			},
		})
		return
	}

	if req.Model == "" {
		c.JSON(http.StatusBadRequest, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: "model is required",
				Type:    "invalid_request_error",
			},
		})
		return
	}

	resp := api.CompletionResponse{
		ID:      fmt.Sprintf("cmpl-%d", time.Now().UnixNano()),
		Object:  "text_completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []api.CompletionChoice{
			{
				Index:        0,
				Text:         "This is a stub completion from HelixLLM.",
				FinishReason: "stop",
			},
		},
		Usage: &api.Usage{
			PromptTokens:     5,
			CompletionTokens: 10,
			TotalTokens:      15,
		},
	}

	c.JSON(http.StatusOK, resp)
}

// ListModels handles GET /v1/models.
func (h *OpenAIHandler) ListModels(c *gin.Context) {
	models := make([]api.Model, 0, len(stubModels))
	for _, m := range stubModels {
		models = append(models, m)
	}

	c.JSON(http.StatusOK, api.ModelList{
		Object: "list",
		Data:   models,
	})
}

// GetModel handles GET /v1/models/:id.
func (h *OpenAIHandler) GetModel(c *gin.Context) {
	id := c.Param("id")
	model, ok := stubModels[id]
	if !ok {
		c.JSON(http.StatusNotFound, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: fmt.Sprintf("The model %q does not exist.", id),
				Type:    "invalid_request_error",
			},
		})
		return
	}

	c.JSON(http.StatusOK, model)
}

// Embeddings handles POST /v1/embeddings.
func (h *OpenAIHandler) Embeddings(c *gin.Context) {
	var req api.EmbeddingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: fmt.Sprintf("Invalid request body: %v", err),
				Type:    "invalid_request_error",
			},
		})
		return
	}

	if req.Model == "" {
		c.JSON(http.StatusBadRequest, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: "model is required",
				Type:    "invalid_request_error",
			},
		})
		return
	}

	// Stub: return a fixed-dimension zero vector.
	dims := 1536
	embedding := make([]float64, dims)

	resp := api.EmbeddingResponse{
		Object: "list",
		Data: []api.Embedding{
			{
				Object:    "embedding",
				Index:     0,
				Embedding: embedding,
			},
		},
		Model: req.Model,
		Usage: &api.EmbeddingUsage{
			PromptTokens: 5,
			TotalTokens:  5,
		},
	}

	c.JSON(http.StatusOK, resp)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/gateway/ -v -count=1 -run TestOpenAI
```

Expected: all 8 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/openai.go internal/gateway/openai_test.go
git commit -m "feat: add OpenAI-compatible stub endpoint handlers with streaming"
```

---

### Task 9: Anthropic Endpoint Handler

**Files:**
- Create: `internal/gateway/anthropic.go`
- Create: `internal/gateway/anthropic_test.go`

- [ ] **Step 1: Write failing tests for Anthropic endpoint**

Create `internal/gateway/anthropic_test.go`:

```go
package gateway_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

func setupAnthropicRouter() *gin.Engine {
	router := gin.New()
	h := gateway.NewAnthropicHandler()
	router.POST("/v1/messages", h.Messages)
	return router
}

func TestAnthropic_Messages(t *testing.T) {
	router := setupAnthropicRouter()

	reqBody := api.MessageRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []api.AnthropicMessage{
			{Role: "user", Content: "Hello, Claude"},
		},
	}
	body, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp api.MessageResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if resp.Type != "message" {
		t.Errorf("Type = %q, want %q", resp.Type, "message")
	}
	if resp.Role != "assistant" {
		t.Errorf("Role = %q, want %q", resp.Role, "assistant")
	}
	if len(resp.Content) == 0 {
		t.Fatal("expected at least one content block")
	}
	if resp.Content[0].Type != "text" {
		t.Errorf("Content[0].Type = %q, want %q", resp.Content[0].Type, "text")
	}
}

func TestAnthropic_Messages_Stream(t *testing.T) {
	router := setupAnthropicRouter()

	reqBody := api.MessageRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []api.AnthropicMessage{
			{Role: "user", Content: "Hello"},
		},
		Stream: true,
	}
	body, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/event-stream")
	}

	sseBody := w.Body.String()
	if !strings.Contains(sseBody, "event: message_start") {
		t.Error("stream missing message_start event")
	}
	if !strings.Contains(sseBody, "event: content_block_delta") {
		t.Error("stream missing content_block_delta event")
	}
	if !strings.Contains(sseBody, "event: message_stop") {
		t.Error("stream missing message_stop event")
	}
}

func TestAnthropic_Messages_MissingModel(t *testing.T) {
	router := setupAnthropicRouter()

	reqBody := api.MessageRequest{
		MaxTokens: 1024,
		Messages: []api.AnthropicMessage{
			{Role: "user", Content: "Hello"},
		},
	}
	body, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAnthropic_Messages_MissingMaxTokens(t *testing.T) {
	router := setupAnthropicRouter()

	reqBody := api.MessageRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []api.AnthropicMessage{
			{Role: "user", Content: "Hello"},
		},
	}
	body, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/gateway/ -v -run TestAnthropic
```

Expected: FAIL -- AnthropicHandler not defined.

- [ ] **Step 3: Implement Anthropic endpoint handler**

Create `internal/gateway/anthropic.go`:

```go
package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

// AnthropicHandler implements Anthropic-compatible API endpoints.
type AnthropicHandler struct{}

// NewAnthropicHandler creates a new AnthropicHandler.
func NewAnthropicHandler() *AnthropicHandler {
	return &AnthropicHandler{}
}

// Messages handles POST /v1/messages.
func (h *AnthropicHandler) Messages(c *gin.Context) {
	var req api.MessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: fmt.Sprintf("Invalid request body: %v", err),
				Type:    "invalid_request_error",
			},
		})
		return
	}

	if req.Model == "" {
		c.JSON(http.StatusBadRequest, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: "model is required",
				Type:    "invalid_request_error",
			},
		})
		return
	}

	if req.MaxTokens == 0 {
		c.JSON(http.StatusBadRequest, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: "max_tokens is required",
				Type:    "invalid_request_error",
			},
		})
		return
	}

	id := fmt.Sprintf("msg_%d", time.Now().UnixNano())

	if req.Stream {
		h.streamMessages(c, id, req.Model)
		return
	}

	stopReason := "end_turn"
	resp := api.MessageResponse{
		ID:         id,
		Type:       "message",
		Role:       "assistant",
		Content:    []api.ContentBlock{{Type: "text", Text: "This is a stub response from HelixLLM. The Brain layer (Phase 3) will provide real Anthropic completions."}},
		Model:      req.Model,
		StopReason: &stopReason,
		Usage:      api.AnthropicUsage{InputTokens: 10, OutputTokens: 25},
	}

	c.JSON(http.StatusOK, resp)
}

func (h *AnthropicHandler) streamMessages(c *gin.Context, id string, model string) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	writeAnthropicEvent := func(eventType string, data interface{}) {
		jsonData, _ := json.Marshal(data)
		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", eventType, string(jsonData))
		c.Writer.Flush()
	}

	// message_start
	writeAnthropicEvent("message_start", api.MessageStreamEvent{
		Type: "message_start",
		Message: &api.MessageResponse{
			ID:    id,
			Type:  "message",
			Role:  "assistant",
			Model: model,
			Usage: api.AnthropicUsage{InputTokens: 10, OutputTokens: 0},
		},
	})

	// content_block_start
	idx := 0
	writeAnthropicEvent("content_block_start", api.MessageStreamEvent{
		Type:         "content_block_start",
		Index:        &idx,
		ContentBlock: &api.ContentBlock{Type: "text"},
	})

	// content_block_delta (stream the stub text word by word)
	words := []string{"This ", "is ", "a ", "stub ", "streaming ", "response ", "from ", "HelixLLM."}
	for _, word := range words {
		writeAnthropicEvent("content_block_delta", api.MessageStreamEvent{
			Type:  "content_block_delta",
			Index: &idx,
			Delta: &api.StreamDelta{Type: "text_delta", Text: word},
		})
	}

	// content_block_stop
	writeAnthropicEvent("content_block_stop", api.MessageStreamEvent{
		Type:  "content_block_stop",
		Index: &idx,
	})

	// message_delta
	stopReason := "end_turn"
	writeAnthropicEvent("message_delta", api.MessageStreamEvent{
		Type:  "message_delta",
		Delta: &api.StreamDelta{Type: "message_delta", StopReason: &stopReason},
		Usage: &api.AnthropicUsage{OutputTokens: 25},
	})

	// message_stop
	writeAnthropicEvent("message_stop", api.MessageStreamEvent{
		Type: "message_stop",
	})
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/gateway/ -v -count=1 -run TestAnthropic
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/anthropic.go internal/gateway/anthropic_test.go
git commit -m "feat: add Anthropic Messages API stub endpoint with streaming"
```

---

### Task 10: Gateway Router

**Files:**
- Create: `internal/gateway/router.go`
- Create: `internal/gateway/router_test.go`
- Modify: `cmd/helixllm/main.go`

- [ ] **Step 1: Write failing tests for gateway router**

Create `internal/gateway/router_test.go`:

```go
package gateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

func TestGatewayRouter_RegistersOpenAIEndpoints(t *testing.T) {
	router := gin.New()
	gw := gateway.NewGateway(gateway.GatewayConfig{})
	gw.Register(router)

	endpoints := []struct {
		method string
		path   string
		code   int
	}{
		{"GET", "/v1/models", http.StatusOK},
	}

	for _, ep := range endpoints {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(ep.method, ep.path, nil)
		router.ServeHTTP(w, req)

		if w.Code != ep.code {
			t.Errorf("%s %s: status = %d, want %d", ep.method, ep.path, w.Code, ep.code)
		}
	}
}

func TestGatewayRouter_ModelsEndpoint(t *testing.T) {
	router := gin.New()
	gw := gateway.NewGateway(gateway.GatewayConfig{})
	gw.Register(router)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/models", nil)
	router.ServeHTTP(w, req)

	var resp api.ModelList
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if resp.Object != "list" {
		t.Errorf("Object = %q, want %q", resp.Object, "list")
	}
}

func TestGatewayRouter_AuthProtectsEndpoints(t *testing.T) {
	router := gin.New()
	gw := gateway.NewGateway(gateway.GatewayConfig{
		APIKeys: "sk-secret-key",
	})
	gw.Register(router)

	// Without auth header.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/models", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	// With valid auth header.
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/v1/models", nil)
	req2.Header.Set("Authorization", "Bearer sk-secret-key")
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("authenticated: status = %d, want %d", w2.Code, http.StatusOK)
	}
}

func TestGatewayRouter_NoAuthWhenNoKeys(t *testing.T) {
	router := gin.New()
	gw := gateway.NewGateway(gateway.GatewayConfig{
		APIKeys: "",
	})
	gw.Register(router)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/models", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("no-auth mode: status = %d, want %d", w.Code, http.StatusOK)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/gateway/ -v -run TestGatewayRouter
```

Expected: FAIL -- Gateway type not defined.

- [ ] **Step 3: Implement gateway router**

Create `internal/gateway/router.go`:

```go
package gateway

import (
	"github.com/gin-gonic/gin"

	gwmw "github.com/HelixDevelopment/HelixLLM/internal/gateway/middleware"
)

// GatewayConfig holds configuration for the Gateway layer.
type GatewayConfig struct {
	// APIKeys is a comma-separated list of valid API keys (from HELIX_AUTH_API_KEYS).
	// If empty, authentication is disabled (open mode).
	APIKeys string

	// RateLimit is the maximum requests per second per IP. 0 disables.
	RateLimit float64

	// RateBurst is the maximum burst size for rate limiting.
	RateBurst int
}

// Gateway wires together all Gateway endpoints and middleware.
type Gateway struct {
	cfg       GatewayConfig
	openai    *OpenAIHandler
	anthropic *AnthropicHandler
}

// NewGateway creates a new Gateway with the given configuration.
func NewGateway(cfg GatewayConfig) *Gateway {
	return &Gateway{
		cfg:       cfg,
		openai:    NewOpenAIHandler(),
		anthropic: NewAnthropicHandler(),
	}
}

// Register adds all Gateway routes and middleware to the Gin engine.
func (g *Gateway) Register(router *gin.Engine) {
	// API route group with auth and rate limiting.
	v1 := router.Group("/v1")
	v1.Use(gwmw.Auth(g.cfg.APIKeys))

	if g.cfg.RateLimit > 0 {
		v1.Use(gwmw.RateLimit(g.cfg.RateLimit, g.cfg.RateBurst))
	}

	// OpenAI-compatible endpoints.
	v1.POST("/chat/completions", g.openai.ChatCompletions)
	v1.POST("/completions", g.openai.Completions)
	v1.GET("/models", g.openai.ListModels)
	v1.GET("/models/:id", g.openai.GetModel)
	v1.POST("/embeddings", g.openai.Embeddings)

	// Anthropic-compatible endpoints.
	v1.POST("/messages", g.anthropic.Messages)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/gateway/ -v -count=1 -run TestGatewayRouter
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Update cmd/helixllm/main.go to use gateway router**

Modify `cmd/helixllm/main.go` to replace the placeholder route with the Gateway router. Replace the placeholder route block:

```go
	// Placeholder route — Phase 2 will add real OpenAI/Anthropic compat routes
	srv.Router().GET("/v1/models", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"object": "list",
			"data":   []interface{}{},
		})
	})
```

With:

```go
	// Gateway layer — OpenAI and Anthropic compatible API endpoints.
	gw := gateway.NewGateway(gateway.GatewayConfig{
		APIKeys:   cfg.Auth.APIKeys,
		RateLimit: 0, // Disabled by default; configure via env in production.
		RateBurst: 0,
	})
	gw.Register(srv.Router())
```

And add the import:

```go
	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
```

Remove the unused `"github.com/gin-gonic/gin"` import since Gin is no longer used directly in main.

- [ ] **Step 6: Verify build**

```bash
go build ./cmd/helixllm/
```

Expected: builds successfully.

- [ ] **Step 7: Commit**

```bash
git add internal/gateway/router.go internal/gateway/router_test.go cmd/helixllm/main.go
git commit -m "feat: add Gateway router wiring all endpoints and update main.go"
```

---

### Task 11: Content Negotiation Middleware

**Files:**
- Create: `internal/gateway/middleware/negotiation.go`
- Create: `internal/gateway/middleware/negotiation_test.go`

- [ ] **Step 1: Write failing tests for content negotiation**

Create `internal/gateway/middleware/negotiation_test.go`:

```go
package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway/middleware"
)

func TestContentNegotiation_DefaultJSON(t *testing.T) {
	router := gin.New()
	router.Use(middleware.ContentNegotiation())
	router.GET("/test", func(c *gin.Context) {
		format, _ := c.Get("response_format")
		c.JSON(http.StatusOK, gin.H{"format": format})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	// Default should be JSON.
	body := w.Body.String()
	if !contains(body, `"format":"json"`) {
		t.Errorf("body = %s, expected format to be json", body)
	}
}

func TestContentNegotiation_AcceptJSON(t *testing.T) {
	router := gin.New()
	router.Use(middleware.ContentNegotiation())
	router.GET("/test", func(c *gin.Context) {
		format, _ := c.Get("response_format")
		c.JSON(http.StatusOK, gin.H{"format": format})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept", "application/json")
	router.ServeHTTP(w, req)

	body := w.Body.String()
	if !contains(body, `"format":"json"`) {
		t.Errorf("body = %s, expected format to be json", body)
	}
}

func TestContentNegotiation_AcceptTOON(t *testing.T) {
	router := gin.New()
	router.Use(middleware.ContentNegotiation())
	router.GET("/test", func(c *gin.Context) {
		format, _ := c.Get("response_format")
		c.JSON(http.StatusOK, gin.H{"format": format})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept", "application/toon")
	router.ServeHTTP(w, req)

	body := w.Body.String()
	if !contains(body, `"format":"toon"`) {
		t.Errorf("body = %s, expected format to be toon", body)
	}
}

func TestContentNegotiation_AcceptWildcard(t *testing.T) {
	router := gin.New()
	router.Use(middleware.ContentNegotiation())
	router.GET("/test", func(c *gin.Context) {
		format, _ := c.Get("response_format")
		c.JSON(http.StatusOK, gin.H{"format": format})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept", "*/*")
	router.ServeHTTP(w, req)

	body := w.Body.String()
	if !contains(body, `"format":"json"`) {
		t.Errorf("body = %s, expected default to be json for wildcard", body)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/gateway/middleware/ -v -run TestContentNegotiation
```

Expected: FAIL -- ContentNegotiation not defined.

- [ ] **Step 3: Implement content negotiation middleware**

Create `internal/gateway/middleware/negotiation.go`:

```go
package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// ContentNegotiation sets the "response_format" context key based on the
// Accept header. Supported formats:
//   - "application/toon" -> "toon"
//   - "application/json" or default -> "json"
//
// Handlers can read c.GetString("response_format") to decide serialization.
// Full TOON serialization requires toon-go; this middleware only signals intent.
func ContentNegotiation() gin.HandlerFunc {
	return func(c *gin.Context) {
		accept := c.GetHeader("Accept")
		format := "json" // default

		if strings.Contains(accept, "application/toon") {
			format = "toon"
		}

		c.Set("response_format", format)
		c.Next()
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/gateway/middleware/ -v -count=1 -run TestContentNegotiation
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/middleware/negotiation.go internal/gateway/middleware/negotiation_test.go
git commit -m "feat: add content negotiation middleware for TOON/JSON via Accept header"
```

---

### Task 12: Security Headers Middleware

**Files:**
- Create: `internal/gateway/middleware/security.go`
- Create: `internal/gateway/middleware/security_test.go`

- [ ] **Step 1: Write failing tests for security headers**

Create `internal/gateway/middleware/security_test.go`:

```go
package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway/middleware"
)

func TestSecurityHeaders_SetsAllHeaders(t *testing.T) {
	router := gin.New()
	router.Use(middleware.SecurityHeaders())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	expectedHeaders := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":       "DENY",
		"X-XSS-Protection":      "1; mode=block",
		"Strict-Transport-Security": "max-age=63072000; includeSubDomains",
		"Content-Security-Policy":   "default-src 'none'",
		"Referrer-Policy":           "no-referrer",
	}

	for header, expected := range expectedHeaders {
		got := w.Header().Get(header)
		if got != expected {
			t.Errorf("%s = %q, want %q", header, got, expected)
		}
	}
}

func TestSecurityHeaders_DoesNotOverrideExisting(t *testing.T) {
	router := gin.New()
	router.Use(middleware.SecurityHeaders())
	router.GET("/test", func(c *gin.Context) {
		// Handler sets its own CSP.
		c.Header("Content-Security-Policy", "default-src 'self'")
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	// The middleware sets headers before the handler runs (via c.Next()),
	// but the handler can override them.
	csp := w.Header().Get("Content-Security-Policy")
	if csp != "default-src 'self'" {
		t.Errorf("CSP = %q, want %q (handler should override)", csp, "default-src 'self'")
	}
}

func TestSecurityHeaders_PassesThroughHandler(t *testing.T) {
	router := gin.New()
	router.Use(middleware.SecurityHeaders())

	called := false
	router.GET("/test", func(c *gin.Context) {
		called = true
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	if !called {
		t.Error("handler was not called")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/gateway/middleware/ -v -run TestSecurityHeaders
```

Expected: FAIL -- SecurityHeaders not defined.

- [ ] **Step 3: Implement security headers middleware**

Create `internal/gateway/middleware/security.go`:

```go
package middleware

import (
	"github.com/gin-gonic/gin"
)

// SecurityHeaders returns a Gin middleware that sets standard HTTP security
// headers on every response. These headers help protect against common web
// vulnerabilities (XSS, clickjacking, MIME-sniffing, etc.).
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		c.Header("Content-Security-Policy", "default-src 'none'")
		c.Header("Referrer-Policy", "no-referrer")
		c.Next()
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/gateway/middleware/ -v -count=1 -run TestSecurityHeaders
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/middleware/security.go internal/gateway/middleware/security_test.go
git commit -m "feat: add HTTP security headers middleware for Gateway"
```

---

## Summary

After completing all 12 tasks, the Gateway layer provides:

1. **OpenAI-compatible API** at `/v1/chat/completions`, `/v1/completions`, `/v1/models`, `/v1/models/:id`, `/v1/embeddings` -- all returning properly formatted stub responses
2. **Anthropic-compatible API** at `/v1/messages` -- with stub responses in the correct format
3. **SSE streaming** matching OpenAI's `data: {json}\n\n` + `data: [DONE]\n\n` format and Anthropic's `event: type\ndata: {json}\n\n` format
4. **API key authentication** via `Authorization: Bearer sk-...` with configurable key list
5. **Rate limiting** per client IP with token bucket
6. **Content negotiation** between TOON and JSON via Accept header
7. **HTTP security headers** (HSTS, CSP, X-Frame-Options, etc.)
8. **Internal types** for cross-layer communication (ready for Phase 3 Brain integration)

All endpoints are stubs. Phase 3 (Brain) will replace stubs with real LLM routing through `digital.vasic.llmprovider`.
