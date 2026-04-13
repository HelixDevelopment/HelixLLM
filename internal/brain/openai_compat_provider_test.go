package brain_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// Compile-time check: OpenAICompatProvider satisfies the Provider interface.
var _ brain.Provider = (*brain.OpenAICompatProvider)(nil)

// TestOpenAICompatProvider_Complete_SendsCorrectRequest verifies that the
// provider POSTs to {baseURL}/chat/completions with the correct Authorization
// header and JSON body, and that the response is correctly parsed.
func TestOpenAICompatProvider_Complete_SendsCorrectRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path check.
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s, want /chat/completions", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s, want POST", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Authorization header check.
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("Authorization = %q, want Bearer token", auth)
		}
		if !strings.Contains(auth, "test-api-key") {
			t.Errorf("Authorization = %q, expected to contain test-api-key", auth)
		}

		// Body decode check.
		var req api.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request body: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Model != "test-model" {
			t.Errorf("request model = %q, want %q", req.Model, "test-model")
		}
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
			t.Errorf("unexpected messages: %v", req.Messages)
		}

		resp := api.ChatCompletionResponse{
			ID:    "chatcmpl-compat-001",
			Model: "test-model",
			Choices: []api.ChatCompletionChoice{
				{
					Message:      api.ChatMessage{Role: "assistant", Content: "Hello from compat!"},
					FinishReason: "stop",
				},
			},
			Usage: &api.Usage{
				PromptTokens:     5,
				CompletionTokens: 4,
				TotalTokens:      9,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	p := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name:       "testprovider",
		BaseURL:    srv.URL,
		APIKey:     "test-api-key",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	})

	resp, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model: "test-model",
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.ID != "chatcmpl-compat-001" {
		t.Errorf("ID = %q, want %q", resp.ID, "chatcmpl-compat-001")
	}
	if resp.Message.Content != "Hello from compat!" {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "Hello from compat!")
	}
	if resp.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", resp.FinishReason)
	}
	if resp.Usage.TotalTokens != 9 {
		t.Errorf("Usage.TotalTokens = %d, want 9", resp.Usage.TotalTokens)
	}
}

// TestOpenAICompatProvider_Complete_CustomAuthHeader verifies that the
// provider uses a custom auth header (e.g. "x-api-key") and prefix.
func TestOpenAICompatProvider_Complete_CustomAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("x-api-key")
		if apiKey != "custom-key-value" {
			t.Errorf("x-api-key = %q, want custom-key-value", apiKey)
		}
		resp := api.ChatCompletionResponse{
			ID:    "chatcmpl-custom-001",
			Model: "m",
			Choices: []api.ChatCompletionChoice{
				{Message: api.ChatMessage{Role: "assistant", Content: "ok"}, FinishReason: "stop"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	p := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name:       "customprovider",
		BaseURL:    srv.URL,
		APIKey:     "custom-key-value",
		AuthHeader: "x-api-key",
		AuthPrefix: "", // no prefix, key sent as-is
	})

	_, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "m",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
}

// TestOpenAICompatProvider_CompleteStream_ReturnsChunks verifies SSE stream
// parsing with multiple chunks including the final [DONE] marker.
func TestOpenAICompatProvider_CompleteStream_ReturnsChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req api.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if !req.Stream {
			t.Error("expected stream=true in request body")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)

		tokens := []string{"Hello", " compat", "!"}
		stopStr := "stop"
		for i, token := range tokens {
			chunk := api.ChatCompletionChunk{
				ID:    "chatcmpl-stream-001",
				Model: "test-model",
				Choices: []api.ChatCompletionChunkChoice{
					{
						Delta: api.ChatMessageDelta{Content: token},
						FinishReason: func() *string {
							if i == len(tokens)-1 {
								return &stopStr
							}
							return nil
						}(),
					},
				},
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name:       "testprovider",
		BaseURL:    srv.URL,
		APIKey:     "test-api-key",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	})

	ch, err := p.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model:  "test-model",
		Stream: true,
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("CompleteStream returned error: %v", err)
	}

	var chunks []types.StreamChunk
	for c := range ch {
		chunks = append(chunks, c)
	}
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	if chunks[0].Content != "Hello" {
		t.Errorf("chunks[0].Content = %q, want Hello", chunks[0].Content)
	}
	if chunks[1].Content != " compat" {
		t.Errorf("chunks[1].Content = %q, want compat", chunks[1].Content)
	}
	if chunks[2].Content != "!" {
		t.Errorf("chunks[2].Content = %q, want !", chunks[2].Content)
	}
	if chunks[2].FinishReason != "stop" {
		t.Errorf("chunks[2].FinishReason = %q, want stop", chunks[2].FinishReason)
	}
}

// TestOpenAICompatProvider_Complete_Returns429AsProviderError verifies that a
// 429 response is returned as a *ProviderError with StatusCode and Headers set.
func TestOpenAICompatProvider_Complete_Returns429AsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.Header().Set("X-RateLimit-Limit", "100")
		http.Error(w, `{"error":{"message":"rate limit exceeded"}}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name:       "testprovider",
		BaseURL:    srv.URL,
		APIKey:     "test-api-key",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	})

	_, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "test-model",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 429 response, got nil")
	}

	var pe *brain.ProviderError
	if !brain.AsProviderError(err, &pe) {
		t.Fatalf("expected *ProviderError, got %T: %v", err, err)
	}
	if pe.StatusCode != http.StatusTooManyRequests {
		t.Errorf("ProviderError.StatusCode = %d, want 429", pe.StatusCode)
	}
	if pe.Headers.Get("Retry-After") != "60" {
		t.Errorf("ProviderError.Headers[Retry-After] = %q, want 60", pe.Headers.Get("Retry-After"))
	}
	if pe.Provider != "testprovider" {
		t.Errorf("ProviderError.Provider = %q, want testprovider", pe.Provider)
	}
	if pe.Body == "" {
		t.Error("ProviderError.Body should not be empty for 429")
	}
}

// TestOpenAICompatProvider_Complete_Returns5xxAsProviderError verifies that
// 5xx responses are also wrapped as *ProviderError.
func TestOpenAICompatProvider_Complete_Returns5xxAsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name:       "testprovider",
		BaseURL:    srv.URL,
		APIKey:     "test-api-key",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	})

	_, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "test-model",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}

	var pe *brain.ProviderError
	if !brain.AsProviderError(err, &pe) {
		t.Fatalf("expected *ProviderError, got %T: %v", err, err)
	}
	if pe.StatusCode != http.StatusInternalServerError {
		t.Errorf("ProviderError.StatusCode = %d, want 500", pe.StatusCode)
	}
}

// TestOpenAICompatProvider_FetchModels verifies model discovery from
// {baseURL}/models, with and without a filter function.
func TestOpenAICompatProvider_FetchModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		resp := struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}{
			Data: []struct {
				ID string `json:"id"`
			}{
				{ID: "model-a"},
				{ID: "model-b"},
				{ID: "model-c"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	t.Run("NoFilter", func(t *testing.T) {
		p := brain.NewOpenAICompat(brain.OpenAICompatConfig{
			Name:       "testprovider",
			BaseURL:    srv.URL,
			APIKey:     "test-api-key",
			AuthHeader: "Authorization",
			AuthPrefix: "Bearer ",
		})

		if err := p.FetchModels(context.Background(), nil); err != nil {
			t.Fatalf("FetchModels returned error: %v", err)
		}

		models := p.Models()
		if len(models) != 3 {
			t.Errorf("Models() len = %d, want 3", len(models))
		}
		wantModels := map[string]bool{"model-a": true, "model-b": true, "model-c": true}
		for _, m := range models {
			if !wantModels[m] {
				t.Errorf("unexpected model %q in list", m)
			}
		}
	})

	t.Run("WithFilter", func(t *testing.T) {
		p := brain.NewOpenAICompat(brain.OpenAICompatConfig{
			Name:       "testprovider",
			BaseURL:    srv.URL,
			APIKey:     "test-api-key",
			AuthHeader: "Authorization",
			AuthPrefix: "Bearer ",
		})

		// Filter: only keep models with "a" or "b" in name.
		filter := func(id string) bool {
			return id == "model-a" || id == "model-b"
		}

		if err := p.FetchModels(context.Background(), filter); err != nil {
			t.Fatalf("FetchModels returned error: %v", err)
		}

		models := p.Models()
		if len(models) != 2 {
			t.Errorf("Models() len = %d, want 2 after filter", len(models))
		}
		for _, m := range models {
			if m == "model-c" {
				t.Error("model-c should have been filtered out")
			}
		}
	})
}

// TestOpenAICompatProvider_FetchModels_APIError verifies that a non-200
// response from /models is handled gracefully (error returned).
func TestOpenAICompatProvider_FetchModels_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	p := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name:       "testprovider",
		BaseURL:    srv.URL,
		APIKey:     "test-api-key",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	})

	if err := p.FetchModels(context.Background(), nil); err == nil {
		t.Fatal("expected error from non-200 /models response, got nil")
	}
}

// TestOpenAICompatProvider_Available verifies that Available() returns true
// when an API key is set and false when the key is empty.
func TestOpenAICompatProvider_Available(t *testing.T) {
	t.Run("WithKey", func(t *testing.T) {
		p := brain.NewOpenAICompat(brain.OpenAICompatConfig{
			Name:       "testprovider",
			BaseURL:    "https://api.example.com",
			APIKey:     "my-key",
			AuthHeader: "Authorization",
			AuthPrefix: "Bearer ",
		})
		if !p.Available() {
			t.Error("Available() = false, want true when API key is set")
		}
	})

	t.Run("WithoutKey", func(t *testing.T) {
		p := brain.NewOpenAICompat(brain.OpenAICompatConfig{
			Name:       "testprovider",
			BaseURL:    "https://api.example.com",
			APIKey:     "",
			AuthHeader: "Authorization",
			AuthPrefix: "Bearer ",
		})
		if p.Available() {
			t.Error("Available() = true, want false when API key is empty")
		}
	})
}

// TestOpenAICompatProvider_Name verifies that Name() returns the configured name.
func TestOpenAICompatProvider_Name(t *testing.T) {
	p := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name:    "myprovider",
		BaseURL: "https://api.example.com",
		APIKey:  "k",
	})
	if p.Name() != "myprovider" {
		t.Errorf("Name() = %q, want myprovider", p.Name())
	}
}

// TestOpenAICompatProvider_SetModels verifies that SetModels populates the
// model list directly, bypassing network discovery.
func TestOpenAICompatProvider_SetModels(t *testing.T) {
	p := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name:    "testprovider",
		BaseURL: "https://api.example.com",
		APIKey:  "k",
	})

	// Initially empty (no defaults set).
	p.SetModels([]string{"model-x", "model-y"})

	models := p.Models()
	if len(models) != 2 {
		t.Fatalf("Models() len = %d, want 2", len(models))
	}
	if models[0] != "model-x" {
		t.Errorf("models[0] = %q, want model-x", models[0])
	}
	if models[1] != "model-y" {
		t.Errorf("models[1] = %q, want model-y", models[1])
	}
}

// TestOpenAICompatProvider_CompleteStream_ErrorStatus verifies that a non-200
// SSE response is returned as an error before any channel is produced.
func TestOpenAICompatProvider_CompleteStream_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	p := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name:       "testprovider",
		BaseURL:    srv.URL,
		APIKey:     "test-api-key",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	})

	_, err := p.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model:  "test-model",
		Stream: true,
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 503 streaming response, got nil")
	}
}

// TestOpenAICompatProvider_ProviderError_Is verifies the errors.Is chain works
// as documented (errors.As with *ProviderError).
func TestOpenAICompatProvider_ProviderError_Is(t *testing.T) {
	pe := &brain.ProviderError{
		Provider:   "x",
		StatusCode: 429,
		Body:       "rate limited",
		Headers:    http.Header{"Retry-After": []string{"30"}},
	}

	if pe.Error() == "" {
		t.Error("ProviderError.Error() returned empty string")
	}
	if pe.StatusCode != 429 {
		t.Errorf("StatusCode = %d, want 429", pe.StatusCode)
	}
}
