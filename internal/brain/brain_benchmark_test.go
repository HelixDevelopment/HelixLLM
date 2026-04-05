package brain_test

import (
	"context"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

func BenchmarkBrain_Complete(b *testing.B) {
	provider := &mockBenchProvider{response: &types.InternalChatResponse{
		ID:           "bench-id",
		Model:        "bench-model",
		Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "hello"},
		FinishReason: "stop",
		Provider:     types.ProviderLocal,
	}}

	br := brain.New(brain.Config{DefaultProvider: "bench"})
	br.RegisterProvider("bench", provider)

	req := &types.InternalChatRequest{
		Model:    "bench-model",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = br.Complete(context.Background(), req)
	}
}

// mockBenchProvider is a minimal Provider implementation for benchmarks.
// It lives in this file (rather than reusing mockProvider from provider_test.go)
// so that the benchmark file is self-contained.
type mockBenchProvider struct {
	response *types.InternalChatResponse
}

func (m *mockBenchProvider) Complete(_ context.Context, _ *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	return m.response, nil
}

func (m *mockBenchProvider) CompleteStream(_ context.Context, _ *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	return nil, nil
}

func (m *mockBenchProvider) Models() []string { return []string{"bench-model"} }
func (m *mockBenchProvider) Name() string     { return "bench" }
func (m *mockBenchProvider) Available() bool   { return true }
