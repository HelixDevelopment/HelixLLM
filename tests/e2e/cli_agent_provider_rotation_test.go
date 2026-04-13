//go:build !short

package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// TestFallbackChain_MultiRequestRotation verifies that a provider that becomes
// rate-limited mid-session is skipped for subsequent requests and the chain
// routes to the next available provider.
//
// Scenario:
//   - provider-a: succeeds for the first 3 calls, then always returns 429.
//   - provider-b: always succeeds.
//   - 10 sequential requests are sent; all must succeed.
//   - After the 429 from provider-a, requests must be served by provider-b.
func TestFallbackChain_MultiRequestRotation(t *testing.T) {
	t.Parallel()

	var aCallCount atomic.Int32

	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := aCallCount.Add(1)
		if n > 3 {
			// Rate-limit after the third successful call.
			w.Header().Set("x-ratelimit-remaining-requests", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		writeOKJSON(w, "response-from-a")
	}))
	defer srvA.Close()

	srvB := makeOKServer(t, "response-from-b")
	defer srvB.Close()

	providerA := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "provider-a", BaseURL: srvA.URL,
		APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})
	providerB := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "provider-b", BaseURL: srvB.URL,
		APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})

	providers := map[string]brain.Provider{
		"provider-a": providerA,
		"provider-b": providerB,
	}
	rl := fallback.NewRateLimitTracker(1, 10)
	chain := fallback.NewChain(providers, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{
			ProviderName:   "provider-a",
			ModelID:        "test-model",
			Score:          9.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(5, 30*time.Second),
		},
		{
			ProviderName:   "provider-b",
			ModelID:        "test-model",
			Score:          7.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(5, 30*time.Second),
		},
	})

	req := &types.InternalChatRequest{
		Model:    "test-model",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "rotation test"}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	successCount := 0
	fromBCount := 0

	const totalRequests = 10
	for i := range totalRequests {
		resp, err := chain.Complete(ctx, req)
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i+1, err)
		}
		successCount++
		if resp.Message.Content == "response-from-b" {
			fromBCount++
		}
	}

	if successCount != totalRequests {
		t.Errorf("expected %d successful requests, got %d", totalRequests, successCount)
	}

	// After provider-a hits 429 on request 4, requests 4–10 (7 requests) must
	// come from provider-b.
	if fromBCount < 7 {
		t.Errorf("expected at least 7 responses from provider-b, got %d", fromBCount)
	}

	// provider-a must be exhausted after 429.
	entries := chain.Entries()
	var aEntry *fallback.ChainEntry
	for i := range entries {
		if entries[i].ProviderName == "provider-a" {
			e := entries[i]
			aEntry = &e
			break
		}
	}
	if aEntry == nil {
		t.Fatal("provider-a entry not found in chain")
	}
	if aEntry.Status != fallback.EntryExhausted {
		t.Errorf("provider-a status = %v, want exhausted", aEntry.Status)
	}
}

// TestFallbackChain_CooldownRecovery verifies that an exhausted entry recovers
// after its CooldownUntil time passes and starts serving requests again.
func TestFallbackChain_CooldownRecovery(t *testing.T) {
	t.Parallel()

	// provider-a: always 429.
	srvA := make429Server()
	defer srvA.Close()

	srvB := makeOKServer(t, "cooldown-response-b")
	defer srvB.Close()

	providerA := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "cooldown-a", BaseURL: srvA.URL,
		APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})
	providerB := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "cooldown-b", BaseURL: srvB.URL,
		APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})

	providers := map[string]brain.Provider{
		"cooldown-a": providerA,
		"cooldown-b": providerB,
	}
	rl := fallback.NewRateLimitTracker(1, 10)
	chain := fallback.NewChain(providers, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{
			ProviderName:   "cooldown-a",
			ModelID:        "m",
			Score:          9.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(5, 30*time.Second),
		},
		{
			ProviderName:   "cooldown-b",
			ModelID:        "m",
			Score:          7.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(5, 30*time.Second),
		},
	})

	req := &types.InternalChatRequest{
		Model:    "m",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "cooldown test"}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// First call: primary returns 429 → chain falls back to secondary.
	resp, err := chain.Complete(ctx, req)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	if resp.Message.Content != "cooldown-response-b" {
		t.Errorf("first call: expected %q, got %q", "cooldown-response-b", resp.Message.Content)
	}

	// primary should now be exhausted with a future CooldownUntil.
	entries := chain.Entries()
	if entries[0].Status != fallback.EntryExhausted {
		t.Errorf("primary status = %v, want exhausted", entries[0].Status)
	}
	if entries[0].CooldownUntil.IsZero() {
		t.Error("expected non-zero CooldownUntil for exhausted primary")
	}

	// Manually back-date CooldownUntil to simulate time passing.
	entries[0].CooldownUntil = time.Now().Add(-1 * time.Second)
	entries[0].Status = fallback.EntryExhausted // still exhausted but cooldown expired
	chain.SetEntries(entries)

	// Now swap provider-a to a healthy server so recovery can be verified.
	srvAHealthy := makeOKServer(t, "cooldown-response-a-recovered")
	defer srvAHealthy.Close()
	providerAHealthy := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "cooldown-a", BaseURL: srvAHealthy.URL,
		APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})
	// Rebuild chain with recovered provider-a.
	providers["cooldown-a"] = providerAHealthy
	chain2 := fallback.NewChain(providers, rl)
	recoveredEntries := chain.Entries()
	chain2.SetEntries(recoveredEntries)

	resp2, err := chain2.Complete(ctx, req)
	if err != nil {
		t.Fatalf("recovery call error: %v", err)
	}
	// After cooldown recovery the primary (now healthy) should answer again.
	if resp2.Message.Content != "cooldown-response-a-recovered" {
		t.Errorf("recovery call: expected %q, got %q",
			"cooldown-response-a-recovered", resp2.Message.Content)
	}
}

// writeOKJSON writes a valid chat-completion JSON response with the given
// content string.  It is a package-level helper used by multiple test files.
func writeOKJSON(w http.ResponseWriter, content string) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{
		"id":    "test-ok",
		"model": "test-model",
		"choices": []map[string]interface{}{
			{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     5,
			"completion_tokens": 3,
			"total_tokens":      8,
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
}
