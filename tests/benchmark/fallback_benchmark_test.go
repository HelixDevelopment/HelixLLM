// Package benchmark_test contains benchmark tests for the HelixLLM fallback chain.
package benchmark_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// mockBenchProvider is a zero-allocation in-process provider used to measure
// chain overhead in isolation, without any network or I/O cost.
type mockBenchProvider struct {
	name      string
	returnErr error
}

func (m *mockBenchProvider) Complete(_ context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	return &types.InternalChatResponse{
		ID:    "bench-id",
		Model: req.Model,
		Message: types.InternalMessage{
			Role:    types.RoleAssistant,
			Content: "bench response",
		},
		FinishReason: "stop",
	}, nil
}

func (m *mockBenchProvider) CompleteStream(_ context.Context, _ *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	ch := make(chan types.StreamChunk, 1)
	ch <- types.StreamChunk{Content: "bench", FinishReason: "stop"}
	close(ch)
	return ch, nil
}

func (m *mockBenchProvider) Models() []string { return []string{"bench-model"} }
func (m *mockBenchProvider) Name() string     { return m.name }
func (m *mockBenchProvider) Available() bool  { return true }

var benchReq = &types.InternalChatRequest{
	Model:    "bench-model",
	Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hello"}},
}

// BenchmarkChain_Complete_SingleProvider measures the overhead of Chain.Complete
// when a single in-process provider succeeds on the first attempt.
func BenchmarkChain_Complete_SingleProvider(b *testing.B) {
	mock := &mockBenchProvider{name: "bench-primary"}
	rl := fallback.NewRateLimitTracker(1, 10)
	chain := fallback.NewChain(map[string]brain.Provider{"bench-primary": mock}, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{
			ProviderName:   "bench-primary",
			ModelID:        "bench-model",
			Score:          9.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 30*time.Second),
		},
	})

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		resp, err := chain.Complete(ctx, benchReq)
		if err != nil || resp == nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

// BenchmarkChain_Complete_FallbackPath measures the overhead of Chain.Complete
// when the primary provider returns 429, forcing a fallback to the secondary.
func BenchmarkChain_Complete_FallbackPath(b *testing.B) {
	primary429 := &mockBenchProvider{
		name: "bench-primary-429",
		returnErr: &brain.ProviderError{
			Provider:   "bench-primary-429",
			StatusCode: http.StatusTooManyRequests,
			Body:       `{"error":"rate limited"}`,
			Headers:    make(http.Header),
		},
	}
	secondary := &mockBenchProvider{name: "bench-secondary"}

	rl := fallback.NewRateLimitTracker(1, 10)
	chain := fallback.NewChain(map[string]brain.Provider{
		"bench-primary-429": primary429,
		"bench-secondary":   secondary,
	}, rl)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Re-set entries each iteration so primary is Active again (not exhausted).
		chain.SetEntries([]fallback.ChainEntry{
			{
				ProviderName:   "bench-primary-429",
				ModelID:        "bench-model",
				Score:          9.0,
				Status:         fallback.EntryActive,
				CircuitBreaker: fallback.NewCircuitBreaker(3, 30*time.Second),
			},
			{
				ProviderName:   "bench-secondary",
				ModelID:        "bench-model",
				Score:          7.0,
				Status:         fallback.EntryActive,
				CircuitBreaker: fallback.NewCircuitBreaker(3, 30*time.Second),
			},
		})

		resp, err := chain.Complete(context.Background(), benchReq)
		if err != nil || resp == nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}
