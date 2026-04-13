package stress_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// mockStressProvider is a lightweight in-process mock that always succeeds,
// avoiding any network overhead in the stress test.
type mockStressProvider struct {
	name string
}

func (m *mockStressProvider) Complete(_ context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	return &types.InternalChatResponse{
		ID:    "stress-resp",
		Model: req.Model,
		Message: types.InternalMessage{
			Role:    types.RoleAssistant,
			Content: "ok",
		},
		FinishReason: "stop",
	}, nil
}

func (m *mockStressProvider) CompleteStream(_ context.Context, _ *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	ch := make(chan types.StreamChunk, 1)
	ch <- types.StreamChunk{Content: "ok", FinishReason: "stop"}
	close(ch)
	return ch, nil
}

func (m *mockStressProvider) Models() []string { return []string{"stress-model"} }
func (m *mockStressProvider) Name() string     { return m.name }
func (m *mockStressProvider) Available() bool  { return true }

// TestFallbackStress_ConcurrentRequests fires 100 concurrent requests through
// a single-provider chain and verifies all succeed without races or panics.
func TestFallbackStress_ConcurrentRequests(t *testing.T) {
	const concurrency = 100

	mock := &mockStressProvider{name: "stress-mock"}
	rl := fallback.NewRateLimitTracker(1, 10)
	chain := fallback.NewChain(map[string]brain.Provider{"stress-mock": mock}, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{
			ProviderName:   "stress-mock",
			ModelID:        "stress-model",
			Score:          9.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(10, 30*time.Second),
		},
	})

	req := &types.InternalChatRequest{
		Model:    "stress-model",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
	}

	var success, failure atomic.Int64
	var wg sync.WaitGroup

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := chain.Complete(ctx, req)
			if err != nil || resp == nil || resp.Message.Content == "" {
				failure.Add(1)
			} else {
				success.Add(1)
			}
		}()
	}

	wg.Wait()

	if failure.Load() > 0 {
		t.Errorf("%d/%d requests failed under concurrent stress", failure.Load(), concurrency)
	}
	t.Logf("stress result: %d/%d succeeded", success.Load(), concurrency)
}

// TestFallbackStress_ConcurrentStream fires 50 concurrent streaming requests
// through a single-provider chain and verifies all succeed.
func TestFallbackStress_ConcurrentStream(t *testing.T) {
	const concurrency = 50

	mock := &mockStressProvider{name: "stream-stress-mock"}
	rl := fallback.NewRateLimitTracker(1, 10)
	chain := fallback.NewChain(map[string]brain.Provider{"stream-stress-mock": mock}, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{
			ProviderName:   "stream-stress-mock",
			ModelID:        "stress-model",
			Score:          9.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(10, 30*time.Second),
		},
	})

	req := &types.InternalChatRequest{
		Model:    "stress-model",
		Stream:   true,
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
	}

	var success, failure atomic.Int64
	var wg sync.WaitGroup

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, err := chain.CompleteStream(ctx, req)
			if err != nil {
				failure.Add(1)
				return
			}
			got := false
			for chunk := range ch {
				if chunk.Content != "" {
					got = true
				}
			}
			if got {
				success.Add(1)
			} else {
				failure.Add(1)
			}
		}()
	}

	wg.Wait()

	if failure.Load() > 0 {
		t.Errorf("%d/%d stream requests failed under concurrent stress", failure.Load(), concurrency)
	}
	t.Logf("stream stress result: %d/%d succeeded", success.Load(), concurrency)
}
