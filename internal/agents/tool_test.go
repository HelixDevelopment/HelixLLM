package agents_test

import (
	"context"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
)

// stubTool is a minimal Tool implementation for testing the registry.
type stubTool struct {
	name        string
	description string
	params      map[string]interface{}
	result      string
	err         error
}

func (s *stubTool) Name() string                                { return s.name }
func (s *stubTool) Description() string                         { return s.description }
func (s *stubTool) Parameters() map[string]interface{}          { return s.params }
func (s *stubTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	return s.result, s.err
}

func TestToolRegistryRegisterAndGet(t *testing.T) {
	reg := agents.NewToolRegistry()

	tool := &stubTool{
		name:        "test_tool",
		description: "A test tool",
		params:      map[string]interface{}{"input": "string"},
		result:      "ok",
	}

	err := reg.Register(tool)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	got, ok := reg.Get("test_tool")
	if !ok {
		t.Fatal("Get returned false for registered tool")
	}
	if got.Name() != "test_tool" {
		t.Errorf("expected name test_tool, got %s", got.Name())
	}
}

func TestToolRegistryGetNotFound(t *testing.T) {
	reg := agents.NewToolRegistry()

	_, ok := reg.Get("nonexistent")
	if ok {
		t.Error("Get returned true for unregistered tool")
	}
}

func TestToolRegistryDuplicateRegistration(t *testing.T) {
	reg := agents.NewToolRegistry()

	tool := &stubTool{name: "dup", description: "first"}
	err := reg.Register(tool)
	if err != nil {
		t.Fatalf("first Register failed: %v", err)
	}

	tool2 := &stubTool{name: "dup", description: "second"}
	err = reg.Register(tool2)
	if err == nil {
		t.Error("expected error on duplicate registration, got nil")
	}
}

func TestToolRegistryList(t *testing.T) {
	reg := agents.NewToolRegistry()

	_ = reg.Register(&stubTool{name: "alpha", description: "tool alpha", params: map[string]interface{}{"x": "int"}})
	_ = reg.Register(&stubTool{name: "beta", description: "tool beta", params: map[string]interface{}{"y": "string"}})

	infos := reg.List()
	if len(infos) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(infos))
	}

	// List should contain both tools (order is not guaranteed).
	names := map[string]bool{}
	for _, info := range infos {
		names[info.Name] = true
		if info.Description == "" {
			t.Errorf("tool %s has empty description", info.Name)
		}
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("expected alpha and beta in list, got %v", names)
	}
}

func TestToolRegistryExecute(t *testing.T) {
	reg := agents.NewToolRegistry()

	tool := &stubTool{
		name:        "exec_tool",
		description: "executes",
		result:      "executed!",
	}
	_ = reg.Register(tool)

	result, err := reg.Execute(context.Background(), "exec_tool", map[string]interface{}{"key": "value"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result != "executed!" {
		t.Errorf("expected 'executed!', got %s", result)
	}
}

func TestToolRegistryExecuteNotFound(t *testing.T) {
	reg := agents.NewToolRegistry()

	_, err := reg.Execute(context.Background(), "missing", nil)
	if err == nil {
		t.Error("expected error for missing tool, got nil")
	}
}

func TestToolRegistryExecuteWithError(t *testing.T) {
	reg := agents.NewToolRegistry()

	tool := &stubTool{
		name:        "fail_tool",
		description: "always fails",
		err:         context.DeadlineExceeded,
	}
	_ = reg.Register(tool)

	_, err := reg.Execute(context.Background(), "fail_tool", nil)
	if err == nil {
		t.Error("expected error from failing tool, got nil")
	}
}
