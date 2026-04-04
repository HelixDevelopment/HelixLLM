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
