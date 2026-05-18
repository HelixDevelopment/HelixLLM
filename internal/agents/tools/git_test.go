package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// CONST-035 / CONST-051(B): we synthesize a real git repo per-test via
// initTempRepo() (defined below) instead of relying on a hardcoded operator-host
// absolute path to a HelixLLM checkout. The on-demand repo exercises the same
// git plumbing on any host.

func gitSandbox(t *testing.T) (*Sandbox, string) {
	t.Helper()
	repo := initTempRepo(t)
	return NewSandbox(SandboxConfig{
		AllowedPaths: []string{"/tmp", os.TempDir(), repo},
	}), repo
}

// ---------------------------------------------------------------------------
// GitStatusTool
// ---------------------------------------------------------------------------

func TestGitStatusTool_Name(t *testing.T) {
	sandbox, _ := gitSandbox(t)
	tool := NewGitStatusTool(sandbox)
	if tool.Name() != "git_status" {
		t.Errorf("expected git_status, got %s", tool.Name())
	}
}

func TestGitStatusTool_Execute(t *testing.T) {
	sandbox, repo := gitSandbox(t)
	tool := NewGitStatusTool(sandbox)
	ctx := context.Background()

	out, err := tool.Execute(ctx, map[string]interface{}{
		"path": repo,
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
	sandbox, _ := gitSandbox(t)
	tool := NewGitStatusTool(sandbox)

	// With nil args, should use current directory.
	out, err := tool.Execute(context.Background(), nil)
	if err != nil {
		// This might fail if cwd is not a git repo, which is acceptable.
		t.Skipf("current directory may not be a git repo: %v (SKIP-OK: #unmarked-skip-needs-ticket)", err)
	}
	if out == "" {
		t.Error("expected non-empty status output")
	}
}

// ---------------------------------------------------------------------------
// GitDiffTool
// ---------------------------------------------------------------------------

func TestGitDiffTool_Name(t *testing.T) {
	sandbox, _ := gitSandbox(t)
	tool := NewGitDiffTool(sandbox)
	if tool.Name() != "git_diff" {
		t.Errorf("expected git_diff, got %s", tool.Name())
	}
}

func TestGitDiffTool_Execute(t *testing.T) {
	sandbox, repo := gitSandbox(t)
	tool := NewGitDiffTool(sandbox)
	ctx := context.Background()

	// This returns either diff content or "No changes." -- both are valid.
	out, err := tool.Execute(ctx, map[string]interface{}{
		"path": repo,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == "" {
		t.Error("expected some output from git diff")
	}
}

func TestGitDiffTool_Staged(t *testing.T) {
	sandbox, repo := gitSandbox(t)
	tool := NewGitDiffTool(sandbox)
	ctx := context.Background()

	out, err := tool.Execute(ctx, map[string]interface{}{
		"path":   repo,
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
	sandbox, _ := gitSandbox(t)
	tool := NewGitLogTool(sandbox)
	if tool.Name() != "git_log" {
		t.Errorf("expected git_log, got %s", tool.Name())
	}
}

func TestGitLogTool_Execute(t *testing.T) {
	sandbox, repo := gitSandbox(t)
	tool := NewGitLogTool(sandbox)
	ctx := context.Background()

	out, err := tool.Execute(ctx, map[string]interface{}{
		"path":  repo,
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
	sandbox, repo := gitSandbox(t)
	tool := NewGitLogTool(sandbox)

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"path": repo,
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
	sandbox, repo := gitSandbox(t)
	tool := NewGitLogTool(sandbox)

	// Negative count should be clamped to 10.
	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"path":  repo,
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
	sandbox, _ := gitSandbox(t)
	tool := NewGitBranchTool(sandbox)
	if tool.Name() != "git_branch" {
		t.Errorf("expected git_branch, got %s", tool.Name())
	}
}

func TestGitBranchTool_Execute(t *testing.T) {
	sandbox, repo := gitSandbox(t)
	tool := NewGitBranchTool(sandbox)
	ctx := context.Background()

	out, err := tool.Execute(ctx, map[string]interface{}{
		"path": repo,
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
	sandbox, _ := gitSandbox(t)
	tool := NewGitBranchTool(sandbox)

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"path": "/tmp",
	})
	if err == nil {
		t.Error("expected error for non-git directory")
	}
}

// ---------------------------------------------------------------------------
// Helpers for mutating-git-tool tests (temp repos)
// ---------------------------------------------------------------------------

// initTempRepo creates a temporary git repository with one initial commit.
func initTempRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	for _, c := range [][]string{
		{"git", "init", dir},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "Test User"},
	} {
		out, err := exec.Command(c[0], c[1:]...).CombinedOutput()
		if err != nil {
			t.Fatalf("setup %v: %v\n%s", c, err, out)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	for _, c := range [][]string{
		{"git", "-C", dir, "add", "-A"},
		{"git", "-C", dir, "commit", "-m", "initial commit"},
	} {
		out, err := exec.Command(c[0], c[1:]...).CombinedOutput()
		if err != nil {
			t.Fatalf("setup %v: %v\n%s", c, err, out)
		}
	}
	return dir
}

// tempSandbox returns a Sandbox whose AllowedPaths include /tmp.
func tempSandbox() *Sandbox {
	return NewSandbox(SandboxConfig{
		AllowedPaths: []string{"/tmp", os.TempDir()},
	})
}

// currentBranch returns the current branch name in the given repo directory.
func currentBranch(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if err != nil {
		t.Fatalf("git branch --show-current: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// ---------------------------------------------------------------------------
// GitCommitTool
// ---------------------------------------------------------------------------

func TestGitCommitTool_Name(t *testing.T) {
	tool := NewGitCommitTool(tempSandbox())
	if tool.Name() != "git_commit" {
		t.Errorf("expected git_commit, got %s", tool.Name())
	}
}

func TestGitCommitTool_Parameters(t *testing.T) {
	tool := NewGitCommitTool(tempSandbox())
	params := tool.Parameters()
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("parameters missing properties")
	}
	if _, ok := props["message"]; !ok {
		t.Fatal("parameters missing 'message' property")
	}
	required, ok := params["required"].([]string)
	if !ok || len(required) == 0 {
		t.Fatal("'message' should be required")
	}
	found := false
	for _, r := range required {
		if r == "message" {
			found = true
		}
	}
	if !found {
		t.Fatal("'message' not in required list")
	}
}

func TestGitCommitTool_Execute(t *testing.T) {
	sandbox := tempSandbox()
	tool := NewGitCommitTool(sandbox)
	ctx := context.Background()

	tests := []struct {
		name    string
		args    map[string]interface{}
		setup   func(dir string)
		wantErr bool
	}{
		{
			name:    "missing message parameter",
			args:    map[string]interface{}{},
			wantErr: true,
		},
		{
			name: "empty message parameter",
			args: map[string]interface{}{
				"message": "",
			},
			wantErr: true,
		},
		{
			name: "commit with pre-staged changes",
			args: map[string]interface{}{
				"message": "test commit",
			},
			setup: func(dir string) {
				os.WriteFile(filepath.Join(dir, "new.txt"), []byte("hello\n"), 0644)
				exec.Command("git", "-C", dir, "add", "-A").Run()
			},
			wantErr: false,
		},
		{
			name: "commit with all flag stages and commits",
			args: map[string]interface{}{
				"message": "auto-staged commit",
				"all":     true,
			},
			setup: func(dir string) {
				os.WriteFile(filepath.Join(dir, "auto.txt"), []byte("world\n"), 0644)
			},
			wantErr: false,
		},
		{
			name: "commit with nothing to commit fails",
			args: map[string]interface{}{
				"message": "empty commit",
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := initTempRepo(t)
			if tc.setup != nil {
				tc.setup(dir)
			}
			// Copy args so parallel sub-tests do not share map.
			args := make(map[string]interface{})
			for k, v := range tc.args {
				args[k] = v
			}
			args["path"] = dir

			out, err := tool.Execute(ctx, args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (output: %s)", out)
				}
				if !strings.Contains(err.Error(), "git_commit") {
					t.Fatalf("error should mention git_commit, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out == "" {
				t.Fatal("expected non-empty output")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GitPushTool
// ---------------------------------------------------------------------------

func TestGitPushTool_Name(t *testing.T) {
	tool := NewGitPushTool(tempSandbox())
	if tool.Name() != "git_push" {
		t.Errorf("expected git_push, got %s", tool.Name())
	}
}

func TestGitPushTool_Parameters(t *testing.T) {
	tool := NewGitPushTool(tempSandbox())
	params := tool.Parameters()
	required, ok := params["required"].([]string)
	if !ok {
		t.Fatal("parameters missing required field")
	}
	if len(required) != 0 {
		t.Fatalf("expected no required params, got %v", required)
	}
}

func TestGitPushTool_Execute(t *testing.T) {
	sandbox := tempSandbox()
	tool := NewGitPushTool(sandbox)
	ctx := context.Background()

	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr bool
	}{
		{
			name:    "push without remote fails (no remote configured)",
			args:    map[string]interface{}{},
			wantErr: true,
		},
		{
			name: "push with explicit remote fails (no remote configured)",
			args: map[string]interface{}{
				"remote": "origin",
				"branch": "main",
			},
			wantErr: true,
		},
		{
			name: "push with branch only defaults remote to origin",
			args: map[string]interface{}{
				"branch": "main",
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := initTempRepo(t)
			args := make(map[string]interface{})
			for k, v := range tc.args {
				args[k] = v
			}
			args["path"] = dir

			_, err := tool.Execute(ctx, args)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr && err != nil {
				if !strings.Contains(err.Error(), "git_push") {
					t.Fatalf("error should mention git_push, got: %v", err)
				}
			}
		})
	}
}

func TestGitPushTool_ExecuteWithBareRemote(t *testing.T) {
	sandbox := tempSandbox()
	tool := NewGitPushTool(sandbox)
	ctx := context.Background()

	// Create a bare remote and a local repo with that remote configured.
	bare := t.TempDir()
	out, err := exec.Command("git", "init", "--bare", bare).CombinedOutput()
	if err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	local := initTempRepo(t)
	exec.Command("git", "-C", local, "remote", "add", "origin", bare).Run()
	branch := currentBranch(t, local)

	result, err := tool.Execute(ctx, map[string]interface{}{
		"path":   local,
		"remote": "origin",
		"branch": branch,
	})
	if err != nil {
		t.Fatalf("push to bare remote failed: %v", err)
	}
	_ = result
}

// ---------------------------------------------------------------------------
// GitPullTool
// ---------------------------------------------------------------------------

func TestGitPullTool_Name(t *testing.T) {
	tool := NewGitPullTool(tempSandbox())
	if tool.Name() != "git_pull" {
		t.Errorf("expected git_pull, got %s", tool.Name())
	}
}

func TestGitPullTool_Parameters(t *testing.T) {
	tool := NewGitPullTool(tempSandbox())
	params := tool.Parameters()
	required, ok := params["required"].([]string)
	if !ok {
		t.Fatal("parameters missing required field")
	}
	if len(required) != 0 {
		t.Fatalf("expected no required params, got %v", required)
	}
}

func TestGitPullTool_Execute(t *testing.T) {
	sandbox := tempSandbox()
	tool := NewGitPullTool(sandbox)
	ctx := context.Background()

	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr bool
	}{
		{
			name:    "pull without remote fails (no remote configured)",
			args:    map[string]interface{}{},
			wantErr: true,
		},
		{
			name: "pull with explicit remote fails (no remote configured)",
			args: map[string]interface{}{
				"remote": "origin",
				"branch": "main",
			},
			wantErr: true,
		},
		{
			name: "pull with branch only defaults remote to origin",
			args: map[string]interface{}{
				"branch": "main",
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := initTempRepo(t)
			args := make(map[string]interface{})
			for k, v := range tc.args {
				args[k] = v
			}
			args["path"] = dir

			_, err := tool.Execute(ctx, args)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr && err != nil {
				if !strings.Contains(err.Error(), "git_pull") {
					t.Fatalf("error should mention git_pull, got: %v", err)
				}
			}
		})
	}
}

func TestGitPullTool_ExecuteWithRemote(t *testing.T) {
	sandbox := tempSandbox()
	tool := NewGitPullTool(sandbox)
	ctx := context.Background()

	// Set up: bare remote -> localA pushes -> clone to localB -> push new commit -> pull into localB.
	bare := t.TempDir()
	out, err := exec.Command("git", "init", "--bare", bare).CombinedOutput()
	if err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	localA := initTempRepo(t)
	exec.Command("git", "-C", localA, "remote", "add", "origin", bare).Run()
	branch := currentBranch(t, localA)

	out, err = exec.Command("git", "-C", localA, "push", "origin", branch).CombinedOutput()
	if err != nil {
		t.Fatalf("push localA: %v\n%s", err, out)
	}

	// Clone into localB.
	localB := t.TempDir()
	os.RemoveAll(localB)
	out, err = exec.Command("git", "clone", bare, localB).CombinedOutput()
	if err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}

	// Add a commit in localA and push.
	os.WriteFile(filepath.Join(localA, "extra.txt"), []byte("data\n"), 0644)
	exec.Command("git", "-C", localA, "add", "-A").Run()
	exec.Command("git", "-C", localA, "-c", "user.email=test@test.com",
		"-c", "user.name=Test", "commit", "-m", "extra").Run()
	exec.Command("git", "-C", localA, "push", "origin", branch).Run()

	// Pull into localB using the tool.
	result, err := tool.Execute(ctx, map[string]interface{}{
		"path":   localB,
		"remote": "origin",
		"branch": branch,
	})
	if err != nil {
		t.Fatalf("pull into localB failed: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty pull output")
	}

	// Verify the file arrived.
	if _, statErr := os.Stat(filepath.Join(localB, "extra.txt")); statErr != nil {
		t.Fatal("extra.txt should exist after pull")
	}
}

// ---------------------------------------------------------------------------
// GitCreateBranchTool
// ---------------------------------------------------------------------------

func TestGitCreateBranchTool_Name(t *testing.T) {
	tool := NewGitCreateBranchTool(tempSandbox())
	if tool.Name() != "git_create_branch" {
		t.Errorf("expected git_create_branch, got %s", tool.Name())
	}
}

func TestGitCreateBranchTool_Parameters(t *testing.T) {
	tool := NewGitCreateBranchTool(tempSandbox())
	params := tool.Parameters()
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("parameters missing properties")
	}
	if _, ok := props["name"]; !ok {
		t.Fatal("parameters missing 'name' property")
	}
	required, ok := params["required"].([]string)
	if !ok || len(required) == 0 {
		t.Fatal("'name' should be required")
	}
	found := false
	for _, r := range required {
		if r == "name" {
			found = true
		}
	}
	if !found {
		t.Fatal("'name' not in required list")
	}
}

func TestGitCreateBranchTool_Execute(t *testing.T) {
	sandbox := tempSandbox()
	tool := NewGitCreateBranchTool(sandbox)
	ctx := context.Background()

	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr bool
		check   func(t *testing.T, dir string)
	}{
		{
			name:    "missing name parameter",
			args:    map[string]interface{}{},
			wantErr: true,
		},
		{
			name: "empty name parameter",
			args: map[string]interface{}{
				"name": "",
			},
			wantErr: true,
		},
		{
			name: "create branch with checkout (default)",
			args: map[string]interface{}{
				"name": "feat/test-branch",
			},
			wantErr: false,
			check: func(t *testing.T, dir string) {
				if b := currentBranch(t, dir); b != "feat/test-branch" {
					t.Fatalf("expected to be on feat/test-branch, got %q", b)
				}
			},
		},
		{
			name: "create branch without checkout",
			args: map[string]interface{}{
				"name":     "feat/no-checkout",
				"checkout": false,
			},
			wantErr: false,
			check: func(t *testing.T, dir string) {
				if b := currentBranch(t, dir); b == "feat/no-checkout" {
					t.Fatal("should not have switched to the new branch")
				}
				// Verify the branch exists.
				out, err := exec.Command("git", "-C", dir, "branch",
					"--list", "feat/no-checkout").Output()
				if err != nil || len(strings.TrimSpace(string(out))) == 0 {
					t.Fatal("branch feat/no-checkout should exist")
				}
			},
		},
		{
			name: "duplicate branch name fails",
			args: map[string]interface{}{
				"name":     "__placeholder__", // replaced in test body
				"checkout": false,
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := initTempRepo(t)

			args := make(map[string]interface{})
			for k, v := range tc.args {
				args[k] = v
			}
			args["path"] = dir

			// For the duplicate test, use the actual default branch name.
			if tc.name == "duplicate branch name fails" {
				args["name"] = currentBranch(t, dir)
			}

			out, err := tool.Execute(ctx, args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (output: %s)", out)
				}
				if !strings.Contains(err.Error(), "git_create_branch") {
					t.Fatalf("error should mention git_create_branch, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, dir)
			}
		})
	}
}
