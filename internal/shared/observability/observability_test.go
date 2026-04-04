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

func TestNew_InvalidExporter_ReturnsError(t *testing.T) {
	// An unrecognised exporter type causes InitTracer to return an error,
	// which New must propagate rather than swallow.
	_, err := observability.New(observability.Options{
		ServiceName: "helixllm-test",
		Exporter:    "invalid-exporter-type-xyz",
	})
	if err == nil {
		t.Fatal("expected error for invalid exporter type, got nil")
	}
}

func TestNew_EmptyExporter_DefaultsToNone(t *testing.T) {
	// When Exporter is empty the New function defaults it to ExporterNone,
	// covering the `if tracerCfg.ExporterType == ""` branch.
	obs, err := observability.New(observability.Options{
		ServiceName: "helixllm-test",
		Exporter:    "", // intentionally empty
	})
	if err != nil {
		t.Fatalf("New() with empty exporter error = %v", err)
	}
	if obs == nil {
		t.Fatal("New() returned nil")
	}
	defer obs.Shutdown()
}
