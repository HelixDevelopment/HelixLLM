package tools

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ExecutePythonTool
// ---------------------------------------------------------------------------

func TestExecutePythonTool_Name(t *testing.T) {
	s := DefaultSandbox()
	tool := NewExecutePythonTool(s)
	if tool.Name() != "execute_python" {
		t.Errorf("expected name execute_python, got %s", tool.Name())
	}
}

func TestExecutePythonTool_Execute(t *testing.T) {
	// Skip if python3 is not available.
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available, skipping")
	}

	s := DefaultSandbox()
	tool := NewExecutePythonTool(s)
	ctx := context.Background()

	out, err := tool.Execute(ctx, map[string]interface{}{
		"code": "print('hello from python')",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "hello from python") {
		t.Errorf("expected 'hello from python', got: %s", out)
	}
}

func TestExecutePythonTool_SyntaxError(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available, skipping")
	}

	s := DefaultSandbox()
	tool := NewExecutePythonTool(s)

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"code": "def broken(",
	})
	if err == nil {
		t.Error("expected error for syntax error in python code")
	}
}

func TestExecutePythonTool_NilArgs(t *testing.T) {
	s := DefaultSandbox()
	tool := NewExecutePythonTool(s)

	_, err := tool.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil args")
	}
}

func TestExecutePythonTool_MissingCode(t *testing.T) {
	s := DefaultSandbox()
	tool := NewExecutePythonTool(s)

	_, err := tool.Execute(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Error("expected error for missing code parameter")
	}
}

// ---------------------------------------------------------------------------
// ExecuteShellTool
// ---------------------------------------------------------------------------

func TestExecuteShellTool_Name(t *testing.T) {
	s := DefaultSandbox()
	tool := NewExecuteShellTool(s)
	if tool.Name() != "execute_shell" {
		t.Errorf("expected name execute_shell, got %s", tool.Name())
	}
}

func TestExecuteShellTool_Execute(t *testing.T) {
	s := DefaultSandbox()
	tool := NewExecuteShellTool(s)
	ctx := context.Background()

	out, err := tool.Execute(ctx, map[string]interface{}{
		"command": "echo hello shell",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "hello shell") {
		t.Errorf("expected 'hello shell', got: %s", out)
	}
}

func TestExecuteShellTool_BlockedCommand(t *testing.T) {
	s := DefaultSandbox()
	tool := NewExecuteShellTool(s)

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "sudo rm -rf /",
	})
	if err == nil {
		t.Error("expected error for blocked command")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected 'blocked' in error, got: %v", err)
	}
}

func TestExecuteShellTool_NilArgs(t *testing.T) {
	s := DefaultSandbox()
	tool := NewExecuteShellTool(s)

	_, err := tool.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil args")
	}
}

func TestExecuteShellTool_MissingCommand(t *testing.T) {
	s := DefaultSandbox()
	tool := NewExecuteShellTool(s)

	_, err := tool.Execute(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Error("expected error for missing command parameter")
	}
}

func TestExecuteShellTool_MultipleCommands(t *testing.T) {
	s := DefaultSandbox()
	tool := NewExecuteShellTool(s)

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "echo first && echo second",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "first") || !strings.Contains(out, "second") {
		t.Errorf("expected both outputs, got: %s", out)
	}
}
