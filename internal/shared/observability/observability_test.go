package observability_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/observability"
)

func TestNewObservability(t *testing.T) {
	obs, err := observability.New(observability.Options{
		ServiceName: "helixllm-test",
		Environment: "test",
		Exporter:    "none",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if obs == nil {
		t.Fatal("New() returned nil")
	}
	defer obs.Shutdown()
}

func TestMetricsCollector(t *testing.T) {
	obs, _ := observability.New(observability.Options{
		ServiceName: "helixllm-test",
		Exporter:    "none",
	})
	defer obs.Shutdown()
	m := obs.Metrics()
	if m == nil {
		t.Fatal("Metrics() returned nil")
	}
	m.IncrementCounter("test_counter", map[string]string{"method": "GET"})
}

func TestTracer(t *testing.T) {
	obs, _ := observability.New(observability.Options{
		ServiceName: "helixllm-test",
		Exporter:    "none",
	})
	defer obs.Shutdown()
	tr := obs.Tracer()
	if tr == nil {
		t.Fatal("Tracer() returned nil")
	}
}
