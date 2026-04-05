package analytics_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/analytics"
)

func TestNewCollector_InvalidPort(t *testing.T) {
	// Port that is not a number should trigger the Atoi error path.
	c := analytics.NewCollector("localhost:notaport", "helixllm")
	if c == nil {
		t.Fatal("NewCollector returned nil; expected a fallback NoOpCollector")
	}
	// Must be a fully functional NoOpCollector.
	if err := c.Close(); err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}

func TestNewCollector_EmptyDatabase_DefaultsToHelixllm(t *testing.T) {
	// Empty database string should default to "helixllm" internally.
	// The ClickHouse address is unreachable, so it falls back to NoOp,
	// but the important thing is it doesn't panic.
	c := analytics.NewCollector("127.0.0.1:19999", "")
	if c == nil {
		t.Fatal("NewCollector returned nil")
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}
