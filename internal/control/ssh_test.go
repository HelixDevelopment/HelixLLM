package control_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/control"
)

func TestNewSSHClient_InvalidKeyPath(t *testing.T) {
	_, err := control.NewSSHClient("localhost", 22, "user", "/nonexistent/key")
	if err == nil {
		t.Fatal("NewSSHClient with nonexistent key should return error")
	}
}

func TestNewSSHClient_InvalidKeyContent(t *testing.T) {
	tmpDir := t.TempDir()
	badKey := filepath.Join(tmpDir, "bad_key")
	if err := os.WriteFile(badKey, []byte("not-a-valid-ssh-key"), 0600); err != nil {
		t.Fatalf("write bad key: %v", err)
	}

	_, err := control.NewSSHClient("localhost", 22, "user", badKey)
	if err == nil {
		t.Fatal("NewSSHClient with invalid key content should return error")
	}
}
