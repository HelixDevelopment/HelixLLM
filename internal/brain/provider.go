// Package brain provides the LLM coordination layer for HelixLLM.
//
// It defines the Provider interface that all LLM backends implement,
// a Router that selects the best provider for each request, and the
// Brain service that ties everything together.
package brain

import (
	"context"

	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// Provider is the interface that all LLM backends must implement.
// Each provider wraps a specific LLM API (llama.cpp local, OpenAI, Anthropic)
// and translates between HelixLLM's internal types and the provider's API format.
type Provider interface {
	// Complete sends a chat completion request and returns the full response.
	Complete(ctx context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error)

	// CompleteStream sends a streaming chat completion request and returns a
	// channel that emits StreamChunks. The channel is closed when the stream
	// ends or an error occurs.
	CompleteStream(ctx context.Context, req *types.InternalChatRequest) (<-chan types.StreamChunk, error)

	// Models returns the list of model IDs this provider supports.
	Models() []string

	// Name returns the provider's display name (e.g. "llamacpp", "openai", "anthropic").
	Name() string

	// Available returns true if the provider is currently reachable and ready
	// to serve requests.
	Available() bool
}
