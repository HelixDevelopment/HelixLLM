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

func TestNewCollector_WithAddr_ReturnsCollector(t *testing.T) {
	// Even with an address, the current implementation returns NoOp (ClickHouse
	// not yet wired).  We verify the returned value satisfies the interface and
	// does not panic.
	c := analytics.NewCollector("localhost:9000", "helixllm")
	if c == nil {
		t.Fatal("NewCollector returned nil")
	}
	ev := analytics.Event{Name: "probe", Timestamp: time.Now()}
	if err := c.Track(context.Background(), ev); err != nil {
		t.Errorf("Track() error = %v, want nil", err)
	}
}

func TestNewCollector_SatisfiesInterface(t *testing.T) {
	// Compile-time check via assignment.
	var _ analytics.Collector = analytics.NewCollector("", "")
	var _ analytics.Collector = &analytics.NoOpCollector{}
}
