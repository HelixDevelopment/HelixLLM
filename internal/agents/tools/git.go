package tools

import (
	"context"
	"fmt"
)

// ---------------------------------------------------------------------------
// GitStatusTool
// ---------------------------------------------------------------------------

// GitStatusTool shows the working tree status of a git repository.
type GitStatusTool struct {
	sandbox *Sandbox
}

// NewGitStatusTool creates a GitStatusTool.
func NewGitStatusTool(sandbox *Sandbox) *GitStatusTool {
	return &GitStatusTool{sandbox: sandbox}
}

func (g *GitStatusTool) Name() string { return "git_status" }
func (g *GitStatusTool) Description() string {
	return "Show the working tree status of a git repository (short format with branch info)."
}

func (g *GitStatusTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the git repository (default: current directory)",
			},
		},
		"required": []string{},
	}
}

func (g *GitStatusTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	repoPath := optionalString(args, "path", ".")
	out, err := g.sandbox.Execute(ctx, "git", "-C", repoPath, "status", "-sb")
	if err != nil {
		return "", fmt.Errorf("git_status: %w", err)
	}
	return truncate(out, 10240), nil
}

// ---------------------------------------------------------------------------
// GitDiffTool
// ---------------------------------------------------------------------------

// GitDiffTool shows changes in a git repository.
type GitDiffTool struct {
	sandbox *Sandbox
}

// NewGitDiffTool creates a GitDiffTool.
func NewGitDiffTool(sandbox *Sandbox) *GitDiffTool {
	return &GitDiffTool{sandbox: sandbox}
}

func (g *GitDiffTool) Name() string { return "git_diff" }
func (g *GitDiffTool) Description() string {
	return "Show changes in a git repository. Use staged=true to see staged changes."
}

func (g *GitDiffTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the git repository (default: current directory)",
			},
			"staged": map[string]interface{}{
				"type":        "boolean",
				"description": "Show staged changes instead of unstaged (default false)",
			},
		},
		"required": []string{},
	}
}

func (g *GitDiffTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	repoPath := optionalString(args, "path", ".")
	staged := optionalBool(args, "staged", false)

	gitArgs := []string{"-C", repoPath, "diff"}
	if staged {
		gitArgs = append(gitArgs, "--staged")
	}

	out, err := g.sandbox.Execute(ctx, "git", gitArgs...)
	if err != nil {
		return "", fmt.Errorf("git_diff: %w", err)
	}
	if out == "" {
		return "No changes.", nil
	}
	return truncate(out, 10240), nil
}

// ---------------------------------------------------------------------------
// GitLogTool
// ---------------------------------------------------------------------------

// GitLogTool shows recent commits in a git repository.
type GitLogTool struct {
	sandbox *Sandbox
}

// NewGitLogTool creates a GitLogTool.
func NewGitLogTool(sandbox *Sandbox) *GitLogTool {
	return &GitLogTool{sandbox: sandbox}
}

func (g *GitLogTool) Name() string { return "git_log" }
func (g *GitLogTool) Description() string {
	return "Show recent commits in a git repository (oneline format)."
}

func (g *GitLogTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the git repository (default: current directory)",
			},
			"count": map[string]interface{}{
				"type":        "integer",
				"description": "Number of commits to show (default 10)",
			},
		},
		"required": []string{},
	}
}

func (g *GitLogTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	repoPath := optionalString(args, "path", ".")
	count := optionalInt(args, "count", 10)
	if count <= 0 {
		count = 10
	}
	if count > 100 {
		count = 100
	}

	out, err := g.sandbox.Execute(ctx, "git", "-C", repoPath, "log",
		fmt.Sprintf("--oneline"), fmt.Sprintf("-n%d", count))
	if err != nil {
		return "", fmt.Errorf("git_log: %w", err)
	}
	if out == "" {
		return "No commits found.", nil
	}
	return truncate(out, 10240), nil
}

// ---------------------------------------------------------------------------
// GitBranchTool
// ---------------------------------------------------------------------------

// GitBranchTool lists branches in a git repository.
type GitBranchTool struct {
	sandbox *Sandbox
}

// NewGitBranchTool creates a GitBranchTool.
func NewGitBranchTool(sandbox *Sandbox) *GitBranchTool {
	return &GitBranchTool{sandbox: sandbox}
}

func (g *GitBranchTool) Name() string { return "git_branch" }
func (g *GitBranchTool) Description() string {
	return "List all branches (local and remote) in a git repository."
}

func (g *GitBranchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the git repository (default: current directory)",
			},
		},
		"required": []string{},
	}
}

func (g *GitBranchTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	repoPath := optionalString(args, "path", ".")
	out, err := g.sandbox.Execute(ctx, "git", "-C", repoPath, "branch", "-a")
	if err != nil {
		return "", fmt.Errorf("git_branch: %w", err)
	}
	if out == "" {
		return "No branches found.", nil
	}
	return truncate(out, 10240), nil
}
