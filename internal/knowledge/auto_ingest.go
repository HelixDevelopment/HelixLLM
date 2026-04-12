package knowledge

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

// maxAutoIngestFiles is the upper bound on how many files AutoIngest will
// process in a single directory walk.
const maxAutoIngestFiles = 5000

// maxAutoIngestFileSize is the maximum file size (in bytes) that AutoIngest
// will read. Files larger than this are silently skipped.
const maxAutoIngestFileSize = 100 * 1024 // 100 KB

// autoIngestExtensions is the set of file extensions eligible for ingestion.
var autoIngestExtensions = map[string]string{
	".go":   "go",
	".py":   "python",
	".js":   "javascript",
	".ts":   "typescript",
	".java": "java",
	".rs":   "rust",
	".md":   "markdown",
	".yaml": "yaml",
	".yml":  "yaml",
	".json": "json",
	".sh":   "shell",
	".sql":  "sql",
	".toml": "toml",
}

// autoIngestSkipDirs is the set of directory base names that should be
// skipped during the recursive walk.
var autoIngestSkipDirs = map[string]bool{
	"vendor":      true,
	"node_modules": true,
	".git":        true,
	"bin":         true,
	"releases":    true,
	"__pycache__": true,
}

// AutoIngest walks dir recursively and ingests eligible source files into the
// given pipeline collection. It skips vendor/, node_modules/, .git/, bin/,
// releases/, __pycache__/, and hidden directories. Files larger than 100 KB
// are skipped. At most 5000 files are processed.
//
// Progress is logged every 50 files. Ingestion errors for individual files
// are logged as warnings but do not abort the walk.
func AutoIngest(pipeline *Pipeline, dir string, collection string) error {
	if pipeline == nil {
		return fmt.Errorf("auto_ingest: pipeline must not be nil")
	}
	if dir == "" {
		return fmt.Errorf("auto_ingest: dir must not be empty")
	}
	if collection == "" {
		return fmt.Errorf("auto_ingest: collection must not be empty")
	}

	log := logrus.WithField("component", "auto_ingest")
	ctx := context.Background()

	var ingested, skipped int

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.WithError(err).WithField("path", path).Warn("walk error, skipping")
			return nil
		}

		if ingested >= maxAutoIngestFiles {
			return fs.SkipAll
		}

		base := d.Name()

		// Skip hidden directories and known skip dirs.
		if d.IsDir() {
			if autoIngestSkipDirs[base] {
				return filepath.SkipDir
			}
			if strings.HasPrefix(base, ".") && base != "." {
				return filepath.SkipDir
			}
			return nil
		}

		// Check extension.
		ext := strings.ToLower(filepath.Ext(base))
		language, ok := autoIngestExtensions[ext]
		if !ok {
			return nil
		}

		// Check file size.
		info, infoErr := d.Info()
		if infoErr != nil {
			log.WithError(infoErr).WithField("path", path).Warn("cannot stat file, skipping")
			return nil
		}
		if info.Size() > maxAutoIngestFileSize {
			skipped++
			return nil
		}

		// Read content.
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			log.WithError(readErr).WithField("path", path).Warn("cannot read file, skipping")
			return nil
		}

		if strings.TrimSpace(string(content)) == "" {
			return nil
		}

		// Compute a relative path for metadata.
		relPath, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			relPath = path
		}

		_, ingestErr := pipeline.Ingest(ctx, IngestRequest{
			Title:      relPath,
			Content:    string(content),
			Source:     relPath,
			Collection: collection,
		})
		if ingestErr != nil {
			log.WithError(ingestErr).WithField("path", relPath).Warn("ingest failed, skipping")
			return nil
		}

		ingested++

		if ingested%50 == 0 {
			log.WithFields(logrus.Fields{
				"ingested": ingested,
				"language": language,
				"path":     relPath,
			}).Info("auto-ingest progress")
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("auto_ingest: walk error: %w", err)
	}

	log.WithFields(logrus.Fields{
		"total_ingested": ingested,
		"total_skipped":  skipped,
		"dir":            dir,
		"collection":     collection,
	}).Info("auto-ingest complete")

	return nil
}
