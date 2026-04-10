package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuditLogger_Log(t *testing.T) {
	dir := t.TempDir()
	logger := NewAuditLogger(dir)

	now := time.Now()
	entry := AuditEntry{
		Timestamp:  now,
		ToolName:   "echo",
		Arguments:  `{"message":"hello"}`,
		ResultHash: HashResult("hello"),
		Success:    true,
		Duration:   42,
	}

	if err := logger.Log(entry); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the file was created with correct name.
	filename := "audit_" + now.Format("2006-01-02") + ".jsonl"
	path := filepath.Join(dir, filename)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	// Parse the JSON line.
	var parsed AuditEntry
	if err := json.Unmarshal(data[:len(data)-1], &parsed); err != nil { // strip trailing \n
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed.ToolName != "echo" {
		t.Errorf("expected tool_name=echo, got %s", parsed.ToolName)
	}
	if parsed.Duration != 42 {
		t.Errorf("expected duration=42, got %d", parsed.Duration)
	}
	if !parsed.Success {
		t.Error("expected success=true")
	}
}

func TestAuditLogger_MultipleEntries(t *testing.T) {
	dir := t.TempDir()
	logger := NewAuditLogger(dir)

	now := time.Now()
	for i := 0; i < 5; i++ {
		entry := AuditEntry{
			Timestamp:  now,
			ToolName:   "test_tool",
			Arguments:  "{}",
			ResultHash: HashResult("result"),
			Success:    true,
			Duration:   int64(i),
		}
		if err := logger.Log(entry); err != nil {
			t.Fatalf("unexpected error on entry %d: %v", i, err)
		}
	}

	filename := "audit_" + now.Format("2006-01-02") + ".jsonl"
	data, err := os.ReadFile(filepath.Join(dir, filename))
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d", len(lines))
	}
}

func TestAuditLogger_FileRotation(t *testing.T) {
	dir := t.TempDir()
	logger := NewAuditLogger(dir)

	// Write entries with different dates.
	day1 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2025, 1, 16, 10, 0, 0, 0, time.UTC)

	logger.Log(AuditEntry{
		Timestamp: day1, ToolName: "tool_a",
		ResultHash: HashResult("a"), Success: true,
	})
	logger.Log(AuditEntry{
		Timestamp: day2, ToolName: "tool_b",
		ResultHash: HashResult("b"), Success: true,
	})

	// Verify two separate files were created.
	file1 := filepath.Join(dir, "audit_2025-01-15.jsonl")
	file2 := filepath.Join(dir, "audit_2025-01-16.jsonl")

	if _, err := os.Stat(file1); err != nil {
		t.Errorf("expected file for day 1: %v", err)
	}
	if _, err := os.Stat(file2); err != nil {
		t.Errorf("expected file for day 2: %v", err)
	}

	data1, _ := os.ReadFile(file1)
	if !strings.Contains(string(data1), "tool_a") {
		t.Error("expected tool_a in day 1 file")
	}

	data2, _ := os.ReadFile(file2)
	if !strings.Contains(string(data2), "tool_b") {
		t.Error("expected tool_b in day 2 file")
	}
}

func TestHashResult(t *testing.T) {
	h1 := HashResult("hello")
	h2 := HashResult("hello")
	h3 := HashResult("world")

	// Same input produces same hash.
	if h1 != h2 {
		t.Errorf("expected same hash for same input, got %s vs %s", h1, h2)
	}

	// Different input produces different hash.
	if h1 == h3 {
		t.Error("expected different hash for different input")
	}

	// Hash should be 16 hex chars.
	if len(h1) != 16 {
		t.Errorf("expected 16 char hash, got %d chars: %s", len(h1), h1)
	}
}

func TestAuditLogger_LogDir(t *testing.T) {
	logger := NewAuditLogger("/tmp/audit-test")
	if logger.LogDir() != "/tmp/audit-test" {
		t.Errorf("expected /tmp/audit-test, got %s", logger.LogDir())
	}
}

func TestAuditLogger_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "audit")
	logger := NewAuditLogger(dir)

	entry := AuditEntry{
		Timestamp:  time.Now(),
		ToolName:   "test",
		ResultHash: HashResult("x"),
		Success:    true,
	}

	if err := logger.Log(entry); err != nil {
		t.Fatalf("expected dir to be auto-created: %v", err)
	}

	if _, err := os.Stat(dir); err != nil {
		t.Errorf("expected directory to exist: %v", err)
	}
}
