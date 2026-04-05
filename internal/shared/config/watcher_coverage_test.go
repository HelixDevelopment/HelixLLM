package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// dirOf
// ---------------------------------------------------------------------------

func TestDirOf_UnixPath(t *testing.T) {
	got := dirOf("/home/user/config.json")
	if got != "/home/user" {
		t.Errorf("dirOf(/home/user/config.json) = %q, want %q", got, "/home/user")
	}
}

func TestDirOf_RootFile(t *testing.T) {
	got := dirOf("/config.json")
	if got != "/" {
		t.Errorf("dirOf(/config.json) = %q, want %q", got, "/")
	}
}

func TestDirOf_NoSlash(t *testing.T) {
	got := dirOf("config.json")
	if got != "." {
		t.Errorf("dirOf(config.json) = %q, want %q", got, ".")
	}
}

func TestDirOf_NestedPath(t *testing.T) {
	got := dirOf("/a/b/c/d.json")
	if got != "/a/b/c" {
		t.Errorf("dirOf(/a/b/c/d.json) = %q, want %q", got, "/a/b/c")
	}
}

// ---------------------------------------------------------------------------
// sameFile
// ---------------------------------------------------------------------------

func TestSameFile_SamePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if !sameFile(path, path) {
		t.Error("sameFile should return true for identical paths")
	}
}

func TestSameFile_DifferentFiles(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.json")
	pathB := filepath.Join(dir, "b.json")
	os.WriteFile(pathA, []byte("{}"), 0o600)
	os.WriteFile(pathB, []byte("{}"), 0o600)

	if sameFile(pathA, pathB) {
		t.Error("sameFile should return false for different files")
	}
}

func TestSameFile_NonexistentA(t *testing.T) {
	dir := t.TempDir()
	pathB := filepath.Join(dir, "b.json")
	os.WriteFile(pathB, []byte("{}"), 0o600)

	if sameFile(filepath.Join(dir, "nonexistent.json"), pathB) {
		t.Error("sameFile should return false when first path does not exist")
	}
}

func TestSameFile_NonexistentB(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.json")
	os.WriteFile(pathA, []byte("{}"), 0o600)

	if sameFile(pathA, filepath.Join(dir, "nonexistent.json")) {
		t.Error("sameFile should return false when second path does not exist")
	}
}

// ---------------------------------------------------------------------------
// loadFromFile
// ---------------------------------------------------------------------------

func TestLoadFromFile_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := &HelixConfig{Mode: "gateway"}
	data, _ := json.Marshal(cfg)
	os.WriteFile(path, data, 0o600)

	loaded, err := loadFromFile(path)
	if err != nil {
		t.Fatalf("loadFromFile: %v", err)
	}
	if loaded.Mode != "gateway" {
		t.Errorf("Mode = %q, want %q", loaded.Mode, "gateway")
	}
}

func TestLoadFromFile_NonexistentFile(t *testing.T) {
	_, err := loadFromFile("/nonexistent/path/config.json")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestLoadFromFile_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("not-valid-json{{{"), 0o600)

	_, err := loadFromFile(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}
