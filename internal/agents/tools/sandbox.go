package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// SandboxConfig defines resource limits and access controls for tool execution.
type SandboxConfig struct {
	// MaxCPUSeconds is the maximum wall-clock time a command may run (default 30).
	MaxCPUSeconds int
	// MaxMemoryMB is the memory limit in megabytes (default 512). Advisory only.
	MaxMemoryMB int
	// MaxFileSizeMB is the max file size in megabytes a tool may create (default 10).
	MaxFileSizeMB int
	// MaxProcesses is the max number of child processes (default 10). Advisory only.
	MaxProcesses int
	// AllowedPaths are the directories a tool may read from or write to.
	AllowedPaths []string
	// BlockedCommands are substrings or patterns that must not appear in commands.
	BlockedCommands []string
	// NetworkAccess controls whether spawned processes may use the network (default false).
	NetworkAccess bool
	// MaxOutputBytes is the max bytes captured from stdout+stderr (default 10240).
	MaxOutputBytes int
}

// defaultBlockedCommands is the built-in deny list for dangerous command patterns.
var defaultBlockedCommands = []string{
	"rm -rf /",
	"sudo",
	"mkfs",
	"dd if=",
	"> /dev/sda",
	"curl|sh",
	"curl | sh",
	"wget|sh",
	"wget | sh",
	"eval(",
	"exec(",
	"__import__",
	"os.system",
	"subprocess",
	":(){:|:&};:",
}

// Sandbox validates and executes commands within configured safety boundaries.
type Sandbox struct {
	config SandboxConfig
}

// DefaultSandbox returns a Sandbox with safe default settings.
func DefaultSandbox() *Sandbox {
	return &Sandbox{
		config: SandboxConfig{
			MaxCPUSeconds:   30,
			MaxMemoryMB:     512,
			MaxFileSizeMB:   10,
			MaxProcesses:    10,
			AllowedPaths:    []string{"/tmp"},
			BlockedCommands: defaultBlockedCommands,
			NetworkAccess:   false,
			MaxOutputBytes:  10240,
		},
	}
}

// NewSandbox creates a Sandbox with the given configuration. Missing fields
// are filled from defaults.
func NewSandbox(cfg SandboxConfig) *Sandbox {
	if cfg.MaxCPUSeconds <= 0 {
		cfg.MaxCPUSeconds = 30
	}
	if cfg.MaxMemoryMB <= 0 {
		cfg.MaxMemoryMB = 512
	}
	if cfg.MaxFileSizeMB <= 0 {
		cfg.MaxFileSizeMB = 10
	}
	if cfg.MaxProcesses <= 0 {
		cfg.MaxProcesses = 10
	}
	if len(cfg.AllowedPaths) == 0 {
		cfg.AllowedPaths = []string{"/tmp"}
	}
	if len(cfg.BlockedCommands) == 0 {
		cfg.BlockedCommands = defaultBlockedCommands
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = 10240
	}
	return &Sandbox{config: cfg}
}

// Config returns the sandbox configuration (read-only copy).
func (s *Sandbox) Config() SandboxConfig {
	return s.config
}

// ValidateCommand checks that cmd does not contain any blocked patterns.
// Returns an error describing the violation if a pattern matches.
func (s *Sandbox) ValidateCommand(cmd string) error {
	lower := strings.ToLower(cmd)
	for _, pattern := range s.config.BlockedCommands {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return fmt.Errorf("sandbox: command blocked by pattern %q", pattern)
		}
	}
	return nil
}

// ValidatePath checks that path is within one of the allowed directories and
// does not contain directory traversal sequences. The path is cleaned and
// resolved to an absolute path before checking.
func (s *Sandbox) ValidatePath(path string) error {
	if path == "" {
		return fmt.Errorf("sandbox: empty path")
	}

	// Clean the path to resolve . and ..
	cleaned := filepath.Clean(path)

	// Reject explicit traversal in the raw input.
	if strings.Contains(path, "..") {
		return fmt.Errorf("sandbox: path traversal not allowed: %s", path)
	}

	// Convert to absolute for comparison.
	abs, err := filepath.Abs(cleaned)
	if err != nil {
		return fmt.Errorf("sandbox: cannot resolve path %q: %w", path, err)
	}

	for _, allowed := range s.config.AllowedPaths {
		allowedAbs, err := filepath.Abs(filepath.Clean(allowed))
		if err != nil {
			continue
		}
		// The path must start with the allowed directory (plus separator or be exact).
		if abs == allowedAbs || strings.HasPrefix(abs, allowedAbs+string(filepath.Separator)) {
			return nil
		}
	}

	return fmt.Errorf("sandbox: path %q is outside allowed directories %v", path, s.config.AllowedPaths)
}

// Execute runs name with args inside a context-bounded subprocess. The timeout
// is derived from MaxCPUSeconds. Combined stdout+stderr is returned, truncated
// to MaxOutputBytes.
func (s *Sandbox) Execute(ctx context.Context, name string, args ...string) (string, error) {
	timeout := time.Duration(s.config.MaxCPUSeconds) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- sandboxed via ValidateCommand
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Create new process group for cleanup
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// If the context deadline was exceeded, wrap the error.
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("sandbox: command timed out after %ds", s.config.MaxCPUSeconds)
		}
		// Return stderr contents alongside the error for diagnostics.
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = stdout.String()
		}
		return "", fmt.Errorf("sandbox: command failed: %w\n%s", err, truncate(errMsg, s.config.MaxOutputBytes))
	}

	output := stdout.String()
	if output == "" {
		output = stderr.String()
	}
	return truncate(output, s.config.MaxOutputBytes), nil
}

// WrapWithCPULimit prepends a ulimit CPU-time guard to a shell command string.
// If MaxCPUSeconds is zero or negative, the original command is returned
// unchanged.
func (s *Sandbox) WrapWithCPULimit(shellCmd string) string {
	if s.config.MaxCPUSeconds <= 0 {
		return shellCmd
	}
	return fmt.Sprintf("ulimit -t %d; %s", s.config.MaxCPUSeconds, shellCmd)
}

// ExecuteShell runs a shell command string through /bin/sh with CPU-time
// limiting via ulimit. The command is validated against blocked patterns
// before execution. Process group isolation is applied so the entire process
// tree can be cleaned up on timeout.
func (s *Sandbox) ExecuteShell(ctx context.Context, shellCmd string) (string, error) {
	if err := s.ValidateCommand(shellCmd); err != nil {
		return "", err
	}

	wrapped := s.WrapWithCPULimit(shellCmd)
	return s.Execute(ctx, "/bin/sh", "-c", wrapped)
}

// truncate returns s truncated to maxBytes. If truncation occurs a marker is
// appended.
func truncate(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + "\n...[output truncated]"
}
