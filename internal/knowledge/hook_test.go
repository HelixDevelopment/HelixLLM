package knowledge_test

import (
	"context"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

func newPopulatedPipeline(t *testing.T) *knowledge.Pipeline {
	t.Helper()
	p := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          knowledge.NewHashEmbedder(64),
		Store:             knowledge.NewMemoryStore(),
		Chunker:           knowledge.NewFixedSizeChunker(100, 10),
		DefaultCollection: "default",
		DefaultTopK:       3,
	})
	_, err := p.Ingest(context.Background(), knowledge.IngestRequest{
		Content:    "Go is a statically typed, compiled programming language.",
		Collection: "docs",
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	return p
}

func TestRAGHook_PrependSystemMessage(t *testing.T) {
	p := newPopulatedPipeline(t)
	hook := knowledge.RAGHook(p, "docs")

	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "Tell me about Go"},
		},
	}

	modified := hook(req)

	if len(modified.Messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(modified.Messages))
	}
	first := modified.Messages[0]
	if first.Role != types.RoleSystem {
		t.Errorf("expected first message role %q, got %q", types.RoleSystem, first.Role)
	}
	if !strings.HasPrefix(first.Content, "Relevant context:") {
		t.Errorf("expected context prefix, got: %q", first.Content)
	}
	// Original messages preserved after the prepended one
	last := modified.Messages[len(modified.Messages)-1]
	if last.Role != types.RoleUser || last.Content != "Tell me about Go" {
		t.Errorf("original user message not preserved: %+v", last)
	}
}

func TestRAGHook_NilPipeline_ReturnsOriginal(t *testing.T) {
	hook := knowledge.RAGHook(nil, "docs")

	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "Hello"},
		},
	}

	result := hook(req)
	if result != req {
		t.Error("expected original request pointer returned when pipeline is nil")
	}
}

func TestRAGHook_NoUserMessage_ReturnsOriginal(t *testing.T) {
	p := newPopulatedPipeline(t)
	hook := knowledge.RAGHook(p, "docs")

	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleSystem, Content: "You are helpful."},
		},
	}

	result := hook(req)
	if len(result.Messages) != 1 {
		t.Errorf("expected 1 message unchanged, got %d", len(result.Messages))
	}
}

func TestRAGHook_EmptyMessages_ReturnsOriginal(t *testing.T) {
	p := newPopulatedPipeline(t)
	hook := knowledge.RAGHook(p, "docs")

	req := &types.InternalChatRequest{Messages: nil}
	result := hook(req)
	if result != req {
		t.Error("expected original request when messages is nil")
	}
}

func TestRAGHook_EmptyCollection_ReturnsOriginal(t *testing.T) {
	p := newPopulatedPipeline(t)
	// Collection "empty" has no documents — query returns no context.
	hook := knowledge.RAGHook(p, "empty")

	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "anything"},
		},
	}

	result := hook(req)
	// No context found → original returned unchanged
	if len(result.Messages) != 1 {
		t.Errorf("expected 1 message (no context injected), got %d", len(result.Messages))
	}
}

func TestRAGHook_DoesNotMutateOriginal(t *testing.T) {
	p := newPopulatedPipeline(t)
	hook := knowledge.RAGHook(p, "docs")

	orig := []types.InternalMessage{
		{Role: types.RoleUser, Content: "Go language"},
	}
	req := &types.InternalChatRequest{Messages: orig}

	modified := hook(req)

	// Original slice must not have been modified
	if len(req.Messages) != 1 {
		t.Errorf("original request messages were mutated: got %d messages", len(req.Messages))
	}
	if modified == req {
		t.Error("expected a new request struct, not the same pointer")
	}
}

func TestRAGHook_UsesLastUserMessage(t *testing.T) {
	p := newPopulatedPipeline(t)
	hook := knowledge.RAGHook(p, "docs")

	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleSystem, Content: "System prompt."},
			{Role: types.RoleUser, Content: "First user message"},
			{Role: types.RoleAssistant, Content: "Assistant reply"},
			{Role: types.RoleUser, Content: "Go programming language"},
		},
	}

	modified := hook(req)

	// Should have prepended a system message
	if modified.Messages[0].Role != types.RoleSystem {
		t.Errorf("expected system message prepended, got role %q", modified.Messages[0].Role)
	}
	// All original messages should still be present
	if len(modified.Messages) != len(req.Messages)+1 {
		t.Errorf("expected %d messages, got %d", len(req.Messages)+1, len(modified.Messages))
	}
}
