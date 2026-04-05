# Lesson 2: Testing Strategy

**Duration:** 30 minutes
**Prerequisites:** Lesson 1 (Development Setup)
**Learning Objectives:**
- Run unit, integration, and end-to-end tests using Make targets
- Write table-driven tests following Go conventions
- Test HTTP handlers using httptest with the full Gin route tree
- Enforce the 85% coverage threshold and detect race conditions

---

## Scene 1: Test Types Overview (4 min)

**Narration:** "HelixLLM uses three levels of testing, each with different scope and rules about mocking."

**Screen:** Show the test types table.

| Type | Location | Mocks Allowed | Make Target |
|------|----------|---------------|-------------|
| Unit | `internal/**/*_test.go` | Yes | `make test-unit` |
| Integration | `tests/integration/` | No | `make test-integration` |
| E2E | `tests/integration/` (build tag) | No | `make test-e2e` |

**Narration:** "The key policy is: unit tests can use mocks and test doubles, but integration and end-to-end tests must use real services. This ensures that integration tests catch issues that mocks would hide."

**Key points:**
- Unit tests: fast, isolated, mocks allowed
- Integration tests: real services, real HTTP calls, no mocks
- E2E tests: full system with build tag `e2e`
- All test types use Go's standard `testing` package -- no external frameworks
- Target coverage: 85%+ enforced by `make coverage`

---

## Scene 2: Running Tests (5 min)

**Narration:** "Let me show you all the ways to run tests."

**Demo steps:**

```bash
# Run all unit tests with verbose output and coverage
make test-unit
# Equivalent to: go test -v -count=1 -coverprofile=coverage-unit.out ./internal/...

# Run integration tests
make test-integration
# Equivalent to: go test -v -count=1 ./tests/integration/

# Run both
make test-all

# Run E2E tests (requires build tag)
make test-e2e
# Equivalent to: go test -v -count=1 -tags=e2e ./tests/integration/...

# Check coverage threshold
make coverage
```

**Narration:** "The -count=1 flag disables Go's test cache, ensuring fresh runs every time. Let me also show you how to run individual packages and tests."

```bash
# Test a specific package
go test -v ./internal/gateway/...
go test -v ./internal/brain/...
go test -v ./internal/knowledge/...

# Test a single function
go test -v -run TestHandleChatCompletions ./internal/gateway/

# Test with a regex pattern
go test -v -run "TestRouter.*" ./internal/brain/
```

**Key points:**
- `make test-unit` for quick feedback during development
- `make test-all` before committing
- `make coverage` to verify the 85% threshold
- `-count=1` disables test caching for reliable results
- Use `-run` with a regex to target specific tests

---

## Scene 3: Writing Unit Tests (7 min)

**Narration:** "HelixLLM follows Go conventions: table-driven tests, the standard testing package, and one test file per source file."

**Screen:** Show a table-driven test example.

```go
// internal/mode/mode_test.go
package mode

import "testing"

func TestParse(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    Mode
        wantErr bool
    }{
        {"full mode", "full", Full, false},
        {"gateway mode", "gateway", Gateway, false},
        {"brain mode", "brain", Brain, false},
        {"knowledge mode", "knowledge", Knowledge, false},
        {"agents mode", "agents", Agents, false},
        {"control mode", "control", Control, false},
        {"empty string", "", 0, true},
        {"invalid mode", "invalid", 0, true},
        {"case insensitive", "FULL", Full, false},
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

**Narration:** "Table-driven tests define all test cases as a slice of structs. Each struct contains the inputs and expected outputs. The loop runs each case as a subtest with t.Run, which means failures are reported individually."

**Key points:**
- One `_test.go` file per source file in the same package
- Table-driven tests with named subtests using `t.Run`
- Struct fields: name, inputs, expected outputs, error flag
- Use `t.Errorf` for assertion failures (not `t.Fatal` unless critical)
- Test both success cases and error cases

---

## Scene 4: HTTP Handler Tests (6 min)

**Narration:** "Testing HTTP handlers requires the Gin engine and httptest. HelixLLM tests handlers by setting up the full route tree and sending requests through httptest."

**Screen:** Show an HTTP handler test.

```go
// internal/gateway/openai_test.go
package gateway

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/gin-gonic/gin"
)

func TestHandleChatCompletions(t *testing.T) {
    gin.SetMode(gin.TestMode)
    r := gin.New()

    // Register routes with test dependencies
    RegisterRoutes(r, Options{
        Brain: &mockBrain{},
    })

    tests := []struct {
        name       string
        body       map[string]interface{}
        wantStatus int
    }{
        {
            name: "valid request",
            body: map[string]interface{}{
                "model":    "test-model",
                "messages": []map[string]string{{"role": "user", "content": "Hello"}},
            },
            wantStatus: http.StatusOK,
        },
        {
            name:       "missing messages",
            body:       map[string]interface{}{"model": "test-model"},
            wantStatus: http.StatusBadRequest,
        },
        {
            name:       "empty body",
            body:       map[string]interface{}{},
            wantStatus: http.StatusBadRequest,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            bodyJSON, _ := json.Marshal(tt.body)
            req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(bodyJSON))
            req.Header.Set("Content-Type", "application/json")

            w := httptest.NewRecorder()
            r.ServeHTTP(w, req)

            if w.Code != tt.wantStatus {
                t.Errorf("status = %d, want %d, body = %s", w.Code, tt.wantStatus, w.Body.String())
            }
        })
    }
}
```

**Narration:** "The pattern is: create a Gin engine in test mode, register routes with mock or test dependencies, build the request with httptest.NewRequest, send it through the engine, and assert on the response. This tests the full middleware and handler chain without starting a real server."

**Key points:**
- `gin.SetMode(gin.TestMode)` suppresses debug output
- Use `httptest.NewRequest` and `httptest.NewRecorder`
- `r.ServeHTTP(w, req)` processes the request through the full route tree
- Test both valid requests and error cases
- Mock brain/knowledge dependencies for unit tests

---

## Scene 5: Coverage and Race Detection (5 min)

**Narration:** "HelixLLM enforces 85% test coverage and uses Go's race detector to catch concurrency bugs."

**Demo steps:**

```bash
# Run tests with coverage
make coverage

# View detailed coverage report
go tool cover -html=coverage-unit.out

# View per-function coverage
go tool cover -func=coverage-unit.out | tail -20
```

**Screen:** Show current coverage by package.

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

**Narration:** "To detect race conditions, add the -race flag."

```bash
# Run tests with race detector
go test -v -race ./internal/...

# Run a specific package with race detection
go test -v -race ./internal/agents/...
```

**Narration:** "The race detector instruments the binary to detect concurrent access to shared memory without proper synchronization. It slows tests down by 2-10x, so run it periodically rather than on every commit."

**Key points:**
- `make coverage` enforces the 85% threshold -- build fails if below
- Coverage reports in HTML show uncovered lines
- `-race` flag detects data races in concurrent code
- Race detection adds significant overhead -- run periodically
- Coverage files (`coverage-unit.out`) are gitignored

---

## Scene 6: What's Next (3 min)

**Narration:** "You now know the full testing strategy. In the next lesson, we will explore challenge banks -- YAML-based test definitions that validate the running system against real-world scenarios."

---

## Exercises

1. Run `make test-unit` and identify the package with the lowest coverage, then write one additional test to improve it
2. Write a table-driven test for a handler endpoint that tests five cases: valid request, missing field, empty body, wrong content type, and invalid JSON
3. Run the tests with `-race` and investigate any race conditions detected -- fix at least one if found
