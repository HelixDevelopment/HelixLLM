package tools_test

import (
	"context"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
	"github.com/HelixDevelopment/HelixLLM/internal/agents/tools"
)

func TestEchoToolInterface(t *testing.T) {
	var _ agents.Tool = (*tools.EchoTool)(nil)
}

func TestEchoToolName(t *testing.T) {
	tool := tools.NewEchoTool()
	if tool.Name() != "echo" {
		t.Errorf("expected name 'echo', got %s", tool.Name())
	}
}

func TestEchoToolDescription(t *testing.T) {
	tool := tools.NewEchoTool()
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestEchoToolParameters(t *testing.T) {
	tool := tools.NewEchoTool()
	params := tool.Parameters()
	if params == nil {
		t.Fatal("expected non-nil parameters")
	}
	if _, ok := params["message"]; !ok {
		t.Error("expected 'message' parameter in schema")
	}
}

func TestEchoToolExecute(t *testing.T) {
	tool := tools.NewEchoTool()
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"message": "hello world",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %s", result)
	}
}

func TestEchoToolExecuteMissingMessage(t *testing.T) {
	tool := tools.NewEchoTool()
	_, err := tool.Execute(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Error("expected error for missing message, got nil")
	}
}

func TestEchoToolExecuteNilArgs(t *testing.T) {
	tool := tools.NewEchoTool()
	_, err := tool.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil args, got nil")
	}
}
