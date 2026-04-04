package logging_test

import (
	"bytes"
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
