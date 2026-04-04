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

func TestAnthropicProvider_Satisfies(t *testing.T) {
	var _ brain.Provider = (*brain.AnthropicProvider)(nil)
}

func TestAnthropicProvider_Name(t *testing.T) {
	p := brain.NewAnthropic("sk-ant-test", "")
	if p.Name() != "anthropic" {
		t.Errorf("Name() = %q, want %q", p.Name(), "anthropic")
	}
}

func TestAnthropicProvider_Models(t *testing.T) {
	p := brain.NewAnthropic("sk-ant-test", "")
	models := p.Models()
	if len(models) == 0 {
		t.Fatal("Models() returned empty slice")
	}
}

func TestAnthropicProvider_Available_WithKey(t *testing.T) {
	p := brain.NewAnthropic("sk-ant-test", "")
	if !p.Available() {
		t.Error("Available() = false, want true (API key is set)")
	}
}

func TestAnthropicProvider_Available_WithoutKey(t *testing.T) {
	p := brain.NewAnthropic("", "")
	if p.Available() {
		t.Error("Available() = true, want false (no API key)")
	}
}

func TestAnthropicProvider_Complete(t *testing.T) {
	stopReason := "end_turn"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Verify required Anthropic headers.
		if r.Header.Get("x-api-key") == "" {
			t.Error("x-api-key header is missing")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("anthropic-version header is missing")
		}

		var req api.MessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Model != "claude-sonnet-4-5" {
			t.Errorf("request model = %q, want %q", req.Model, "claude-sonnet-4-5")
		}

		resp := api.MessageResponse{
			ID:   "msg-anthropic-001",
			Type: "message",
			Role: "assistant",
			Content: []api.ContentBlock{
				{Type: "text", Text: "Hello from Anthropic!"},
			},
			Model:      "claude-sonnet-4-5",
			StopReason: &stopReason,
			Usage: api.AnthropicUsage{
				InputTokens:  10,
				OutputTokens: 5,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := brain.NewAnthropic("sk-ant-test", srv.URL)

	resp, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model: "claude-sonnet-4-5",
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.ID != "msg-anthropic-001" {
		t.Errorf("ID = %q, want %q", resp.ID, "msg-anthropic-001")
	}
	if resp.Message.Content != "Hello from Anthropic!" {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "Hello from Anthropic!")
	}
	if resp.Provider != types.ProviderAnthropic {
		t.Errorf("Provider = %q, want %q", resp.Provider, types.ProviderAnthropic)
	}
	if resp.FinishReason != "end_turn" {
		t.Errorf("FinishReason = %q, want %q", resp.FinishReason, "end_turn")
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("Usage.PromptTokens = %d, want 10", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 5 {
		t.Errorf("Usage.CompletionTokens = %d, want 5", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("Usage.TotalTokens = %d, want 15", resp.Usage.TotalTokens)
	}
}

func TestAnthropicProvider_CompleteWithSystemMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req api.MessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// The system message must be in the top-level system field.
		if req.System != "You are a helpful assistant." {
			t.Errorf("System = %q, want %q", req.System, "You are a helpful assistant.")
		}
		// The messages slice must not contain the system message.
		for _, m := range req.Messages {
			if m.Role == "system" {
				t.Error("system message found in messages array, should be in top-level system field")
			}
		}

		stopReason := "end_turn"
		resp := api.MessageResponse{
			ID:   "msg-sys-001",
			Type: "message",
			Role: "assistant",
			Content: []api.ContentBlock{
				{Type: "text", Text: "Sure!"},
			},
			Model:      "claude-sonnet-4-5",
			StopReason: &stopReason,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := brain.NewAnthropic("sk-ant-test", srv.URL)

	_, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model: "claude-sonnet-4-5",
		Messages: []types.InternalMessage{
			{Role: types.RoleSystem, Content: "You are a helpful assistant."},
			{Role: types.RoleUser, Content: "Help me."},
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
}

func TestAnthropicProvider_CompleteStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req api.MessageRequest
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

		// Write Anthropic-format SSE events.
		writeEvent := func(event, data string) {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
			flusher.Flush()
		}

		writeEvent("message_start", `{"type":"message_start","message":{"id":"msg-stream-001","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-5","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`)
		writeEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		writeEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`)
		writeEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" from Anthropic"}}`)
		writeEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`)
		writeEvent("message_stop", `{"type":"message_stop"}`)
	}))
	defer srv.Close()

	p := brain.NewAnthropic("sk-ant-test", srv.URL)

	ch, err := p.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model: "claude-sonnet-4-5",
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
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if chunks[0].Content != "Hello" {
		t.Errorf("chunks[0].Content = %q, want %q", chunks[0].Content, "Hello")
	}
	if chunks[1].Content != " from Anthropic" {
		t.Errorf("chunks[1].Content = %q, want %q", chunks[1].Content, " from Anthropic")
	}
}

func TestAnthropicProvider_CompleteError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := brain.NewAnthropic("sk-ant-test", srv.URL)

	_, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error from 500 response, got nil")
	}
}

func TestAnthropicProvider_CompleteStream_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := brain.NewAnthropic("sk-ant-test", srv.URL)

	_, err := p.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
		Stream:   true,
	})
	if err == nil {
		t.Fatal("expected error from 500 streaming response, got nil")
	}
}

func TestAnthropicProvider_CompleteStream_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		// Write one event then block until client disconnects.
		fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n")
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	p := brain.NewAnthropic("sk-ant-test", srv.URL)

	ctx, cancel := context.WithCancel(context.Background())

	ch, err := p.CompleteStream(ctx, &types.InternalChatRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hello"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("CompleteStream returned error: %v", err)
	}

	// Read one chunk then cancel.
	<-ch
	cancel()

	// Drain the channel after cancel — should close cleanly.
	for range ch {
	}
}

func TestAnthropicProvider_ToMessageRequest_WithTemperatureAndMaxTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req api.MessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		// Verify max_tokens and temperature were forwarded.
		if req.MaxTokens != 256 {
			t.Errorf("MaxTokens = %d, want 256", req.MaxTokens)
		}
		if req.Temperature == nil || *req.Temperature != 0.7 {
			t.Errorf("Temperature not set correctly")
		}

		stopReason := "end_turn"
		resp := api.MessageResponse{
			ID:         "msg-temp-001",
			Type:       "message",
			Role:       "assistant",
			Content:    []api.ContentBlock{{Type: "text", Text: "ok"}},
			Model:      "claude-haiku-4-5",
			StopReason: &stopReason,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := brain.NewAnthropic("sk-ant-test", srv.URL)

	_, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model: "claude-haiku-4-5",
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "Hello"},
		},
		MaxTokens:   256,
		Temperature: 0.7,
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
}

func TestAnthropicProvider_SSEStream_BlankLineResetsEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)

		// Send: an unknown event followed by a blank line (should reset currentEvent),
		// then a content_block_delta with text.
		fmt.Fprintf(w, "event: unknown_event\n")
		fmt.Fprintf(w, "data: {\"type\":\"unknown\"}\n\n") // blank line resets event
		fmt.Fprintf(w, "event: content_block_delta\n")
		fmt.Fprintf(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n")
		fmt.Fprintf(w, "event: message_stop\n")
		fmt.Fprintf(w, "data: {\"type\":\"message_stop\"}\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := brain.NewAnthropic("sk-ant-test", srv.URL)

	ch, err := p.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	var chunks []types.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 1 || chunks[0].Content != "hello" {
		t.Errorf("unexpected chunks: %v", chunks)
	}
}

func TestAnthropicProvider_CompleteStream_NetworkError(t *testing.T) {
	// Use a server that accepts then immediately closes the connection.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hijack and close immediately to simulate network error.
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", 500)
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	p := brain.NewAnthropic("sk-ant-test", srv.URL)

	_, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error for connection reset, got nil")
	}
}

func TestAnthropicProvider_ReadSSEStream_BadJSON(t *testing.T) {
	// A content_block_delta event with bad JSON data → readAnthropicSSEStream skips it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		// Bad JSON on a content_block_delta.
		fmt.Fprintf(w, "event: content_block_delta\ndata: {bad json\n\n")
		// Good delta.
		fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n")
		fmt.Fprintf(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := brain.NewAnthropic("sk-ant-test", srv.URL)
	ch, err := p.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model:    "claude-sonnet-4-5",
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
	if len(chunks) != 1 || chunks[0].Content != "hi" {
		t.Errorf("unexpected chunks: %v", chunks)
	}
}

func TestAnthropicProvider_Complete_DecodeError(t *testing.T) {
	// Server returns 200 but with non-JSON body → decode must fail.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "not valid json}")
	}))
	defer srv.Close()

	p := brain.NewAnthropic("sk-ant-test", srv.URL)
	_, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
}

func TestAnthropicProvider_CompleteStream_CtxDoneInLoop(t *testing.T) {
	// Cancel the context immediately after CompleteStream returns.
	// The SSE reader goroutine will see ctx.Done() in the select after
	// its first scanner.Scan() succeeds.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		// Keep writing events so the scanner has data to scan.
		for i := 0; i < 1000; i++ {
			fmt.Fprintf(w,
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"x\"}}\n\n")
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())

	p := brain.NewAnthropic("sk-ant-test", srv.URL)
	ch, err := p.CompleteStream(ctx, &types.InternalChatRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("CompleteStream returned error: %v", err)
	}
	// Cancel immediately — goroutine will hit ctx.Done() in its select loop.
	cancel()
	for range ch {
	}
}

func TestAnthropicProvider_CompleteStream_DoError(t *testing.T) {
	// Server closes the connection immediately, causing httpClient.Do to error
	// for the CompleteStream request (covers the Do-error branch in CompleteStream).
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

	p := brain.NewAnthropic("sk-ant-test", srv.URL)

	_, err := p.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
		Stream:   true,
	})
	if err == nil {
		t.Fatal("expected error for connection reset in CompleteStream, got nil")
	}
}

func TestAnthropicProvider_FromMessageResponse_NoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a response with no content blocks.
		resp := api.MessageResponse{
			ID:    "msg-empty-001",
			Type:  "message",
			Role:  "assistant",
			Model: "claude-sonnet-4-5",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := brain.NewAnthropic("sk-ant-test", srv.URL)

	resp, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.Message.Content != "" {
		t.Errorf("expected empty content for response with no blocks, got %q", resp.Message.Content)
	}
}
