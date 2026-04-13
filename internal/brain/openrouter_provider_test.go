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

// Compile-time check: OpenRouterProvider satisfies the Provider interface.
var _ brain.Provider = (*brain.OpenRouterProvider)(nil)

func TestOpenRouterProvider_Name(t *testing.T) {
	p := brain.NewOpenRouterProvider("key", "")
	if p.Name() != "openrouter" {
		t.Errorf("Name() = %q, want openrouter", p.Name())
	}
}

func TestOpenRouterProvider_Available_WithKey(t *testing.T) {
	p := brain.NewOpenRouterProvider("sk-or-key", "")
	if !p.Available() {
		t.Error("Available() = false, want true when API key is set")
	}
}

func TestOpenRouterProvider_Available_WithoutKey(t *testing.T) {
	p := brain.NewOpenRouterProvider("", "")
	if p.Available() {
		t.Error("Available() = true, want false when API key is empty")
	}
}

func TestOpenRouterProvider_DiscoverFreeModels_FiltersToFreeOnly(t *testing.T) {
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
				{ID: "meta-llama/llama-3.1-8b-instruct:free"},
				{ID: "google/gemma-3-27b-it:free"},
				{ID: "openai/gpt-4o"},                     // paid — must be filtered out
				{ID: "anthropic/claude-3.5-sonnet"},        // paid — must be filtered out
				{ID: "mistralai/mistral-7b-instruct:free"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	p := brain.NewOpenRouterProvider("key", srv.URL)
	if err := p.DiscoverFreeModels(context.Background()); err != nil {
		t.Fatalf("DiscoverFreeModels returned error: %v", err)
	}

	models := p.Models()
	if len(models) != 3 {
		t.Errorf("Models() len = %d, want 3 (only :free models)", len(models))
	}
	for _, m := range models {
		if !strings.HasSuffix(m, ":free") {
			t.Errorf("model %q should have :free suffix but does not", m)
		}
	}
}

func TestOpenRouterProvider_DiscoverFreeModels_EmptyWhenNoneFree(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}{
			Data: []struct {
				ID string `json:"id"`
			}{
				{ID: "openai/gpt-4o"},
				{ID: "anthropic/claude-opus-4"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	p := brain.NewOpenRouterProvider("key", srv.URL)
	if err := p.DiscoverFreeModels(context.Background()); err != nil {
		t.Fatalf("DiscoverFreeModels returned error: %v", err)
	}
	if len(p.Models()) != 0 {
		t.Errorf("expected 0 free models, got %d", len(p.Models()))
	}
}

func TestOpenRouterProvider_FetchModels_NoFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}{
			Data: []struct {
				ID string `json:"id"`
			}{
				{ID: "openai/gpt-4o"},
				{ID: "meta-llama/llama-3.1-8b-instruct:free"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	p := brain.NewOpenRouterProvider("key", srv.URL)
	if err := p.FetchModels(context.Background(), nil); err != nil {
		t.Fatalf("FetchModels returned error: %v", err)
	}
	if len(p.Models()) != 2 {
		t.Errorf("Models() len = %d, want 2 (no filter)", len(p.Models()))
	}
}

func TestOpenRouterProvider_Complete_SendsCorrectAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("Authorization = %q, want Bearer prefix", auth)
		}
		if !strings.Contains(auth, "sk-or-test-key") {
			t.Errorf("Authorization = %q, want to contain sk-or-test-key", auth)
		}

		resp := api.ChatCompletionResponse{
			ID:    "chatcmpl-openrouter-001",
			Model: "meta-llama/llama-3.1-8b-instruct:free",
			Choices: []api.ChatCompletionChoice{
				{
					Message:      api.ChatMessage{Role: "assistant", Content: "OpenRouter reply"},
					FinishReason: "stop",
				},
			},
			Usage: &api.Usage{PromptTokens: 4, CompletionTokens: 3, TotalTokens: 7},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	p := brain.NewOpenRouterProvider("sk-or-test-key", srv.URL)
	resp, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "meta-llama/llama-3.1-8b-instruct:free",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.ID != "chatcmpl-openrouter-001" {
		t.Errorf("ID = %q, want chatcmpl-openrouter-001", resp.ID)
	}
	if resp.Message.Content != "OpenRouter reply" {
		t.Errorf("Content = %q, want 'OpenRouter reply'", resp.Message.Content)
	}
}

func TestOpenRouterProvider_CompleteStream_ReturnsChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)

		tokens := []string{"OR", " stream"}
		stopStr := "stop"
		for i, token := range tokens {
			chunk := api.ChatCompletionChunk{
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

	p := brain.NewOpenRouterProvider("key", srv.URL)
	ch, err := p.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model:    "m",
		Stream:   true,
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("CompleteStream returned error: %v", err)
	}
	var chunks []types.StreamChunk
	for c := range ch {
		chunks = append(chunks, c)
	}
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if chunks[1].FinishReason != "stop" {
		t.Errorf("chunks[1].FinishReason = %q, want stop", chunks[1].FinishReason)
	}
}

func TestOpenRouterProvider_Complete_Returns429AsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := brain.NewOpenRouterProvider("key", srv.URL)
	_, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "m",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 429, got nil")
	}
	var pe *brain.ProviderError
	if !brain.AsProviderError(err, &pe) {
		t.Fatalf("expected *ProviderError, got %T", err)
	}
	if pe.StatusCode != http.StatusTooManyRequests {
		t.Errorf("StatusCode = %d, want 429", pe.StatusCode)
	}
	if pe.Provider != "openrouter" {
		t.Errorf("Provider = %q, want openrouter", pe.Provider)
	}
}
