package brain_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

func TestAnthropicProvider_Satisfies(t *testing.T) {
	var _ brain.Provider = (*brain.AnthropicProvider)(nil)
}

func TestAnthropicProvider_Name(t *testing.T) {
	p := brain.NewAnthropic("sk-ant-test", "")
	if p.Name() != "anthropic" {
		t.Errorf("Name() = %q, want %q", p.Name(), "anthropic")
	}
}

func TestAnthropicProvider_Models(t *testing.T) {
	p := brain.NewAnthropic("sk-ant-test", "")
	models := p.Models()
	if len(models) == 0 {
		t.Fatal("Models() returned empty slice")
	}
}

func TestAnthropicProvider_Available_WithKey(t *testing.T) {
	p := brain.NewAnthropic("sk-ant-test", "")
	if !p.Available() {
		t.Error("Available() = false, want true (API key is set)")
	}
}

func TestAnthropicProvider_Available_WithoutKey(t *testing.T) {
	p := brain.NewAnthropic("", "")
	if p.Available() {
		t.Error("Available() = true, want false (no API key)")
	}
}

func TestAnthropicProvider_Complete(t *testing.T) {
	stopReason := "end_turn"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Verify required Anthropic headers.
		if r.Header.Get("x-api-key") == "" {
			t.Error("x-api-key header is missing")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("anthropic-version header is missing")
		}

		var req api.MessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Model != "claude-sonnet-4-5" {
			t.Errorf("request model = %q, want %q", req.Model, "claude-sonnet-4-5")
		}

		resp := api.MessageResponse{
			ID:   "msg-anthropic-001",
			Type: "message",
			Role: "assistant",
			Content: []api.ContentBlock{
				{Type: "text", Text: "Hello from Anthropic!"},
			},
			Model:      "claude-sonnet-4-5",
			StopReason: &stopReason,
			Usage: api.AnthropicUsage{
				InputTokens:  10,
				OutputTokens: 5,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := brain.NewAnthropic("sk-ant-test", srv.URL)

	resp, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model: "claude-sonnet-4-5",
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.ID != "msg-anthropic-001" {
		t.Errorf("ID = %q, want %q", resp.ID, "msg-anthropic-001")
	}
	if resp.Message.Content != "Hello from Anthropic!" {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "Hello from Anthropic!")
	}
	if resp.Provider != types.ProviderAnthropic {
		t.Errorf("Provider = %q, want %q", resp.Provider, types.ProviderAnthropic)
	}
	if resp.FinishReason != "end_turn" {
		t.Errorf("FinishReason = %q, want %q", resp.FinishReason, "end_turn")
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("Usage.PromptTokens = %d, want 10", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 5 {
		t.Errorf("Usage.CompletionTokens = %d, want 5", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("Usage.TotalTokens = %d, want 15", resp.Usage.TotalTokens)
	}
}

func TestAnthropicProvider_CompleteWithSystemMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req api.MessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// The system message must be in the top-level system field.
		if req.System != "You are a helpful assistant." {
			t.Errorf("System = %q, want %q", req.System, "You are a helpful assistant.")
		}
		// The messages slice must not contain the system message.
		for _, m := range req.Messages {
			if m.Role == "system" {
				t.Error("system message found in messages array, should be in top-level system field")
			}
		}

		stopReason := "end_turn"
		resp := api.MessageResponse{
			ID:   "msg-sys-001",
			Type: "message",
			Role: "assistant",
			Content: []api.ContentBlock{
				{Type: "text", Text: "Sure!"},
			},
			Model:      "claude-sonnet-4-5",
			StopReason: &stopReason,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := brain.NewAnthropic("sk-ant-test", srv.URL)

	_, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model: "claude-sonnet-4-5",
		Messages: []types.InternalMessage{
			{Role: types.RoleSystem, Content: "You are a helpful assistant."},
			{Role: types.RoleUser, Content: "Help me."},
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
}

func TestAnthropicProvider_CompleteStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req api.MessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if !req.Stream {
			t.Error("expected stream=true in request")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)

		// Write Anthropic-format SSE events.
		writeEvent := func(event, data string) {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
			flusher.Flush()
		}

		writeEvent("message_start", `{"type":"message_start","message":{"id":"msg-stream-001","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-5","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`)
		writeEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		writeEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`)
		writeEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" from Anthropic"}}`)
		writeEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`)
		writeEvent("message_stop", `{"type":"message_stop"}`)
	}))
	defer srv.Close()

	p := brain.NewAnthropic("sk-ant-test", srv.URL)

	ch, err := p.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model: "claude-sonnet-4-5",
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "Hello"},
		},
		Stream: true,
	})
	if err != nil {
		t.Fatalf("CompleteStream returned error: %v", err)
	}

	var chunks []types.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if chunks[0].Content != "Hello" {
		t.Errorf("chunks[0].Content = %q, want %q", chunks[0].Content, "Hello")
	}
	if chunks[1].Content != " from Anthropic" {
		t.Errorf("chunks[1].Content = %q, want %q", chunks[1].Content, " from Anthropic")
	}
}

func TestAnthropicProvider_CompleteError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := brain.NewAnthropic("sk-ant-test", srv.URL)

	_, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error from 500 response, got nil")
	}
}
