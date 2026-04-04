// Package observability provides a unified setup for distributed tracing and
// Prometheus metrics, wrapping digital.vasic.observability sub-packages.
package observability

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	obsmetrics "digital.vasic.observability/pkg/metrics"
	obstrace "digital.vasic.observability/pkg/trace"
)

// Options configures the Observability bundle.
type Options struct {
	// ServiceName is the name reported in traces and metrics.
	ServiceName string
	// ServiceVersion is the version of the service.
	ServiceVersion string
	// Environment is the deployment environment (e.g. "production", "test").
	Environment string
	// Exporter selects the trace exporter: "none", "stdout", "otlp".
	Exporter string
	// Endpoint is the collector endpoint (used with otlp/jaeger/zipkin).
	Endpoint string
	// Namespace is prepended to all Prometheus metric names.
	Namespace string
}

// Observability holds an initialised Tracer and a Prometheus metrics Collector.
type Observability struct {
	tracer    *obstrace.Tracer
	collector obsmetrics.Collector
	gatherer  prometheus.Gatherer
}

// New initialises tracing and metrics from the given options.
// Each call gets its own isolated Prometheus registry so multiple instances
// (e.g. in tests) do not conflict on metric registration.
func New(opts Options) (*Observability, error) {
	// --- tracer ---
	tracerCfg := &obstrace.TracerConfig{
		ServiceName:    opts.ServiceName,
		ServiceVersion: opts.ServiceVersion,
		Environment:    opts.Environment,
		ExporterType:   obstrace.ExporterType(opts.Exporter),
		Endpoint:       opts.Endpoint,
	}
	if tracerCfg.ExporterType == "" {
		tracerCfg.ExporterType = obstrace.ExporterNone
	}

	tracer, err := obstrace.InitTracer(tracerCfg)
	if err != nil {
		return nil, fmt.Errorf("init tracer: %w", err)
	}

	// --- metrics ---
	// Use a fresh registry per instance to avoid duplicate-registration errors
	// when multiple Observability values are created in the same process (tests).
	registry := prometheus.NewRegistry()
	metricsCfg := &obsmetrics.PrometheusConfig{
		Namespace: opts.Namespace,
		Registry:  registry,
	}
	collector := obsmetrics.NewPrometheusCollector(metricsCfg)

	return &Observability{
		tracer:    tracer,
		collector: collector,
		gatherer:  registry,
	}, nil
}

// Tracer returns the underlying distributed tracer.
func (o *Observability) Tracer() *obstrace.Tracer {
	return o.tracer
}

// Metrics returns the Prometheus metrics collector.
func (o *Observability) Metrics() obsmetrics.Collector {
	return o.collector
}

// Gatherer returns the prometheus.Gatherer for this instance's registry,
// suitable for use with promhttp.HandlerFor to expose a /metrics endpoint.
func (o *Observability) Gatherer() prometheus.Gatherer {
	return o.gatherer
}

// Shutdown gracefully flushes and shuts down the tracer provider.
func (o *Observability) Shutdown() {
	if o.tracer != nil {
		_ = o.tracer.Shutdown(context.Background())
	}
}
