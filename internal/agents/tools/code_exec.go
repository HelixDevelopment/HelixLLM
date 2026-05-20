package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ---------------------------------------------------------------------------
// ExecutePythonTool
// ---------------------------------------------------------------------------

// ExecutePythonTool writes Python code to a temp file and runs it via python3.
type ExecutePythonTool struct {
	sandbox *Sandbox
}

// NewExecutePythonTool creates an ExecutePythonTool with the given sandbox.
func NewExecutePythonTool(sandbox *Sandbox) *ExecutePythonTool {
	return &ExecutePythonTool{sandbox: sandbox}
}

func (e *ExecutePythonTool) Name() string { return "execute_python" }
func (e *ExecutePythonTool) Description() string {
	return tr(keyExecPythonDesc)
}

func (e *ExecutePythonTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"code": map[string]interface{}{
				"type":        "string",
				"description": tr(keyExecPythonParamCode),
			},
		},
		"required": []string{"code"},
	}
}

func (e *ExecutePythonTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if args == nil {
		return "", fmt.Errorf("execute_python: arguments must not be nil")
	}

	code, err := requireString(args, "code")
	if err != nil {
		return "", fmt.Errorf("execute_python: %w", err)
	}

	// Write code to a temp file.
	tmpDir := os.TempDir()
	tmpFile := filepath.Join(tmpDir, fmt.Sprintf("helix_py_%d.py", time.Now().UnixNano()))

	if err := os.WriteFile(tmpFile, []byte(code), 0o600); err != nil {
		return "", fmt.Errorf("execute_python: failed to write temp file: %w", err)
	}
	defer os.Remove(tmpFile)

	// Use a 60-second sandbox for python execution.
	pySandbox := NewSandbox(SandboxConfig{
		MaxCPUSeconds:   60,
		MaxMemoryMB:     e.sandbox.config.MaxMemoryMB,
		MaxOutputBytes:  e.sandbox.config.MaxOutputBytes,
		AllowedPaths:    e.sandbox.config.AllowedPaths,
		BlockedCommands: e.sandbox.config.BlockedCommands,
	})

	out, err := pySandbox.Execute(ctx, "python3", tmpFile)
	if err != nil {
		return "", fmt.Errorf("execute_python: %w", err)
	}

	return truncate(out, 10240), nil
}

// ---------------------------------------------------------------------------
// ExecuteShellTool
// ---------------------------------------------------------------------------

// ExecuteShellTool runs a shell command through the sandbox after validation.
type ExecuteShellTool struct {
	sandbox *Sandbox
}

// NewExecuteShellTool creates an ExecuteShellTool with the given sandbox.
func NewExecuteShellTool(sandbox *Sandbox) *ExecuteShellTool {
	return &ExecuteShellTool{sandbox: sandbox}
}

func (e *ExecuteShellTool) Name() string { return "execute_shell" }
func (e *ExecuteShellTool) Description() string {
	return tr(keyExecShellDesc)
}

func (e *ExecuteShellTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": tr(keyExecShellParamCmd),
			},
		},
		"required": []string{"command"},
	}
}

func (e *ExecuteShellTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if args == nil {
		return "", fmt.Errorf("execute_shell: arguments must not be nil")
	}

	command, err := requireString(args, "command")
	if err != nil {
		return "", fmt.Errorf("execute_shell: %w", err)
	}

	// Validate command against blocked patterns.
	if err := e.sandbox.ValidateCommand(command); err != nil {
		return "", fmt.Errorf("execute_shell: %w", err)
	}

	out, err := e.sandbox.Execute(ctx, "sh", "-c", command) // #nosec G204 -- validated above
	if err != nil {
		return "", fmt.Errorf("execute_shell: %w", err)
	}

	return truncate(out, 10240), nil
}
