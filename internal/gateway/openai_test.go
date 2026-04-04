package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
	"github.com/gin-gonic/gin"
)

// setupOpenAIRouter builds a Gin engine with nil Brain (stub mode).
func setupOpenAIRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/chat/completions", gateway.HandleChatCompletions(nil))
	r.POST("/v1/completions", gateway.HandleCompletions(nil))
	r.GET("/v1/models", gateway.HandleListModels(nil))
	r.GET("/v1/models/:id", gateway.HandleGetModel(nil))
	r.POST("/v1/embeddings", gateway.HandleEmbeddings(nil))
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

// ---------------------------------------------------------------------------
// Brain-backed handler tests
// ---------------------------------------------------------------------------

// mockBrainProvider is a local test double implementing brain.Provider so we
// can build a real brain.Brain with injected mock providers in gateway tests.
type mockBrainProvider struct {
	name      string
	models    []string
	available bool
	response  *types.InternalChatResponse
	chunks    []types.StreamChunk
	err       error
}

func (m *mockBrainProvider) Complete(_ context.Context, _ *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func (m *mockBrainProvider) CompleteStream(_ context.Context, _ *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan types.StreamChunk, len(m.chunks))
	for _, c := range m.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (m *mockBrainProvider) Models() []string { return m.models }
func (m *mockBrainProvider) Name() string     { return m.name }
func (m *mockBrainProvider) Available() bool  { return m.available }

// newTestBrain builds a Brain with a single mock provider for gateway tests.
func newTestBrain(p brain.Provider) *brain.Brain {
	b := brain.New(brain.Config{DefaultProvider: p.Name()})
	b.RegisterProvider(p.Name(), p)
	return b
}

func TestChatCompletions_WithBrain_NonStreaming(t *testing.T) {
	mock := &mockBrainProvider{
		name:      "openai",
		available: true,
		models:    []string{"gpt-4o"},
		response: &types.InternalChatResponse{
			ID:           "chatcmpl-brain-1",
			Model:        "gpt-4o",
			Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "Brain says hi"},
			FinishReason: "stop",
			Provider:     types.ProviderOpenAI,
		},
	}
	b := newTestBrain(mock)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/chat/completions", gateway.HandleChatCompletions(b))

	body, _ := json.Marshal(api.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []api.ChatMessage{{Role: "user", Content: "Hello"}},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}

	var resp api.ChatCompletionResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Choices) == 0 {
		t.Fatal("no choices returned")
	}
	content, _ := resp.Choices[0].Message.Content.(string)
	if content != "Brain says hi" {
		t.Errorf("content = %q, want %q", content, "Brain says hi")
	}
}

func TestChatCompletions_WithBrain_Streaming(t *testing.T) {
	mock := &mockBrainProvider{
		name:      "openai",
		available: true,
		models:    []string{"gpt-4o"},
		chunks: []types.StreamChunk{
			{Content: "Hello"},
			{Content: " brain", FinishReason: "stop"},
		},
	}
	b := newTestBrain(mock)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/chat/completions", gateway.HandleChatCompletions(b))

	body, _ := json.Marshal(api.ChatCompletionRequest{
		Model:    "gpt-4o",
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
	bodyStr := w.Body.String()
	if !strings.Contains(bodyStr, "data: [DONE]") {
		t.Error("stream missing [DONE] sentinel")
	}
	if !strings.Contains(bodyStr, "Hello") {
		t.Error("stream missing expected content token")
	}
}

func TestListModels_WithBrain(t *testing.T) {
	mock := &mockBrainProvider{
		name:      "openai",
		available: true,
		models:    []string{"gpt-4o", "gpt-4o-mini"},
	}
	b := newTestBrain(mock)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/v1/models", gateway.HandleListModels(b))

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
	if len(list.Data) != 2 {
		t.Errorf("len(models) = %d, want 2", len(list.Data))
	}
}
