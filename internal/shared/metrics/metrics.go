// Package metrics provides Prometheus instrumentation for HelixLLM.
//
// Call Register() once at startup to register all collectors with the
// default Prometheus registerer. Use the Track* helpers from hot paths
// (inference, tool execution, API handlers) to record observations.
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ── Model metrics ──────────────────────────────────────────────────────

// ModelInferenceTotal counts completed inferences per model.
var ModelInferenceTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "helixllm_model_inference_total",
		Help: helpText(keyHelpModelInferenceTotal),
	},
	[]string{"model_id"},
)

// ModelInferenceDuration tracks inference latency per model (seconds).
var ModelInferenceDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "helixllm_model_inference_duration_seconds",
		Help:    helpText(keyHelpModelInferenceDuration),
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	},
	[]string{"model_id"},
)

// ModelTokensGenerated counts tokens produced per model.
var ModelTokensGenerated = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "helixllm_model_tokens_generated_total",
		Help: helpText(keyHelpModelTokensGenerated),
	},
	[]string{"model_id"},
)

// ModelTokensPerSecond exposes the current token generation rate.
var ModelTokensPerSecond = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "helixllm_model_tokens_per_second",
		Help: helpText(keyHelpModelTokensPerSecond),
	},
	[]string{"model_id"},
)

// ── RAG metrics ────────────────────────────────────────────────────────

// RAGSearchDuration tracks RAG retrieval latency (seconds).
var RAGSearchDuration = prometheus.NewHistogram(
	prometheus.HistogramOpts{
		Name:    "helixllm_rag_search_duration_seconds",
		Help:    helpText(keyHelpRAGSearchDuration),
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5},
	},
)

// RAGDocumentsIndexed tracks the total number of indexed documents.
var RAGDocumentsIndexed = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "helixllm_rag_documents_indexed",
		Help: helpText(keyHelpRAGDocumentsIndexed),
	},
)

// ── Tool metrics ───────────────────────────────────────────────────────

// ToolExecutionTotal counts tool invocations by name and outcome.
var ToolExecutionTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "helixllm_tool_execution_total",
		Help: helpText(keyHelpToolExecutionTotal),
	},
	[]string{"tool_name", "status"},
)

// ToolExecutionDuration tracks tool execution latency (seconds).
var ToolExecutionDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "helixllm_tool_execution_duration_seconds",
		Help:    helpText(keyHelpToolExecutionDuration),
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
	},
	[]string{"tool_name"},
)

// ── API metrics ────────────────────────────────────────────────────────

// APIRequestsTotal counts HTTP requests by endpoint, method, and status.
var APIRequestsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "helixllm_api_requests_total",
		Help: helpText(keyHelpAPIRequestsTotal),
	},
	[]string{"endpoint", "method", "status"},
)

// APIRequestDuration tracks request latency per endpoint (seconds).
var APIRequestDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "helixllm_api_request_duration_seconds",
		Help:    helpText(keyHelpAPIRequestDuration),
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
	},
	[]string{"endpoint"},
)

// ActiveConnections tracks the number of in-flight requests.
var ActiveConnections = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "helixllm_active_connections",
		Help: helpText(keyHelpActiveConnections),
	},
)

// ── System metrics ─────────────────────────────────────────────────────

// VRAMUsageBytes reports current GPU VRAM consumption.
var VRAMUsageBytes = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "helixllm_vram_usage_bytes",
		Help: helpText(keyHelpVRAMUsageBytes),
	},
)

// RAMUsageBytes reports current system RAM consumption.
var RAMUsageBytes = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "helixllm_ram_usage_bytes",
		Help: helpText(keyHelpRAMUsageBytes),
	},
)

// allCollectors is the ordered list of Prometheus collectors to register.
var allCollectors = []prometheus.Collector{
	ModelInferenceTotal,
	ModelInferenceDuration,
	ModelTokensGenerated,
	ModelTokensPerSecond,
	RAGSearchDuration,
	RAGDocumentsIndexed,
	ToolExecutionTotal,
	ToolExecutionDuration,
	APIRequestsTotal,
	APIRequestDuration,
	ActiveConnections,
	VRAMUsageBytes,
	RAMUsageBytes,
}

// Register registers every HelixLLM metric with the default Prometheus
// registerer. It is safe to call once at startup; duplicate registration
// panics are intentional — they indicate a programmer error.
func Register() {
	for _, c := range allCollectors {
		prometheus.MustRegister(c)
	}
}

// ── Convenience helpers ────────────────────────────────────────────────

// TrackInference updates model inference counters and latency histogram.
// Call it after each completed inference with the wall-clock duration and
// the number of tokens produced.
func TrackInference(modelID string, duration time.Duration, tokens int) {
	ModelInferenceTotal.WithLabelValues(modelID).Inc()
	ModelInferenceDuration.WithLabelValues(modelID).Observe(duration.Seconds())
	ModelTokensGenerated.WithLabelValues(modelID).Add(float64(tokens))
	if duration > 0 {
		ModelTokensPerSecond.WithLabelValues(modelID).Set(
			float64(tokens) / duration.Seconds(),
		)
	}
}

// TrackToolExecution updates tool execution counters and latency.
func TrackToolExecution(toolName string, duration time.Duration, success bool) {
	status := "success"
	if !success {
		status = "error"
	}
	ToolExecutionTotal.WithLabelValues(toolName, status).Inc()
	ToolExecutionDuration.WithLabelValues(toolName).Observe(duration.Seconds())
}

// TrackAPIRequest updates API request counters and latency.
func TrackAPIRequest(endpoint, method string, status int, duration time.Duration) {
	statusStr := statusBucket(status)
	APIRequestsTotal.WithLabelValues(endpoint, method, statusStr).Inc()
	APIRequestDuration.WithLabelValues(endpoint).Observe(duration.Seconds())
}

// statusBucket converts an HTTP status code to a string label.
func statusBucket(code int) string {
	switch {
	case code >= 500:
		return "5xx"
	case code >= 400:
		return "4xx"
	case code >= 300:
		return "3xx"
	case code >= 200:
		return "2xx"
	default:
		return "1xx"
	}
}
