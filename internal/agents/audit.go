package agents

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// AuditEntry represents a single auditable event in the agent lifecycle.
type AuditEntry struct {
	Timestamp time.Time     `json:"timestamp"`
	EventType string        `json:"event_type"` // "tool_call", "llm_completion", "tool_result"
	SessionID string        `json:"session_id,omitempty"`
	ToolName  string        `json:"tool_name,omitempty"`
	Arguments string        `json:"arguments,omitempty"`
	Result    string        `json:"result,omitempty"`
	Duration  time.Duration `json:"duration_ns"`
	Error     string        `json:"error,omitempty"`
}

// AuditLogger provides structured audit logging for agent operations. Each
// entry is emitted as a JSON-structured logrus line with the "audit" field
// set to true, making it easy to filter in log aggregation systems.
type AuditLogger struct {
	logger *logrus.Logger
	mu     sync.Mutex
}

// NewAuditLogger creates an AuditLogger backed by a logrus.Logger configured
// for JSON output.
func NewAuditLogger() *AuditLogger {
	l := logrus.New()
	l.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
	})
	l.SetLevel(logrus.InfoLevel)
	return &AuditLogger{logger: l}
}

// LogToolCall records that a tool is about to be invoked.
func (a *AuditLogger) LogToolCall(entry AuditEntry) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.logger.WithFields(logrus.Fields{
		"audit":      true,
		"event_type": entry.EventType,
		"session_id": entry.SessionID,
		"tool_name":  entry.ToolName,
		"arguments":  entry.Arguments,
		"result":     entry.Result,
		"duration":   entry.Duration.String(),
		"error":      entry.Error,
	}).Info("audit: tool_call")
}

// LogCompletion records an LLM completion event.
func (a *AuditLogger) LogCompletion(entry AuditEntry) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.logger.WithFields(logrus.Fields{
		"audit":      true,
		"event_type": entry.EventType,
		"session_id": entry.SessionID,
		"duration":   entry.Duration.String(),
		"error":      entry.Error,
	}).Info("audit: llm_completion")
}

// Logger returns the underlying logrus.Logger for testing or reconfiguration.
func (a *AuditLogger) Logger() *logrus.Logger {
	return a.logger
}
