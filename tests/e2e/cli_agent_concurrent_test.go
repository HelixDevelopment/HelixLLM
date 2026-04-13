//go:build !short

package e2e_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// TestFallbackChain_ConcurrentAgentSessions simulates 10 concurrent CLI agent
// "sessions" each sending 5 requests through a shared fallback Chain.  All
// requests must succeed and there must be no data races (run with -race).
func TestFallbackChain_ConcurrentAgentSessions(t *testing.T) {
	t.Parallel()

	// Single healthy server backing both chain entries so all goroutines
	// succeed without contention on provider state.
	srv := makeOKServer(t, "concurrent-ok")
	defer srv.Close()

	primary := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "concurrent-primary", BaseURL: srv.URL,
		APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})
	secondary := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "concurrent-secondary", BaseURL: srv.URL,
		APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})

	providers := map[string]brain.Provider{
		"concurrent-primary":   primary,
		"concurrent-secondary": secondary,
	}
	rl := fallback.NewRateLimitTracker(1, 10)
	chain := fallback.NewChain(providers, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{
			ProviderName:   "concurrent-primary",
			ModelID:        "test-model",
			Score:          9.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(10, 30*time.Second),
		},
		{
			ProviderName:   "concurrent-secondary",
			ModelID:        "test-model",
			Score:          7.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(10, 30*time.Second),
		},
	})

	const (
		numSessions         = 10
		requestsPerSession  = 5
		totalRequests       = numSessions * requestsPerSession
	)

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		failures []string
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for session := range numSessions {
		wg.Add(1)
		go func(sessionID int) {
			defer wg.Done()
			for req := range requestsPerSession {
				r := &types.InternalChatRequest{
					Model: "test-model",
					Messages: []types.InternalMessage{
						{
							Role:    types.RoleUser,
							Content: fmt.Sprintf("session %d request %d", sessionID, req),
						},
					},
					MaxTokens: 200,
				}
				resp, err := chain.Complete(ctx, r)
				if err != nil {
					mu.Lock()
					failures = append(failures,
						fmt.Sprintf("session %d req %d: %v", sessionID, req, err))
					mu.Unlock()
					return
				}
				if resp == nil || resp.Message.Content == "" {
					mu.Lock()
					failures = append(failures,
						fmt.Sprintf("session %d req %d: empty response", sessionID, req))
					mu.Unlock()
				}
			}
		}(session)
	}

	wg.Wait()

	if len(failures) > 0 {
		for _, f := range failures {
			t.Error(f)
		}
		t.Fatalf("%d/%d requests failed", len(failures), totalRequests)
	}
}

// TestFallbackChain_ConcurrentWithFallback simulates concurrent sessions where
// the primary provider becomes rate-limited mid-flight.  All sessions must
// eventually succeed via the secondary provider.
func TestFallbackChain_ConcurrentWithFallback(t *testing.T) {
	t.Parallel()

	// Primary: rate-limited from the start.
	primarySrv := make429Server()
	defer primarySrv.Close()

	// Secondary: always healthy.
	secondarySrv := makeOKServer(t, "concurrent-fallback-ok")
	defer secondarySrv.Close()

	primary := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "conc-primary-429", BaseURL: primarySrv.URL,
		APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})
	secondary := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "conc-secondary-ok", BaseURL: secondarySrv.URL,
		APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})

	providers := map[string]brain.Provider{
		"conc-primary-429":  primary,
		"conc-secondary-ok": secondary,
	}
	rl := fallback.NewRateLimitTracker(1, 10)
	chain := fallback.NewChain(providers, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{
			ProviderName:   "conc-primary-429",
			ModelID:        "m",
			Score:          9.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(5, 30*time.Second),
		},
		{
			ProviderName:   "conc-secondary-ok",
			ModelID:        "m",
			Score:          7.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(5, 30*time.Second),
		},
	})

	const numGoroutines = 8

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		failures []string
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for g := range numGoroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			req := &types.InternalChatRequest{
				Model:    "m",
				Messages: []types.InternalMessage{{Role: types.RoleUser, Content: fmt.Sprintf("agent %d", id)}},
			}
			resp, err := chain.Complete(ctx, req)
			if err != nil {
				mu.Lock()
				failures = append(failures, fmt.Sprintf("goroutine %d: %v", id, err))
				mu.Unlock()
				return
			}
			if resp.Message.Content != "concurrent-fallback-ok" {
				mu.Lock()
				failures = append(failures,
					fmt.Sprintf("goroutine %d: unexpected content %q", id, resp.Message.Content))
				mu.Unlock()
			}
		}(g)
	}

	wg.Wait()

	if len(failures) > 0 {
		for _, f := range failures {
			t.Error(f)
		}
		t.Fatalf("%d goroutine(s) failed", len(failures))
	}
}
