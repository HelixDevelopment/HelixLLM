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

// Compile-time check: HuggingFaceProvider satisfies the Provider interface.
var _ brain.Provider = (*brain.HuggingFaceProvider)(nil)

func TestHuggingFaceProvider_Name(t *testing.T) {
	p := brain.NewHuggingFaceProvider("key", "")
	if p.Name() != "huggingface" {
		t.Errorf("Name() = %q, want huggingface", p.Name())
	}
}

func TestHuggingFaceProvider_Available_WithKey(t *testing.T) {
	p := brain.NewHuggingFaceProvider("hf-token", "")
	if !p.Available() {
		t.Error("Available() = false, want true when API key is set")
	}
}

func TestHuggingFaceProvider_Available_WithoutKey(t *testing.T) {
	p := brain.NewHuggingFaceProvider("", "")
	if p.Available() {
		t.Error("Available() = true, want false when API key is empty")
	}
}

func TestHuggingFaceProvider_DiscoverFreeModels_ReturnsAllModels(t *testing.T) {
	// HuggingFace router only exposes free inference models, so
	// DiscoverFreeModels should return all models from the /models endpoint
	// without filtering any out.
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
				{ID: "meta-llama/Llama-3.1-8B-Instruct"},
				{ID: "Qwen/Qwen2.5-72B-Instruct"},
				{ID: "mistralai/Mistral-7B-Instruct-v0.3"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	p := brain.NewHuggingFaceProvider("hf-token", srv.URL)
	if err := p.DiscoverFreeModels(context.Background()); err != nil {
		t.Fatalf("DiscoverFreeModels returned error: %v", err)
	}

	models := p.Models()
	if len(models) != 3 {
		t.Errorf("Models() len = %d, want 3 (all models pass through)", len(models))
	}
	wantModels := map[string]bool{
		"meta-llama/Llama-3.1-8B-Instruct":       true,
		"Qwen/Qwen2.5-72B-Instruct":               true,
		"mistralai/Mistral-7B-Instruct-v0.3":      true,
	}
	for _, m := range models {
		if !wantModels[m] {
			t.Errorf("unexpected model %q in list", m)
		}
	}
}

func TestHuggingFaceProvider_Complete_SendsCorrectAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("Authorization = %q, want Bearer prefix", auth)
		}
		if !strings.Contains(auth, "hf-test-token") {
			t.Errorf("Authorization = %q, want to contain hf-test-token", auth)
		}

		resp := api.ChatCompletionResponse{
			ID:    "chatcmpl-hf-001",
			Model: "meta-llama/Llama-3.1-8B-Instruct",
			Choices: []api.ChatCompletionChoice{
				{
					Message:      api.ChatMessage{Role: "assistant", Content: "HuggingFace reply"},
					FinishReason: "stop",
				},
			},
			Usage: &api.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	p := brain.NewHuggingFaceProvider("hf-test-token", srv.URL)
	resp, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "meta-llama/Llama-3.1-8B-Instruct",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.ID != "chatcmpl-hf-001" {
		t.Errorf("ID = %q, want chatcmpl-hf-001", resp.ID)
	}
	if resp.Message.Content != "HuggingFace reply" {
		t.Errorf("Content = %q, want 'HuggingFace reply'", resp.Message.Content)
	}
	if resp.Usage.TotalTokens != 8 {
		t.Errorf("Usage.TotalTokens = %d, want 8", resp.Usage.TotalTokens)
	}
}

func TestHuggingFaceProvider_FetchModels_WithFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}{
			Data: []struct {
				ID string `json:"id"`
			}{
				{ID: "meta-llama/Llama-3.1-8B-Instruct"},
				{ID: "Qwen/Qwen2.5-72B-Instruct"},
				{ID: "mistralai/Mistral-7B-Instruct-v0.3"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	p := brain.NewHuggingFaceProvider("key", srv.URL)
	// Apply a custom filter: only Qwen models.
	if err := p.FetchModels(context.Background(), func(id string) bool {
		return strings.HasPrefix(id, "Qwen/")
	}); err != nil {
		t.Fatalf("FetchModels returned error: %v", err)
	}
	models := p.Models()
	if len(models) != 1 {
		t.Errorf("Models() len = %d, want 1 after Qwen filter", len(models))
	}
	if len(models) > 0 && models[0] != "Qwen/Qwen2.5-72B-Instruct" {
		t.Errorf("models[0] = %q, want Qwen/Qwen2.5-72B-Instruct", models[0])
	}
}

func TestHuggingFaceProvider_Complete_Returns429AsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "15")
		http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := brain.NewHuggingFaceProvider("key", srv.URL)
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
	if pe.Provider != "huggingface" {
		t.Errorf("Provider = %q, want huggingface", pe.Provider)
	}
}

func TestHuggingFaceProvider_CompleteStream_ReturnsChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)

		tokens := []string{"HF", " stream", "!"}
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

	p := brain.NewHuggingFaceProvider("key", srv.URL)
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
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	if chunks[0].Content != "HF" {
		t.Errorf("chunks[0].Content = %q, want HF", chunks[0].Content)
	}
	if chunks[2].FinishReason != "stop" {
		t.Errorf("chunks[2].FinishReason = %q, want stop", chunks[2].FinishReason)
	}
}
