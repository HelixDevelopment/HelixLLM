# Lesson 3: Monitoring

**Duration:** 30 minutes
**Prerequisites:** Lesson 1 (Containerization)
**Learning Objectives:**
- Set up Prometheus to scrape HelixLLM metrics
- Build Grafana dashboards for request throughput, latency, and LLM performance
- Configure structured logging with Loki for log aggregation
- Enable OpenTelemetry distributed tracing across all layers

---

## Scene 1: Observability Stack Overview (4 min)

**Narration:** "HelixLLM provides four pillars of observability: health checks for uptime monitoring, Prometheus metrics for quantitative measurements, structured logging for event records, and OpenTelemetry tracing for request flow visualization. Together, they give you complete visibility into system behavior."

**Screen:** Show the observability stack diagram.

```
HelixLLM
  |
  |--- /internal/health        --> Uptime monitors, load balancers
  |--- Prometheus metrics       --> Prometheus --> Grafana dashboards
  |--- Structured logs (JSON)   --> Loki --> Grafana log explorer
  |--- OpenTelemetry traces     --> OTEL Collector --> Jaeger/Zipkin
```

| Component | Role | Image |
|-----------|------|-------|
| Prometheus | Metrics collection and storage | `prom/prometheus` |
| Grafana | Dashboards and visualization | `grafana/grafana` |
| Loki | Log aggregation | `grafana/loki` |
| OTEL Collector | Trace collection (optional) | `otel/opentelemetry-collector` |

**Key points:**
- All four pillars are independent -- enable as many as you need
- Prometheus and Grafana are the minimum recommended for production
- The control plane can deploy the monitoring stack as containers
- Each component is configured via environment variables

---

## Scene 2: Prometheus Metrics (7 min)

**Narration:** "HelixLLM exposes Prometheus-compatible metrics that track HTTP requests, LLM performance, RAG queries, and cluster health."

**Screen:** Show the key metrics table.

| Metric | Type | Description |
|--------|------|-------------|
| `http_requests_total` | Counter | Total HTTP requests by method, path, status |
| `http_request_duration_seconds` | Histogram | Request latency distribution |
| `llm_requests_total` | Counter | LLM provider requests by provider, model, status |
| `llm_request_duration_seconds` | Histogram | LLM inference latency |
| `rag_queries_total` | Counter | RAG pipeline queries |
| `rag_query_duration_seconds` | Histogram | RAG query latency |
| `agent_turns_total` | Counter | Agent ReAct loop iterations |
| `cluster_hosts_healthy` | Gauge | Number of healthy hosts |

**Demo steps:**

```bash
# Configure Prometheus port
# In .env:
# HELIX_PROMETHEUS_PORT=9090

# Start HelixLLM
make dev

# Create a Prometheus configuration file
cat > /tmp/prometheus.yml << 'EOF'
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'helixllm'
    scheme: https
    tls_config:
      insecure_skip_verify: true
    static_configs:
      - targets: ['host.containers.internal:8443']
EOF

# Start Prometheus
podman run -d \
  --name prometheus \
  -p 9090:9090 \
  -v /tmp/prometheus.yml:/etc/prometheus/prometheus.yml:ro,z \
  prom/prometheus
```

**Narration:** "Prometheus scrapes the HelixLLM metrics endpoint every 15 seconds. Open Prometheus at http://localhost:9090 and try some queries."

```bash
# Open Prometheus UI and try these queries:

# Request rate over the last 5 minutes
# rate(http_requests_total[5m])

# 95th percentile latency
# histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))

# LLM requests by provider
# sum by (provider) (rate(llm_requests_total[5m]))
```

**Key points:**
- Prometheus scrapes HelixLLM at a configurable interval
- Use `rate()` for counter metrics and `histogram_quantile()` for latency percentiles
- Labels on metrics enable filtering by method, path, provider, model
- The Prometheus port defaults to 9090

---

## Scene 3: Grafana Dashboards (7 min)

**Narration:** "Grafana turns Prometheus data into visual dashboards. Let me set up Grafana and create a dashboard for HelixLLM."

**Demo steps:**

```bash
# Start Grafana
podman run -d \
  --name grafana \
  -p 3001:3000 \
  -e GF_SECURITY_ADMIN_PASSWORD=admin \
  grafana/grafana
```

**Narration:** "Open Grafana at http://localhost:3001, log in with admin/admin, and add Prometheus as a data source."

**Screen:** Walk through Grafana UI steps.

1. Navigate to **Configuration > Data Sources > Add data source**
2. Select **Prometheus**
3. Set URL to `http://host.containers.internal:9090`
4. Click **Save & Test**

**Narration:** "Now create a dashboard with panels for the most important metrics."

**Screen:** Show recommended dashboard panels.

| Panel | Query | Visualization |
|-------|-------|---------------|
| Request Rate | `rate(http_requests_total[5m])` | Time series |
| Request Latency (p95) | `histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))` | Time series |
| LLM Latency by Provider | `histogram_quantile(0.95, rate(llm_request_duration_seconds_bucket[5m]))` | Time series |
| Error Rate | `rate(http_requests_total{status=~"5.."}[5m])` | Time series |
| RAG Query Rate | `rate(rag_queries_total[5m])` | Time series |
| Cluster Health | `cluster_hosts_healthy` | Gauge |

**Narration:** "These six panels give you a comprehensive view of system health: how many requests you are handling, how fast they complete, which providers are in use, whether errors are occurring, and the cluster status."

**Key points:**
- Grafana port defaults to 3001 (`HELIX_GRAFANA_PORT`)
- Add Prometheus as a data source first
- Build panels for rate, latency, errors, and cluster health
- Set up alerts on error rate and latency thresholds
- Pre-configured dashboards are deployed by the control plane

---

## Scene 4: Structured Logging with Loki (6 min)

**Narration:** "HelixLLM uses logrus for structured logging. In production, set the format to JSON for machine-parseable logs that Loki can ingest."

**Demo steps:**

```bash
# Configure structured logging in .env
HELIX_LOG_LEVEL=info
HELIX_LOG_FORMAT=json
```

**Narration:** "JSON logs look like this."

**Screen:** Show JSON log output.

```json
{"level":"info","msg":"starting HelixLLM","mode":"full","time":"2026-04-05T14:30:00Z"}
{"level":"info","msg":"server listening","addr":"0.0.0.0:8443","time":"2026-04-05T14:30:00Z"}
{"level":"info","msg":"request completed","method":"POST","path":"/v1/chat/completions","status":200,"duration_ms":245,"request_id":"req_abc123","time":"2026-04-05T14:30:05Z"}
```

**Narration:** "Set up Loki to aggregate logs from all hosts."

```bash
# Start Loki
podman run -d \
  --name loki \
  -p 3100:3100 \
  grafana/loki

# Add Loki as a data source in Grafana:
# Configuration > Data Sources > Add > Loki
# URL: http://host.containers.internal:3100
```

**Narration:** "In Grafana, use the Explore view to query logs. Filter by level, request_id, or any structured field."

```
# Loki query examples in Grafana:

# All error logs
{job="helixllm"} |= "error"

# Slow requests (over 1 second)
{job="helixllm"} | json | duration_ms > 1000

# Logs for a specific request ID
{job="helixllm"} |= "req_abc123"
```

**Key points:**
- `HELIX_LOG_FORMAT=json` for machine-parseable logs
- Every request gets a unique `request_id` via the `X-Request-Id` header
- Log levels: debug, info, warn, error
- Loki integrates with Grafana for unified metrics and log visualization
- Use request IDs to correlate logs with traces

---

## Scene 5: OpenTelemetry Tracing (6 min)

**Narration:** "OpenTelemetry tracing shows you the complete path of a request through all layers. Each request becomes a trace with spans for each operation."

**Demo steps:**

```bash
# Configure tracing in .env
HELIX_OTEL_EXPORTER=otlp
HELIX_OTEL_ENDPOINT=http://localhost:4317

# Or for simpler debugging:
HELIX_OTEL_EXPORTER=stdout
```

**Narration:** "The stdout exporter prints traces to the console, which is useful for development. For production, use the otlp exporter with a collector."

| Exporter | Description | Use Case |
|----------|-------------|----------|
| `none` | Tracing disabled | Default |
| `stdout` | Print to console | Development debugging |
| `otlp` | Send to OTEL Collector | Production |
| `jaeger` | Send to Jaeger directly | When using Jaeger |
| `zipkin` | Send to Zipkin directly | When using Zipkin |

**Narration:** "A typical trace for a chat completion shows spans for: HTTP handler, authentication middleware, brain provider selection, LLM inference, and response serialization."

**Screen:** Show a trace example.

```
Trace: req_abc123 (total: 1.2s)
  |-- HTTP Handler /v1/chat/completions (1.2s)
      |-- Auth Middleware (0.5ms)
      |-- Rate Limit Check (0.2ms)
      |-- Brain.ChatCompletion (1.19s)
          |-- Router.SelectProvider (0.1ms)
          |-- LlamaCPP.Complete (1.18s)
      |-- Response Serialization (2ms)
```

**Key points:**
- Every request gets a trace with correlated spans
- The `X-Request-Id` header links logs, metrics, and traces
- Use stdout exporter for development, otlp for production
- Traces show exactly where time is spent in each request
- Spans cover middleware, provider selection, LLM inference, and response

---

## Exercises

1. Set up Prometheus and Grafana, create a dashboard with request rate and latency panels, then generate traffic with curl and watch the metrics update
2. Enable JSON logging, send several requests, and use Loki (or grep) to filter logs by request_id and trace a single request through the system
3. Enable the stdout OTEL exporter, send a chat completion, and read the trace output to identify which operation consumed the most time
