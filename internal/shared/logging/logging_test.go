package logging_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/logging"
)

func TestNewLogger(t *testing.T) {
	log := logging.New("info", "text")
	if log == nil {
		t.Fatal("New() returned nil")
	}
}

func TestLoggerLevels(t *testing.T) {
	var buf bytes.Buffer
	log := logging.NewWithOutput("debug", "text", &buf)
	log.Info("test info")
	if !bytes.Contains(buf.Bytes(), []byte("test info")) {
		t.Error("Info message not found in output")
	}
	buf.Reset()
	log.Debug("test debug")
	if !bytes.Contains(buf.Bytes(), []byte("test debug")) {
		t.Error("Debug message not found in output")
	}
}

func TestLoggerWithField(t *testing.T) {
	var buf bytes.Buffer
	log := logging.NewWithOutput("info", "text", &buf)
	log.WithField("request_id", "abc123").Info("request received")
	if !bytes.Contains(buf.Bytes(), []byte("abc123")) {
		t.Error("Field value not found in output")
	}
}

func TestLoggerWarnLevel(t *testing.T) {
	var buf bytes.Buffer
	log := logging.NewWithOutput("warn", "text", &buf)
	log.Warn("test warning")
	if !bytes.Contains(buf.Bytes(), []byte("test warning")) {
		t.Error("Warn message not found in output")
	}
	buf.Reset()
	// Info should be suppressed at warn level.
	log.Info("should not appear")
	if bytes.Contains(buf.Bytes(), []byte("should not appear")) {
		t.Error("Info message should be suppressed at warn level")
	}
}

func TestLoggerErrorLevel(t *testing.T) {
	var buf bytes.Buffer
	log := logging.NewWithOutput("error", "text", &buf)
	log.Error("test error")
	if !bytes.Contains(buf.Bytes(), []byte("test error")) {
		t.Error("Error message not found in output")
	}
}

func TestLoggerWithError(t *testing.T) {
	var buf bytes.Buffer
	log := logging.NewWithOutput("info", "text", &buf)
	log.WithError(fmt.Errorf("connection refused")).Error("db failed")
	output := buf.String()
	if !strings.Contains(output, "connection refused") {
		t.Error("Error value not found in output")
	}
	if !strings.Contains(output, "db failed") {
		t.Error("Error message not found in output")
	}
}

func TestLoggerJSONFormat(t *testing.T) {
	var buf bytes.Buffer
	log := logging.NewWithOutput("info", "json", &buf)
	log.Info("json test")
	// JSON format output should contain braces.
	output := buf.String()
	if !strings.Contains(output, "{") {
		t.Error("JSON format should produce JSON output with braces")
	}
}

func TestNewLogger_DefaultLevel(t *testing.T) {
	var buf bytes.Buffer
	// Unknown level should default to info.
	log := logging.NewWithOutput("unknown_level", "text", &buf)
	log.Info("default level test")
	if !bytes.Contains(buf.Bytes(), []byte("default level test")) {
		t.Error("Info message should work at default level")
	}
}

func TestLoggerWarningAlias(t *testing.T) {
	var buf bytes.Buffer
	// "warning" should be treated as warn level.
	log := logging.NewWithOutput("warning", "text", &buf)
	log.Warn("warning alias test")
	if !bytes.Contains(buf.Bytes(), []byte("warning alias test")) {
		t.Error("Warn message not found for 'warning' level alias")
	}
}
