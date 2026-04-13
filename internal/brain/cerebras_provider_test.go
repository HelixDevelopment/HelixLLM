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

// Compile-time check: CerebrasProvider satisfies the Provider interface.
var _ brain.Provider = (*brain.CerebrasProvider)(nil)

func TestCerebrasProvider_Name(t *testing.T) {
	p := brain.NewCerebrasProvider("key", "")
	if p.Name() != "cerebras" {
		t.Errorf("Name() = %q, want cerebras", p.Name())
	}
}

func TestCerebrasProvider_Available_WithKey(t *testing.T) {
	p := brain.NewCerebrasProvider("csk-key", "")
	if !p.Available() {
		t.Error("Available() = false, want true when API key is set")
	}
}

func TestCerebrasProvider_Available_WithoutKey(t *testing.T) {
	p := brain.NewCerebrasProvider("", "")
	if p.Available() {
		t.Error("Available() = true, want false when API key is empty")
	}
}

func TestCerebrasProvider_Complete_SendsCorrectAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("Authorization = %q, want Bearer prefix", auth)
		}
		if !strings.Contains(auth, "csk-test-key") {
			t.Errorf("Authorization = %q, want to contain csk-test-key", auth)
		}

		resp := api.ChatCompletionResponse{
			ID:    "chatcmpl-cerebras-001",
			Model: "llama-3.3-70b",
			Choices: []api.ChatCompletionChoice{
				{
					Message:      api.ChatMessage{Role: "assistant", Content: "Cerebras reply"},
					FinishReason: "stop",
				},
			},
			Usage: &api.Usage{PromptTokens: 4, CompletionTokens: 2, TotalTokens: 6},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	p := brain.NewCerebrasProvider("csk-test-key", srv.URL)
	resp, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "llama-3.3-70b",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.ID != "chatcmpl-cerebras-001" {
		t.Errorf("ID = %q, want chatcmpl-cerebras-001", resp.ID)
	}
	if resp.Message.Content != "Cerebras reply" {
		t.Errorf("Content = %q, want 'Cerebras reply'", resp.Message.Content)
	}
}

func TestCerebrasProvider_FetchModels(t *testing.T) {
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
				{ID: "llama-3.3-70b"},
				{ID: "llama-4-scout-17b-16e-instruct"},
				{ID: "qwen-3-32b"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	p := brain.NewCerebrasProvider("key", srv.URL)
	if err := p.FetchModels(context.Background(), nil); err != nil {
		t.Fatalf("FetchModels returned error: %v", err)
	}
	if len(p.Models()) != 3 {
		t.Errorf("Models() len = %d, want 3", len(p.Models()))
	}
}

func TestCerebrasProvider_Complete_Returns5xxAsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := brain.NewCerebrasProvider("key", srv.URL)
	_, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "m",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
	var pe *brain.ProviderError
	if !brain.AsProviderError(err, &pe) {
		t.Fatalf("expected *ProviderError, got %T", err)
	}
	if pe.Provider != "cerebras" {
		t.Errorf("Provider = %q, want cerebras", pe.Provider)
	}
	if pe.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want 500", pe.StatusCode)
	}
}
