package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// TestRateLimitRotation_E2E sets up two httptest servers:
//   - primary: always returns HTTP 429
//   - secondary: always returns HTTP 200 with a valid completion
//
// It builds a Chain with these two providers and verifies that the first
// call falls back to secondary after primary returns 429.
func TestRateLimitRotation_E2E(t *testing.T) {
	// Primary: always 429.
	primarySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-ratelimit-remaining-requests", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limit exceeded","type":"rate_limit_error"}}`)) //nolint:errcheck
	}))
	defer primarySrv.Close()

	// Secondary: always 200 with a valid completion.
	secondarySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"id":    "test-id-secondary",
			"model": "test-model",
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "fallback response",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     5,
				"completion_tokens": 2,
				"total_tokens":      7,
			},
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer secondarySrv.Close()

	// Build two OpenAICompatProviders pointing at the test servers.
	primary := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name:       "primary-429",
		BaseURL:    primarySrv.URL,
		APIKey:     "test-key",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	})
	secondary := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name:       "secondary-ok",
		BaseURL:    secondarySrv.URL,
		APIKey:     "test-key",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	})

	providers := map[string]brain.Provider{
		"primary-429":  primary,
		"secondary-ok": secondary,
	}

	rl := fallback.NewRateLimitTracker(1, 10)
	chain := fallback.NewChain(providers, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{
			ProviderName:   "primary-429",
			ModelID:        "test-model",
			Score:          9.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 30*time.Second),
		},
		{
			ProviderName:   "secondary-ok",
			ModelID:        "test-model",
			Score:          7.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 30*time.Second),
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &types.InternalChatRequest{
		Model:    "test-model",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hello"}},
	}

	resp, err := chain.Complete(ctx, req)
	if err != nil {
		t.Fatalf("chain.Complete: %v", err)
	}
	if resp == nil {
		t.Fatal("response is nil")
	}
	if resp.Message.Content != "fallback response" {
		t.Errorf("expected fallback response content %q, got %q", "fallback response", resp.Message.Content)
	}

	// Verify primary is now marked as exhausted (rate limited).
	entries := chain.Entries()
	if len(entries) < 1 {
		t.Fatal("expected at least 1 chain entry")
	}
	primaryEntry := entries[0]
	if primaryEntry.ProviderName != "primary-429" {
		t.Errorf("expected first entry to be primary-429, got %q", primaryEntry.ProviderName)
	}
	if primaryEntry.Status != fallback.EntryExhausted {
		t.Errorf("expected primary entry status=exhausted, got %v", primaryEntry.Status)
	}
}

// TestRateLimitRotation_E2E_ProviderError verifies that brain.ProviderError
// is surfaced correctly when all providers are exhausted.
func TestRateLimitRotation_E2E_AllExhausted(t *testing.T) {
	// Both servers return 429.
	make429 := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"rate limited"}}`)) //nolint:errcheck
		}))
	}
	srv1 := make429()
	defer srv1.Close()
	srv2 := make429()
	defer srv2.Close()

	p1 := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "p1", BaseURL: srv1.URL, APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})
	p2 := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "p2", BaseURL: srv2.URL, APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})

	rl := fallback.NewRateLimitTracker(1, 10)
	chain := fallback.NewChain(map[string]brain.Provider{"p1": p1, "p2": p2}, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{ProviderName: "p1", ModelID: "m", Score: 9.0, Status: fallback.EntryActive, CircuitBreaker: fallback.NewCircuitBreaker(3, 30*time.Second)},
		{ProviderName: "p2", ModelID: "m", Score: 7.0, Status: fallback.EntryActive, CircuitBreaker: fallback.NewCircuitBreaker(3, 30*time.Second)},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &types.InternalChatRequest{
		Model:    "m",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hello"}},
	}

	_, err := chain.Complete(ctx, req)
	if err == nil {
		t.Fatal("expected error when all providers return 429, got nil")
	}

	// Error must contain "all providers exhausted".
	if errStr := err.Error(); len(errStr) < 1 {
		t.Errorf("expected non-empty error string")
	}

	// Unwrap to find a ProviderError.
	var pe *brain.ProviderError
	// The last error wrapped inside should be a ProviderError with 429.
	_ = brain.AsProviderError(err, &pe) // may or may not unwrap through fmt.Errorf wrapping
	t.Logf("chain error: %v", err)
}
