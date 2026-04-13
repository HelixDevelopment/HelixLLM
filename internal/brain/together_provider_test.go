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

// Compile-time check: TogetherProvider satisfies the Provider interface.
var _ brain.Provider = (*brain.TogetherProvider)(nil)

func TestTogetherProvider_Name(t *testing.T) {
	p := brain.NewTogetherProvider("key", "")
	if p.Name() != "together" {
		t.Errorf("Name() = %q, want together", p.Name())
	}
}

func TestTogetherProvider_Available_WithKey(t *testing.T) {
	p := brain.NewTogetherProvider("together-api-key", "")
	if !p.Available() {
		t.Error("Available() = false, want true when API key is set")
	}
}

func TestTogetherProvider_Available_WithoutKey(t *testing.T) {
	p := brain.NewTogetherProvider("", "")
	if p.Available() {
		t.Error("Available() = true, want false when API key is empty")
	}
}

func TestTogetherProvider_Complete_SendsCorrectAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("Authorization = %q, want Bearer prefix", auth)
		}
		if !strings.Contains(auth, "together-test-key") {
			t.Errorf("Authorization = %q, want to contain together-test-key", auth)
		}

		resp := api.ChatCompletionResponse{
			ID:    "chatcmpl-together-001",
			Model: "meta-llama/Llama-3.3-70B-Instruct-Turbo-Free",
			Choices: []api.ChatCompletionChoice{
				{
					Message:      api.ChatMessage{Role: "assistant", Content: "Together reply"},
					FinishReason: "stop",
				},
			},
			Usage: &api.Usage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	p := brain.NewTogetherProvider("together-test-key", srv.URL)
	resp, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "meta-llama/Llama-3.3-70B-Instruct-Turbo-Free",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.ID != "chatcmpl-together-001" {
		t.Errorf("ID = %q, want chatcmpl-together-001", resp.ID)
	}
	if resp.Message.Content != "Together reply" {
		t.Errorf("Content = %q, want 'Together reply'", resp.Message.Content)
	}
	if resp.Usage.TotalTokens != 7 {
		t.Errorf("Usage.TotalTokens = %d, want 7", resp.Usage.TotalTokens)
	}
}

func TestTogetherProvider_FetchModels(t *testing.T) {
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
				{ID: "meta-llama/Llama-3.3-70B-Instruct-Turbo-Free"},
				{ID: "deepseek-ai/DeepSeek-R1-Distill-Llama-70B-free"},
				{ID: "Qwen/Qwen2.5-72B-Instruct-Turbo"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	p := brain.NewTogetherProvider("key", srv.URL)
	if err := p.FetchModels(context.Background(), nil); err != nil {
		t.Fatalf("FetchModels returned error: %v", err)
	}
	if len(p.Models()) != 3 {
		t.Errorf("Models() len = %d, want 3", len(p.Models()))
	}
}

func TestTogetherProvider_CompleteStream_ReturnsChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)

		tokens := []string{"Together", " AI"}
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

	p := brain.NewTogetherProvider("key", srv.URL)
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
	if chunks[0].Content != "Together" {
		t.Errorf("chunks[0].Content = %q, want Together", chunks[0].Content)
	}
	if chunks[1].Content != " AI" {
		t.Errorf("chunks[1].Content = %q, want ' AI'", chunks[1].Content)
	}
	if chunks[1].FinishReason != "stop" {
		t.Errorf("chunks[1].FinishReason = %q, want stop", chunks[1].FinishReason)
	}
}

func TestTogetherProvider_Complete_Returns429AsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := brain.NewTogetherProvider("key", srv.URL)
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
	if pe.Provider != "together" {
		t.Errorf("Provider = %q, want together", pe.Provider)
	}
	if pe.StatusCode != http.StatusTooManyRequests {
		t.Errorf("StatusCode = %d, want 429", pe.StatusCode)
	}
}
