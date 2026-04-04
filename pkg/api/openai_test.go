package api_test

import (
	"encoding/json"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

func TestChatCompletionRequestJSON(t *testing.T) {
	temp := 0.7
	req := api.ChatCompletionRequest{
		Model: "llama-3.1-70b",
		Messages: []api.ChatMessage{
			{Role: "user", Content: "Hello"},
		},
		Temperature: &temp,
		Stream:      true,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded api.ChatCompletionRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.Model != "llama-3.1-70b" {
		t.Errorf("Model = %q, want %q", decoded.Model, "llama-3.1-70b")
	}
	if len(decoded.Messages) != 1 || decoded.Messages[0].Role != "user" {
		t.Error("Messages not preserved")
	}
	if !decoded.Stream {
		t.Error("Stream should be true")
	}
}

func TestChatCompletionResponseJSON(t *testing.T) {
	resp := api.ChatCompletionResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion",
		Created: 1700000000,
		Model:   "llama-3.1-70b",
		Choices: []api.ChatCompletionChoice{
			{
				Index:        0,
				Message:      api.ChatMessage{Role: "assistant", Content: "Hi there!"},
				FinishReason: "stop",
			},
		},
		Usage: &api.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Verify key fields in JSON
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	if raw["object"] != "chat.completion" {
		t.Errorf("object = %v, want chat.completion", raw["object"])
	}
}

func TestModelListJSON(t *testing.T) {
	list := api.ModelList{
		Object: "list",
		Data: []api.Model{
			{ID: "llama-3.1-70b", Object: "model", Created: 1700000000, OwnedBy: "local"},
		},
	}

	data, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded api.ModelList
	json.Unmarshal(data, &decoded)
	if len(decoded.Data) != 1 || decoded.Data[0].ID != "llama-3.1-70b" {
		t.Error("Model list not preserved")
	}
}

func TestErrorResponseJSON(t *testing.T) {
	resp := api.ErrorResponse{
		Error: api.ErrorDetail{
			Message: "Invalid API key",
			Type:    "invalid_request_error",
		},
	}

	data, _ := json.Marshal(resp)
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)

	errObj := raw["error"].(map[string]interface{})
	if errObj["message"] != "Invalid API key" {
		t.Error("Error message not preserved")
	}
}
