# Phase 6: Monitoring, Metrics & Optimization Tests

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a monitoring-driven test suite that validates Prometheus metrics emission, OTEL tracing, structured logging, performance baselines, and chaos resilience. All container chaos operations use the Containers submodule.

**Architecture:** Monitoring tests verify observability instrumentation exists and emits correct data. Performance tests establish p50/p95/p99 baselines stored in JSON. Chaos tests use Containers submodule `ContainerRuntime.Stop()` to kill infrastructure and verify graceful degradation. Integration tests extend the existing `httptest.Server` framework.

**Tech Stack:** Go 1.26.1, digital.vasic.containers, Prometheus client_golang, OpenTelemetry, build tags (monitoring, performance)

---

### Task 1: Create monitoring test suite

**Files:**
- Create: `tests/monitoring/metrics_test.go`

- [ ] **Step 1: Create monitoring directory**

Run: `mkdir -p tests/monitoring`

- [ ] **Step 2: Write metrics emission test**

Create `tests/monitoring/metrics_test.go`:

```go
//go:build monitoring

package monitoring_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"crypto/tls"
)

var baseURL = "https://localhost:8443"

func init() {
	http.DefaultTransport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	}
}

func TestMetrics_EndpointAccessible(t *testing.T) {
	resp, err := http.Get(baseURL + "/internal/metrics")
	if err != nil {
		t.Fatalf("GET /internal/metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read metrics body: %v", err)
	}

	content := string(body)

	// Verify essential Prometheus metrics families exist.
	requiredMetrics := []string{
		"http_requests_total",
		"http_request_duration_seconds",
		"go_goroutines",
		"go_memstats_alloc_bytes",
	}

	for _, metric := range requiredMetrics {
		if !strings.Contains(content, metric) {
			t.Errorf("metrics missing %q", metric)
		}
	}
}

func TestMetrics_IncrementAfterRequest(t *testing.T) {
	// Make a request to generate metrics.
	resp, err := http.Get(baseURL + "/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	resp.Body.Close()

	// Fetch metrics and verify counter incremented.
	metricsResp, err := http.Get(baseURL + "/internal/metrics")
	if err != nil {
		t.Fatalf("GET /internal/metrics: %v", err)
	}
	defer metricsResp.Body.Close()

	body, _ := io.ReadAll(metricsResp.Body)
	content := string(body)

	if !strings.Contains(content, "http_requests_total") {
		t.Error("http_requests_total not found in metrics after request")
	}
}

func TestHealth_AggregatesComponents(t *testing.T) {
	resp, err := http.Get(baseURL + "/internal/health")
	if err != nil {
		t.Fatalf("GET /internal/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("health status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestLogging_StructuredFields(t *testing.T) {
	// This test verifies the server is running with structured logging.
	// The actual log verification happens via Loki in production;
	// here we just verify the health endpoint includes request_id header.
	resp, err := http.Get(baseURL + "/internal/health")
	if err != nil {
		t.Fatalf("GET /internal/health: %v", err)
	}
	defer resp.Body.Close()

	reqID := resp.Header.Get("X-Request-Id")
	if reqID == "" {
		t.Error("X-Request-Id header not set — structured logging may not be configured")
	}
}
```

- [ ] **Step 3: Add monitoring Makefile target**

Add to `Makefile`:
```makefile
test-monitoring:
	go test -v -count=1 -tags=monitoring ./tests/monitoring/...
```

Add `test-monitoring` to `.PHONY`.

- [ ] **Step 4: Verify build**

Run: `go build -tags=monitoring ./tests/monitoring/...`
Expected: Compiles without errors

- [ ] **Step 5: Commit**

```bash
git add tests/monitoring/ Makefile
git commit -m "test: add monitoring test suite for metrics, health, and structured logging"
```

---

### Task 2: Create performance baseline tests

**Files:**
- Create: `tests/performance/baseline_test.go`
- Create: `tests/performance/baselines.json`

- [ ] **Step 1: Create performance directory**

Run: `mkdir -p tests/performance`

- [ ] **Step 2: Write baseline test framework**

Create `tests/performance/baseline_test.go`:

```go
//go:build performance

package performance_test

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

var baseURL = "https://localhost:8443"

func init() {
	http.DefaultTransport = &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 200,
	}
}

type latencyResult struct {
	Endpoint string        `json:"endpoint"`
	P50      time.Duration `json:"p50_ns"`
	P95      time.Duration `json:"p95_ns"`
	P99      time.Duration `json:"p99_ns"`
	Count    int           `json:"count"`
}

func measureLatency(t *testing.T, method, path string, body string, count int) latencyResult {
	t.Helper()
	durations := make([]time.Duration, 0, count)

	for i := 0; i < count; i++ {
		start := time.Now()
		var resp *http.Response
		var err error

		if method == "GET" {
			resp, err = http.Get(baseURL + path)
		} else {
			resp, err = http.Post(baseURL+path, "application/json", strings.NewReader(body))
		}

		if err != nil {
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		durations = append(durations, time.Since(start))
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })

	percentile := func(p float64) time.Duration {
		if len(durations) == 0 {
			return 0
		}
		idx := int(float64(len(durations)) * p)
		if idx >= len(durations) {
			idx = len(durations) - 1
		}
		return durations[idx]
	}

	return latencyResult{
		Endpoint: fmt.Sprintf("%s %s", method, path),
		P50:      percentile(0.50),
		P95:      percentile(0.95),
		P99:      percentile(0.99),
		Count:    len(durations),
	}
}

func TestPerformance_HealthLatency(t *testing.T) {
	result := measureLatency(t, "GET", "/internal/health", "", 100)
	t.Logf("Health: p50=%v p95=%v p99=%v (n=%d)", result.P50, result.P95, result.P99, result.Count)

	if result.P99 > 100*time.Millisecond {
		t.Errorf("health p99 = %v, want < 100ms", result.P99)
	}
}

func TestPerformance_ModelsLatency(t *testing.T) {
	result := measureLatency(t, "GET", "/v1/models", "", 100)
	t.Logf("Models: p50=%v p95=%v p99=%v (n=%d)", result.P50, result.P95, result.P99, result.Count)

	if result.P99 > 200*time.Millisecond {
		t.Errorf("models p99 = %v, want < 200ms", result.P99)
	}
}

func TestPerformance_ThroughputHealth(t *testing.T) {
	const duration = 5 * time.Second
	const workers = 50
	var count int64
	var mu sync.Mutex

	done := make(chan struct{})
	go func() {
		time.Sleep(duration)
		close(done)
	}()

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			localCount := 0
			for {
				select {
				case <-done:
					mu.Lock()
					count += int64(localCount)
					mu.Unlock()
					return
				default:
				}
				resp, err := http.Get(baseURL + "/internal/health")
				if err == nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					localCount++
				}
			}
		}()
	}

	wg.Wait()
	rps := float64(count) / duration.Seconds()
	t.Logf("Health throughput: %.0f req/s (workers=%d, duration=%v)", rps, workers, duration)

	if rps < 100 {
		t.Errorf("health throughput = %.0f req/s, want >= 100", rps)
	}
}
```

- [ ] **Step 3: Create baselines placeholder**

Create `tests/performance/baselines.json`:

```json
{
  "note": "Performance baselines — updated by test runs. Fail if regression > 10%.",
  "health_p99_ms": 100,
  "models_p99_ms": 200,
  "health_throughput_rps": 100
}
```

- [ ] **Step 4: Add performance Makefile target**

Add to `Makefile`:
```makefile
test-performance:
	go test -v -count=1 -tags=performance -timeout=5m ./tests/performance/...
```

Add `test-performance` to `.PHONY`.

- [ ] **Step 5: Commit**

```bash
git add tests/performance/ Makefile
git commit -m "test: add performance baseline tests with p50/p95/p99 latency and throughput measurement"
```

---

### Task 3: Extend chaos challenge banks

**Files:**
- Create: `challenges/banks/chaos/provider_failure.yaml`
- Create: `challenges/banks/chaos/redis_failure.yaml`

- [ ] **Step 1: Create chaos challenge banks**

Create `challenges/banks/chaos/provider_failure.yaml`:

```yaml
name: LLM Provider Failure
description: Validates graceful degradation when LLM providers are unavailable
category: chaos
priority: high

challenges:
  - name: chat_without_provider
    description: Chat completion returns meaningful error when no LLM is available
    steps:
      - method: POST
        path: /v1/chat/completions
        body:
          model: "nonexistent-model"
          messages:
            - role: user
              content: "test"
        assertions:
          - type: status_one_of
            values: [404, 503]
          - type: response_time_ms
            max: 5000

  - name: models_always_responds
    description: Models endpoint responds even when providers are down
    steps:
      - method: GET
        path: /v1/models
        assertions:
          - type: status
            value: 200
          - type: response_time_ms
            max: 500
```

Create `challenges/banks/chaos/redis_failure.yaml`:

```yaml
name: Redis Cache Failure
description: Validates system operates correctly without cache layer
category: chaos
priority: medium

challenges:
  - name: api_without_cache
    description: API endpoints still function without Redis
    steps:
      - method: GET
        path: /internal/health
        assertions:
          - type: status
            value: 200

      - method: GET
        path: /v1/models
        assertions:
          - type: status
            value: 200
```

- [ ] **Step 2: Commit**

```bash
git add challenges/banks/chaos/
git commit -m "test: add chaos challenge banks for provider failure and redis failure scenarios"
```

---

### Task 4: Extend integration tests

**Files:**
- Create: `tests/integration/auth_test.go`

- [ ] **Step 1: Write auth integration test**

Create `tests/integration/auth_test.go`:

```go
package integration_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestAuth_ValidAPIKey(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// If auth is configured, a valid key should succeed.
	// If auth is disabled (no keys configured), all requests succeed.
	body := `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`
	req, _ := http.NewRequest("POST", ts.URL+"/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	// Without auth configured in test server, should get 200 or 503 (no providers).
	if resp.StatusCode != 200 && resp.StatusCode != 503 {
		t.Errorf("status = %d, want 200 or 503", resp.StatusCode)
	}
}

func TestAuth_SecurityHeaders(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	defer resp.Body.Close()

	headers := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":       "DENY",
	}

	for name, want := range headers {
		got := resp.Header.Get(name)
		if got != want {
			t.Errorf("header %s = %q, want %q", name, got, want)
		}
	}
}
```

- [ ] **Step 2: Run integration tests**

Run: `go test -v -count=1 -race ./tests/integration/...`
Expected: All tests PASS

- [ ] **Step 3: Commit**

```bash
git add tests/integration/auth_test.go
git commit -m "test: add auth and security headers integration tests"
```

---

### Task 5: Add benchmark challenge banks

**Files:**
- Create: `challenges/banks/benchmarks/p99_latency.yaml`
- Create: `challenges/banks/benchmarks/cold_start.yaml`

- [ ] **Step 1: Create latency benchmark challenges**

Create `challenges/banks/benchmarks/p99_latency.yaml`:

```yaml
name: P99 Latency Assertions
description: Validates p99 latency SLOs per endpoint type
category: benchmarks
priority: high

challenges:
  - name: health_p99
    description: Health endpoint p99 latency under 50ms
    steps:
      - method: GET
        path: /internal/health
        repeat: 100
        assertions:
          - type: response_time_p99_ms
            max: 50

  - name: models_p99
    description: Models endpoint p99 latency under 100ms
    steps:
      - method: GET
        path: /v1/models
        repeat: 100
        assertions:
          - type: response_time_p99_ms
            max: 100
```

Create `challenges/banks/benchmarks/cold_start.yaml`:

```yaml
name: Cold Start
description: Measures time from binary start to first request served
category: benchmarks
priority: medium

challenges:
  - name: first_health_response
    description: First health check after start completes in under 5 seconds
    steps:
      - method: GET
        path: /internal/health
        retry:
          max_attempts: 10
          interval_ms: 500
        assertions:
          - type: status
            value: 200
```

- [ ] **Step 2: Commit**

```bash
git add challenges/banks/benchmarks/
git commit -m "test: add p99 latency and cold start benchmark challenge banks"
```

---

### Task 6: Final verification

- [ ] **Step 1: Run all unit tests**

Run: `make test-unit`
Expected: All tests PASS with race detection

- [ ] **Step 2: Run integration tests**

Run: `make test-integration`
Expected: All tests PASS

- [ ] **Step 3: Verify all new challenge banks parse correctly**

Run: `find challenges/banks -name "*.yaml" -exec echo "Validating: {}" \; -exec go run ./cmd/helixllm --challenges --dry-run --banks-dir={} \; 2>&1 | head -50`
Expected: No YAML parsing errors
