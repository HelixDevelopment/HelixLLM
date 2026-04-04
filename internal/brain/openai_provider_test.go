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

func TestOpenAIProvider_Satisfies(t *testing.T) {
	var _ brain.Provider = (*brain.OpenAIProvider)(nil)
}

func TestOpenAIProvider_Name(t *testing.T) {
	p := brain.NewOpenAI("sk-test", "")
	if p.Name() != "openai" {
		t.Errorf("Name() = %q, want %q", p.Name(), "openai")
	}
}

func TestOpenAIProvider_Models(t *testing.T) {
	p := brain.NewOpenAI("sk-test", "")
	models := p.Models()
	if len(models) == 0 {
		t.Fatal("Models() returned empty slice")
	}
	// Default models must include at least gpt-4o.
	found := false
	for _, m := range models {
		if m == "gpt-4o" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Models() = %v, expected gpt-4o to be present", models)
	}
}

func TestOpenAIProvider_Available_WithKey(t *testing.T) {
	p := brain.NewOpenAI("sk-test-key", "")
	if !p.Available() {
		t.Error("Available() = false, want true (API key is set)")
	}
}

func TestOpenAIProvider_Available_WithoutKey(t *testing.T) {
	p := brain.NewOpenAI("", "")
	if p.Available() {
		t.Error("Available() = true, want false (no API key)")
	}
}

func TestOpenAIProvider_Complete(t *testing.T) {
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

		// Verify Authorization header.
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("Authorization header = %q, want Bearer token", auth)
		}

		var req api.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Model != "gpt-4o" {
			t.Errorf("request model = %q, want %q", req.Model, "gpt-4o")
		}

		resp := api.ChatCompletionResponse{
			ID:      "chatcmpl-openai-001",
			Object:  "chat.completion",
			Created: 1700000000,
			Model:   "gpt-4o",
			Choices: []api.ChatCompletionChoice{
				{
					Index: 0,
					Message: api.ChatMessage{
						Role:    "assistant",
						Content: "Hello from OpenAI!",
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

	p := brain.NewOpenAI("sk-test", srv.URL)

	resp, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model: "gpt-4o",
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.ID != "chatcmpl-openai-001" {
		t.Errorf("ID = %q, want %q", resp.ID, "chatcmpl-openai-001")
	}
	if resp.Message.Content != "Hello from OpenAI!" {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "Hello from OpenAI!")
	}
	if resp.Provider != types.ProviderOpenAI {
		t.Errorf("Provider = %q, want %q", resp.Provider, types.ProviderOpenAI)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("Usage.TotalTokens = %d, want 15", resp.Usage.TotalTokens)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", resp.FinishReason, "stop")
	}
}

func TestOpenAIProvider_CompleteStream(t *testing.T) {
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

		tokens := []string{"Hello", " from", " OpenAI!"}
		stopStr := "stop"
		for i, token := range tokens {
			chunk := api.ChatCompletionChunk{
				ID:      "chatcmpl-openai-002",
				Object:  "chat.completion.chunk",
				Created: 1700000000,
				Model:   "gpt-4o",
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

	p := brain.NewOpenAI("sk-test", srv.URL)

	ch, err := p.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model: "gpt-4o",
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
	if chunks[2].Content != " OpenAI!" {
		t.Errorf("chunks[2].Content = %q, want %q", chunks[2].Content, " OpenAI!")
	}
	if chunks[2].FinishReason != "stop" {
		t.Errorf("chunks[2].FinishReason = %q, want %q", chunks[2].FinishReason, "stop")
	}
}

func TestOpenAIProvider_CompleteError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := brain.NewOpenAI("sk-test", srv.URL)

	_, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "gpt-4o",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error from 500 response, got nil")
	}
}

func TestOpenAIProvider_DefaultBaseURL(t *testing.T) {
	p := brain.NewOpenAI("sk-test", "")
	// Verify that the provider was created without panic and that Available() works.
	if !p.Available() {
		t.Error("Available() = false, want true")
	}
}

func TestOpenAIProvider_CompleteStream_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	p := brain.NewOpenAI("sk-test", srv.URL)

	_, err := p.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model:    "gpt-4o",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
		Stream:   true,
	})
	if err == nil {
		t.Fatal("expected error from 503 streaming response, got nil")
	}
}

func TestOpenAIProvider_CompleteStream_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		chunk := api.ChatCompletionChunk{
			ID:    "chatcmpl-ctx-001",
			Model: "gpt-4o",
			Choices: []api.ChatCompletionChunkChoice{
				{Delta: api.ChatMessageDelta{Content: "hi"}},
			},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	p := brain.NewOpenAI("sk-test", srv.URL)
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := p.CompleteStream(ctx, &types.InternalChatRequest{
		Model:    "gpt-4o",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("CompleteStream returned error: %v", err)
	}

	<-ch
	cancel()
	for range ch {
	}
}

func TestOpenAIProvider_ToAPIRequest_WithMaxTokensAndTemperature(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req api.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.MaxTokens == nil || *req.MaxTokens != 1024 {
			t.Errorf("MaxTokens = %v, want 1024", req.MaxTokens)
		}
		if req.Temperature == nil || *req.Temperature != 0.9 {
			t.Errorf("Temperature = %v, want 0.9", req.Temperature)
		}
		resp := api.ChatCompletionResponse{
			ID:    "chatcmpl-mt-001",
			Model: "gpt-4o",
			Choices: []api.ChatCompletionChoice{
				{Message: api.ChatMessage{Role: "assistant", Content: "ok"}, FinishReason: "stop"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := brain.NewOpenAI("sk-test", srv.URL)
	_, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:       "gpt-4o",
		Messages:    []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
		MaxTokens:   1024,
		Temperature: 0.9,
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
}

func TestOpenAIProvider_FromAPIResponse_NoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := api.ChatCompletionResponse{
			ID:      "chatcmpl-nc-001",
			Model:   "gpt-4o",
			Choices: []api.ChatCompletionChoice{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := brain.NewOpenAI("sk-test", srv.URL)
	resp, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "gpt-4o",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.Message.Content != "" {
		t.Errorf("expected empty content for no-choices response, got %q", resp.Message.Content)
	}
}

func TestOpenAIProvider_ReadSSEStream_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "data: {bad json\n\n")
		chunk := api.ChatCompletionChunk{
			Model: "gpt-4o",
			Choices: []api.ChatCompletionChunkChoice{
				{Delta: api.ChatMessageDelta{Content: "ok"}},
			},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := brain.NewOpenAI("sk-test", srv.URL)
	ch, err := p.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model:    "gpt-4o",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}
	var chunks []types.StreamChunk
	for c := range ch {
		chunks = append(chunks, c)
	}
	if len(chunks) != 1 || chunks[0].Content != "ok" {
		t.Errorf("unexpected chunks: %v", chunks)
	}
}

func TestOpenAIProvider_ReadSSEStream_SkipsNonDataLines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, ": comment line\n\n")
		fmt.Fprintf(w, "\n")
		chunk := api.ChatCompletionChunk{
			Model: "gpt-4o",
			Choices: []api.ChatCompletionChunkChoice{
				{Delta: api.ChatMessageDelta{Content: "world"}},
			},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := brain.NewOpenAI("sk-test", srv.URL)
	ch, err := p.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model:    "gpt-4o",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}
	var chunks []types.StreamChunk
	for c := range ch {
		chunks = append(chunks, c)
	}
	if len(chunks) != 1 || chunks[0].Content != "world" {
		t.Errorf("unexpected chunks: %v", chunks)
	}
}

func TestOpenAIProvider_Complete_DecodeError(t *testing.T) {
	// Server returns 200 but with non-JSON body → decode must fail.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "not valid json}")
	}))
	defer srv.Close()

	p := brain.NewOpenAI("sk-test", srv.URL)
	_, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "gpt-4o",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
}

func TestOpenAIProvider_NetworkError(t *testing.T) {
	// Use a server that accepts then immediately closes.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", 500)
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	p := brain.NewOpenAI("sk-test", srv.URL)
	_, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "gpt-4o",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error for connection reset, got nil")
	}
}

func TestOpenAIProvider_CompleteStream_CtxDoneInLoop(t *testing.T) {
	// Cancel the context immediately after CompleteStream returns so the
	// SSE reader goroutine hits the ctx.Done() select case.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		chunk := api.ChatCompletionChunk{
			ID:    "burst",
			Model: "gpt-4o",
			Choices: []api.ChatCompletionChunkChoice{
				{Delta: api.ChatMessageDelta{Content: "x"}},
			},
		}
		data, _ := json.Marshal(chunk)
		for i := 0; i < 1000; i++ {
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	p := brain.NewOpenAI("sk-test", srv.URL)
	ch, err := p.CompleteStream(ctx, &types.InternalChatRequest{
		Model:    "gpt-4o",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("CompleteStream returned error: %v", err)
	}
	cancel()
	for range ch {
	}
}

func TestOpenAIProvider_CompleteStream_NetworkError(t *testing.T) {
	// Server closes the connection immediately, causing httpClient.Do to error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", 500)
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	p := brain.NewOpenAI("sk-test", srv.URL)
	_, err := p.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model:    "gpt-4o",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
		Stream:   true,
	})
	if err == nil {
		t.Fatal("expected error for connection reset in CompleteStream, got nil")
	}
}
