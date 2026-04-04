package agents

import (
	"context"
	"fmt"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// mockBrainProvider returns a sequence of canned responses, cycling through
// them in order. It satisfies the brain.Provider interface.
type mockBrainProvider struct {
	responses []string
	index     int
}

func (m *mockBrainProvider) Complete(_ context.Context, _ *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	resp := m.responses[m.index%len(m.responses)]
	m.index++
	return &types.InternalChatResponse{
		ID:           "test",
		Model:        "mock",
		Message:      types.InternalMessage{Role: types.RoleAssistant, Content: resp},
		FinishReason: "stop",
	}, nil
}

func (m *mockBrainProvider) CompleteStream(_ context.Context, _ *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	ch := make(chan types.StreamChunk, 1)
	resp := m.responses[m.index%len(m.responses)]
	m.index++
	ch <- types.StreamChunk{Content: resp, FinishReason: "stop"}
	close(ch)
	return ch, nil
}

func (m *mockBrainProvider) Models() []string  { return []string{"mock"} }
func (m *mockBrainProvider) Name() string      { return "mock" }
func (m *mockBrainProvider) Available() bool   { return true }

// newTestBrain creates a Brain with the given mock provider registered.
func newTestBrain(responses []string) *brain.Brain {
	b := brain.New(brain.Config{DefaultProvider: "mock"})
	b.RegisterProvider("mock", &mockBrainProvider{responses: responses})
	return b
}

// newTestRegistry creates a ToolRegistry with an EchoTool registered.
func newTestRegistry() *ToolRegistry {
	reg := NewToolRegistry()
	_ = reg.Register(&echoTool{})
	return reg
}

// echoTool is a minimal tool used only in agent tests to avoid importing the
// tools sub-package (which would create a circular-like separation violation).
type echoTool struct{}

func (e *echoTool) Name() string        { return "echo" }
func (e *echoTool) Description() string { return "echoes input" }
func (e *echoTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"message": map[string]interface{}{"type": "string"},
	}
}
func (e *echoTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	if args == nil {
		return "", fmt.Errorf("echo: nil args")
	}
	msg, ok := args["message"]
	if !ok {
		return "", fmt.Errorf("echo: missing message")
	}
	return fmt.Sprintf("%v", msg), nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestAgent_DirectAnswer(t *testing.T) {
	b := newTestBrain([]string{"Hello, I am the assistant."})
	agent := NewAgent(AgentConfig{Brain: b})

	msgs := []types.InternalMessage{
		{Role: types.RoleUser, Content: "hi"},
	}
	resp, err := agent.Run(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Content != "Hello, I am the assistant." {
		t.Errorf("unexpected content: %q", resp.Message.Content)
	}
}

func TestAgent_ToolCallThenFinalAnswer(t *testing.T) {
	// First response is a tool call; second is the final answer.
	toolCallJSON := `{"tool_call": {"name": "echo", "arguments": {"message": "ping"}}}`
	finalAnswer := "The echo result was: ping"

	b := newTestBrain([]string{toolCallJSON, finalAnswer})
	reg := newTestRegistry()

	agent := NewAgent(AgentConfig{Brain: b, Tools: reg, MaxTurns: 5})

	msgs := []types.InternalMessage{
		{Role: types.RoleUser, Content: "please echo ping"},
	}
	resp, err := agent.Run(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Content != finalAnswer {
		t.Errorf("unexpected content: %q", resp.Message.Content)
	}
}

func TestAgent_MaxTurnsExceeded(t *testing.T) {
	// Brain always returns a tool call — loop should stop at maxTurns.
	toolCallJSON := `{"tool_call": {"name": "echo", "arguments": {"message": "loop"}}}`

	b := newTestBrain([]string{toolCallJSON})
	reg := newTestRegistry()

	maxTurns := 3
	agent := NewAgent(AgentConfig{Brain: b, Tools: reg, MaxTurns: maxTurns})

	msgs := []types.InternalMessage{
		{Role: types.RoleUser, Content: "loop forever"},
	}
	resp, err := agent.Run(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return the last response (a tool call response) without error.
	if resp == nil {
		t.Fatal("expected non-nil response when max turns exceeded")
	}
}

func TestAgent_UnknownTool(t *testing.T) {
	// Brain calls a tool that doesn't exist, then returns a final answer.
	unknownToolCall := `{"tool_call": {"name": "nonexistent", "arguments": {}}}`
	finalAnswer := "I could not find that tool."

	b := newTestBrain([]string{unknownToolCall, finalAnswer})
	reg := newTestRegistry() // only "echo" is registered

	agent := NewAgent(AgentConfig{Brain: b, Tools: reg, MaxTurns: 5})

	msgs := []types.InternalMessage{
		{Role: types.RoleUser, Content: "use nonexistent tool"},
	}
	resp, err := agent.Run(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Loop should have continued after the error observation.
	if resp.Message.Content != finalAnswer {
		t.Errorf("unexpected content: %q", resp.Message.Content)
	}
}

func TestAgent_NilBrain(t *testing.T) {
	agent := NewAgent(AgentConfig{Brain: nil})
	_, err := agent.Run(context.Background(), []types.InternalMessage{
		{Role: types.RoleUser, Content: "hello"},
	})
	if err == nil {
		t.Fatal("expected error for nil brain, got nil")
	}
}

func TestAgent_RAGHookApplied(t *testing.T) {
	var hookCalled bool
	ragHook := func(req *types.InternalChatRequest) *types.InternalChatRequest {
		hookCalled = true
		return req
	}

	b := newTestBrain([]string{"final answer"})
	agent := NewAgent(AgentConfig{Brain: b, RAGHook: ragHook})

	msgs := []types.InternalMessage{
		{Role: types.RoleUser, Content: "query"},
	}
	_, err := agent.Run(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hookCalled {
		t.Error("expected RAG hook to be called")
	}
}

func TestAgent_DefaultMaxTurns(t *testing.T) {
	agent := NewAgent(AgentConfig{Brain: newTestBrain([]string{"ok"})})
	if agent.maxTurns != defaultMaxTurns {
		t.Errorf("expected default maxTurns %d, got %d", defaultMaxTurns, agent.maxTurns)
	}
}

func TestExtractToolCall_Valid(t *testing.T) {
	content := `{"tool_call": {"name": "echo", "arguments": {"message": "hello"}}}`
	tc, ok := extractToolCall(content)
	if !ok {
		t.Fatal("expected valid tool call to be extracted")
	}
	if tc.Name != "echo" {
		t.Errorf("expected name %q, got %q", "echo", tc.Name)
	}
	if tc.Arguments["message"] != "hello" {
		t.Errorf("unexpected argument: %v", tc.Arguments["message"])
	}
}

func TestExtractToolCall_NoToolCall(t *testing.T) {
	_, ok := extractToolCall("This is a plain answer.")
	if ok {
		t.Error("expected no tool call to be extracted from plain text")
	}
}

func TestExtractToolCall_NilToolCall(t *testing.T) {
	_, ok := extractToolCall(`{"tool_call": null}`)
	if ok {
		t.Error("expected no tool call when tool_call is null")
	}
}

func TestExtractToolCall_MissingName(t *testing.T) {
	_, ok := extractToolCall(`{"tool_call": {"name": "", "arguments": {}}}`)
	if ok {
		t.Error("expected no tool call when name is empty")
	}
}
