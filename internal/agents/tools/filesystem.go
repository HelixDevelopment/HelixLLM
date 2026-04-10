package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// ReadFileTool
// ---------------------------------------------------------------------------

// ReadFileTool reads the contents of a file, optionally with line offset/limit.
type ReadFileTool struct {
	sandbox *Sandbox
}

// NewReadFileTool creates a ReadFileTool that validates paths via sandbox.
func NewReadFileTool(sandbox *Sandbox) *ReadFileTool {
	return &ReadFileTool{sandbox: sandbox}
}

func (r *ReadFileTool) Name() string { return "read_file" }
func (r *ReadFileTool) Description() string {
	return "Read the contents of a file. Supports optional line offset and limit."
}

func (r *ReadFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Absolute path to the file to read",
			},
			"offset": map[string]interface{}{
				"type":        "integer",
				"description": "Line number to start reading from (0-based, default 0)",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of lines to return (default: all)",
			},
		},
		"required": []string{"path"},
	}
}

func (r *ReadFileTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	if args == nil {
		return "", fmt.Errorf("read_file: arguments must not be nil")
	}

	path, err := requireString(args, "path")
	if err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}

	if err := r.sandbox.ValidatePath(path); err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}

	lines := strings.Split(string(data), "\n")

	offset := optionalInt(args, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	if offset > len(lines) {
		offset = len(lines)
	}

	limit := optionalInt(args, "limit", 0)
	end := len(lines)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}

	result := strings.Join(lines[offset:end], "\n")
	return truncate(result, 10240), nil
}

// ---------------------------------------------------------------------------
// WriteFileTool
// ---------------------------------------------------------------------------

// WriteFileTool writes content to a file, creating parent directories as needed.
type WriteFileTool struct {
	sandbox *Sandbox
}

// NewWriteFileTool creates a WriteFileTool that validates paths via sandbox.
func NewWriteFileTool(sandbox *Sandbox) *WriteFileTool {
	return &WriteFileTool{sandbox: sandbox}
}

func (w *WriteFileTool) Name() string { return "write_file" }
func (w *WriteFileTool) Description() string {
	return "Write content to a file. Creates the file and parent directories if they do not exist."
}

func (w *WriteFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Absolute path to the file to write",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Content to write to the file",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (w *WriteFileTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	if args == nil {
		return "", fmt.Errorf("write_file: arguments must not be nil")
	}

	path, err := requireString(args, "path")
	if err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}

	content, err := requireString(args, "content")
	if err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}

	if err := w.sandbox.ValidatePath(path); err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}

	// Check file size limit.
	maxBytes := w.sandbox.config.MaxFileSizeMB * 1024 * 1024
	if len(content) > maxBytes {
		return "", fmt.Errorf("write_file: content size %d exceeds limit %d bytes",
			len(content), maxBytes)
	}

	// Ensure parent directory exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("write_file: cannot create directory %q: %w", dir, err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}

	return fmt.Sprintf("Wrote %d bytes to %s", len(content), path), nil
}

// ---------------------------------------------------------------------------
// ListDirectoryTool
// ---------------------------------------------------------------------------

// ListDirectoryTool lists the entries of a directory.
type ListDirectoryTool struct {
	sandbox *Sandbox
}

// NewListDirectoryTool creates a ListDirectoryTool that validates paths via sandbox.
func NewListDirectoryTool(sandbox *Sandbox) *ListDirectoryTool {
	return &ListDirectoryTool{sandbox: sandbox}
}

func (l *ListDirectoryTool) Name() string { return "list_directory" }
func (l *ListDirectoryTool) Description() string {
	return "List the contents of a directory. Optionally recurse into subdirectories."
}

func (l *ListDirectoryTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Absolute path to the directory to list",
			},
			"recursive": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether to recurse into subdirectories (default false)",
			},
		},
		"required": []string{"path"},
	}
}

func (l *ListDirectoryTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	if args == nil {
		return "", fmt.Errorf("list_directory: arguments must not be nil")
	}

	path, err := requireString(args, "path")
	if err != nil {
		return "", fmt.Errorf("list_directory: %w", err)
	}

	if err := l.sandbox.ValidatePath(path); err != nil {
		return "", fmt.Errorf("list_directory: %w", err)
	}

	recursive := optionalBool(args, "recursive", false)

	var entries []string
	if recursive {
		err = filepath.Walk(path, func(p string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return nil // skip errors
			}
			rel, _ := filepath.Rel(path, p)
			if rel == "." {
				return nil
			}
			suffix := ""
			if info.IsDir() {
				suffix = "/"
			}
			entries = append(entries, rel+suffix)
			if len(entries) >= 1000 {
				return fmt.Errorf("list_directory: too many entries (>1000)")
			}
			return nil
		})
	} else {
		dirEntries, readErr := os.ReadDir(path)
		if readErr != nil {
			return "", fmt.Errorf("list_directory: %w", readErr)
		}
		for _, e := range dirEntries {
			suffix := ""
			if e.IsDir() {
				suffix = "/"
			}
			entries = append(entries, e.Name()+suffix)
		}
	}

	if err != nil {
		return "", fmt.Errorf("list_directory: %w", err)
	}

	if len(entries) == 0 {
		return "(empty directory)", nil
	}
	return truncate(strings.Join(entries, "\n"), 10240), nil
}

// ---------------------------------------------------------------------------
// SearchFilesTool
// ---------------------------------------------------------------------------

// SearchFilesTool searches for files matching a glob pattern or content pattern.
type SearchFilesTool struct {
	sandbox *Sandbox
}

// NewSearchFilesTool creates a SearchFilesTool that validates paths via sandbox.
func NewSearchFilesTool(sandbox *Sandbox) *SearchFilesTool {
	return &SearchFilesTool{sandbox: sandbox}
}

func (s *SearchFilesTool) Name() string { return "search_files" }
func (s *SearchFilesTool) Description() string {
	return "Search for files by glob pattern or grep for content within files under a directory."
}

func (s *SearchFilesTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "Glob pattern (e.g. '*.go') or text to search for in file contents",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Directory to search in (default: current directory)",
			},
		},
		"required": []string{"pattern"},
	}
}

func (s *SearchFilesTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	if args == nil {
		return "", fmt.Errorf("search_files: arguments must not be nil")
	}

	pattern, err := requireString(args, "pattern")
	if err != nil {
		return "", fmt.Errorf("search_files: %w", err)
	}

	searchPath := optionalString(args, "path", ".")

	if searchPath != "." {
		if err := s.sandbox.ValidatePath(searchPath); err != nil {
			return "", fmt.Errorf("search_files: %w", err)
		}
	}

	// Try glob match first.
	globPattern := filepath.Join(searchPath, pattern)
	matches, _ := filepath.Glob(globPattern)

	if len(matches) > 0 {
		var results []string
		for _, m := range matches {
			if len(results) >= 100 {
				results = append(results, "...(truncated)")
				break
			}
			results = append(results, m)
		}
		return truncate(strings.Join(results, "\n"), 10240), nil
	}

	// Fall back to content search (grep-like).
	var results []string
	_ = filepath.Walk(searchPath, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return nil
		}
		if len(results) >= 100 {
			return filepath.SkipAll
		}
		// Only search text files (skip large or binary).
		if info.Size() > 1024*1024 { // skip files > 1MB
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			if strings.Contains(scanner.Text(), pattern) {
				results = append(results, fmt.Sprintf("%s:%d: %s", p, lineNum, scanner.Text()))
				if len(results) >= 100 {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})

	if len(results) == 0 {
		return "No matches found.", nil
	}
	return truncate(strings.Join(results, "\n"), 10240), nil
}

// ---------------------------------------------------------------------------
// FileInfoTool
// ---------------------------------------------------------------------------

// FileInfoTool returns metadata about a file (size, permissions, modification time).
type FileInfoTool struct {
	sandbox *Sandbox
}

// NewFileInfoTool creates a FileInfoTool that validates paths via sandbox.
func NewFileInfoTool(sandbox *Sandbox) *FileInfoTool {
	return &FileInfoTool{sandbox: sandbox}
}

func (f *FileInfoTool) Name() string { return "file_info" }
func (f *FileInfoTool) Description() string {
	return "Get metadata about a file: size, permissions, modification time, and type."
}

func (f *FileInfoTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Absolute path to the file",
			},
		},
		"required": []string{"path"},
	}
}

func (f *FileInfoTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	if args == nil {
		return "", fmt.Errorf("file_info: arguments must not be nil")
	}

	path, err := requireString(args, "path")
	if err != nil {
		return "", fmt.Errorf("file_info: %w", err)
	}

	if err := f.sandbox.ValidatePath(path); err != nil {
		return "", fmt.Errorf("file_info: %w", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("file_info: %w", err)
	}

	kind := "file"
	if info.IsDir() {
		kind = "directory"
	}

	return fmt.Sprintf("Name: %s\nType: %s\nSize: %d bytes\nPermissions: %s\nModified: %s",
		info.Name(),
		kind,
		info.Size(),
		info.Mode().String(),
		info.ModTime().Format(time.RFC3339),
	), nil
}

// ---------------------------------------------------------------------------
// Argument helpers
// ---------------------------------------------------------------------------

// requireString extracts a required string argument from args.
func requireString(args map[string]interface{}, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required parameter %q", key)
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("parameter %q must be a non-empty string", key)
	}
	return s, nil
}

// optionalString extracts an optional string argument, returning def if absent.
func optionalString(args map[string]interface{}, key, def string) string {
	v, ok := args[key]
	if !ok {
		return def
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return def
	}
	return s
}

// optionalInt extracts an optional integer argument. Handles both int and
// float64 (common from JSON unmarshaling). Returns def if absent or invalid.
func optionalInt(args map[string]interface{}, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return def
	}
}

// optionalBool extracts an optional boolean argument, returning def if absent.
func optionalBool(args map[string]interface{}, key string, def bool) bool {
	v, ok := args[key]
	if !ok {
		return def
	}
	b, ok := v.(bool)
	if !ok {
		return def
	}
	return b
}
