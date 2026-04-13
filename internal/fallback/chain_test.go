package fallback_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// ---------------------------------------------------------------------------
// mockProvider
// ---------------------------------------------------------------------------

type mockProvider struct {
	name      string
	available bool
	modelList []string
	resp      *types.InternalChatResponse
	err       error
}

func (m *mockProvider) Complete(_ context.Context, _ *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	return m.resp, m.err
}

func (m *mockProvider) CompleteStream(_ context.Context, _ *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan types.StreamChunk, 1)
	ch <- types.StreamChunk{Content: "ok", FinishReason: "stop"}
	close(ch)
	return ch, nil
}

func (m *mockProvider) Models() []string { return m.modelList }
func (m *mockProvider) Name() string     { return m.name }
func (m *mockProvider) Available() bool  { return m.available }

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newCB() *fallback.CircuitBreaker {
	return fallback.NewCircuitBreaker(3, 30*time.Second)
}

func newRL() *fallback.RateLimitTracker {
	return fallback.NewRateLimitTracker(1, 10)
}

func okResp(id string) *types.InternalChatResponse {
	return &types.InternalChatResponse{
		ID:    id,
		Model: "test-model",
		Message: types.InternalMessage{
			Role:    types.RoleAssistant,
			Content: "hello from " + id,
		},
	}
}

// ---------------------------------------------------------------------------
// Test 1: first available entry succeeds
// ---------------------------------------------------------------------------

func TestChain_Complete_UsesFirstAvailableEntry(t *testing.T) {
	primary := &mockProvider{name: "primary", available: true, resp: okResp("primary")}
	secondary := &mockProvider{name: "secondary", available: true, resp: okResp("secondary")}

	providers := map[string]brain.Provider{
		"primary":   primary,
		"secondary": secondary,
	}
	rl := newRL()
	chain := fallback.NewChain(providers, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{ProviderName: "primary", ModelID: "m1", Score: 9.0, Status: fallback.EntryActive, CircuitBreaker: newCB()},
		{ProviderName: "secondary", ModelID: "m2", Score: 7.0, Status: fallback.EntryActive, CircuitBreaker: newCB()},
	})

	req := &types.InternalChatRequest{Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}}}
	resp, err := chain.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "primary" {
		t.Errorf("expected response from primary, got %q", resp.ID)
	}
}

// ---------------------------------------------------------------------------
// Test 2: 429 on first entry → fallback to second
// ---------------------------------------------------------------------------

func TestChain_Complete_FallsBackOn429(t *testing.T) {
	pe429 := &brain.ProviderError{
		Provider:   "primary",
		StatusCode: 429,
		Headers:    http.Header{},
	}
	primary := &mockProvider{name: "primary", available: true, err: pe429}
	secondary := &mockProvider{name: "secondary", available: true, resp: okResp("secondary")}

	providers := map[string]brain.Provider{
		"primary":   primary,
		"secondary": secondary,
	}
	rl := newRL()
	chain := fallback.NewChain(providers, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{ProviderName: "primary", ModelID: "m1", Score: 9.0, Status: fallback.EntryActive, CircuitBreaker: newCB()},
		{ProviderName: "secondary", ModelID: "m2", Score: 7.0, Status: fallback.EntryActive, CircuitBreaker: newCB()},
	})

	req := &types.InternalChatRequest{Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}}}
	resp, err := chain.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "secondary" {
		t.Errorf("expected fallback to secondary, got %q", resp.ID)
	}
}

// ---------------------------------------------------------------------------
// Test 3: 5xx on first entry → fallback to second
// ---------------------------------------------------------------------------

func TestChain_Complete_FallsBackOn5xx(t *testing.T) {
	pe500 := &brain.ProviderError{
		Provider:   "primary",
		StatusCode: 500,
		Headers:    http.Header{},
	}
	primary := &mockProvider{name: "primary", available: true, err: pe500}
	secondary := &mockProvider{name: "secondary", available: true, resp: okResp("secondary")}

	providers := map[string]brain.Provider{
		"primary":   primary,
		"secondary": secondary,
	}
	rl := newRL()
	chain := fallback.NewChain(providers, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{ProviderName: "primary", ModelID: "m1", Score: 9.0, Status: fallback.EntryActive, CircuitBreaker: newCB()},
		{ProviderName: "secondary", ModelID: "m2", Score: 7.0, Status: fallback.EntryActive, CircuitBreaker: newCB()},
	})

	req := &types.InternalChatRequest{Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}}}
	resp, err := chain.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "secondary" {
		t.Errorf("expected fallback to secondary, got %q", resp.ID)
	}
}

// ---------------------------------------------------------------------------
// Test 4: cloud fails → llama.cpp local fallback catches it
// ---------------------------------------------------------------------------

func TestChain_Complete_LlamaCppAlwaysLast(t *testing.T) {
	cloudErr := &brain.ProviderError{Provider: "cloud", StatusCode: 503, Headers: http.Header{}}
	cloud := &mockProvider{name: "cloud", available: true, err: cloudErr}
	local := &mockProvider{name: "llamacpp", available: true, resp: okResp("llamacpp")}

	providers := map[string]brain.Provider{
		"cloud":    cloud,
		"llamacpp": local,
	}
	rl := newRL()
	chain := fallback.NewChain(providers, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{ProviderName: "cloud", ModelID: "gpt-4o", Score: 8.5, Status: fallback.EntryActive, CircuitBreaker: newCB(), IsLocalFallback: false},
		{ProviderName: "llamacpp", ModelID: "qwen2.5-coder-3b", Score: 6.0, Status: fallback.EntryActive, CircuitBreaker: newCB(), IsLocalFallback: true},
	})

	req := &types.InternalChatRequest{Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}}}
	resp, err := chain.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "llamacpp" {
		t.Errorf("expected local fallback to serve, got %q", resp.ID)
	}
}

// ---------------------------------------------------------------------------
// Test 5: single provider fails → "all providers exhausted" error
// ---------------------------------------------------------------------------

func TestChain_Complete_AllExhausted_ReturnsError(t *testing.T) {
	underlying := errors.New("connection refused")
	only := &mockProvider{name: "only", available: true, err: underlying}

	providers := map[string]brain.Provider{"only": only}
	rl := newRL()
	chain := fallback.NewChain(providers, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{ProviderName: "only", ModelID: "m1", Score: 7.0, Status: fallback.EntryActive, CircuitBreaker: newCB()},
	})

	req := &types.InternalChatRequest{Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}}}
	_, err := chain.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	const want = "all providers exhausted"
	if !contains(err.Error(), want) {
		t.Errorf("error %q does not contain %q", err.Error(), want)
	}
	if !errors.Is(err, underlying) {
		t.Errorf("expected error to wrap underlying error %v", underlying)
	}
}

// ---------------------------------------------------------------------------
// Test 6: concurrent safety — 20 goroutines all succeed
// ---------------------------------------------------------------------------

func TestChain_Complete_ConcurrentSafety(t *testing.T) {
	provider := &mockProvider{name: "worker", available: true, resp: okResp("worker")}
	providers := map[string]brain.Provider{"worker": provider}
	rl := newRL()
	chain := fallback.NewChain(providers, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{ProviderName: "worker", ModelID: "m1", Score: 8.0, Status: fallback.EntryActive, CircuitBreaker: newCB()},
	})

	const goroutines = 20
	var wg sync.WaitGroup
	var errCount atomic.Int64

	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			req := &types.InternalChatRequest{
				Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "ping"}},
			}
			_, err := chain.Complete(context.Background(), req)
			if err != nil {
				errCount.Add(1)
			}
		}()
	}
	wg.Wait()

	if n := errCount.Load(); n != 0 {
		t.Errorf("%d out of %d goroutines received errors", n, goroutines)
	}
}

// ---------------------------------------------------------------------------
// helper
// ---------------------------------------------------------------------------

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
