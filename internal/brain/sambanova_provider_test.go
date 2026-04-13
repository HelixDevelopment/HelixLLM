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

// Compile-time check: SambaNovaProvider satisfies the Provider interface.
var _ brain.Provider = (*brain.SambaNovaProvider)(nil)

func TestSambaNovaProvider_Name(t *testing.T) {
	p := brain.NewSambaNovaProvider("key", "")
	if p.Name() != "sambanova" {
		t.Errorf("Name() = %q, want sambanova", p.Name())
	}
}

func TestSambaNovaProvider_Available_WithKey(t *testing.T) {
	p := brain.NewSambaNovaProvider("sn-api-key", "")
	if !p.Available() {
		t.Error("Available() = false, want true when API key is set")
	}
}

func TestSambaNovaProvider_Available_WithoutKey(t *testing.T) {
	p := brain.NewSambaNovaProvider("", "")
	if p.Available() {
		t.Error("Available() = true, want false when API key is empty")
	}
}

func TestSambaNovaProvider_Complete_SendsCorrectAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("Authorization = %q, want Bearer prefix", auth)
		}
		if !strings.Contains(auth, "sn-test-key") {
			t.Errorf("Authorization = %q, want to contain sn-test-key", auth)
		}

		resp := api.ChatCompletionResponse{
			ID:    "chatcmpl-sn-001",
			Model: "Meta-Llama-3.3-70B-Instruct",
			Choices: []api.ChatCompletionChoice{
				{
					Message:      api.ChatMessage{Role: "assistant", Content: "SambaNova reply"},
					FinishReason: "stop",
				},
			},
			Usage: &api.Usage{PromptTokens: 4, CompletionTokens: 3, TotalTokens: 7},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	p := brain.NewSambaNovaProvider("sn-test-key", srv.URL)
	resp, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "Meta-Llama-3.3-70B-Instruct",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.ID != "chatcmpl-sn-001" {
		t.Errorf("ID = %q, want chatcmpl-sn-001", resp.ID)
	}
	if resp.Message.Content != "SambaNova reply" {
		t.Errorf("Content = %q, want 'SambaNova reply'", resp.Message.Content)
	}
	if resp.Usage.TotalTokens != 7 {
		t.Errorf("Usage.TotalTokens = %d, want 7", resp.Usage.TotalTokens)
	}
}

func TestSambaNovaProvider_FetchModels(t *testing.T) {
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
				{ID: "Meta-Llama-3.3-70B-Instruct"},
				{ID: "Meta-Llama-3.1-405B-Instruct"},
				{ID: "DeepSeek-R1"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	p := brain.NewSambaNovaProvider("key", srv.URL)
	if err := p.FetchModels(context.Background(), nil); err != nil {
		t.Fatalf("FetchModels returned error: %v", err)
	}
	models := p.Models()
	if len(models) != 3 {
		t.Errorf("Models() len = %d, want 3", len(models))
	}
}

func TestSambaNovaProvider_Complete_Returns429AsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "20")
		http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := brain.NewSambaNovaProvider("key", srv.URL)
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
	if pe.Provider != "sambanova" {
		t.Errorf("Provider = %q, want sambanova", pe.Provider)
	}
	if pe.StatusCode != http.StatusTooManyRequests {
		t.Errorf("StatusCode = %d, want 429", pe.StatusCode)
	}
}
