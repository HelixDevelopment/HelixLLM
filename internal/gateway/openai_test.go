package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// Mock embedder for HandleEmbeddings tests
// ---------------------------------------------------------------------------

type mockEmbedder struct {
	dim int
	vec []float64
	err error
}

func (m *mockEmbedder) Embed(_ string) ([]float64, error) { return m.vec, m.err }
func (m *mockEmbedder) EmbedBatch(_ []string) ([][]float64, error) {
	return nil, m.err
}
func (m *mockEmbedder) Dimension() int { return m.dim }

// setupOpenAIRouter builds a Gin engine with nil Brain (stub mode).
func setupOpenAIRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/chat/completions", gateway.HandleChatCompletions(nil, nil))
	r.POST("/v1/completions", gateway.HandleCompletions(nil))
	r.GET("/v1/models", gateway.HandleListModels(nil))
	r.GET("/v1/models/:id", gateway.HandleGetModel(nil))
	r.POST("/v1/embeddings", gateway.HandleEmbeddings(nil, nil))
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
	r.POST("/v1/chat/completions", gateway.HandleChatCompletions(b, nil))

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
	r.POST("/v1/chat/completions", gateway.HandleChatCompletions(b, nil))

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

func TestChatCompletions_BadJSON(t *testing.T) {
	r := setupOpenAIRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestCompletions_NonStreaming(t *testing.T) {
	r := setupOpenAIRouter()
	body, _ := json.Marshal(api.CompletionRequest{
		Model:  "llama-3.1-70b",
		Prompt: "Hello",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp api.CompletionResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Object != "text_completion" {
		t.Errorf("object = %q, want text_completion", resp.Object)
	}
}

func TestCompletions_BadJSON(t *testing.T) {
	r := setupOpenAIRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/completions", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestEmbeddings_BadJSON(t *testing.T) {
	r := setupOpenAIRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/embeddings", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestEmbeddings_DefaultModel(t *testing.T) {
	r := setupOpenAIRouter()
	body, _ := json.Marshal(api.EmbeddingRequest{Input: "test"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/embeddings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp api.EmbeddingResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Model != "text-embedding-ada-002" {
		t.Errorf("model = %q, want text-embedding-ada-002", resp.Model)
	}
}

func TestChatCompletions_WithBrain_Error(t *testing.T) {
	mock := &mockBrainProvider{
		name:      "openai",
		available: true,
		models:    []string{"gpt-4o"},
		err:       fmt.Errorf("provider error"),
	}
	b := newTestBrain(mock)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/chat/completions", gateway.HandleChatCompletions(b, nil))

	body, _ := json.Marshal(api.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []api.ChatMessage{{Role: "user", Content: "Hello"}},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 500 {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestChatCompletions_WithBrain_StreamError(t *testing.T) {
	mock := &mockBrainProvider{
		name:      "openai",
		available: true,
		models:    []string{"gpt-4o"},
		err:       fmt.Errorf("stream error"),
	}
	b := newTestBrain(mock)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/chat/completions", gateway.HandleChatCompletions(b, nil))

	body, _ := json.Marshal(api.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []api.ChatMessage{{Role: "user", Content: "Hello"}},
		Stream:   true,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 500 {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestCompletions_WithBrain(t *testing.T) {
	mock := &mockBrainProvider{
		name:      "openai",
		available: true,
		models:    []string{"gpt-4o"},
		response: &types.InternalChatResponse{
			ID:           "cmpl-brain-1",
			Model:        "gpt-4o",
			Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "Brain completion"},
			FinishReason: "stop",
			Provider:     types.ProviderOpenAI,
		},
	}
	b := newTestBrain(mock)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/completions", gateway.HandleCompletions(b))

	temp := float64(0.7)
	maxTok := 100
	body, _ := json.Marshal(api.CompletionRequest{
		Model:       "gpt-4o",
		Prompt:      "Hello",
		Temperature: &temp,
		MaxTokens:   &maxTok,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp api.CompletionResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Choices) == 0 || resp.Choices[0].Text != "Brain completion" {
		t.Errorf("unexpected completion response: %+v", resp)
	}
}

func TestCompletions_WithBrain_Error(t *testing.T) {
	mock := &mockBrainProvider{
		name:      "openai",
		available: true,
		models:    []string{"gpt-4o"},
		err:       fmt.Errorf("completion error"),
	}
	b := newTestBrain(mock)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/completions", gateway.HandleCompletions(b))

	body, _ := json.Marshal(api.CompletionRequest{Model: "gpt-4o", Prompt: "Hello"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 500 {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestGetModel_WithBrain(t *testing.T) {
	mock := &mockBrainProvider{
		name:      "openai",
		available: true,
		models:    []string{"gpt-4o"},
	}
	b := newTestBrain(mock)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/v1/models/:id", gateway.HandleGetModel(b))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models/gpt-4o", nil)
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestGetModel_WithBrain_NotFound(t *testing.T) {
	mock := &mockBrainProvider{
		name:      "openai",
		available: true,
		models:    []string{"gpt-4o"},
	}
	b := newTestBrain(mock)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/v1/models/:id", gateway.HandleGetModel(b))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models/nonexistent", nil)
	r.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestChatCompletions_DefaultModel(t *testing.T) {
	r := setupOpenAIRouter()
	body, _ := json.Marshal(api.ChatCompletionRequest{
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
	if resp.Model != "llama-3.1-70b" {
		t.Errorf("default model = %q, want llama-3.1-70b", resp.Model)
	}
}

// TestChatCompletions_WithBrain_TemperatureAndMaxTokens exercises the
// Temperature and MaxTokens branches in openAIToInternal.
func TestChatCompletions_WithBrain_TemperatureAndMaxTokens(t *testing.T) {
	mock := &mockBrainProvider{
		name:      "openai",
		available: true,
		models:    []string{"gpt-4o"},
		response: &types.InternalChatResponse{
			ID:           "chatcmpl-temp-1",
			Model:        "gpt-4o",
			Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "Warm reply"},
			FinishReason: "stop",
			Provider:     types.ProviderOpenAI,
		},
	}
	b := newTestBrain(mock)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/chat/completions", gateway.HandleChatCompletions(b, nil))

	temp := float64(0.7)
	maxTok := 256
	body, _ := json.Marshal(api.ChatCompletionRequest{
		Model:       "gpt-4o",
		Messages:    []api.ChatMessage{{Role: "user", Content: "Hello"}},
		Temperature: &temp,
		MaxTokens:   &maxTok,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
}

// TestChatCompletions_NonStringContent exercises the non-string Content branch
// in openAIToInternal.
func TestChatCompletions_NonStringContent(t *testing.T) {
	mock := &mockBrainProvider{
		name:      "openai",
		available: true,
		models:    []string{"gpt-4o"},
		response: &types.InternalChatResponse{
			ID:           "chatcmpl-nsc-1",
			Model:        "gpt-4o",
			Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "OK"},
			FinishReason: "stop",
		},
	}
	b := newTestBrain(mock)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/chat/completions", gateway.HandleChatCompletions(b, nil))

	// Content as array (non-string) → openAIToInternal falls through to empty string.
	rawBody := `{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"text","text":"Hi"}]}]}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
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

func TestHandleCompletions_EmptyModel(t *testing.T) {
	// When model is omitted in the request, HandleCompletions defaults to
	// "llama-3.1-70b" (covers the model == "" branch).
	r := setupOpenAIRouter()

	body, _ := json.Marshal(api.CompletionRequest{
		// Model intentionally omitted.
		Prompt: "Hello",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var resp api.CompletionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Model != "llama-3.1-70b" {
		t.Errorf("model = %q, want llama-3.1-70b (default)", resp.Model)
	}
}

// ---------------------------------------------------------------------------
// HandleEmbeddings — embedder integration tests
// ---------------------------------------------------------------------------

func TestEmbeddings_NilEmbedder_ZeroVector(t *testing.T) {
	// nil embedder → 1536-dim zero vector (existing fallback behavior).
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/embeddings", gateway.HandleEmbeddings(nil, nil))

	body, _ := json.Marshal(api.EmbeddingRequest{
		Model: "text-embedding-ada-002",
		Input: "hello",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/embeddings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp api.EmbeddingResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("len(data) = %d, want 1", len(resp.Data))
	}
	vec := resp.Data[0].Embedding
	if len(vec) != 1536 {
		t.Fatalf("dimension = %d, want 1536", len(vec))
	}
	for i, v := range vec {
		if v != 0 {
			t.Fatalf("vec[%d] = %f, want 0 (zero vector)", i, v)
		}
	}
}

func TestEmbeddings_RealEmbedder_ReturnsVectors(t *testing.T) {
	// Mock embedder returns [0.1, 0.2, 0.3]; verify response has those values.
	emb := &mockEmbedder{
		dim: 3,
		vec: []float64{0.1, 0.2, 0.3},
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/embeddings", gateway.HandleEmbeddings(nil, emb))

	body, _ := json.Marshal(api.EmbeddingRequest{
		Model: "nomic-embed-text",
		Input: "hello world",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/embeddings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp api.EmbeddingResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("len(data) = %d, want 1", len(resp.Data))
	}
	vec := resp.Data[0].Embedding
	if len(vec) != 3 {
		t.Fatalf("dimension = %d, want 3", len(vec))
	}
	want := []float64{0.1, 0.2, 0.3}
	for i, v := range vec {
		if v != want[i] {
			t.Errorf("vec[%d] = %f, want %f", i, v, want[i])
		}
	}
}

func TestEmbeddings_StringInput(t *testing.T) {
	// Input is a plain string "hello" — verify it reaches the embedder and
	// produces a valid response.
	emb := &mockEmbedder{
		dim: 2,
		vec: []float64{0.5, -0.5},
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/embeddings", gateway.HandleEmbeddings(nil, emb))

	body, _ := json.Marshal(api.EmbeddingRequest{
		Model: "nomic-embed-text",
		Input: "hello",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/embeddings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp api.EmbeddingResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Object != "list" {
		t.Errorf("object = %q, want list", resp.Object)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("len(data) = %d, want 1", len(resp.Data))
	}
	if resp.Data[0].Embedding[0] != 0.5 {
		t.Errorf("vec[0] = %f, want 0.5", resp.Data[0].Embedding[0])
	}
}

func TestEmbeddings_ArrayInput(t *testing.T) {
	// Input is ["hello", "world"]; the handler uses the first element.
	emb := &mockEmbedder{
		dim: 2,
		vec: []float64{0.9, 0.1},
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/embeddings", gateway.HandleEmbeddings(nil, emb))

	// Build raw JSON with array input since EmbeddingRequest.Input is interface{}.
	rawBody := `{"model":"nomic-embed-text","input":["hello","world"]}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/embeddings", strings.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp api.EmbeddingResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("len(data) = %d, want 1", len(resp.Data))
	}
	vec := resp.Data[0].Embedding
	if len(vec) != 2 {
		t.Fatalf("dimension = %d, want 2", len(vec))
	}
	if vec[0] != 0.9 || vec[1] != 0.1 {
		t.Errorf("vec = %v, want [0.9, 0.1]", vec)
	}
}

func TestEmbeddings_EmbedderError_FallsBackToZero(t *testing.T) {
	// Embedder returns error → handler falls back to zero-vector.
	emb := &mockEmbedder{
		dim: 4,
		vec: nil,
		err: errors.New("embedding service unavailable"),
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/embeddings", gateway.HandleEmbeddings(nil, emb))

	body, _ := json.Marshal(api.EmbeddingRequest{
		Model: "nomic-embed-text",
		Input: "test",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/embeddings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp api.EmbeddingResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("len(data) = %d, want 1", len(resp.Data))
	}
	vec := resp.Data[0].Embedding
	// Dimension comes from embedder.Dimension() = 4.
	if len(vec) != 4 {
		t.Fatalf("dimension = %d, want 4", len(vec))
	}
	for i, v := range vec {
		if v != 0 {
			t.Fatalf("vec[%d] = %f, want 0 (zero vector fallback)", i, v)
		}
	}
}
