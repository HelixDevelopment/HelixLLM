package control

import (
	"os"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// expandTilde
// ---------------------------------------------------------------------------

func TestExpandTilde_WithTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}

	result := expandTilde("~/.ssh/id_ed25519")
	if !strings.HasPrefix(result, home) {
		t.Errorf("expected path starting with %q, got %q", home, result)
	}
	if !strings.HasSuffix(result, ".ssh/id_ed25519") {
		t.Errorf("expected path ending with .ssh/id_ed25519, got %q", result)
	}
}

func TestExpandTilde_WithoutTilde(t *testing.T) {
	result := expandTilde("/absolute/path/key")
	if result != "/absolute/path/key" {
		t.Errorf("expected unchanged path, got %q", result)
	}
}

func TestExpandTilde_TildeOnly(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}

	result := expandTilde("~")
	if result != home {
		t.Errorf("expected %q, got %q", home, result)
	}
}

func TestExpandTilde_RelativePath(t *testing.T) {
	result := expandTilde("relative/path")
	if result != "relative/path" {
		t.Errorf("expected unchanged relative path, got %q", result)
	}
}

func TestExpandTilde_EmptyString(t *testing.T) {
	result := expandTilde("")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}
