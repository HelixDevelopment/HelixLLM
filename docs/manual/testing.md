# Testing Guide

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
