---
title: "Testing Guide"
weight: 1
bookToC: true
---


HelixLLM uses multiple test types to ensure reliability. The project targets 85%+ coverage enforced by `make coverage`.

## Test Types

| Type | Location | Framework | Mocks Allowed |
|------|----------|-----------|---------------|
| Unit | `internal/**/*_test.go` | Go `testing` | Yes |
| Integration | `tests/integration/` | Go `testing` + real services | No |
| Challenge Banks | `challenges/banks/` | Challenges + HelixQA | No |

### Mock Policy

- **Unit tests:** Mocks, stubs, and test doubles are allowed. Use `httptest.NewServer` for HTTP mocking and interface-based test doubles.
- **Integration tests and above:** No mocks. Real services, real containers, real responses.

## Running Tests

### Unit Tests

```bash
make test-unit
```

Runs all unit tests under `internal/` with verbose output and coverage profiling:

```bash
go test -v -count=1 -coverprofile=coverage-unit.out ./internal/...
```

### Integration Tests

```bash
make test-integration
```

Runs integration tests that exercise layer-to-layer communication with real service instances.

### All Tests

```bash
make test-all
```

Runs unit and integration tests sequentially.

### Coverage Check

```bash
make coverage
```

This runs unit tests and then verifies the total coverage meets the 85% threshold. If coverage drops below the threshold, the command exits with a non-zero status.

To view a detailed HTML coverage report:

```bash
go tool cover -html=coverage-unit.out
```

To see per-function coverage:

```bash
go tool cover -func=coverage-unit.out
```

## Test Organization

### Unit Test Conventions

Each source file has a corresponding `_test.go` in the same package:

```
internal/gateway/
  openai.go
  openai_test.go
  anthropic.go
  anthropic_test.go
  router.go
  router_test.go
```

Tests use table-driven patterns:

```go
func TestParse(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    Mode
        wantErr bool
    }{
        {"full mode", "full", Full, false},
        {"gateway mode", "gateway", Gateway, false},
        {"invalid mode", "invalid", 0, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := Parse(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("Parse(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
                return
            }
            if got != tt.want {
                t.Errorf("Parse(%q) = %v, want %v", tt.input, got, tt.want)
            }
        })
    }
}
```

### HTTP Handler Tests

Use `httptest.NewRecorder` and the Gin test mode:

```go
func TestEndpoint(t *testing.T) {
    gin.SetMode(gin.TestMode)
    r := gin.New()
    RegisterRoutes(r, Options{})

    req := httptest.NewRequest(http.MethodGet, "/path", nil)
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("expected 200, got %d", w.Code)
    }
}
```

### Server Tests

The server package uses `httptest`-compatible patterns. The `Server.Handler()` method returns an `http.Handler` for use with test recorders, avoiding the need to start a real TLS server in unit tests.

## Challenge Banks

Challenge banks are YAML test definitions in `challenges/banks/` organized by domain:

```
challenges/banks/
  llm/          Code generation, multi-turn, tool calling, streaming
  rag/          Retrieval quality, ingestion, embedding accuracy
  api/          OpenAI/Anthropic compat, error handling, auth
  cluster/      Deployment, failover, rebalancing, host probing
  chaos/        Container kills, network partitions, resource exhaustion
  security/     Injection, auth bypass, PII, rate limiting
  benchmarks/   Latency, throughput, concurrent users
  workflows/    Real developer scenarios (coding, review, debugging)
  regression/   Known-fixed bugs, edge cases
```

These are executed by the Challenges framework (`digital.vasic.challenges`) and HelixQA orchestrator (`digital.vasic.helixqa`).

## Writing a Challenge

Challenges are defined in YAML:

```yaml
name: "Chat completion returns valid response"
category: api
severity: critical
steps:
  - action: http_post
    url: /v1/chat/completions
    body:
      model: "Llama-3.1-70B-Instruct-Q4_K_M"
      messages:
        - role: user
          content: "Say hello"
    assertions:
      - status_code: 200
      - json_path: "$.choices[0].message.role"
        equals: "assistant"
      - json_path: "$.choices[0].message.content"
        not_empty: true
```

## Running Specific Packages

Test a single package:

```bash
go test -v ./internal/gateway/...
go test -v ./internal/brain/...
go test -v ./internal/knowledge/...
go test -v ./internal/agents/...
go test -v ./internal/control/...
```

Test a single function:

```bash
go test -v -run TestHandleChatCompletions ./internal/gateway/
```

## Coverage by Package

Current coverage across the main packages:

| Package | Coverage |
|---------|----------|
| `internal/gateway` | 90%+ |
| `internal/brain` | 90%+ |
| `internal/knowledge` | 90%+ |
| `internal/agents` | 85%+ |
| `internal/control` | 85%+ |
| `internal/server` | 85%+ |
| `internal/shared` | 90%+ |
| `internal/mode` | 100% |
| **Total** | **89.5%** |

## Makefile Test Targets

| Target | What It Does |
|--------|--------------|
| `make test-unit` | Unit tests with coverage profile |
| `make test-integration` | Integration tests with real services |
| `make test-all` | All test types sequentially |
| `make coverage` | Unit tests + coverage threshold check (85%) |

## Tips

- Run `make test-unit` before committing to catch regressions early
- Use `HELIX_LOG_LEVEL=debug` in tests to see detailed output
- The `-count=1` flag disables test caching, ensuring fresh runs
- Coverage reports in `coverage-unit.out` are gitignored
- `make clean` removes all coverage files

---

## Complete Test Type Reference

The following sections document every test type available in HelixLLM, including build-tag-gated tests, stress tests, and the full challenge bank system.

### Unit Tests (with Race Detection)

```bash
make test-unit
```

Underlying command:

```bash
go test -v -count=1 -race -coverprofile=coverage-unit.out ./internal/...
```

The `-race` flag enables the Go race detector, which instruments memory accesses at compile time. Any concurrent access to shared state without proper synchronization causes a hard test failure. All unit tests run with race detection enabled by default.

### Integration Tests

```bash
make test-integration
```

Underlying command:

```bash
go test -v -count=1 -race ./tests/integration/
```

Integration tests exercise layer-to-layer communication using real Gin route trees via `httptest.Server`. No mocks or stubs are permitted. Tests validate the full middleware chain (auth, rate limiting, compression) and real provider routing.

### End-to-End Tests

```bash
make test-e2e
```

Underlying command:

```bash
go test -v -count=1 -race -tags=e2e ./tests/integration/...
```

E2E tests are gated behind the `e2e` build tag. They require a running HelixLLM server (typically started with `make dev` in a separate terminal) and exercise the full HTTP/3 request path from an external client. These tests validate TLS negotiation, streaming responses, and real LLM provider round-trips.

To run: start the server first, then execute `make test-e2e` in a separate shell.

### Stress Tests (Go)

```bash
make test-stress-go
```

Underlying command:

```bash
go test -v -count=1 -tags=stress -timeout=10m ./tests/stress/...
```

Stress tests are gated behind the `stress` build tag with a 10-minute timeout. They exercise concurrency limits, connection pool exhaustion, and memory behavior under sustained load. The `GOMAXPROCS` is not constrained, allowing full CPU utilization.

### Performance Tests

```bash
make test-performance
```

Performance tests use challenge banks in `challenges/banks/performance/` to measure latency percentiles, throughput under load, and resource consumption. They require a running server and the built binary.

### Monitoring Tests

```bash
make test-monitoring
```

Monitoring tests validate Prometheus metric emission, OpenTelemetry trace propagation, and health endpoint accuracy under various system states. Gated behind the `monitoring` build tag.

### Benchmark Tests (Go)

```bash
make test-benchmark-go
```

Underlying command:

```bash
go test -bench=. -benchmem -count=3 -run=^$ ./internal/...
```

Runs Go benchmarks across all internal packages. The `-benchmem` flag reports memory allocations per operation. The `-count=3` flag runs each benchmark three times for statistical reliability. The `-run=^$` pattern skips regular tests and only runs benchmarks.

Example benchmark output:

```
BenchmarkChunker_FixedSize-16     50000    23456 ns/op    4096 B/op    2 allocs/op
BenchmarkRouter_SelectProvider-16 100000   11234 ns/op     512 B/op    5 allocs/op
```

### Race Detection (Standalone)

```bash
make test-race
```

Underlying command:

```bash
GOMAXPROCS=$(nproc) go test -count=1 -race -p 1 ./internal/... ./pkg/... ./tests/...
```

Runs the entire test suite (internal, pkg, and tests) with the race detector and sequential package execution (`-p 1`). This is more thorough than `test-unit` because it covers `pkg/` and `tests/` and forces sequential execution to surface race conditions that parallel execution might mask. `GOMAXPROCS` is set to the number of CPU cores.

### Challenge Banks

Challenge banks are black-box acceptance tests defined in YAML. They exercise a running HelixLLM instance over HTTP.

#### Running All Banks

```bash
make test-challenges
```

This runs every bank in `challenges/banks/` against `https://localhost:8443`.

#### Running by Category

```bash
make test-challenges-api     # API compatibility banks only
make test-security           # Security banks (injection, auth bypass, PII)
make test-chaos              # Chaos engineering banks (container kills, partitions)
make test-stress             # Benchmark/stress banks
make test-usecases           # Real workflow scenario banks
```

#### Running with Filters

The binary supports fine-grained filtering:

```bash
./bin/helixllm --challenges --banks-dir=challenges/banks/security/ --base-url=https://localhost:8443
./bin/helixllm --challenges --category=rag --priority=high
```

#### Bank Categories

| Directory | Purpose |
|-----------|---------|
| `challenges/banks/api/` | OpenAI/Anthropic API compatibility, error handling, auth |
| `challenges/banks/llm/` | Code generation, multi-turn, tool calling, streaming |
| `challenges/banks/rag/` | Retrieval quality, ingestion, embedding accuracy |
| `challenges/banks/security/` | Injection, auth bypass, PII leakage, rate limiting |
| `challenges/banks/chaos/` | Container kills, network partitions, resource exhaustion |
| `challenges/banks/benchmarks/` | Latency, throughput, concurrent users |
| `challenges/banks/performance/` | Performance profiling and regression detection |
| `challenges/banks/stress/` | Sustained load and concurrency limit testing |
| `challenges/banks/workflows/` | Real developer scenarios (coding, review, debugging) |
| `challenges/banks/regression/` | Known-fixed bugs and edge cases |
| `challenges/banks/safety/` | Content safety and guardrail validation |
| `challenges/banks/e2e/` | End-to-end full-stack validation |

### Coverage Enforcement

```bash
make coverage
```

The coverage threshold is **91%** (defined as `COVERAGE_THRESHOLD` in the Makefile). The target runs unit tests with coverage profiling and then verifies the total meets the threshold:

```bash
go tool cover -func=coverage-unit.out
# Extracts total percentage and compares against 91%
```

If coverage drops below 91%, the command exits with a non-zero status, failing CI.

To view a detailed HTML coverage report:

```bash
go tool cover -html=coverage-unit.out
```

To see per-function coverage:

```bash
go tool cover -func=coverage-unit.out
```

## Writing Challenge Banks

Challenge banks are YAML files processed by the Challenges framework (`digital.vasic.challenges`) and orchestrated by HelixQA (`digital.vasic.helixqa`).

### File Structure

Each bank file defines one or more challenges:

```yaml
name: "Chat completion returns valid response"
category: api
severity: critical
priority: high
tags:
  - openai-compat
  - smoke
steps:
  - action: http_post
    url: /v1/chat/completions
    headers:
      Content-Type: application/json
      Authorization: "Bearer ${API_KEY}"
    body:
      model: "Llama-3.1-70B-Instruct-Q4_K_M"
      messages:
        - role: user
          content: "Say hello"
    assertions:
      - status_code: 200
      - json_path: "$.choices[0].message.role"
        equals: "assistant"
      - json_path: "$.choices[0].message.content"
        not_empty: true
      - header: "Content-Type"
        contains: "application/json"
      - response_time_ms:
          less_than: 5000
```

### Available Actions

| Action | Description |
|--------|-------------|
| `http_get` | Sends a GET request to the specified URL |
| `http_post` | Sends a POST request with a JSON body |
| `http_put` | Sends a PUT request with a JSON body |
| `http_delete` | Sends a DELETE request |
| `wait` | Pauses execution for a specified duration |
| `assert_status` | Validates the cluster or service status |

### Available Assertions

| Assertion | Description |
|-----------|-------------|
| `status_code` | HTTP status code equals the expected value |
| `json_path` | Extract a value using JSONPath and compare |
| `equals` | Exact string or numeric match |
| `not_empty` | Value must be non-empty |
| `contains` | String contains the expected substring |
| `matches` | Value matches a regular expression |
| `header` | Response header equals or contains expected value |
| `response_time_ms` | Response time is within bounds (`less_than`, `greater_than`) |
| `body_length` | Response body length within bounds |

### Severity and Priority

Challenges declare a `severity` and optional `priority`:

- **Severity:** `critical`, `high`, `medium`, `low` -- determines failure impact on the overall test run
- **Priority:** `high`, `medium`, `low` -- used for filtering with `--priority`

Critical-severity failures cause the entire bank run to fail. Lower severities are reported but do not block.

### Multi-Step Challenges

Challenges can define multiple steps that execute sequentially. Variables from one step can be referenced in later steps:

```yaml
name: "Create session then send message"
category: api
severity: high
steps:
  - action: http_post
    url: /v1/agents/chat
    body:
      messages:
        - role: user
          content: "Hello"
    capture:
      session_id: "$.session_id"
    assertions:
      - status_code: 200

  - action: http_post
    url: /v1/agents/chat
    body:
      session_id: "${session_id}"
      messages:
        - role: user
          content: "What did I just say?"
    assertions:
      - status_code: 200
      - json_path: "$.choices[0].message.content"
        not_empty: true
```

### Environment Variables in Banks

Banks can reference environment variables using `${VAR_NAME}` syntax. Common variables:

- `${API_KEY}` -- authentication token
- `${BASE_URL}` -- server base URL (set by `--base-url`)
- `${MODEL}` -- default model name

## Complete Makefile Test Targets

| Target | Build Tag | Requires Server | Description |
|--------|-----------|-----------------|-------------|
| `make test-unit` | none | no | Unit tests with race detection and coverage |
| `make test-integration` | none | no | Integration tests with real services |
| `make test-e2e` | `e2e` | yes | End-to-end tests over HTTP/3 |
| `make test-race` | none | no | Full race detection across all packages |
| `make test-stress-go` | `stress` | no | Go stress tests with 10-minute timeout |
| `make test-benchmark-go` | none | no | Go benchmarks with memory profiling |
| `make test-challenges` | none | yes | All challenge banks |
| `make test-challenges-api` | none | yes | API compatibility banks |
| `make test-security` | none | yes | Security challenge banks |
| `make test-chaos` | none | yes | Chaos engineering banks |
| `make test-stress` | none | yes | Benchmark/stress challenge banks |
| `make test-usecases` | none | yes | Workflow scenario banks |
| `make test-all` | none | no | Unit + integration sequentially |
| `make test-automation` | none | yes | Unit + integration + all challenges |
| `make coverage` | none | no | Unit tests + 91% coverage enforcement |
