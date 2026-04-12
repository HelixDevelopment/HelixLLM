package agents

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNewAuditLogger(t *testing.T) {
	al := NewAuditLogger()
	if al == nil {
		t.Fatal("expected non-nil AuditLogger")
	}
	if al.logger == nil {
		t.Fatal("expected non-nil underlying logger")
	}
}

func TestAuditLogger_LogToolCall(t *testing.T) {
	al := NewAuditLogger()

	// Capture output.
	var buf bytes.Buffer
	al.logger.SetOutput(&buf)

	entry := AuditEntry{
		Timestamp: time.Now(),
		EventType: "tool_call",
		SessionID: "sess-123",
		ToolName:  "echo",
		Arguments: `{"message":"hello"}`,
	}

	al.LogToolCall(entry)

	output := buf.String()
	if output == "" {
		t.Fatal("expected log output, got empty string")
	}

	// Parse JSON line.
	var fields map[string]interface{}
	if err := json.Unmarshal([]byte(output), &fields); err != nil {
		t.Fatalf("failed to parse JSON log line: %v\noutput: %s", err, output)
	}

	if fields["audit"] != true {
		t.Errorf("expected audit=true, got %v", fields["audit"])
	}
	if fields["event_type"] != "tool_call" {
		t.Errorf("expected event_type=tool_call, got %v", fields["event_type"])
	}
	if fields["session_id"] != "sess-123" {
		t.Errorf("expected session_id=sess-123, got %v", fields["session_id"])
	}
	if fields["tool_name"] != "echo" {
		t.Errorf("expected tool_name=echo, got %v", fields["tool_name"])
	}
	if fields["arguments"] != `{"message":"hello"}` {
		t.Errorf("expected arguments, got %v", fields["arguments"])
	}
}

func TestAuditLogger_LogCompletion(t *testing.T) {
	al := NewAuditLogger()

	var buf bytes.Buffer
	al.logger.SetOutput(&buf)

	entry := AuditEntry{
		Timestamp: time.Now(),
		EventType: "llm_completion",
		SessionID: "sess-456",
		Duration:  150 * time.Millisecond,
	}

	al.LogCompletion(entry)

	output := buf.String()
	if output == "" {
		t.Fatal("expected log output, got empty string")
	}

	var fields map[string]interface{}
	if err := json.Unmarshal([]byte(output), &fields); err != nil {
		t.Fatalf("failed to parse JSON log line: %v", err)
	}

	if fields["audit"] != true {
		t.Errorf("expected audit=true, got %v", fields["audit"])
	}
	if fields["event_type"] != "llm_completion" {
		t.Errorf("expected event_type=llm_completion, got %v", fields["event_type"])
	}
	if fields["session_id"] != "sess-456" {
		t.Errorf("expected session_id=sess-456, got %v", fields["session_id"])
	}
}

func TestAuditLogger_LogToolCall_WithError(t *testing.T) {
	al := NewAuditLogger()

	var buf bytes.Buffer
	al.logger.SetOutput(&buf)

	entry := AuditEntry{
		Timestamp: time.Now(),
		EventType: "tool_result",
		ToolName:  "exec_shell",
		Duration:  2 * time.Second,
		Error:     "command timed out",
	}

	al.LogToolCall(entry)

	output := buf.String()

	var fields map[string]interface{}
	if err := json.Unmarshal([]byte(output), &fields); err != nil {
		t.Fatalf("failed to parse JSON log line: %v", err)
	}

	if fields["error"] != "command timed out" {
		t.Errorf("expected error='command timed out', got %v", fields["error"])
	}
	if fields["tool_name"] != "exec_shell" {
		t.Errorf("expected tool_name=exec_shell, got %v", fields["tool_name"])
	}
}

func TestAuditLogger_MultipleEntries(t *testing.T) {
	al := NewAuditLogger()

	var buf bytes.Buffer
	al.logger.SetOutput(&buf)

	for i := 0; i < 5; i++ {
		al.LogToolCall(AuditEntry{
			Timestamp: time.Now(),
			EventType: "tool_call",
			ToolName:  "echo",
		})
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 5 {
		t.Errorf("expected 5 log lines, got %d", len(lines))
	}

	// Each line should be valid JSON.
	for i, line := range lines {
		var fields map[string]interface{}
		if err := json.Unmarshal([]byte(line), &fields); err != nil {
			t.Errorf("line %d: invalid JSON: %v", i, err)
		}
	}
}

func TestAuditLogger_Logger(t *testing.T) {
	al := NewAuditLogger()
	if al.Logger() == nil {
		t.Error("expected Logger() to return non-nil")
	}
}
