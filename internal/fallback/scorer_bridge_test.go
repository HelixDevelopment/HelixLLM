package fallback_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
)

// ---------------------------------------------------------------------------
// Test 1: StaticFallbackScores ordering
// ---------------------------------------------------------------------------

func TestScorerBridge_StaticFallbackScores_Ordering(t *testing.T) {
	sb := fallback.NewScorerBridge(fallback.ScorerBridgeConfig{})
	scores := sb.StaticFallbackScores()

	expected := map[string]float64{
		"openrouter":  90,
		"chutes":      85,
		"huggingface": 80,
		"nvidia":      75,
		"cerebras":    70,
		"sambanova":   65,
		"together":    60,
		"llamacpp":    10,
	}

	for name, want := range expected {
		got, ok := scores[name]
		if !ok {
			t.Errorf("missing provider %q in static scores", name)
			continue
		}
		if got != want {
			t.Errorf("provider %q: want score %.0f, got %.0f", name, want, got)
		}
	}

	// All scores must be positive.
	for name, score := range scores {
		if score <= 0 {
			t.Errorf("provider %q: score must be positive, got %.2f", name, score)
		}
	}

	// openrouter > chutes > huggingface > nvidia
	order := []string{"openrouter", "chutes", "huggingface", "nvidia"}
	for i := 1; i < len(order); i++ {
		if scores[order[i-1]] <= scores[order[i]] {
			t.Errorf("expected %s (%.0f) > %s (%.0f)",
				order[i-1], scores[order[i-1]],
				order[i], scores[order[i]])
		}
	}
}

// ---------------------------------------------------------------------------
// Test 2: FetchScores from a live mock server
// ---------------------------------------------------------------------------

func TestScorerBridge_FetchScores_FromMockServer(t *testing.T) {
	payload := map[string]interface{}{
		"scores": map[string]interface{}{
			"openrouter": map[string]interface{}{"total": 92.5},
			"chutes":     map[string]interface{}{"total": 87.0},
			"llamacpp":   map[string]interface{}{"total": 12.0},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/scores" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	sb := fallback.NewScorerBridge(fallback.ScorerBridgeConfig{
		VerifierURL: srv.URL,
	})

	scores, err := sb.FetchScores(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if scores["openrouter"] != 92.5 {
		t.Errorf("openrouter: want 92.5, got %.2f", scores["openrouter"])
	}
	if scores["chutes"] != 87.0 {
		t.Errorf("chutes: want 87.0, got %.2f", scores["chutes"])
	}
	if scores["llamacpp"] != 12.0 {
		t.Errorf("llamacpp: want 12.0, got %.2f", scores["llamacpp"])
	}
}

// ---------------------------------------------------------------------------
// Test 3: FetchScores graceful degradation — unreachable server returns static
// ---------------------------------------------------------------------------

func TestScorerBridge_FetchScores_UnreachableReturnsStatic(t *testing.T) {
	sb := fallback.NewScorerBridge(fallback.ScorerBridgeConfig{
		VerifierURL: "http://127.0.0.1:19999", // nothing listening
	})

	scores, err := sb.FetchScores(context.Background())

	// Must NOT return an error.
	if err != nil {
		t.Fatalf("expected no error on unreachable server, got: %v", err)
	}
	// Must return the static fallback.
	if len(scores) == 0 {
		t.Fatal("expected non-empty scores map on unreachable server")
	}
	if _, ok := scores["openrouter"]; !ok {
		t.Error("expected static fallback scores to include openrouter")
	}
}

// ---------------------------------------------------------------------------
// Test 4: BuildEntries — llamacpp always last, sorted descending
// ---------------------------------------------------------------------------

func TestScorerBridge_BuildEntries_LlamaCppAlwaysLast(t *testing.T) {
	sb := fallback.NewScorerBridge(fallback.ScorerBridgeConfig{})

	scores := map[string]float64{
		"openrouter":  90,
		"chutes":      85,
		"huggingface": 80,
		"llamacpp":    10,
	}
	models := map[string]string{
		"openrouter":  "meta-llama/llama-3.1-8b-instruct:free",
		"chutes":      "deepseek-ai/DeepSeek-V3-0324",
		"huggingface": "Qwen/Qwen2.5-72B-Instruct",
		"llamacpp":    "qwen2.5-coder-3b",
	}

	entries := sb.BuildEntries(scores, models)

	if len(entries) == 0 {
		t.Fatal("BuildEntries returned empty slice")
	}

	// Last entry must always be llamacpp.
	last := entries[len(entries)-1]
	if last.ProviderName != "llamacpp" {
		t.Errorf("last entry must be llamacpp, got %q", last.ProviderName)
	}
	if !last.IsLocalFallback {
		t.Error("llamacpp entry must have IsLocalFallback=true")
	}

	// Entries before llamacpp must be sorted descending by score.
	cloud := entries[:len(entries)-1]
	for i := 1; i < len(cloud); i++ {
		if cloud[i].Score > cloud[i-1].Score {
			t.Errorf("entries not sorted descending: entries[%d].Score=%.0f > entries[%d].Score=%.0f",
				i, cloud[i].Score, i-1, cloud[i-1].Score)
		}
	}

	// Every entry must have a CircuitBreaker.
	for i, e := range entries {
		if e.CircuitBreaker == nil {
			t.Errorf("entries[%d] (%s) has nil CircuitBreaker", i, e.ProviderName)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 5: RefreshLoop — chain entries updated after tick
// ---------------------------------------------------------------------------

func TestScorerBridge_RefreshLoop_UpdatesChain(t *testing.T) {
	// Serve scores that differ from the static defaults so we can detect a fetch.
	// Each provider value must be {"total": N} to match verifierResponse's struct shape.
	type providerScore struct {
		Total float64 `json:"total"`
	}
	updated := map[string]interface{}{
		"scores": map[string]providerScore{
			"openrouter":  {Total: 99.0},
			"chutes":      {Total: 98.0},
			"huggingface": {Total: 97.0},
			"nvidia":      {Total: 96.0},
			"cerebras":    {Total: 95.0},
			"sambanova":   {Total: 94.0},
			"together":    {Total: 93.0},
			"llamacpp":    {Total: 11.0},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(updated)
	}))
	defer srv.Close()

	sb := fallback.NewScorerBridge(fallback.ScorerBridgeConfig{
		VerifierURL:     srv.URL,
		RefreshInterval: 50 * time.Millisecond, // fast tick for test
	})

	// Empty chain with no providers registered — we only care that SetEntries is called.
	rl := fallback.NewRateLimitTracker(1, 10)
	chain := fallback.NewChain(nil, rl)

	models := map[string]string{
		"openrouter":  "meta-llama/llama-3.1-8b-instruct:free",
		"chutes":      "deepseek-ai/DeepSeek-V3-0324",
		"huggingface": "Qwen/Qwen2.5-72B-Instruct",
		"nvidia":      "nvidia/llama-3.1-nemotron-70b-instruct",
		"cerebras":    "llama3.1-70b",
		"sambanova":   "Meta-Llama-3.1-70B-Instruct",
		"together":    "meta-llama/Meta-Llama-3.1-8B-Instruct-Turbo",
		"llamacpp":    "qwen2.5-coder-3b",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sb.StartRefreshLoop(ctx, chain, models)

	// The loop performs an immediate fetch before the first ticker fires.
	// Give the goroutine a generous window to complete that initial fetch.
	time.Sleep(200 * time.Millisecond)
	cancel() // stop the loop
	sb.Wait()

	entries := chain.Entries()
	if len(entries) == 0 {
		t.Fatal("expected chain entries to be populated by the refresh loop")
	}

	// Verify llamacpp is still last.
	last := entries[len(entries)-1]
	if last.ProviderName != "llamacpp" {
		t.Errorf("after refresh: last entry must be llamacpp, got %q", last.ProviderName)
	}

	// Verify the refreshed score made it in (openrouter should have score 99).
	for _, e := range entries {
		if e.ProviderName == "openrouter" && e.Score != 99.0 {
			t.Errorf("openrouter score after refresh: want 99.0, got %.2f", e.Score)
		}
	}
}
