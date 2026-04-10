package tools

import (
	"context"
	"strings"
	"testing"
)

// The HelixLLM directory is a git repo, so we use it for real git tests.
const helixLLMRoot = "/run/media/milosvasic/DATA4TB/Projects/HelixAgent/HelixLLM"

func gitSandbox() *Sandbox {
	return NewSandbox(SandboxConfig{
		AllowedPaths: []string{"/tmp", helixLLMRoot},
	})
}

// ---------------------------------------------------------------------------
// GitStatusTool
// ---------------------------------------------------------------------------

func TestGitStatusTool_Name(t *testing.T) {
	tool := NewGitStatusTool(gitSandbox())
	if tool.Name() != "git_status" {
		t.Errorf("expected git_status, got %s", tool.Name())
	}
}

func TestGitStatusTool_Execute(t *testing.T) {
	tool := NewGitStatusTool(gitSandbox())
	ctx := context.Background()

	out, err := tool.Execute(ctx, map[string]interface{}{
		"path": helixLLMRoot,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should contain branch info (## prefix in short format).
	if !strings.Contains(out, "##") {
		t.Errorf("expected branch info in status output, got: %s", out)
	}
}

func TestGitStatusTool_DefaultPath(t *testing.T) {
	tool := NewGitStatusTool(gitSandbox())

	// With nil args, should use current directory.
	out, err := tool.Execute(context.Background(), nil)
	if err != nil {
		// This might fail if cwd is not a git repo, which is acceptable.
		t.Skipf("current directory may not be a git repo: %v", err)
	}
	if out == "" {
		t.Error("expected non-empty status output")
	}
}

// ---------------------------------------------------------------------------
// GitDiffTool
// ---------------------------------------------------------------------------

func TestGitDiffTool_Name(t *testing.T) {
	tool := NewGitDiffTool(gitSandbox())
	if tool.Name() != "git_diff" {
		t.Errorf("expected git_diff, got %s", tool.Name())
	}
}

func TestGitDiffTool_Execute(t *testing.T) {
	tool := NewGitDiffTool(gitSandbox())
	ctx := context.Background()

	// This returns either diff content or "No changes." -- both are valid.
	out, err := tool.Execute(ctx, map[string]interface{}{
		"path": helixLLMRoot,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == "" {
		t.Error("expected some output from git diff")
	}
}

func TestGitDiffTool_Staged(t *testing.T) {
	tool := NewGitDiffTool(gitSandbox())
	ctx := context.Background()

	out, err := tool.Execute(ctx, map[string]interface{}{
		"path":   helixLLMRoot,
		"staged": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// May be empty ("No changes.") if nothing is staged -- that is fine.
	if out == "" {
		t.Error("expected some output")
	}
}

// ---------------------------------------------------------------------------
// GitLogTool
// ---------------------------------------------------------------------------

func TestGitLogTool_Name(t *testing.T) {
	tool := NewGitLogTool(gitSandbox())
	if tool.Name() != "git_log" {
		t.Errorf("expected git_log, got %s", tool.Name())
	}
}

func TestGitLogTool_Execute(t *testing.T) {
	tool := NewGitLogTool(gitSandbox())
	ctx := context.Background()

	out, err := tool.Execute(ctx, map[string]interface{}{
		"path":  helixLLMRoot,
		"count": 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should contain at least one commit hash.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 || lines[0] == "No commits found." {
		t.Error("expected at least one commit in log")
	}
}

func TestGitLogTool_DefaultCount(t *testing.T) {
	tool := NewGitLogTool(gitSandbox())

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"path": helixLLMRoot,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > 10 {
		t.Errorf("expected at most 10 lines (default), got %d", len(lines))
	}
}

func TestGitLogTool_ClampCount(t *testing.T) {
	tool := NewGitLogTool(gitSandbox())

	// Negative count should be clamped to 10.
	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"path":  helixLLMRoot,
		"count": -5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == "" {
		t.Error("expected non-empty output")
	}
}

// ---------------------------------------------------------------------------
// GitBranchTool
// ---------------------------------------------------------------------------

func TestGitBranchTool_Name(t *testing.T) {
	tool := NewGitBranchTool(gitSandbox())
	if tool.Name() != "git_branch" {
		t.Errorf("expected git_branch, got %s", tool.Name())
	}
}

func TestGitBranchTool_Execute(t *testing.T) {
	tool := NewGitBranchTool(gitSandbox())
	ctx := context.Background()

	out, err := tool.Execute(ctx, map[string]interface{}{
		"path": helixLLMRoot,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should contain at least one branch (e.g. main or master).
	if out == "No branches found." {
		t.Error("expected at least one branch")
	}
}

func TestGitBranchTool_InvalidRepo(t *testing.T) {
	tool := NewGitBranchTool(gitSandbox())

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"path": "/tmp",
	})
	if err == nil {
		t.Error("expected error for non-git directory")
	}
}
