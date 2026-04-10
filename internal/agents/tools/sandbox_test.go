package tools

import (
	"context"
	"testing"
)

func TestDefaultSandbox(t *testing.T) {
	s := DefaultSandbox()
	cfg := s.Config()

	if cfg.MaxCPUSeconds != 30 {
		t.Errorf("expected MaxCPUSeconds=30, got %d", cfg.MaxCPUSeconds)
	}
	if cfg.MaxMemoryMB != 512 {
		t.Errorf("expected MaxMemoryMB=512, got %d", cfg.MaxMemoryMB)
	}
	if cfg.MaxFileSizeMB != 10 {
		t.Errorf("expected MaxFileSizeMB=10, got %d", cfg.MaxFileSizeMB)
	}
	if cfg.MaxOutputBytes != 10240 {
		t.Errorf("expected MaxOutputBytes=10240, got %d", cfg.MaxOutputBytes)
	}
	if len(cfg.AllowedPaths) == 0 {
		t.Error("expected at least one allowed path")
	}
	if len(cfg.BlockedCommands) == 0 {
		t.Error("expected default blocked commands")
	}
}

func TestNewSandbox_Defaults(t *testing.T) {
	s := NewSandbox(SandboxConfig{})
	cfg := s.Config()

	if cfg.MaxCPUSeconds != 30 {
		t.Errorf("expected default MaxCPUSeconds=30, got %d", cfg.MaxCPUSeconds)
	}
	if cfg.MaxOutputBytes != 10240 {
		t.Errorf("expected default MaxOutputBytes=10240, got %d", cfg.MaxOutputBytes)
	}
}

func TestNewSandbox_CustomValues(t *testing.T) {
	s := NewSandbox(SandboxConfig{
		MaxCPUSeconds:   60,
		MaxMemoryMB:     1024,
		AllowedPaths:    []string{"/home"},
		BlockedCommands: []string{"dangerous"},
	})
	cfg := s.Config()

	if cfg.MaxCPUSeconds != 60 {
		t.Errorf("expected MaxCPUSeconds=60, got %d", cfg.MaxCPUSeconds)
	}
	if cfg.MaxMemoryMB != 1024 {
		t.Errorf("expected MaxMemoryMB=1024, got %d", cfg.MaxMemoryMB)
	}
	if len(cfg.AllowedPaths) != 1 || cfg.AllowedPaths[0] != "/home" {
		t.Errorf("expected AllowedPaths=[/home], got %v", cfg.AllowedPaths)
	}
}

func TestValidateCommand_Blocked(t *testing.T) {
	s := DefaultSandbox()

	// These strings are test inputs that must be caught by the sandbox deny list.
	blocked := []string{
		"rm -rf /",
		"sudo apt install something",
		"mkfs.ext4 /dev/sda1",
		"dd if=/dev/zero of=/dev/sda",
		":(){:|:&};:",
	}

	for _, cmd := range blocked {
		if err := s.ValidateCommand(cmd); err == nil {
			t.Errorf("expected command %q to be blocked", cmd)
		}
	}
}

func TestValidateCommand_Allowed(t *testing.T) {
	s := DefaultSandbox()

	allowed := []string{
		"ls -la",
		"echo hello",
		"cat /tmp/file.txt",
		"git status",
		"python3 script.py",
		"go test ./...",
		"wc -l *.go",
	}

	for _, cmd := range allowed {
		if err := s.ValidateCommand(cmd); err != nil {
			t.Errorf("expected command %q to be allowed, got: %v", cmd, err)
		}
	}
}

func TestValidateCommand_CaseInsensitive(t *testing.T) {
	s := DefaultSandbox()

	// "SUDO" in uppercase must still be blocked.
	if err := s.ValidateCommand("SUDO reboot"); err == nil {
		t.Error("expected case-insensitive blocking of SUDO")
	}
}

func TestValidatePath_Allowed(t *testing.T) {
	s := DefaultSandbox()

	if err := s.ValidatePath("/tmp/test.txt"); err != nil {
		t.Errorf("expected /tmp/test.txt to be allowed: %v", err)
	}
	if err := s.ValidatePath("/tmp/subdir/file.go"); err != nil {
		t.Errorf("expected /tmp/subdir/file.go to be allowed: %v", err)
	}
}

func TestValidatePath_Blocked(t *testing.T) {
	s := DefaultSandbox()

	blocked := []string{
		"/etc/passwd",
		"/root/.ssh/id_rsa",
		"/home/user/secret",
	}

	for _, p := range blocked {
		if err := s.ValidatePath(p); err == nil {
			t.Errorf("expected path %q to be blocked", p)
		}
	}
}

func TestValidatePath_TraversalBlocked(t *testing.T) {
	s := DefaultSandbox()

	traversals := []string{
		"/tmp/../etc/passwd",
		"/tmp/../../root",
		"/tmp/foo/../../../etc/shadow",
	}

	for _, p := range traversals {
		if err := s.ValidatePath(p); err == nil {
			t.Errorf("expected traversal path %q to be blocked", p)
		}
	}
}

func TestValidatePath_EmptyPath(t *testing.T) {
	s := DefaultSandbox()
	if err := s.ValidatePath(""); err == nil {
		t.Error("expected empty path to be rejected")
	}
}

func TestValidatePath_CustomAllowedPaths(t *testing.T) {
	s := NewSandbox(SandboxConfig{
		AllowedPaths: []string{"/tmp", "/home/testuser"},
	})

	if err := s.ValidatePath("/home/testuser/project/main.go"); err != nil {
		t.Errorf("expected /home/testuser path to be allowed: %v", err)
	}
	if err := s.ValidatePath("/home/otheruser/file"); err == nil {
		t.Error("expected /home/otheruser to be blocked")
	}
}

func TestExecute_SimpleCommand(t *testing.T) {
	s := DefaultSandbox()
	ctx := context.Background()

	out, err := s.Execute(ctx, "echo", "hello sandbox")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "hello sandbox\n"
	if out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}
}

func TestExecute_CommandNotFound(t *testing.T) {
	s := DefaultSandbox()
	ctx := context.Background()

	_, err := s.Execute(ctx, "nonexistent_command_xyz_123")
	if err == nil {
		t.Error("expected error for nonexistent command")
	}
}

func TestExecute_Timeout(t *testing.T) {
	s := NewSandbox(SandboxConfig{
		MaxCPUSeconds: 1,
	})
	ctx := context.Background()

	_, err := s.Execute(ctx, "sleep", "10")
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		max      int
		expected string
	}{
		{"short", "hello", 100, "hello"},
		{"exact", "hello", 5, "hello"},
		{"truncated", "hello world", 5, "hello\n...[output truncated]"},
		{"zero max", "hello", 0, "hello"},
		{"negative max", "hello", -1, "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.max)
			if got != tt.expected {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.expected)
			}
		})
	}
}
