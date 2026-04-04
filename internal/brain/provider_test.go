package brain_test

import (
	"context"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// mockProvider is a test double that implements Provider.
type mockProvider struct {
	name      string
	models    []string
	available bool
	response  *types.InternalChatResponse
	chunks    []types.StreamChunk
	err       error
}

func (m *mockProvider) Complete(_ context.Context, _ *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func (m *mockProvider) CompleteStream(_ context.Context, _ *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan types.StreamChunk, len(m.chunks))
	for _, c := range m.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (m *mockProvider) Models() []string { return m.models }
func (m *mockProvider) Name() string     { return m.name }
func (m *mockProvider) Available() bool  { return m.available }

func TestProviderInterfaceSatisfied(t *testing.T) {
	// Verify that mockProvider satisfies the Provider interface at compile time.
	var _ brain.Provider = (*mockProvider)(nil)
}

func TestProviderComplete(t *testing.T) {
	p := &mockProvider{
		name:      "test",
		available: true,
		response: &types.InternalChatResponse{
			ID:           "test-1",
			Model:        "test-model",
			Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "Hello"},
			FinishReason: "stop",
			Provider:     types.ProviderLocal,
		},
	}

	resp, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "test-model",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.ID != "test-1" {
		t.Errorf("ID = %q, want %q", resp.ID, "test-1")
	}
	if resp.Message.Content != "Hello" {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "Hello")
	}
}

func TestProviderCompleteStream(t *testing.T) {
	p := &mockProvider{
		name:      "test",
		available: true,
		chunks: []types.StreamChunk{
			{Content: "Hello"},
			{Content: " world", FinishReason: "stop"},
		},
	}

	ch, err := p.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model:    "test-model",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("CompleteStream returned error: %v", err)
	}

	var chunks []types.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if chunks[0].Content != "Hello" {
		t.Errorf("chunks[0].Content = %q, want %q", chunks[0].Content, "Hello")
	}
	if chunks[1].FinishReason != "stop" {
		t.Errorf("chunks[1].FinishReason = %q, want %q", chunks[1].FinishReason, "stop")
	}
}

func TestProviderMetadata(t *testing.T) {
	p := &mockProvider{
		name:      "test-provider",
		models:    []string{"model-a", "model-b"},
		available: true,
	}

	if p.Name() != "test-provider" {
		t.Errorf("Name() = %q, want %q", p.Name(), "test-provider")
	}
	if len(p.Models()) != 2 {
		t.Errorf("Models() length = %d, want 2", len(p.Models()))
	}
	if !p.Available() {
		t.Error("Available() = false, want true")
	}
}
