# Phase 7: Testing & Validation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Testing & Validation layer for HelixLLM. This covers adding the HelixQA submodule, increasing unit test coverage to 90%+ across all packages, creating challenge bank YAML files for key domains, building an integration test framework, writing integration tests for the full API surface, and enforcing coverage thresholds via the Makefile.

**Architecture:** Phase 7 is a testing-only phase -- no new production code is introduced. The focus is on exercising existing code paths (Phases 1-6) from multiple angles: unit tests for edge cases and error paths, YAML challenge banks that define structured test scenarios following the Challenges module format, and integration tests that exercise the full HTTP API surface end-to-end using httptest. The integration test framework starts a real Gin server (via httptest) with all routes wired, including Gateway, Knowledge, Agents, Control, and Health endpoints. Coverage enforcement is added to the Makefile so CI can gate on a minimum threshold (85%).

**Tech Stack:** Go 1.26+, `testing` (stdlib), `net/http/httptest` (integration tests), `encoding/json` (API assertions), Gin (test server), YAML (challenge bank definitions), `digital.vasic.challenges` (already a submodule), Makefile (coverage enforcement)

**Spec Reference:** `docs/superpowers/specs/2026-04-04-helixllm-master-design.md` -- Section 11 (Testing Strategy)

**Important notes:**
- The `digital.vasic.challenges` submodule already exists at `submodules/Challenges/` with a replace directive in `go.mod`. Challenge banks use its `pkg/bank` YAML format.
- HelixQA does not yet exist as a Git submodule. Task 1 adds it from the HelixDevelopment org.
- All existing tests (18 packages) must continue to pass after each task.
- Complex infrastructure tests (chaos, stress, multi-host) are deferred -- they require real deployment.
- Current coverage baseline: 85.4% total, with `internal/server` at 37.9% and `internal/shared/config` at 52.6% being the lowest.
- Integration tests run against an in-process httptest server, not a live TLS server. This keeps them fast and CI-friendly.

---

## File Structure

```
helixllm/
  submodules/
    HelixQA/                              Task 1: HelixQA submodule
  challenges/
    banks/
      api/
        openai_compat.yaml                Task 3: OpenAI compatibility challenges
        anthropic_compat.yaml             Task 3: Anthropic compatibility challenges
      llm/
        routing.yaml                      Task 3: LLM routing challenges
      rag/
        ingestion.yaml                    Task 3: RAG ingestion challenges
        retrieval.yaml                    Task 3: RAG retrieval quality challenges
  tests/
    integration/
      framework.go                        Task 4: Integration test framework
      framework_test.go                   Task 4: Framework self-test
      api_test.go                         Task 5: API integration tests
  internal/
    (existing packages)                   Task 2: Additional unit tests for coverage
  Makefile                                Task 6: Updated coverage targets
  go.mod                                  Task 1: HelixQA replace directive
```

---

### Task 1: Add HelixQA Submodule

**Files:**
- Add: `submodules/HelixQA/` (git submodule from HelixDevelopment org)
- Modify: `go.mod` (add replace directive for dev.helix.qa)
- Modify: `.gitmodules` (new submodule entry)

- [ ] **Step 1: Add the HelixQA submodule**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
git submodule add git@github.com:HelixDevelopment/HelixQA.git submodules/HelixQA
```

- [ ] **Step 2: Add replace directive to go.mod**

Add to the replace block:
```
dev.helix.qa => ./submodules/HelixQA
```

- [ ] **Step 3: Verify build**

```bash
go build ./...
go test ./... -count=1
```

---

### Task 2: Increase Unit Test Coverage

**Files:**
- Modify: `internal/gateway/openai_test.go` (error paths, Brain error, completions with Brain)
- Modify: `internal/gateway/anthropic_test.go` (Brain-backed messages, streaming, error paths)
- Modify: `internal/gateway/streaming_test.go` (SSEWriter edge cases)
- Modify: `internal/brain/brain_test.go` (stream error propagation)
- Modify: `internal/brain/router_test.go` (additional edge cases)
- Modify: `internal/knowledge/pipeline_test.go` (error propagation)
- Modify: `internal/knowledge/hook_test.go` (RAGHook nil/empty scenarios)
- Modify: `internal/agents/agent_test.go` (max turns, nil tools, tool error)
- Modify: `internal/agents/context_test.go` (unlimited history, Sessions)
- Modify: `internal/control/scheduler_test.go` (strategy edge cases)
- Modify: `internal/control/deployer_test.go` (undeploy, SetRuntime)
- Modify: `internal/control/monitor_test.go` (Start/Stop, empty hosts)
- Modify: `internal/shared/config/config_test.go` (HostList, Validate errors)
- Modify: `internal/shared/events/events_test.go` (concurrent, close)
- Modify: `internal/shared/logging/logging_test.go` (all log levels, formats)
- Modify: `internal/server/server_test.go` (handler, router, alt-svc)
- Modify: `internal/server/middleware/compression_test.go`
- Modify: `internal/server/middleware/requestid_test.go`

Coverage targets per package:
- `internal/gateway`: 78.7% -> 90%+
- `internal/server`: 37.9% -> 85%+
- `internal/shared/config`: 52.6% -> 90%+
- `internal/shared/events`: 66.7% -> 85%+
- `internal/shared/logging`: 75.0% -> 85%+
- All other packages: maintain or increase to 90%+

- [ ] **Step 1: Write gateway error-path tests** -- Bad JSON, Brain errors for completions/streaming, HandleGetModel with Brain.

- [ ] **Step 2: Write gateway Anthropic Brain-backed tests** -- HandleMessages with Brain non-streaming and streaming, error paths.

- [ ] **Step 3: Write server handler tests** -- Test Handler() and Router() methods, health handler with unhealthy status, alt-svc middleware.

- [ ] **Step 4: Write config edge-case tests** -- HostList with empty/multiple hosts, Validate with all error conditions (invalid mode, port, log level).

- [ ] **Step 5: Write events edge-case tests** -- Concurrent publish/subscribe, Close() behavior, unsubscribe.

- [ ] **Step 6: Write logging level tests** -- Debug, Warn, Error levels, JSON format, WithField, WithError.

- [ ] **Step 7: Write control edge-case tests** -- Deployer SetRuntime, Undeploy, Monitor Start/Stop lifecycle, Scheduler with all strategies explicitly.

- [ ] **Step 8: Verify coverage meets 85%+ total**

```bash
go test -coverprofile=coverage-unit.out ./internal/...
go tool cover -func=coverage-unit.out | tail -1
```

---

### Task 3: Challenge Bank YAML Files

**Files:**
- Create: `challenges/banks/api/openai_compat.yaml`
- Create: `challenges/banks/api/anthropic_compat.yaml`
- Create: `challenges/banks/llm/routing.yaml`
- Create: `challenges/banks/rag/ingestion.yaml`
- Create: `challenges/banks/rag/retrieval.yaml`

Each YAML follows the Challenges module bank format with:
- `version`, `name`, `description`
- `challenges[]` with `id`, `name`, `category`, `priority`, `steps[]`
- Steps have `name`, `action`, `expected`

- [ ] **Step 1: Create api/openai_compat.yaml** -- 5 challenges covering: chat completions, streaming, models list, model by ID, embeddings.

- [ ] **Step 2: Create api/anthropic_compat.yaml** -- 4 challenges covering: messages, streaming messages, system prompt, max tokens.

- [ ] **Step 3: Create llm/routing.yaml** -- 4 challenges covering: prefix routing, explicit provider override, fallback behavior, no-provider error.

- [ ] **Step 4: Create rag/ingestion.yaml** -- 4 challenges covering: document ingest, chunking, empty content rejection, duplicate handling.

- [ ] **Step 5: Create rag/retrieval.yaml** -- 4 challenges covering: query with results, empty collection, top-K limiting, min-score filtering.

---

### Task 4: Integration Test Framework

**Files:**
- Create: `tests/integration/framework.go`
- Create: `tests/integration/framework_test.go`

**Design:**

```go
// Package integration provides a test framework for HelixLLM integration tests.
package integration

// TestServer wraps an httptest.Server with the full HelixLLM route tree.
type TestServer struct {
    Server *httptest.Server
    Brain  *brain.Brain
}

// NewTestServer creates an httptest.Server with all HelixLLM routes wired.
func NewTestServer() *TestServer

// Close shuts down the test server.
func (ts *TestServer) Close()

// URL returns the base URL for requests.
func (ts *TestServer) URL() string

// PostJSON sends a POST request with JSON body and returns the response.
func (ts *TestServer) PostJSON(path string, body interface{}) (*http.Response, error)

// Get sends a GET request and returns the response.
func (ts *TestServer) Get(path string) (*http.Response, error)

// ReadJSON reads the response body into the target struct.
func ReadJSON(resp *http.Response, target interface{}) error
```

The TestServer wires:
- Gateway routes (OpenAI + Anthropic) with a mock-backed Brain
- Knowledge routes with in-memory pipeline
- Agent routes with mock Brain + tools
- Control routes with mock SSH
- Health endpoint

- [ ] **Step 1: Write framework_test.go** -- Test that NewTestServer creates a working server, health endpoint returns 200.

- [ ] **Step 2: Write framework.go** -- Implement TestServer with all route wiring, helper methods.

- [ ] **Step 3: Verify tests pass**

```bash
go test ./tests/integration/ -count=1
```

---

### Task 5: Integration Tests

**Files:**
- Create: `tests/integration/api_test.go`

**Tests:**

| Test | Endpoint | Validates |
|------|----------|-----------|
| TestIntegration_HealthEndpoint | GET /internal/health | 200, healthy status |
| TestIntegration_OpenAI_ChatCompletions | POST /v1/chat/completions | 200, valid response |
| TestIntegration_OpenAI_ChatCompletionsStreaming | POST /v1/chat/completions (stream) | SSE format, [DONE] |
| TestIntegration_OpenAI_ListModels | GET /v1/models | 200, model list |
| TestIntegration_OpenAI_Embeddings | POST /v1/embeddings | 200, embedding vector |
| TestIntegration_Anthropic_Messages | POST /v1/messages | 200, message response |
| TestIntegration_Anthropic_MessagesStreaming | POST /v1/messages (stream) | SSE events |
| TestIntegration_Knowledge_IngestAndQuery | POST ingest + POST query | Round-trip works |
| TestIntegration_Agents_Chat | POST /v1/agents/chat | Agent response |
| TestIntegration_Control_Status | GET /internal/cluster/status | 200, status JSON |

- [ ] **Step 1: Write integration tests for health and OpenAI endpoints.**

- [ ] **Step 2: Write integration tests for Anthropic and Knowledge endpoints.**

- [ ] **Step 3: Write integration tests for Agents and Control endpoints.**

- [ ] **Step 4: Verify all integration tests pass**

```bash
go test ./tests/integration/ -count=1 -v
```

---

### Task 6: Coverage Enforcement

**Files:**
- Modify: `Makefile` (update `coverage`, `test-all`, `test-integration` targets)

**Changes:**

1. `coverage` target: extract total percentage and fail if below 85%.
2. `test-integration` target: run `go test ./tests/integration/ -count=1 -v`.
3. `test-all` target: run unit tests then integration tests.

- [ ] **Step 1: Update Makefile** -- Add coverage threshold enforcement, integration test target.

- [ ] **Step 2: Verify make targets work**

```bash
make test-unit
make test-integration
make coverage
```

---

## Completion Criteria

- [ ] All 6 tasks implemented with tests passing
- [ ] `go test ./... -count=1` passes with zero failures
- [ ] `go build ./...` succeeds
- [ ] Coverage total >= 85%
- [ ] Each task committed separately
- [ ] Code pushed to `origin main`
