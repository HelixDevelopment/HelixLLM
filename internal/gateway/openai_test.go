package gateway_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/gin-gonic/gin"
)

func setupOpenAIRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/chat/completions", gateway.HandleChatCompletions)
	r.POST("/v1/completions", gateway.HandleCompletions)
	r.GET("/v1/models", gateway.HandleListModels)
	r.GET("/v1/models/:id", gateway.HandleGetModel)
	r.POST("/v1/embeddings", gateway.HandleEmbeddings)
	return r
}

func TestChatCompletionsNonStreaming(t *testing.T) {
	r := setupOpenAIRouter()
	body, _ := json.Marshal(api.ChatCompletionRequest{
		Model:    "llama-3.1-70b",
		Messages: []api.ChatMessage{{Role: "user", Content: "Hello"}},
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp api.ChatCompletionResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Object != "chat.completion" {
		t.Errorf("object = %q, want chat.completion", resp.Object)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("no choices returned")
	}
	if resp.Choices[0].Message.Role != "assistant" {
		t.Errorf("role = %q, want assistant", resp.Choices[0].Message.Role)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason = %q, want stop", resp.Choices[0].FinishReason)
	}
}

func TestChatCompletionsStreaming(t *testing.T) {
	r := setupOpenAIRouter()
	body, _ := json.Marshal(api.ChatCompletionRequest{
		Model:    "llama-3.1-70b",
		Messages: []api.ChatMessage{{Role: "user", Content: "Hello"}},
		Stream:   true,
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", w.Header().Get("Content-Type"))
	}

	body2 := w.Body.String()
	if !strings.Contains(body2, "data: [DONE]") {
		t.Error("stream missing [DONE] sentinel")
	}

	// Parse first chunk
	for _, line := range strings.Split(body2, "\n") {
		if strings.HasPrefix(line, "data: ") && line != "data: [DONE]" {
			var chunk api.ChatCompletionChunk
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &chunk); err != nil {
				t.Fatalf("chunk parse error: %v", err)
			}
			if chunk.Object != "chat.completion.chunk" {
				t.Errorf("chunk object = %q, want chat.completion.chunk", chunk.Object)
			}
			break
		}
	}
}

func TestListModels(t *testing.T) {
	r := setupOpenAIRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models", nil)
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var list api.ModelList
	json.Unmarshal(w.Body.Bytes(), &list)
	if list.Object != "list" {
		t.Errorf("object = %q, want list", list.Object)
	}
	if len(list.Data) < 1 {
		t.Error("no models returned")
	}
}

func TestGetModel(t *testing.T) {
	r := setupOpenAIRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models/llama-3.1-70b", nil)
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var model api.Model
	json.Unmarshal(w.Body.Bytes(), &model)
	if model.ID != "llama-3.1-70b" {
		t.Errorf("model ID = %q, want llama-3.1-70b", model.ID)
	}
}

func TestGetModelNotFound(t *testing.T) {
	r := setupOpenAIRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models/nonexistent", nil)
	r.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestEmbeddings(t *testing.T) {
	r := setupOpenAIRouter()
	body, _ := json.Marshal(api.EmbeddingRequest{Model: "text-embedding-ada-002", Input: "test"})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/embeddings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp api.EmbeddingResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Object != "list" {
		t.Errorf("object = %q, want list", resp.Object)
	}
	if len(resp.Data) == 0 {
		t.Error("no embeddings returned")
	}
}
