package brain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// ---------------------------------------------------------------------------
// Brain construction tests
// ---------------------------------------------------------------------------

func TestNew_LlamaCppOnly(t *testing.T) {
	b := brain.New(brain.Config{
		LlamaCppURL:    "http://localhost:8080",
		LlamaCppModels: []string{"llama-3.1-70b"},
	})
	if b == nil {
		t.Fatal("New returned nil")
	}
	// Models should return entries for llamacpp (but Available() does a real
	// health-check, so we only test that the Brain can be constructed and
	// exposes a Models() method without panicking.  The provider won't be
	// Available() in a unit-test environment, so the result may be empty).
	_ = b.Models()
}

func TestNew_AllProviders(t *testing.T) {
	b := brain.New(brain.Config{
		LlamaCppURL:      "http://localhost:8080",
		LlamaCppModels:   []string{"llama-3.1-70b"},
		OpenAIKey:        "sk-test-openai",
		AnthropicKey:     "sk-ant-test",
		DefaultProvider:  "llamacpp",
	})
	if b == nil {
		t.Fatal("New returned nil")
	}
}

func TestNew_NoProviders(t *testing.T) {
	// An empty config should still produce a valid (but empty) Brain.
	b := brain.New(brain.Config{})
	if b == nil {
		t.Fatal("New returned nil")
	}
}

func TestNew_DefaultLlamaCppModels(t *testing.T) {
	// When LlamaCppModels is nil but a URL is provided the Brain should still
	// register the llamacpp provider (with a non-empty model list).
	b := brain.New(brain.Config{
		LlamaCppURL: "http://localhost:8080",
	})
	if b == nil {
		t.Fatal("New returned nil")
	}
}

// ---------------------------------------------------------------------------
// Brain.Complete tests (using a test-double Brain built via NewForTest)
// ---------------------------------------------------------------------------

// newBrainWithMocks builds a Brain that uses mock providers injected via the
// exported RegisterProvider method so we can exercise routing without real
// HTTP calls.
func newBrainWithMocks(providers map[string]brain.Provider, defaultProvider string) *brain.Brain {
	b := brain.New(brain.Config{DefaultProvider: defaultProvider})
	for name, p := range providers {
		b.RegisterProvider(name, p)
	}
	return b
}

func TestBrain_Complete_RoutesToCorrectProvider(t *testing.T) {
	openaiResp := &types.InternalChatResponse{
		ID:           "chatcmpl-1",
		Model:        "gpt-4o",
		Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "Hi from OpenAI"},
		FinishReason: "stop",
		Provider:     types.ProviderOpenAI,
	}
	mock := &mockProvider{
		name:      "openai",
		available: true,
		models:    []string{"gpt-4o"},
		response:  openaiResp,
	}

	b := newBrainWithMocks(map[string]brain.Provider{"openai": mock}, "openai")

	resp, err := b.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "gpt-4o",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.Message.Content != "Hi from OpenAI" {
		t.Errorf("content = %q, want %q", resp.Message.Content, "Hi from OpenAI")
	}
}

func TestBrain_Complete_NoProviders_ReturnsError(t *testing.T) {
	b := brain.New(brain.Config{})

	_, err := b.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "gpt-4o",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hello"}},
	})
	if err == nil {
		t.Fatal("expected error when no providers registered, got nil")
	}
}

func TestBrain_Complete_ProviderError_Propagated(t *testing.T) {
	mock := &mockProvider{
		name:      "openai",
		available: true,
		models:    []string{"gpt-4o"},
		err:       errors.New("upstream timeout"),
	}

	b := newBrainWithMocks(map[string]brain.Provider{"openai": mock}, "openai")

	_, err := b.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "gpt-4o",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hello"}},
	})
	if err == nil {
		t.Fatal("expected error to be propagated, got nil")
	}
}

// ---------------------------------------------------------------------------
// Brain.CompleteStream tests
// ---------------------------------------------------------------------------

func TestBrain_CompleteStream_RoutesToCorrectProvider(t *testing.T) {
	chunks := []types.StreamChunk{
		{Content: "Hello"},
		{Content: " world", FinishReason: "stop"},
	}
	mock := &mockProvider{
		name:      "llamacpp",
		available: true,
		models:    []string{"llama-3.1-70b"},
		chunks:    chunks,
	}

	b := newBrainWithMocks(map[string]brain.Provider{"llamacpp": mock}, "llamacpp")

	ch, err := b.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model:    "llama-3.1-70b",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("CompleteStream returned error: %v", err)
	}

	var got []types.StreamChunk
	for c := range ch {
		got = append(got, c)
	}
	if len(got) != 2 {
		t.Fatalf("got %d chunks, want 2", len(got))
	}
	if got[0].Content != "Hello" {
		t.Errorf("chunk[0].Content = %q, want %q", got[0].Content, "Hello")
	}
	if got[1].FinishReason != "stop" {
		t.Errorf("chunk[1].FinishReason = %q, want %q", got[1].FinishReason, "stop")
	}
}

func TestBrain_CompleteStream_NoProviders_ReturnsError(t *testing.T) {
	b := brain.New(brain.Config{})

	_, err := b.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model:    "llama-3.1-70b",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
		Stream:   true,
	})
	if err == nil {
		t.Fatal("expected error when no providers registered, got nil")
	}
}

// ---------------------------------------------------------------------------
// Brain.Models tests
// ---------------------------------------------------------------------------

func TestBrain_Models_AggregatesFromAllAvailableProviders(t *testing.T) {
	openai := &mockProvider{
		name:      "openai",
		available: true,
		models:    []string{"gpt-4o", "gpt-4o-mini"},
	}
	anthropic := &mockProvider{
		name:      "anthropic",
		available: true,
		models:    []string{"claude-opus-4-5"},
	}
	// unavailable provider — should be excluded
	llamacpp := &mockProvider{
		name:      "llamacpp",
		available: false,
		models:    []string{"llama-3.1-70b"},
	}

	b := newBrainWithMocks(map[string]brain.Provider{
		"openai":    openai,
		"anthropic": anthropic,
		"llamacpp":  llamacpp,
	}, "openai")

	models := b.Models()
	// Expect 3 models from 2 available providers; llamacpp excluded.
	if len(models) != 3 {
		t.Errorf("Models() returned %d models, want 3", len(models))
	}
	// Verify all returned models have non-empty IDs.
	for _, m := range models {
		if m.ID == "" {
			t.Errorf("model with empty ID in result")
		}
		if m.Object != "model" {
			t.Errorf("model.Object = %q, want %q", m.Object, "model")
		}
	}
}

func TestBrain_Models_EmptyWhenNoProvidersAvailable(t *testing.T) {
	b := brain.New(brain.Config{})
	models := b.Models()
	if len(models) != 0 {
		t.Errorf("Models() = %d, want 0", len(models))
	}
}
