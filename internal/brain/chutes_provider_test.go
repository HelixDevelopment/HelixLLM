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

// Compile-time check: ChutesProvider satisfies the Provider interface.
var _ brain.Provider = (*brain.ChutesProvider)(nil)

func TestChutesProvider_Name(t *testing.T) {
	p := brain.NewChutesProvider("key", "")
	if p.Name() != "chutes" {
		t.Errorf("Name() = %q, want chutes", p.Name())
	}
}

func TestChutesProvider_Available_WithKey(t *testing.T) {
	p := brain.NewChutesProvider("my-api-key", "")
	if !p.Available() {
		t.Error("Available() = false, want true when API key is set")
	}
}

func TestChutesProvider_Available_WithoutKey(t *testing.T) {
	p := brain.NewChutesProvider("", "")
	if p.Available() {
		t.Error("Available() = true, want false when API key is empty")
	}
}

func TestChutesProvider_DefaultBaseURL(t *testing.T) {
	// Verify that when no baseURL is given the provider uses the Chutes endpoint.
	// We test indirectly: if the default URL is wrong the FetchModels call would
	// target the wrong host. Here we just confirm no panic and that the struct
	// is constructed correctly by calling Name/Available.
	p := brain.NewChutesProvider("key", "")
	if p.Name() != "chutes" {
		t.Errorf("unexpected name with default URL: %s", p.Name())
	}
}

func TestChutesProvider_Complete_SendsCorrectAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("Authorization = %q, want Bearer prefix", auth)
		}
		if !strings.Contains(auth, "chutes-api-key") {
			t.Errorf("Authorization = %q, want to contain chutes-api-key", auth)
		}

		resp := api.ChatCompletionResponse{
			ID:    "chatcmpl-chutes-001",
			Model: "deepseek-ai/DeepSeek-V3-0324",
			Choices: []api.ChatCompletionChoice{
				{
					Message:      api.ChatMessage{Role: "assistant", Content: "Hello from Chutes!"},
					FinishReason: "stop",
				},
			},
			Usage: &api.Usage{PromptTokens: 6, CompletionTokens: 4, TotalTokens: 10},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	p := brain.NewChutesProvider("chutes-api-key", srv.URL)
	resp, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "deepseek-ai/DeepSeek-V3-0324",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.ID != "chatcmpl-chutes-001" {
		t.Errorf("ID = %q, want chatcmpl-chutes-001", resp.ID)
	}
	if resp.Message.Content != "Hello from Chutes!" {
		t.Errorf("Content = %q, want 'Hello from Chutes!'", resp.Message.Content)
	}
	if resp.Usage.TotalTokens != 10 {
		t.Errorf("Usage.TotalTokens = %d, want 10", resp.Usage.TotalTokens)
	}
}

func TestChutesProvider_FetchModels(t *testing.T) {
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
				{ID: "deepseek-ai/DeepSeek-V3-0324"},
				{ID: "Qwen/Qwen3-235B-A22B"},
				{ID: "meta-llama/Llama-4-Maverick-17B-128E-Instruct-FP8"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	p := brain.NewChutesProvider("key", srv.URL)
	if err := p.FetchModels(context.Background(), nil); err != nil {
		t.Fatalf("FetchModels returned error: %v", err)
	}
	models := p.Models()
	if len(models) != 3 {
		t.Errorf("Models() len = %d, want 3", len(models))
	}
	wantModels := map[string]bool{
		"deepseek-ai/DeepSeek-V3-0324":                   true,
		"Qwen/Qwen3-235B-A22B":                           true,
		"meta-llama/Llama-4-Maverick-17B-128E-Instruct-FP8": true,
	}
	for _, m := range models {
		if !wantModels[m] {
			t.Errorf("unexpected model %q in list", m)
		}
	}
}

func TestChutesProvider_Complete_ErrorPropagation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "10")
		http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := brain.NewChutesProvider("key", srv.URL)
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
	if pe.Provider != "chutes" {
		t.Errorf("Provider = %q, want chutes", pe.Provider)
	}
}

func TestChutesProvider_CompleteStream_ReturnsChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)

		tokens := []string{"Chutes", " rocks"}
		stopStr := "stop"
		for i, token := range tokens {
			chunk := api.ChatCompletionChunk{
				ID:    "chatcmpl-stream-chutes",
				Model: "m",
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

	p := brain.NewChutesProvider("key", srv.URL)
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
	if chunks[0].Content != "Chutes" {
		t.Errorf("chunks[0].Content = %q, want Chutes", chunks[0].Content)
	}
	if chunks[1].Content != " rocks" {
		t.Errorf("chunks[1].Content = %q, want ' rocks'", chunks[1].Content)
	}
	if chunks[1].FinishReason != "stop" {
		t.Errorf("chunks[1].FinishReason = %q, want stop", chunks[1].FinishReason)
	}
}
