---
title: "Monitoring"
weight: 1
bookToC: true
---


HelixLLM provides health checks, Prometheus metrics, OpenTelemetry tracing, and structured logging for comprehensive observability.

## Health Checks

The health endpoint aggregates status from all subsystems:

```bash
curl -k https://localhost:8443/internal/health
```

**Healthy response** (HTTP 200):

```json
{
  "status": "healthy",
  "checks": []
}
```

**Unhealthy response** (HTTP 503):

```json
{
  "status": "unhealthy",
  "checks": [
    {
      "name": "database",
      "status": "unhealthy",
      "error": "connection refused"
    }
  ]
}
```

Use this endpoint for container health checks and load balancer probes.

## Prometheus Metrics

HelixLLM exposes Prometheus-compatible metrics. Configure the Prometheus scrape target to point at your HelixLLM instance.

### Configuration

```bash
HELIX_PROMETHEUS_PORT=9090   # Prometheus server port
```

### Key Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `http_requests_total` | Counter | Total HTTP requests by method, path, status |
| `http_request_duration_seconds` | Histogram | Request latency distribution |
| `llm_requests_total` | Counter | LLM provider requests by provider, model, status |
| `llm_request_duration_seconds` | Histogram | LLM inference latency |
| `rag_queries_total` | Counter | RAG pipeline queries |
| `rag_query_duration_seconds` | Histogram | RAG query latency |
| `agent_turns_total` | Counter | Agent loop iterations |
| `cluster_hosts_healthy` | Gauge | Number of healthy hosts in cluster |

## OpenTelemetry Tracing

Distributed tracing with correlation IDs across all subsystems.

### Configuration

```bash
HELIX_OTEL_EXPORTER=otlp                    # none | stdout | otlp | jaeger | zipkin
HELIX_OTEL_ENDPOINT=http://localhost:4317    # Collector endpoint (gRPC)
```

### Exporter Options

| Exporter | Description |
|----------|-------------|
| `none` | Tracing disabled (default) |
| `stdout` | Print traces to stdout (debugging) |
| `otlp` | Send to an OpenTelemetry Collector via gRPC |
| `jaeger` | Send directly to Jaeger |
| `zipkin` | Send directly to Zipkin |

### Trace Propagation

Every request gets a unique request ID (via the `X-Request-Id` header). This ID is propagated through all internal calls and included in logs, enabling end-to-end request tracing.

The server middleware automatically:
- Generates a request ID if none is provided
- Passes the ID through the Gin context
- Includes it in response headers

## Structured Logging

### Configuration

```bash
HELIX_LOG_LEVEL=info    # debug | info | warn | error
HELIX_LOG_FORMAT=text   # text | json
```

### Log Levels

| Level | Use |
|-------|-----|
| `debug` | Detailed internal state, request/response bodies |
| `info` | Server start/stop, request summaries, key events |
| `warn` | Recoverable errors, deprecation notices |
| `error` | Failures requiring attention |

### JSON Format

For production, use `HELIX_LOG_FORMAT=json` for machine-parseable logs:

```json
{"level":"info","msg":"starting HelixLLM","mode":"full","time":"2026-04-04T14:30:00Z"}
{"level":"info","msg":"server listening","addr":"0.0.0.0:8443","time":"2026-04-04T14:30:00Z"}
```

### Log Aggregation

In a multi-host setup, logs from all hosts can be aggregated using Loki (deployed by the control plane). Grafana provides a unified log viewing interface.

## Grafana Dashboards

```bash
HELIX_GRAFANA_PORT=3001   # Grafana dashboard port
```

When deployed via the control plane, Grafana is available at `http://<host>:3001` with pre-configured dashboards for:

- Request throughput and latency
- LLM provider performance
- RAG pipeline metrics
- Cluster health overview
- Container resource usage

## Monitoring Stack

The full observability stack consists of:

| Component | Role | Image |
|-----------|------|-------|
| Prometheus | Metrics collection | `prom/prometheus` |
| Grafana | Dashboards and visualization | `grafana/grafana` |
| Loki | Log aggregation | `grafana/loki` |
| OpenTelemetry Collector | Trace collection | (optional) |

These are deployed as containers by the control plane using the `binpack` scheduling strategy.

## Cluster Monitoring

For multi-host deployments, monitor the cluster status:

```bash
# Check cluster health
curl -k https://localhost:8443/internal/cluster/status

# Probe all hosts
curl -k -X POST https://localhost:8443/internal/cluster/probe
```

The control plane provides continuous monitoring with auto-remediation:

- **Container died:** Automatically restart on the same host
- **Host unreachable:** Reschedule services to surviving hosts
- **Performance degraded:** Trigger rebalancing

## Alt-Svc Header

Every response includes an `Alt-Svc` header advertising HTTP/3 support:

```
Alt-Svc: h3=":8443"; ma=86400
```

Clients that support HTTP/3 will upgrade automatically on subsequent requests, reducing latency.

## Request Compression

The server automatically compresses responses based on the `Accept-Encoding` header:

- **Brotli** (`br`): Primary compression, best ratio
- **gzip**: Fallback compression

Monitor compression effectiveness by comparing response sizes in your HTTP logs.
