package api_test

import (
	"encoding/json"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

func TestMessageRequestJSON(t *testing.T) {
	req := api.MessageRequest{
		Model:     "claude-sonnet-4-20250514",
		Messages:  []api.AnthropicMessage{{Role: "user", Content: "Hello"}},
		MaxTokens: 1024,
		Stream:    true,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded api.MessageRequest
	json.Unmarshal(data, &decoded)
	if decoded.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want %q", decoded.Model, "claude-sonnet-4-20250514")
	}
	if decoded.MaxTokens != 1024 {
		t.Errorf("MaxTokens = %d, want 1024", decoded.MaxTokens)
	}
}

func TestMessageResponseJSON(t *testing.T) {
	stop := "end_turn"
	resp := api.MessageResponse{
		ID:   "msg_123",
		Type: "message",
		Role: "assistant",
		Content: []api.ContentBlock{
			{Type: "text", Text: "Hello!"},
		},
		Model:      "claude-sonnet-4-20250514",
		StopReason: &stop,
		Usage:      api.AnthropicUsage{InputTokens: 10, OutputTokens: 5},
	}

	data, _ := json.Marshal(resp)
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	if raw["type"] != "message" {
		t.Errorf("type = %v, want message", raw["type"])
	}
}

func TestMessageStreamEventJSON(t *testing.T) {
	evt := api.MessageStreamEvent{
		Type: "content_block_delta",
		Delta: &api.StreamDelta{
			Type: "text_delta",
			Text: "Hello",
		},
	}

	data, _ := json.Marshal(evt)
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	if raw["type"] != "content_block_delta" {
		t.Errorf("type = %v, want content_block_delta", raw["type"])
	}
}
