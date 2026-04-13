package integration_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
)

// TestScorerBridge_FetchScores_LiveVerifier contacts the live LLMsVerifier
// at HELIX_LLM_VERIFIER_URL and verifies that FetchScores returns a non-empty
// score map.  Skipped unless HELIX_LLM_VERIFIER_URL is set.
func TestScorerBridge_FetchScores_LiveVerifier(t *testing.T) {
	verifierURL := os.Getenv("HELIX_LLM_VERIFIER_URL")
	if verifierURL == "" {
		t.Skip("HELIX_LLM_VERIFIER_URL not set — skipping scorer bridge integration test")
	}

	bridge := fallback.NewScorerBridge(fallback.ScorerBridgeConfig{
		VerifierURL:     verifierURL,
		RefreshInterval: 5 * time.Minute,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	scores, err := bridge.FetchScores(ctx)
	if err != nil {
		t.Fatalf("FetchScores: %v", err)
	}
	if len(scores) == 0 {
		t.Fatal("FetchScores returned empty score map")
	}

	for name, score := range scores {
		t.Logf("provider=%s score=%.2f", name, score)
		if score < 0 {
			t.Errorf("provider %q has negative score %.2f", name, score)
		}
	}
}

// TestScorerBridge_FetchScores_Unreachable verifies that FetchScores gracefully
// falls back to static scores when the verifier URL is unreachable.
// This always runs (no env-var guard) because it uses a bogus URL.
func TestScorerBridge_FetchScores_Unreachable(t *testing.T) {
	bridge := fallback.NewScorerBridge(fallback.ScorerBridgeConfig{
		VerifierURL:     "http://127.0.0.1:19999", // nothing listening here
		RefreshInterval: 5 * time.Minute,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	scores, err := bridge.FetchScores(ctx)
	if err != nil {
		t.Fatalf("FetchScores should not propagate errors: %v", err)
	}
	if len(scores) == 0 {
		t.Fatal("FetchScores should return static scores on failure")
	}

	// Confirm static scores are returned.
	static := bridge.StaticFallbackScores()
	for name := range static {
		if _, ok := scores[name]; !ok {
			t.Errorf("static provider %q missing from fallback scores", name)
		}
	}
}
