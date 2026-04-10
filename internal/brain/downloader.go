package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"
)

// DownloadRequest describes a single model file to download.
type DownloadRequest struct {
	URL      string
	Filename string
	SHA256   string // optional; reserved for future checksum verification
}

// ManifestEntry records metadata about a downloaded model.
type ManifestEntry struct {
	ModelID      string    `json:"model_id"`
	Filename     string    `json:"filename"`
	SHA256       string    `json:"sha256,omitempty"`
	SizeBytes    int64     `json:"size_bytes"`
	DownloadedAt time.Time `json:"downloaded_at"`
	Verified     bool      `json:"verified"`
}

// ModelManifest is the top-level manifest that tracks all downloaded models.
type ModelManifest struct {
	Models    map[string]ManifestEntry `json:"models"`
	UpdatedAt time.Time               `json:"updated_at"`
}

// Save writes the manifest as indented JSON to the given path.
func (m *ModelManifest) Save(path string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("downloader: marshal manifest: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("downloader: write manifest %s: %w", path, err)
	}
	return nil
}

// LoadManifest reads a manifest from path. If the file does not exist it
// returns a fresh empty manifest rather than an error.
func LoadManifest(path string) (*ModelManifest, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &ModelManifest{Models: make(map[string]ManifestEntry)}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("downloader: read manifest %s: %w", path, err)
	}

	var m ModelManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("downloader: parse manifest %s: %w", path, err)
	}
	if m.Models == nil {
		m.Models = make(map[string]ManifestEntry)
	}
	return &m, nil
}

// Downloader handles downloading GGUF model files from HuggingFace (or any
// HTTP source) into a local models directory.
type Downloader struct {
	modelsDir string
	baseURL   string
	client    *http.Client
}

// NewDownloader creates a Downloader that stores files in modelsDir and uses
// "https://huggingface.co" as the default base URL.
func NewDownloader(modelsDir string) *Downloader {
	return &Downloader{
		modelsDir: modelsDir,
		baseURL:   "https://huggingface.co",
		client: &http.Client{
			Timeout: 30 * time.Minute, // large GGUF files can take a while
		},
	}
}

// ModelExists reports whether a model file is present in the models directory
// and has a non-zero size (a zero-byte file is treated as an incomplete
// download).
func (d *Downloader) ModelExists(filename string) bool {
	info, err := os.Stat(filepath.Join(d.modelsDir, filename))
	if err != nil {
		return false
	}
	return info.Size() > 0
}

// HuggingFaceURL returns the canonical download URL for a file in a HuggingFace
// repository: https://huggingface.co/{repo}/resolve/main/{filename}.
func (d *Downloader) HuggingFaceURL(repo, filename string) string {
	return fmt.Sprintf("%s/%s/resolve/main/%s", d.baseURL, repo, filename)
}

// Download fetches the file described by req and writes it atomically to
// d.modelsDir/req.Filename. A temp file is used during transfer; it is renamed
// into place only on success, so a failed download never leaves a partial file.
//
// If the HF_TOKEN environment variable is set its value is forwarded as a
// Bearer token to authenticate against gated repositories.
func (d *Downloader) Download(ctx context.Context, req DownloadRequest) error {
	log.WithFields(log.Fields{
		"url":      req.URL,
		"filename": req.Filename,
	}).Info("downloader: starting download")

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URL, nil)
	if err != nil {
		return fmt.Errorf("downloader: build request for %s: %w", req.URL, err)
	}

	if token := os.Getenv("HF_TOKEN"); token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := d.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("downloader: GET %s: %w", req.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloader: GET %s returned status %d", req.URL, resp.StatusCode)
	}

	// Ensure the models directory exists.
	if err := os.MkdirAll(d.modelsDir, 0o755); err != nil {
		return fmt.Errorf("downloader: create models dir %s: %w", d.modelsDir, err)
	}

	// Write to a temp file first, then atomically rename on success.
	dest := filepath.Join(d.modelsDir, req.Filename)
	tmp, err := os.CreateTemp(d.modelsDir, ".dl-tmp-*")
	if err != nil {
		return fmt.Errorf("downloader: create temp file: %w", err)
	}
	tmpName := tmp.Name()

	// Clean up the temp file if anything goes wrong.
	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("downloader: write %s: %w", req.Filename, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("downloader: close temp file: %w", err)
	}

	if err := os.Rename(tmpName, dest); err != nil {
		return fmt.Errorf("downloader: rename to %s: %w", dest, err)
	}

	success = true
	log.WithFields(log.Fields{
		"filename": req.Filename,
		"dest":     dest,
	}).Info("downloader: download complete")

	return nil
}
