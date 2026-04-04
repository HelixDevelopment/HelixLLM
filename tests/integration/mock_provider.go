package integration

import (
	"context"

	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// mockIntegrationProvider is a test-double LLM provider that returns canned
// responses. It implements brain.Provider.
type mockIntegrationProvider struct{}

func (m *mockIntegrationProvider) Complete(_ context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	model := req.Model
	if model == "" {
		model = "mock-model"
	}
	return &types.InternalChatResponse{
		ID:    "int-test-1",
		Model: model,
		Message: types.InternalMessage{
			Role:    types.RoleAssistant,
			Content: "Hello from the integration test mock.",
		},
		FinishReason: "stop",
		Provider:     types.ProviderLocal,
	}, nil
}

func (m *mockIntegrationProvider) CompleteStream(_ context.Context, req *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	ch := make(chan types.StreamChunk, 3)
	ch <- types.StreamChunk{Content: "Hello"}
	ch <- types.StreamChunk{Content: " from mock"}
	ch <- types.StreamChunk{Content: ".", FinishReason: "stop"}
	close(ch)
	return ch, nil
}

func (m *mockIntegrationProvider) Models() []string {
	return []string{"mock-model", "gpt-4o", "claude-sonnet-4-20250514", "llama-3.1-70b"}
}

func (m *mockIntegrationProvider) Name() string {
	return "mock"
}

func (m *mockIntegrationProvider) Available() bool {
	return true
}
