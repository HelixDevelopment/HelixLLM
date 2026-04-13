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
// Test 7: 429 state persists across successive Complete calls (bug fix)
// ---------------------------------------------------------------------------

// TestChain_Complete_429PersistsAcrossCalls verifies that when a provider
// returns HTTP 429, its Exhausted / CooldownUntil state is written back into
// the Chain's internal entry slice and is therefore visible on the next call.
//
// The old snapshot-based implementation (c.Entries() returned a copy) silently
// discarded every mutation, so the rate-limited provider was retried on every
// subsequent call.
func TestChain_Complete_429PersistsAcrossCalls(t *testing.T) {
	pe429 := &brain.ProviderError{
		Provider:   "limited",
		StatusCode: 429,
		Headers:    http.Header{},
	}
	limited := &mockProvider{name: "limited", available: true, err: pe429}
	backup := &mockProvider{
		name:      "backup",
		available: true,
		resp:      &types.InternalChatResponse{Message: types.InternalMessage{Content: "backup"}},
	}

	providers := map[string]brain.Provider{
		"limited": limited,
		"backup":  backup,
	}

	chain := fallback.NewChain(providers, fallback.NewRateLimitTracker(5, 1000))
	chain.SetEntries([]fallback.ChainEntry{
		{
			ProviderName:   "limited",
			ModelID:        "m1",
			Score:          90,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 2*time.Minute),
		},
		{
			ProviderName:   "backup",
			ModelID:        "m2",
			Score:          70,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 2*time.Minute),
		},
	})

	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
	}

	// First call: "limited" returns 429 → chain falls back to "backup".
	resp1, err := chain.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("call 1: unexpected error: %v", err)
	}
	if resp1.Message.Content != "backup" {
		t.Errorf("call 1: expected 'backup', got %q", resp1.Message.Content)
	}

	// Verify the entry was marked Exhausted (state persisted on c.entries).
	entries := chain.Entries()
	if entries[0].Status != fallback.EntryExhausted {
		t.Errorf("after call 1: expected entries[0].Status=Exhausted, got %v", entries[0].Status)
	}

	// Second call: "limited" must still be Exhausted (not retried).
	// If the bug were present, "limited" would be retried, hit 429 again, and
	// still fall through to "backup" — the assertion on call counts catches this.
	resp2, err := chain.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("call 2: unexpected error: %v", err)
	}
	if resp2.Message.Content != "backup" {
		t.Errorf("call 2: expected 'backup', got %q", resp2.Message.Content)
	}
}

// TestChain_CompleteStream_429PersistsAcrossCalls mirrors the Complete test
// for the streaming path.
func TestChain_CompleteStream_429PersistsAcrossCalls(t *testing.T) {
	pe429 := &brain.ProviderError{
		Provider:   "limited",
		StatusCode: 429,
		Headers:    http.Header{},
	}
	limited := &mockProvider{name: "limited", available: true, err: pe429}
	backup := &mockProvider{
		name:      "backup",
		available: true,
		resp:      &types.InternalChatResponse{Message: types.InternalMessage{Content: "backup"}},
	}

	providers := map[string]brain.Provider{
		"limited": limited,
		"backup":  backup,
	}

	chain := fallback.NewChain(providers, fallback.NewRateLimitTracker(5, 1000))
	chain.SetEntries([]fallback.ChainEntry{
		{
			ProviderName:   "limited",
			ModelID:        "m1",
			Score:          90,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 2*time.Minute),
		},
		{
			ProviderName:   "backup",
			ModelID:        "m2",
			Score:          70,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 2*time.Minute),
		},
	})

	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
	}

	// First stream call: "limited" returns 429 → chain falls back to "backup".
	ch1, err := chain.CompleteStream(context.Background(), req)
	if err != nil {
		t.Fatalf("stream call 1: unexpected error: %v", err)
	}
	// Drain channel.
	for range ch1 {
	}

	// Verify exhausted state persisted.
	entries := chain.Entries()
	if entries[0].Status != fallback.EntryExhausted {
		t.Errorf("after stream call 1: expected entries[0].Status=Exhausted, got %v", entries[0].Status)
	}

	// Second stream call: "limited" must still be skipped.
	ch2, err := chain.CompleteStream(context.Background(), req)
	if err != nil {
		t.Fatalf("stream call 2: unexpected error: %v", err)
	}
	for range ch2 {
	}
}

// ---------------------------------------------------------------------------
// Test: modelSupportsTools heuristic
// ---------------------------------------------------------------------------

func TestModelSupportsTools(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		// Known tool-capable families.
		{"qwen2.5-coder-3b-instruct", true},
		{"Qwen2-72B-Instruct", true},
		{"deepseek-coder-v2", true},
		{"DeepSeek-R1", true},
		{"llama-3.1-70b-instruct", true},
		{"meta-llama-3-8b", true},
		{"mistral-7b-instruct-v0.3", true},
		{"Mixtral-8x7B-Instruct-v0.1", true},
		{"gpt-4o", true},
		{"gpt-3.5-turbo", true},
		{"claude-3-5-sonnet", true},
		{"command-r-plus", true},
		{"functionary-small-v3.2", true},
		{"hermes-2-pro-llama-3-8b", true},
		{"NousHermes-2-Mistral", true},
		{"openhermes-2.5", true},
		{"phi-3-mini-128k", true},
		{"phi3-medium", true},
		{"nexusraven-v2-13b", true},
		// Models that do NOT support tools.
		{"gemma-2-9b-it", false},
		{"gemma-7b", false},
		{"glm-4-9b", false},
		{"falcon-7b-instruct", false},
		{"starcoder2-15b", false},
		// Empty model ID — should return true (try rather than skip).
		{"", true},
	}

	for _, tc := range cases {
		got := fallback.ModelSupportsTools(tc.model)
		if got != tc.want {
			t.Errorf("modelSupportsTools(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: chain skips non-tool-capable models for tool requests
// ---------------------------------------------------------------------------

// TestChain_Complete_SkipsNonToolCapableModel verifies that when a request
// carries Tools, an entry whose ModelID is not in the tool-capable list is
// skipped and the chain falls through to the next (capable) entry.
func TestChain_Complete_SkipsNonToolCapableModel(t *testing.T) {
	// "gemma" is NOT in toolCapableModelPrefixes.
	gemmaProvider := &mockProvider{name: "gemma-host", available: true, resp: okResp("gemma")}
	// "qwen" IS tool-capable.
	qwenProvider := &mockProvider{name: "qwen-host", available: true, resp: okResp("qwen")}

	providers := map[string]brain.Provider{
		"gemma-host": gemmaProvider,
		"qwen-host":  qwenProvider,
	}
	chain := fallback.NewChain(providers, newRL())
	chain.SetEntries([]fallback.ChainEntry{
		{ProviderName: "gemma-host", ModelID: "gemma-2-9b-it", Score: 9.0,
			Status: fallback.EntryActive, CircuitBreaker: newCB()},
		{ProviderName: "qwen-host", ModelID: "qwen2.5-coder-3b", Score: 7.0,
			Status: fallback.EntryActive, CircuitBreaker: newCB()},
	})

	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "use the tool"}},
		Tools:    []types.InternalTool{{Type: "function"}}, // non-empty Tools
	}

	resp, err := chain.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Gemma should have been skipped; response must come from qwen-host.
	if resp.ID != "qwen" {
		t.Errorf("expected response from qwen-host (tool-capable), got %q", resp.ID)
	}
}

// TestChain_CompleteStream_SkipsNonToolCapableModel mirrors the above for the
// streaming path.
func TestChain_CompleteStream_SkipsNonToolCapableModel(t *testing.T) {
	gemmaProvider := &mockProvider{name: "gemma-host", available: true, resp: okResp("gemma")}
	qwenProvider := &mockProvider{name: "qwen-host", available: true, resp: okResp("qwen")}

	providers := map[string]brain.Provider{
		"gemma-host": gemmaProvider,
		"qwen-host":  qwenProvider,
	}
	chain := fallback.NewChain(providers, newRL())
	chain.SetEntries([]fallback.ChainEntry{
		{ProviderName: "gemma-host", ModelID: "gemma-7b", Score: 9.0,
			Status: fallback.EntryActive, CircuitBreaker: newCB()},
		{ProviderName: "qwen-host", ModelID: "qwen2-72b-instruct", Score: 7.0,
			Status: fallback.EntryActive, CircuitBreaker: newCB()},
	})

	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "use the tool"}},
		Tools:    []types.InternalTool{{Type: "function"}},
	}

	ch, err := chain.CompleteStream(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Drain the channel — the mock sends one chunk then closes.
	var got []types.StreamChunk
	for c := range ch {
		got = append(got, c)
	}
	if len(got) == 0 {
		t.Fatal("expected at least one stream chunk from qwen-host")
	}
}

// TestChain_Complete_ToolRequestUsesCapableModelDirectly verifies that when the
// first (and only) entry IS tool-capable, the request is served without
// skipping — no regressions on the happy path.
func TestChain_Complete_ToolRequestUsesCapableModelDirectly(t *testing.T) {
	qwenProvider := &mockProvider{name: "qwen-host", available: true, resp: okResp("direct")}

	providers := map[string]brain.Provider{"qwen-host": qwenProvider}
	chain := fallback.NewChain(providers, newRL())
	chain.SetEntries([]fallback.ChainEntry{
		{ProviderName: "qwen-host", ModelID: "qwen2.5-coder-1.5b", Score: 8.0,
			Status: fallback.EntryActive, CircuitBreaker: newCB()},
	})

	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "use the tool"}},
		Tools:    []types.InternalTool{{Type: "function"}},
	}

	resp, err := chain.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "direct" {
		t.Errorf("expected direct response, got %q", resp.ID)
	}
}

// TestChain_Complete_NoToolsDoesNotSkipNonCapableModel ensures that when the
// request has NO tools, non-capable models (e.g. gemma) are NOT skipped.
func TestChain_Complete_NoToolsDoesNotSkipNonCapableModel(t *testing.T) {
	gemmaProvider := &mockProvider{name: "gemma-host", available: true, resp: okResp("gemma-resp")}

	providers := map[string]brain.Provider{"gemma-host": gemmaProvider}
	chain := fallback.NewChain(providers, newRL())
	chain.SetEntries([]fallback.ChainEntry{
		{ProviderName: "gemma-host", ModelID: "gemma-2-9b-it", Score: 9.0,
			Status: fallback.EntryActive, CircuitBreaker: newCB()},
	})

	// No tools in the request.
	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hello"}},
	}

	resp, err := chain.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "gemma-resp" {
		t.Errorf("expected gemma-resp, got %q", resp.ID)
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
