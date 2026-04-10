package brain_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// TestDownloader_Download_Success
// ---------------------------------------------------------------------------

// TestDownloader_Download_Success verifies that Download writes the content
// returned by the HTTP server to the correct file on disk.
func TestDownloader_Download_Success(t *testing.T) {
	wantContent := []byte("fake gguf model bytes")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(wantContent)
	}))
	defer srv.Close()

	dir := t.TempDir()
	d := brain.NewDownloader(dir)

	req := brain.DownloadRequest{
		URL:      srv.URL + "/model.gguf",
		Filename: "model.gguf",
	}

	err := d.Download(context.Background(), req)
	require.NoError(t, err, "Download should succeed")

	got, err := os.ReadFile(filepath.Join(dir, "model.gguf"))
	require.NoError(t, err, "file should exist after download")
	assert.Equal(t, wantContent, got, "file content must match server response")
}

// TestDownloader_Download_NonOKStatus verifies that a non-200 response returns
// an error and does not leave a partial file behind.
func TestDownloader_Download_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	d := brain.NewDownloader(dir)

	req := brain.DownloadRequest{
		URL:      srv.URL + "/missing.gguf",
		Filename: "missing.gguf",
	}

	err := d.Download(context.Background(), req)
	assert.Error(t, err, "non-200 response should return an error")

	// Final file must NOT exist (atomic rename should not have happened).
	_, statErr := os.Stat(filepath.Join(dir, "missing.gguf"))
	assert.True(t, os.IsNotExist(statErr), "partial file must not exist after failed download")
}

// ---------------------------------------------------------------------------
// TestDownloader_EnsureModels_SkipExisting
// ---------------------------------------------------------------------------

// TestDownloader_EnsureModels_SkipExisting checks that ModelExists returns true
// for a file that is present on disk (size > 0) and false for a missing file.
func TestDownloader_EnsureModels_SkipExisting(t *testing.T) {
	dir := t.TempDir()
	d := brain.NewDownloader(dir)

	// Create a fake model file.
	fakeModel := filepath.Join(dir, "existing.gguf")
	require.NoError(t, os.WriteFile(fakeModel, []byte("not empty"), 0o644))

	assert.True(t, d.ModelExists("existing.gguf"), "existing non-empty file should be detected")
	assert.False(t, d.ModelExists("missing.gguf"), "absent file should not be detected")
}

// TestDownloader_ModelExists_EmptyFile verifies that a zero-byte file is NOT
// treated as an existing model (the download was likely interrupted).
func TestDownloader_ModelExists_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	d := brain.NewDownloader(dir)

	emptyFile := filepath.Join(dir, "empty.gguf")
	require.NoError(t, os.WriteFile(emptyFile, []byte{}, 0o644))

	assert.False(t, d.ModelExists("empty.gguf"), "zero-byte file must not count as existing model")
}

// ---------------------------------------------------------------------------
// TestManifest_SaveLoad
// ---------------------------------------------------------------------------

// TestManifest_SaveLoad saves a manifest with one entry and loads it back,
// verifying that all fields survive the round-trip.
func TestManifest_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.json")

	now := time.Now().UTC().Truncate(time.Second)

	original := &brain.ModelManifest{
		UpdatedAt: now,
		Models: map[string]brain.ManifestEntry{
			"qwen3-coder": {
				ModelID:      "qwen3-coder",
				Filename:     "qwen3-coder-30b-q4_k_m.gguf",
				SHA256:       "abc123",
				SizeBytes:    17_300_000_000,
				DownloadedAt: now,
				Verified:     true,
			},
		},
	}

	require.NoError(t, original.Save(manifestPath), "Save should not return an error")

	loaded, err := brain.LoadManifest(manifestPath)
	require.NoError(t, err, "LoadManifest should succeed")

	assert.Equal(t, original.UpdatedAt, loaded.UpdatedAt)
	require.Contains(t, loaded.Models, "qwen3-coder")

	entry := loaded.Models["qwen3-coder"]
	assert.Equal(t, "qwen3-coder", entry.ModelID)
	assert.Equal(t, "qwen3-coder-30b-q4_k_m.gguf", entry.Filename)
	assert.Equal(t, "abc123", entry.SHA256)
	assert.Equal(t, int64(17_300_000_000), entry.SizeBytes)
	assert.Equal(t, now, entry.DownloadedAt)
	assert.True(t, entry.Verified)
}

// TestLoadManifest_MissingFile verifies that a missing manifest file returns an
// empty (not nil) manifest and no error.
func TestLoadManifest_MissingFile(t *testing.T) {
	dir := t.TempDir()
	m, err := brain.LoadManifest(filepath.Join(dir, "nonexistent.json"))
	require.NoError(t, err, "missing manifest must not return an error")
	require.NotNil(t, m, "returned manifest must not be nil")
	assert.Empty(t, m.Models, "empty manifest must have no entries")
}

// ---------------------------------------------------------------------------
// TestDownloader_HuggingFaceURL
// ---------------------------------------------------------------------------

func TestDownloader_HuggingFaceURL(t *testing.T) {
	d := brain.NewDownloader(t.TempDir())
	got := d.HuggingFaceURL("Qwen/Qwen3-Coder-30B-A3B-GGUF", "qwen3-coder-30b-q4_k_m.gguf")
	want := "https://huggingface.co/Qwen/Qwen3-Coder-30B-A3B-GGUF/resolve/main/qwen3-coder-30b-q4_k_m.gguf"
	assert.Equal(t, want, got)
}
