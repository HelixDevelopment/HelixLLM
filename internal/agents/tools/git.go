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

// ---------------------------------------------------------------------------
// GitCommitTool
// ---------------------------------------------------------------------------

// GitCommitTool creates a commit in a git repository.
type GitCommitTool struct {
	sandbox *Sandbox
}

// NewGitCommitTool creates a GitCommitTool.
func NewGitCommitTool(sandbox *Sandbox) *GitCommitTool {
	return &GitCommitTool{sandbox: sandbox}
}

func (g *GitCommitTool) Name() string { return "git_commit" }
func (g *GitCommitTool) Description() string {
	return "Create a git commit with the given message. Optionally stage all changes first."
}

func (g *GitCommitTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"message": map[string]interface{}{
				"type":        "string",
				"description": "Commit message (required)",
			},
			"all": map[string]interface{}{
				"type":        "boolean",
				"description": "Stage all changes before committing (git add -A) (default false)",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the git repository (default: current directory)",
			},
		},
		"required": []string{"message"},
	}
}

func (g *GitCommitTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	message, err := requireString(args, "message")
	if err != nil {
		return "", fmt.Errorf("git_commit: %w", err)
	}

	repoPath := optionalString(args, "path", ".")
	addAll := optionalBool(args, "all", false)

	if addAll {
		_, err := g.sandbox.Execute(ctx, "git", "-C", repoPath, "add", "-A")
		if err != nil {
			return "", fmt.Errorf("git_commit: failed to stage changes: %w", err)
		}
	}

	out, err := g.sandbox.Execute(ctx, "git", "-C", repoPath, "commit", "-m", message)
	if err != nil {
		return "", fmt.Errorf("git_commit: %w", err)
	}
	return truncate(out, 10240), nil
}

// ---------------------------------------------------------------------------
// GitPushTool
// ---------------------------------------------------------------------------

// GitPushTool pushes commits to a remote repository.
type GitPushTool struct {
	sandbox *Sandbox
}

// NewGitPushTool creates a GitPushTool.
func NewGitPushTool(sandbox *Sandbox) *GitPushTool {
	return &GitPushTool{sandbox: sandbox}
}

func (g *GitPushTool) Name() string { return "git_push" }
func (g *GitPushTool) Description() string {
	return "Push commits to a remote repository. Defaults to 'origin' if no remote is specified."
}

func (g *GitPushTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"remote": map[string]interface{}{
				"type":        "string",
				"description": "Remote name (default: origin)",
			},
			"branch": map[string]interface{}{
				"type":        "string",
				"description": "Branch to push (default: current branch)",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the git repository (default: current directory)",
			},
		},
		"required": []string{},
	}
}

func (g *GitPushTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	repoPath := optionalString(args, "path", ".")
	remote := optionalString(args, "remote", "")
	branch := optionalString(args, "branch", "")

	gitArgs := []string{"-C", repoPath, "push"}
	if remote != "" {
		gitArgs = append(gitArgs, remote)
		if branch != "" {
			gitArgs = append(gitArgs, branch)
		}
	} else if branch != "" {
		gitArgs = append(gitArgs, "origin", branch)
	}

	out, err := g.sandbox.Execute(ctx, "git", gitArgs...)
	if err != nil {
		return "", fmt.Errorf("git_push: %w", err)
	}
	if out == "" {
		return "Push completed successfully.", nil
	}
	return truncate(out, 10240), nil
}

// ---------------------------------------------------------------------------
// GitPullTool
// ---------------------------------------------------------------------------

// GitPullTool pulls changes from a remote repository.
type GitPullTool struct {
	sandbox *Sandbox
}

// NewGitPullTool creates a GitPullTool.
func NewGitPullTool(sandbox *Sandbox) *GitPullTool {
	return &GitPullTool{sandbox: sandbox}
}

func (g *GitPullTool) Name() string { return "git_pull" }
func (g *GitPullTool) Description() string {
	return "Pull changes from a remote repository into the current branch."
}

func (g *GitPullTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"remote": map[string]interface{}{
				"type":        "string",
				"description": "Remote name (default: origin)",
			},
			"branch": map[string]interface{}{
				"type":        "string",
				"description": "Branch to pull (default: current branch)",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the git repository (default: current directory)",
			},
		},
		"required": []string{},
	}
}

func (g *GitPullTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	repoPath := optionalString(args, "path", ".")
	remote := optionalString(args, "remote", "")
	branch := optionalString(args, "branch", "")

	gitArgs := []string{"-C", repoPath, "pull"}
	if remote != "" {
		gitArgs = append(gitArgs, remote)
		if branch != "" {
			gitArgs = append(gitArgs, branch)
		}
	} else if branch != "" {
		gitArgs = append(gitArgs, "origin", branch)
	}

	out, err := g.sandbox.Execute(ctx, "git", gitArgs...)
	if err != nil {
		return "", fmt.Errorf("git_pull: %w", err)
	}
	if out == "" {
		return "Pull completed successfully.", nil
	}
	return truncate(out, 10240), nil
}

// ---------------------------------------------------------------------------
// GitCreateBranchTool
// ---------------------------------------------------------------------------

// GitCreateBranchTool creates a new branch in a git repository.
type GitCreateBranchTool struct {
	sandbox *Sandbox
}

// NewGitCreateBranchTool creates a GitCreateBranchTool.
func NewGitCreateBranchTool(sandbox *Sandbox) *GitCreateBranchTool {
	return &GitCreateBranchTool{sandbox: sandbox}
}

func (g *GitCreateBranchTool) Name() string { return "git_create_branch" }
func (g *GitCreateBranchTool) Description() string {
	return "Create a new git branch. Optionally switch to it immediately."
}

func (g *GitCreateBranchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Name of the new branch (required)",
			},
			"checkout": map[string]interface{}{
				"type":        "boolean",
				"description": "Switch to the new branch after creating it (default true)",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the git repository (default: current directory)",
			},
		},
		"required": []string{"name"},
	}
}

func (g *GitCreateBranchTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return "", fmt.Errorf("git_create_branch: %w", err)
	}

	repoPath := optionalString(args, "path", ".")
	checkout := optionalBool(args, "checkout", true)

	var out string
	if checkout {
		out, err = g.sandbox.Execute(ctx, "git", "-C", repoPath, "checkout", "-b", name)
	} else {
		out, err = g.sandbox.Execute(ctx, "git", "-C", repoPath, "branch", name)
	}
	if err != nil {
		return "", fmt.Errorf("git_create_branch: %w", err)
	}
	if out == "" {
		if checkout {
			return fmt.Sprintf("Switched to a new branch '%s'.", name), nil
		}
		return fmt.Sprintf("Branch '%s' created.", name), nil
	}
	return truncate(out, 10240), nil
}
