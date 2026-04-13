package brain_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// Compile-time check: NvidiaProvider satisfies the Provider interface.
var _ brain.Provider = (*brain.NvidiaProvider)(nil)

func TestNvidiaProvider_Name(t *testing.T) {
	p := brain.NewNvidiaProvider("key", "")
	if p.Name() != "nvidia" {
		t.Errorf("Name() = %q, want nvidia", p.Name())
	}
}

func TestNvidiaProvider_Available_WithKey(t *testing.T) {
	p := brain.NewNvidiaProvider("nvapi-key", "")
	if !p.Available() {
		t.Error("Available() = false, want true when API key is set")
	}
}

func TestNvidiaProvider_Available_WithoutKey(t *testing.T) {
	p := brain.NewNvidiaProvider("", "")
	if p.Available() {
		t.Error("Available() = true, want false when API key is empty")
	}
}

func TestNvidiaProvider_Complete_SendsCorrectAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("Authorization = %q, want Bearer prefix", auth)
		}
		if !strings.Contains(auth, "nvapi-test") {
			t.Errorf("Authorization = %q, want to contain nvapi-test", auth)
		}

		resp := api.ChatCompletionResponse{
			ID:    "chatcmpl-nvidia-001",
			Model: "nvidia/llama-3.1-nemotron-ultra-253b-v1",
			Choices: []api.ChatCompletionChoice{
				{
					Message:      api.ChatMessage{Role: "assistant", Content: "Nvidia reply"},
					FinishReason: "stop",
				},
			},
			Usage: &api.Usage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	p := brain.NewNvidiaProvider("nvapi-test", srv.URL)
	resp, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "nvidia/llama-3.1-nemotron-ultra-253b-v1",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.ID != "chatcmpl-nvidia-001" {
		t.Errorf("ID = %q, want chatcmpl-nvidia-001", resp.ID)
	}
	if resp.Message.Content != "Nvidia reply" {
		t.Errorf("Content = %q, want 'Nvidia reply'", resp.Message.Content)
	}
}

func TestNvidiaProvider_FetchModels(t *testing.T) {
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
				{ID: "nvidia/llama-3.1-nemotron-ultra-253b-v1"},
				{ID: "meta/llama-3.1-8b-instruct"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	p := brain.NewNvidiaProvider("key", srv.URL)
	if err := p.FetchModels(context.Background(), nil); err != nil {
		t.Fatalf("FetchModels returned error: %v", err)
	}
	if len(p.Models()) != 2 {
		t.Errorf("Models() len = %d, want 2", len(p.Models()))
	}
}

func TestNvidiaProvider_Complete_Returns429AsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := brain.NewNvidiaProvider("key", srv.URL)
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
	if pe.Provider != "nvidia" {
		t.Errorf("Provider = %q, want nvidia", pe.Provider)
	}
	if pe.StatusCode != http.StatusTooManyRequests {
		t.Errorf("StatusCode = %d, want 429", pe.StatusCode)
	}
}
