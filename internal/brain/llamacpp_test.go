package brain_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

func TestLlamaCppProvider_Satisfies(t *testing.T) {
	var _ brain.Provider = (*brain.LlamaCppProvider)(nil)
}

func TestLlamaCppProvider_Name(t *testing.T) {
	p := brain.NewLlamaCppProvider("http://localhost:8080", []string{"llama-3.1-70b"})
	if p.Name() != "llamacpp" {
		t.Errorf("Name() = %q, want %q", p.Name(), "llamacpp")
	}
}

func TestLlamaCppProvider_Models(t *testing.T) {
	models := []string{"llama-3.1-70b", "llama-3.1-8b"}
	p := brain.NewLlamaCppProvider("http://localhost:8080", models)
	got := p.Models()
	if len(got) != 2 {
		t.Fatalf("Models() length = %d, want 2", len(got))
	}
	if got[0] != "llama-3.1-70b" {
		t.Errorf("Models()[0] = %q, want %q", got[0], "llama-3.1-70b")
	}
}

func TestLlamaCppProvider_Complete(t *testing.T) {
	// Mock llama.cpp's OpenAI-compatible /v1/chat/completions endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Decode request to verify it was sent correctly.
		var req api.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Model != "llama-3.1-70b" {
			t.Errorf("request model = %q, want %q", req.Model, "llama-3.1-70b")
		}

		resp := api.ChatCompletionResponse{
			ID:      "chatcmpl-local-001",
			Object:  "chat.completion",
			Created: 1700000000,
			Model:   "llama-3.1-70b",
			Choices: []api.ChatCompletionChoice{
				{
					Index: 0,
					Message: api.ChatMessage{
						Role:    "assistant",
						Content: "Hello from llama.cpp!",
					},
					FinishReason: "stop",
				},
			},
			Usage: &api.Usage{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := brain.NewLlamaCppProvider(srv.URL, []string{"llama-3.1-70b"})

	resp, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model: "llama-3.1-70b",
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.ID != "chatcmpl-local-001" {
		t.Errorf("ID = %q, want %q", resp.ID, "chatcmpl-local-001")
	}
	if resp.Message.Content != "Hello from llama.cpp!" {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "Hello from llama.cpp!")
	}
	if resp.Provider != types.ProviderLocal {
		t.Errorf("Provider = %q, want %q", resp.Provider, types.ProviderLocal)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("Usage.TotalTokens = %d, want 15", resp.Usage.TotalTokens)
	}
}

func TestLlamaCppProvider_CompleteStream(t *testing.T) {
	// Mock llama.cpp streaming endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req api.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if !req.Stream {
			t.Error("expected stream=true in request")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)

		tokens := []string{"Hello", " from", " llama.cpp!"}
		stopStr := "stop"
		for i, token := range tokens {
			chunk := api.ChatCompletionChunk{
				ID:      "chatcmpl-local-002",
				Object:  "chat.completion.chunk",
				Created: 1700000000,
				Model:   "llama-3.1-70b",
				Choices: []api.ChatCompletionChunkChoice{
					{
						Index: 0,
						Delta: api.ChatMessageDelta{
							Content: token,
						},
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

	p := brain.NewLlamaCppProvider(srv.URL, []string{"llama-3.1-70b"})

	ch, err := p.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model: "llama-3.1-70b",
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "Hello"},
		},
		Stream: true,
	})
	if err != nil {
		t.Fatalf("CompleteStream returned error: %v", err)
	}

	var chunks []types.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	if chunks[0].Content != "Hello" {
		t.Errorf("chunks[0].Content = %q, want %q", chunks[0].Content, "Hello")
	}
	if chunks[1].Content != " from" {
		t.Errorf("chunks[1].Content = %q, want %q", chunks[1].Content, " from")
	}
	if chunks[2].Content != " llama.cpp!" {
		t.Errorf("chunks[2].Content = %q, want %q", chunks[2].Content, " llama.cpp!")
	}
	if chunks[2].FinishReason != "stop" {
		t.Errorf("chunks[2].FinishReason = %q, want %q", chunks[2].FinishReason, "stop")
	}
}

func TestLlamaCppProvider_Available(t *testing.T) {
	// Available server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	p := brain.NewLlamaCppProvider(srv.URL, []string{"llama-3.1-70b"})
	if !p.Available() {
		t.Error("Available() = false, want true (server is up)")
	}

	// Unavailable server (closed).
	srv.Close()
	p2 := brain.NewLlamaCppProvider(srv.URL, []string{"llama-3.1-70b"})
	if p2.Available() {
		t.Error("Available() = true, want false (server is down)")
	}
}

func TestLlamaCppProvider_CompleteError(t *testing.T) {
	// Server that returns a 500 error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := brain.NewLlamaCppProvider(srv.URL, []string{"llama-3.1-70b"})

	_, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "llama-3.1-70b",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error from 500 response, got nil")
	}
}

func TestLlamaCppProvider_CompleteContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response; context should cancel before we respond.
		<-r.Context().Done()
	}))
	defer srv.Close()

	p := brain.NewLlamaCppProvider(srv.URL, []string{"llama-3.1-70b"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := p.Complete(ctx, &types.InternalChatRequest{
		Model:    "llama-3.1-70b",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}
