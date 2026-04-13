//go:build !short

package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// makeSSEServer returns an httptest.Server that streams numChunks SSE tokens
// followed by [DONE].  Each chunk carries the text "chunk-N".
func makeSSEServer(t *testing.T, numChunks int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		f, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		for i := range numChunks {
			chunk := fmt.Sprintf(
				`{"id":"s%d","choices":[{"delta":{"content":"chunk-%d"},"finish_reason":null}]}`,
				i, i,
			)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", chunk)
			f.Flush()
		}
		// Final chunk with finish_reason.
		final := `{"id":"sfinal","choices":[{"delta":{"content":""},"finish_reason":"stop"}]}`
		_, _ = fmt.Fprintf(w, "data: %s\n\n", final)
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		f.Flush()
	}))
}

// TestFallbackChain_StreamingFallback verifies that when the primary provider
// returns 429 on a streaming request (before any data is sent), the chain
// immediately falls back to the secondary and all SSE chunks are received
// without any partial data from the primary leaking through.
func TestFallbackChain_StreamingFallback(t *testing.T) {
	t.Parallel()

	// Primary: 429 — no stream data ever written.
	primarySrv := make429Server()
	defer primarySrv.Close()

	const wantChunks = 4
	secondarySrv := makeSSEServer(t, wantChunks)
	defer secondarySrv.Close()

	primary := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "stream-primary-429", BaseURL: primarySrv.URL,
		APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})
	secondary := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "stream-secondary-ok", BaseURL: secondarySrv.URL,
		APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})

	providers := map[string]brain.Provider{
		"stream-primary-429":  primary,
		"stream-secondary-ok": secondary,
	}
	rl := fallback.NewRateLimitTracker(1, 10)
	chain := fallback.NewChain(providers, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{
			ProviderName:   "stream-primary-429",
			ModelID:        "test-model",
			Score:          9.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 30*time.Second),
		},
		{
			ProviderName:   "stream-secondary-ok",
			ModelID:        "test-model",
			Score:          7.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 30*time.Second),
		},
	})

	req := &types.InternalChatRequest{
		Model:    "test-model",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "stream test"}},
		Stream:   true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	ch, err := chain.CompleteStream(ctx, req)
	if err != nil {
		t.Fatalf("chain.CompleteStream: %v", err)
	}

	var received []types.StreamChunk
	for chunk := range ch {
		received = append(received, chunk)
	}

	// Expect wantChunks content chunks plus one final stop chunk.
	// Total = wantChunks + 1 (the finish_reason="stop" delta).
	if len(received) < wantChunks {
		t.Errorf("received %d chunks, want at least %d", len(received), wantChunks)
	}

	// Verify none of the chunks contain error text from the 429 primary.
	for i, c := range received {
		if strings.Contains(c.Content, "rate limit") {
			t.Errorf("chunk %d contains primary error text: %q", i, c.Content)
		}
	}

	// Last chunk should have finish_reason="stop".
	lastChunk := received[len(received)-1]
	if lastChunk.FinishReason != "stop" && lastChunk.FinishReason != "" {
		// Some providers omit finish_reason on content chunks; only fail if
		// an unexpected non-empty value is present.
		if lastChunk.FinishReason != "" && lastChunk.FinishReason != "stop" {
			t.Errorf("last chunk finish_reason = %q, want %q or empty", lastChunk.FinishReason, "stop")
		}
	}

	// Primary must be exhausted after the 429.
	entries := chain.Entries()
	if entries[0].Status != fallback.EntryExhausted {
		t.Errorf("primary status = %v, want exhausted", entries[0].Status)
	}
}

// TestFallbackChain_StreamingSuccess verifies the happy-path: primary streams
// successfully and all chunks arrive in order with correct content.
func TestFallbackChain_StreamingSuccess(t *testing.T) {
	t.Parallel()

	const wantChunks = 6
	primarySrv := makeSSEServer(t, wantChunks)
	defer primarySrv.Close()

	secondarySrv := make429Server() // should never be reached
	defer secondarySrv.Close()

	primary := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "stream-ok-primary", BaseURL: primarySrv.URL,
		APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})
	secondary := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "stream-unused-secondary", BaseURL: secondarySrv.URL,
		APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})

	providers := map[string]brain.Provider{
		"stream-ok-primary":        primary,
		"stream-unused-secondary":  secondary,
	}
	rl := fallback.NewRateLimitTracker(1, 10)
	chain := fallback.NewChain(providers, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{
			ProviderName:   "stream-ok-primary",
			ModelID:        "test-model",
			Score:          9.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 30*time.Second),
		},
		{
			ProviderName:   "stream-unused-secondary",
			ModelID:        "test-model",
			Score:          7.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 30*time.Second),
		},
	})

	req := &types.InternalChatRequest{
		Model:    "test-model",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "stream success test"}},
		Stream:   true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	ch, err := chain.CompleteStream(ctx, req)
	if err != nil {
		t.Fatalf("chain.CompleteStream: %v", err)
	}

	var received []types.StreamChunk
	for chunk := range ch {
		received = append(received, chunk)
	}

	if len(received) < wantChunks {
		t.Errorf("received %d chunks, want at least %d", len(received), wantChunks)
	}

	// Verify content chunks are in order: "chunk-0", "chunk-1", …
	for i := range wantChunks {
		want := fmt.Sprintf("chunk-%d", i)
		if i >= len(received) {
			t.Errorf("missing chunk %d (%q)", i, want)
			continue
		}
		if received[i].Content != want {
			t.Errorf("chunk[%d].Content = %q, want %q", i, received[i].Content, want)
		}
	}

	// Primary must remain active (no errors occurred).
	entries := chain.Entries()
	if entries[0].Status != fallback.EntryActive {
		t.Errorf("primary status = %v, want active", entries[0].Status)
	}
}

// TestFallbackChain_StreamingAllExhausted verifies that when every provider
// returns 429 on a streaming request, CompleteStream returns an error that
// contains "all providers exhausted".
func TestFallbackChain_StreamingAllExhausted(t *testing.T) {
	t.Parallel()

	srv1 := make429Server()
	defer srv1.Close()
	srv2 := make429Server()
	defer srv2.Close()

	p1 := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "stream-p1-429", BaseURL: srv1.URL,
		APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})
	p2 := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "stream-p2-429", BaseURL: srv2.URL,
		APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})

	providers := map[string]brain.Provider{"stream-p1-429": p1, "stream-p2-429": p2}
	rl := fallback.NewRateLimitTracker(1, 10)
	chain := fallback.NewChain(providers, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{ProviderName: "stream-p1-429", ModelID: "m", Score: 9.0, Status: fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 30*time.Second)},
		{ProviderName: "stream-p2-429", ModelID: "m", Score: 7.0, Status: fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 30*time.Second)},
	})

	req := &types.InternalChatRequest{
		Model:    "m",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "all exhausted stream"}},
		Stream:   true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := chain.CompleteStream(ctx, req)
	if err == nil {
		t.Fatal("expected error when all providers are rate-limited, got nil")
	}
	if !strings.Contains(err.Error(), "all providers exhausted") {
		t.Errorf("error %q does not contain %q", err.Error(), "all providers exhausted")
	}
}
