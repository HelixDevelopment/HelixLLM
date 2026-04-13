// Package integration_test holds integration tests for HelixLLM.
// These tests require live environment variables and are skipped unless
// HELIX_INTEGRATION_TESTS=true is set.
package integration_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// TestFallbackChain_Integration builds a real fallback chain from whatever
// API keys are available in the environment and sends a simple chat request
// through it.  The test is skipped unless HELIX_INTEGRATION_TESTS=true.
func TestFallbackChain_Integration(t *testing.T) {
	if os.Getenv("HELIX_INTEGRATION_TESTS") != "true" {
		t.Skip("HELIX_INTEGRATION_TESTS not set — skipping integration test")
	}

	providers := map[string]brain.Provider{}
	entries := []fallback.ChainEntry{}

	// Register whichever cloud providers have keys configured.
	score := 90.0
	if key := os.Getenv("HELIX_LLM_CHUTES_KEY"); key != "" {
		p := brain.NewChutesProvider(key, "")
		providers["chutes"] = p
		entries = append(entries, fallback.ChainEntry{
			ProviderName:   "chutes",
			Score:          score,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 2*time.Minute),
		})
		score -= 10
	}
	if key := os.Getenv("HELIX_LLM_OPENROUTER_KEY"); key != "" {
		p := brain.NewOpenRouterProvider(key, "")
		providers["openrouter"] = p
		entries = append(entries, fallback.ChainEntry{
			ProviderName:   "openrouter",
			Score:          score,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 2*time.Minute),
		})
		score -= 10
	}
	if key := os.Getenv("HELIX_LLM_CEREBRAS_KEY"); key != "" {
		p := brain.NewCerebrasProvider(key, "")
		providers["cerebras"] = p
		entries = append(entries, fallback.ChainEntry{
			ProviderName:   "cerebras",
			Score:          score,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 2*time.Minute),
		})
	}

	if len(providers) == 0 {
		t.Skip("no provider API keys set — skipping integration test")
	}

	rl := fallback.NewRateLimitTracker(5, 100)
	bridge := fallback.NewScorerBridge(fallback.ScorerBridgeConfig{
		VerifierURL:     "", // use static scores
		RefreshInterval: 5 * time.Minute,
	})

	chain := fallback.NewChain(providers, rl)

	// Use scorer bridge to order entries by static scores.
	scores := bridge.StaticFallbackScores()
	models := map[string]string{}
	orderedEntries := bridge.BuildEntries(scores, models)

	// Filter to only the providers we have keys for.
	var available []fallback.ChainEntry
	for _, e := range orderedEntries {
		if _, ok := providers[e.ProviderName]; ok {
			available = append(available, e)
		}
	}
	if len(available) == 0 {
		available = entries
	}
	chain.SetEntries(available)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &types.InternalChatRequest{
		Model: "auto",
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "Reply with exactly one word: hello"},
		},
		MaxTokens: 10,
	}

	resp, err := chain.Complete(ctx, req)
	if err != nil {
		t.Fatalf("chain.Complete: %v", err)
	}
	if resp == nil {
		t.Fatal("response is nil")
	}
	if resp.Message.Content == "" {
		t.Errorf("expected non-empty response content, got empty string")
	}
	t.Logf("provider=%s model=%s content=%q", resp.Provider, resp.Model, resp.Message.Content)
}
