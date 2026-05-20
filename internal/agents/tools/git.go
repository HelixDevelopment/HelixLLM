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
	return tr(keyGitStatusDesc)
}

func (g *GitStatusTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": tr(keyGitParamRepoPath),
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
	return tr(keyGitDiffDesc)
}

func (g *GitDiffTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": tr(keyGitParamRepoPath),
			},
			"staged": map[string]interface{}{
				"type":        "boolean",
				"description": tr(keyGitParamDiffStaged),
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
		return tr(keyGitResultNoChanges), nil
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
	return tr(keyGitLogDesc)
}

func (g *GitLogTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": tr(keyGitParamRepoPath),
			},
			"count": map[string]interface{}{
				"type":        "integer",
				"description": tr(keyGitParamLogCount),
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
		"--oneline", fmt.Sprintf("-n%d", count))
	if err != nil {
		return "", fmt.Errorf("git_log: %w", err)
	}
	if out == "" {
		return tr(keyGitResultNoCommits), nil
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
	return tr(keyGitBranchDesc)
}

func (g *GitBranchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": tr(keyGitParamRepoPath),
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
		return tr(keyGitResultNoBranches), nil
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
	return tr(keyGitCommitDesc)
}

func (g *GitCommitTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"message": map[string]interface{}{
				"type":        "string",
				"description": tr(keyGitParamCommitMsg),
			},
			"all": map[string]interface{}{
				"type":        "boolean",
				"description": tr(keyGitParamCommitAll),
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": tr(keyGitParamRepoPath),
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
	return tr(keyGitPushDesc)
}

func (g *GitPushTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"remote": map[string]interface{}{
				"type":        "string",
				"description": tr(keyGitParamRemote),
			},
			"branch": map[string]interface{}{
				"type":        "string",
				"description": tr(keyGitParamBranch),
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": tr(keyGitParamRepoPath),
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
		return tr(keyGitResultPushDone), nil
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
	return tr(keyGitPullDesc)
}

func (g *GitPullTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"remote": map[string]interface{}{
				"type":        "string",
				"description": tr(keyGitParamRemote),
			},
			"branch": map[string]interface{}{
				"type":        "string",
				"description": tr(keyGitParamBranch),
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": tr(keyGitParamRepoPath),
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
		return tr(keyGitResultPullDone), nil
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
	return tr(keyGitCreateBranchDesc)
}

func (g *GitCreateBranchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":        "string",
				"description": tr(keyGitParamNewBranch),
			},
			"checkout": map[string]interface{}{
				"type":        "boolean",
				"description": tr(keyGitParamCheckout),
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": tr(keyGitParamRepoPath),
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
			return tr(keyGitResultSwitchedBranch, map[string]string{"name": name}), nil
		}
		return tr(keyGitResultBranchCreated, map[string]string{"name": name}), nil
	}
	return truncate(out, 10240), nil
}
