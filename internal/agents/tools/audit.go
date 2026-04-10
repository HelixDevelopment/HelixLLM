package tools

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditEntry represents a single tool execution event for the audit log.
type AuditEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	ToolName   string    `json:"tool_name"`
	Arguments  string    `json:"arguments"`
	ResultHash string    `json:"result_hash"` // SHA-256 truncated to 16 hex chars
	Success    bool      `json:"success"`
	Duration   int64     `json:"duration_ms"`
}

// AuditLogger writes tool execution audit entries to daily JSONL files.
type AuditLogger struct {
	mu     sync.Mutex
	logDir string
}

// NewAuditLogger creates an AuditLogger that writes to the given directory.
// The directory is created if it does not exist.
func NewAuditLogger(logDir string) *AuditLogger {
	return &AuditLogger{logDir: logDir}
}

// Log appends an audit entry as a JSON line to the daily log file
// (audit_YYYY-MM-DD.jsonl). This method is safe for concurrent use.
func (a *AuditLogger) Log(entry AuditEntry) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := os.MkdirAll(a.logDir, 0o755); err != nil {
		return fmt.Errorf("audit: cannot create log directory: %w", err)
	}

	filename := fmt.Sprintf("audit_%s.jsonl", entry.Timestamp.Format("2006-01-02"))
	path := filepath.Join(a.logDir, filename)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("audit: cannot open log file: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("audit: cannot marshal entry: %w", err)
	}

	data = append(data, '\n')
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("audit: cannot write entry: %w", err)
	}

	return nil
}

// HashResult computes a SHA-256 hash of a result string and returns the
// first 16 hex characters.
func HashResult(result string) string {
	h := sha256.Sum256([]byte(result))
	return fmt.Sprintf("%x", h[:8])
}

// LogDir returns the configured log directory.
func (a *AuditLogger) LogDir() string {
	return a.logDir
}
