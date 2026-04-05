package observability_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/observability"
)

func TestObservability_Gatherer(t *testing.T) {
	obs, err := observability.New(observability.Options{
		ServiceName: "helixllm-test",
		Exporter:    "none",
		Namespace:   "test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer obs.Shutdown()

	g := obs.Gatherer()
	if g == nil {
		t.Fatal("Gatherer() returned nil")
	}

	// Gatherer must be usable: Gather should return without error.
	metrics, err := g.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	// metrics may be empty (no counters incremented yet), but must not be nil-causing.
	_ = metrics
}

func TestObservability_Shutdown_NilSafe(t *testing.T) {
	obs, err := observability.New(observability.Options{
		ServiceName: "test",
		Exporter:    "none",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Multiple shutdowns must not panic.
	obs.Shutdown()
	obs.Shutdown()
}
