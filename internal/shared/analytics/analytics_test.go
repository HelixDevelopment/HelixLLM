package analytics_test

import (
	"context"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/analytics"
)

func TestNoOpCollector_Track(t *testing.T) {
	c := &analytics.NoOpCollector{}
	ev := analytics.Event{
		Name:      "test.event",
		Timestamp: time.Now(),
		Properties: map[string]interface{}{
			"model": "llama-3.1-70b",
		},
		Tags: map[string]string{
			"env": "test",
		},
	}
	if err := c.Track(context.Background(), ev); err != nil {
		t.Errorf("Track() error = %v, want nil", err)
	}
}

func TestNoOpCollector_Close(t *testing.T) {
	c := &analytics.NoOpCollector{}
	if err := c.Close(); err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}

func TestNewCollector_EmptyAddr_ReturnsNoOp(t *testing.T) {
	c := analytics.NewCollector("", "helixllm")
	if c == nil {
		t.Fatal("NewCollector returned nil")
	}
	// Should not error for any event.
	ev := analytics.Event{Name: "probe", Timestamp: time.Now()}
	if err := c.Track(context.Background(), ev); err != nil {
		t.Errorf("Track() on no-op collector error = %v, want nil", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close() on no-op collector error = %v, want nil", err)
	}
}

// TestNewCollector_UnreachableClickHouse verifies that when a ClickHouse
// address is provided but the server is not reachable, NewCollector gracefully
// falls back to a NoOpCollector rather than panicking or returning nil.
func TestNewCollector_UnreachableClickHouse(t *testing.T) {
	// Port 19999 is used here as an address that is very unlikely to have a
	// ClickHouse instance listening during CI/local test runs.
	c := analytics.NewCollector("127.0.0.1:19999", "helixllm")
	if c == nil {
		t.Fatal("NewCollector returned nil; expected a fallback NoOpCollector")
	}
	// The fallback collector must be fully functional.
	ev := analytics.Event{Name: "probe", Timestamp: time.Now()}
	if err := c.Track(context.Background(), ev); err != nil {
		t.Errorf("Track() on fallback collector error = %v, want nil", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close() on fallback collector error = %v, want nil", err)
	}
}

// TestNewCollector_InvalidAddr verifies that a malformed address falls back
// gracefully without panicking.
func TestNewCollector_InvalidAddr(t *testing.T) {
	c := analytics.NewCollector("not-a-valid-addr", "helixllm")
	if c == nil {
		t.Fatal("NewCollector returned nil; expected a fallback NoOpCollector")
	}
	ev := analytics.Event{Name: "probe", Timestamp: time.Now()}
	if err := c.Track(context.Background(), ev); err != nil {
		t.Errorf("Track() on fallback collector error = %v, want nil", err)
	}
}

func TestNewCollector_SatisfiesInterface(t *testing.T) {
	// Compile-time check via assignment.
	var _ analytics.Collector = analytics.NewCollector("", "")
	var _ analytics.Collector = &analytics.NoOpCollector{}
}
