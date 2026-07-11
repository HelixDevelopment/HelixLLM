package gateway_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
	"github.com/gin-gonic/gin"
)

func TestMessagesNonStreaming(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/messages", gateway.HandleMessages(nil))

	body, _ := json.Marshal(api.MessageRequest{
		Model:     "claude-sonnet-4-20250514",
		Messages:  []api.AnthropicMessage{{Role: "user", Content: "Hello"}},
		MaxTokens: 1024,
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp api.MessageResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Type != "message" {
		t.Errorf("type = %q, want message", resp.Type)
	}
	if resp.Role != "assistant" {
		t.Errorf("role = %q, want assistant", resp.Role)
	}
	if len(resp.Content) == 0 {
		t.Error("no content blocks")
	}
}

func TestMessagesStreaming(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/messages", gateway.HandleMessages(nil))

	body, _ := json.Marshal(api.MessageRequest{
		Model:     "claude-sonnet-4-20250514",
		Messages:  []api.AnthropicMessage{{Role: "user", Content: "Hello"}},
		MaxTokens: 1024,
		Stream:    true,
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q", w.Header().Get("Content-Type"))
	}

	responseBody := w.Body.String()
	if !strings.Contains(responseBody, "event: message_start") {
		t.Error("missing message_start event")
	}
	if !strings.Contains(responseBody, "event: message_stop") {
		t.Error("missing message_stop event")
	}
}

// ---------------------------------------------------------------------------
// Brain-backed HandleMessages tests
// ---------------------------------------------------------------------------

func TestMessages_WithBrain_NonStreaming(t *testing.T) {
	mock := &mockBrainProvider{
		name:      "anthropic",
		available: true,
		models:    []string{"claude-opus-4-5"},
		response: &types.InternalChatResponse{
			ID:           "msg-brain-1",
			Model:        "claude-opus-4-5",
			Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "Brain anthropic reply"},
			FinishReason: "end_turn",
			Provider:     types.ProviderAnthropic,
		},
	}
	b := newTestBrain(mock)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/messages", gateway.HandleMessages(b))

	body, _ := json.Marshal(api.MessageRequest{
		Model:     "claude-opus-4-5",
		Messages:  []api.AnthropicMessage{{Role: "user", Content: "Hello"}},
		MaxTokens: 1024,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}

	var resp api.MessageResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Type != "message" {
		t.Errorf("type = %q, want message", resp.Type)
	}
	if resp.Role != "assistant" {
		t.Errorf("role = %q, want assistant", resp.Role)
	}
	if len(resp.Content) == 0 {
		t.Fatal("no content blocks")
	}
	if resp.Content[0].Text != "Brain anthropic reply" {
		t.Errorf("text = %q, want %q", resp.Content[0].Text, "Brain anthropic reply")
	}
}

func TestMessages_BadJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/messages", gateway.HandleMessages(nil))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestMessages_WithBrain_Error(t *testing.T) {
	mock := &mockBrainProvider{
		name:      "anthropic",
		available: true,
		models:    []string{"claude-opus-4-5"},
		err:       fmt.Errorf("brain error"),
	}
	b := newTestBrain(mock)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/messages", gateway.HandleMessages(b))

	body, _ := json.Marshal(api.MessageRequest{
		Model:     "claude-opus-4-5",
		Messages:  []api.AnthropicMessage{{Role: "user", Content: "Hello"}},
		MaxTokens: 1024,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 500 {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestMessages_WithBrain_StreamError(t *testing.T) {
	mock := &mockBrainProvider{
		name:      "anthropic",
		available: true,
		models:    []string{"claude-opus-4-5"},
		err:       fmt.Errorf("stream error"),
	}
	b := newTestBrain(mock)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/messages", gateway.HandleMessages(b))

	body, _ := json.Marshal(api.MessageRequest{
		Model:     "claude-opus-4-5",
		Messages:  []api.AnthropicMessage{{Role: "user", Content: "Hello"}},
		MaxTokens: 1024,
		Stream:    true,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 500 {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestMessages_DefaultModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/messages", gateway.HandleMessages(nil))

	body, _ := json.Marshal(api.MessageRequest{
		Messages:  []api.AnthropicMessage{{Role: "user", Content: "Hello"}},
		MaxTokens: 1024,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp api.MessageResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Model != "claude-sonnet-4-20250514" {
		t.Errorf("default model = %q, want claude-sonnet-4-20250514", resp.Model)
	}
}

func TestMessages_WithBrain_SystemPrompt(t *testing.T) {
	mock := &mockBrainProvider{
		name:      "anthropic",
		available: true,
		models:    []string{"claude-opus-4-5"},
		response: &types.InternalChatResponse{
			ID:           "msg-sys-1",
			Model:        "claude-opus-4-5",
			Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "System aware reply"},
			FinishReason: "end_turn",
			Provider:     types.ProviderAnthropic,
		},
	}
	b := newTestBrain(mock)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/messages", gateway.HandleMessages(b))

	body, _ := json.Marshal(api.MessageRequest{
		Model:     "claude-opus-4-5",
		Messages:  []api.AnthropicMessage{{Role: "user", Content: "Hello"}},
		MaxTokens: 1024,
		System:    "You are a helpful assistant.",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// TestMessages_anthropicToInternal_Temperature exercises the Temperature branch
// of anthropicToInternal (when req.Temperature is non-nil).
func TestMessages_WithBrain_Temperature(t *testing.T) {
	mock := &mockBrainProvider{
		name:      "anthropic",
		available: true,
		models:    []string{"claude-opus-4-5"},
		response: &types.InternalChatResponse{
			ID:           "msg-temp-1",
			Model:        "claude-opus-4-5",
			Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "Temp reply"},
			FinishReason: "end_turn",
			Provider:     types.ProviderAnthropic,
		},
	}
	b := newTestBrain(mock)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/messages", gateway.HandleMessages(b))

	temp := float64(0.8)
	body, _ := json.Marshal(api.MessageRequest{
		Model:       "claude-opus-4-5",
		Messages:    []api.AnthropicMessage{{Role: "user", Content: "Hello"}},
		MaxTokens:   512,
		Temperature: &temp,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
}

// TestMessages_internalToAnthropic_EmptyIDAndModel exercises the branches in
// internalToAnthropic that fall back to the provided id and model when the
// response fields are empty.
func TestMessages_internalToAnthropic_EmptyResponseFields(t *testing.T) {
	mock := &mockBrainProvider{
		name:      "anthropic",
		available: true,
		models:    []string{"claude-haiku-4-5"},
		response: &types.InternalChatResponse{
			// ID and Model intentionally blank — handler uses fallback values.
			ID:           "",
			Model:        "",
			Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "Fallback"},
			FinishReason: "", // empty → "end_turn"
			Provider:     types.ProviderAnthropic,
		},
	}
	b := newTestBrain(mock)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/messages", gateway.HandleMessages(b))

	body, _ := json.Marshal(api.MessageRequest{
		Model:     "claude-haiku-4-5",
		Messages:  []api.AnthropicMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 256,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp api.MessageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.StopReason == nil || *resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %v, want end_turn", resp.StopReason)
	}
}

// TestMessages_anthropicToInternal_NonStringContent exercises the non-string
// content branch in anthropicToInternal.
func TestMessages_NonStringContent(t *testing.T) {
	mock := &mockBrainProvider{
		name:      "anthropic",
		available: true,
		models:    []string{"claude-opus-4-5"},
		response: &types.InternalChatResponse{
			ID:           "msg-nsc-1",
			Model:        "claude-opus-4-5",
			Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "OK"},
			FinishReason: "end_turn",
		},
	}
	b := newTestBrain(mock)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/messages", gateway.HandleMessages(b))

	// Build request with a non-string Content block (map with type/text).
	rawBody := `{"model":"claude-opus-4-5","max_tokens":1024,"messages":[{"role":"user","content":[{"type":"text","text":"Hello"}]}]}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// The handler should still return 200 (content falls through to empty string for non-string).
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestMessages_WithBrain_Streaming(t *testing.T) {
	mock := &mockBrainProvider{
		name:      "anthropic",
		available: true,
		models:    []string{"claude-opus-4-5"},
		chunks: []types.StreamChunk{
			{Content: "Hello"},
			{Content: " from brain", FinishReason: "end_turn"},
		},
	}
	b := newTestBrain(mock)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/messages", gateway.HandleMessages(b))

	body, _ := json.Marshal(api.MessageRequest{
		Model:     "claude-opus-4-5",
		Messages:  []api.AnthropicMessage{{Role: "user", Content: "Hello"}},
		MaxTokens: 1024,
		Stream:    true,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", w.Header().Get("Content-Type"))
	}
	bodyStr := w.Body.String()
	if !strings.Contains(bodyStr, "event: message_start") {
		t.Error("missing message_start event")
	}
	if !strings.Contains(bodyStr, "event: message_stop") {
		t.Error("missing message_stop event")
	}
	if !strings.Contains(bodyStr, "Hello") {
		t.Error("stream missing expected content token")
	}
}

func TestMessages_WithBrain_Streaming_NoFinishReason(t *testing.T) {
	// Stream where no chunk has FinishReason → stopReason stays "" and gets
	// replaced with "end_turn" (covers the stopReason == "" branch).
	mock := &mockBrainProvider{
		name:      "anthropic",
		available: true,
		models:    []string{"claude-opus-4-5"},
		chunks: []types.StreamChunk{
			{Content: "Hello"},
			{Content: " world"}, // no FinishReason on any chunk
		},
	}
	b := newTestBrain(mock)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/messages", gateway.HandleMessages(b))

	body, _ := json.Marshal(api.MessageRequest{
		Model:     "claude-opus-4-5",
		Messages:  []api.AnthropicMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 1024,
		Stream:    true,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", w.Header().Get("Content-Type"))
	}
	bodyStr := w.Body.String()
	// The default stop reason "end_turn" should appear in the message_delta event.
	if !strings.Contains(bodyStr, "end_turn") {
		t.Error("expected default stop reason 'end_turn' in stream output")
	}
}

// ---------------------------------------------------------------------------
// §11.4.135 standing regression guard: ANTHROPIC-WIRE-DROPS-TOOLS.
//
// Forensic anchor (V&V finding, 2026-07-11 wave-2): the Anthropic
// /v1/messages facade accepted tools/tool_choice in its schema
// (api.MessageRequest.Tools / ToolChoice) but anthropicToInternal silently
// dropped them — the OpenAI wire (openAIToInternal/internalToOpenAI) did
// tools end-to-end, the Anthropic wire did not. Unexported-function-level
// coverage lives in anthropic_internal_test.go (package gateway); this is
// the full-facade HTTP-level production-path proof: a request with Tools
// defined reaches the brain with Tools populated, and a brain response
// carrying ToolCalls comes back out of POST /v1/messages as a real
// "tool_use" content block with stop_reason "tool_use" (the live-coder
// proof lives in internal/gateway/anthropic_tools_live_test.go).
// ---------------------------------------------------------------------------

func TestMessages_WithBrain_ToolCallRoundTrip(t *testing.T) {
	mock := &mockBrainProvider{
		name:      "anthropic",
		available: true,
		models:    []string{"claude-opus-4-5"},
		response: &types.InternalChatResponse{
			ID:    "msg-tool-1",
			Model: "claude-opus-4-5",
			Message: types.InternalMessage{
				Role: types.RoleAssistant,
				ToolCalls: []types.InternalToolCall{
					{
						ID:   "toolu_round_trip",
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{Name: "get_weather", Arguments: `{"city":"Paris"}`},
					},
				},
			},
			FinishReason: "tool_calls",
		},
	}
	b := newTestBrain(mock)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/messages", gateway.HandleMessages(b))

	body, _ := json.Marshal(api.MessageRequest{
		Model:     "claude-opus-4-5",
		Messages:  []api.AnthropicMessage{{Role: "user", Content: "What's the weather in Paris? Use the get_weather tool."}},
		MaxTokens: 512,
		Tools: []api.AnthropicTool{
			{
				Name:        "get_weather",
				Description: "Get the current weather for a city",
				InputSchema: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
				},
			},
		},
		ToolChoice: map[string]interface{}{"type": "any"},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}

	// PRODUCTION-PATH PROOF (request direction): the brain must have
	// received the tools the client sent.
	if mock.capturedReq == nil {
		t.Fatal("brain never received a request")
	}
	if len(mock.capturedReq.Tools) != 1 {
		t.Fatalf("brain received %d tools, want 1 — Tools were dropped en route to the brain (ANTHROPIC-WIRE-DROPS-TOOLS)", len(mock.capturedReq.Tools))
	}
	if mock.capturedReq.ToolChoice != "required" {
		t.Errorf("brain received ToolChoice=%v, want %q", mock.capturedReq.ToolChoice, "required")
	}

	// PRODUCTION-PATH PROOF (response direction): the HTTP response must
	// carry a real tool_use content block, not silently drop it.
	var resp api.MessageResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.StopReason == nil || *resp.StopReason != "tool_use" {
		t.Fatalf("response StopReason = %v, want tool_use", resp.StopReason)
	}
	found := false
	for _, block := range resp.Content {
		if block.Type == "tool_use" && block.Name == "get_weather" && block.ID == "toolu_round_trip" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no tool_use block for get_weather found in response content=%+v — the tool call did not round-trip through POST /v1/messages", resp.Content)
	}
}
