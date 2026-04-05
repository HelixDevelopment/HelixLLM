# HelixLLM Completion Initiative Design

**Version:** 1.0
**Date:** 2026-04-05
**Status:** Approved
**Approach:** Layer-by-Layer Parallel Tracks (4 concurrent work streams across 10 phases)

---

## 1. Overview

This document defines the comprehensive plan to bring every aspect of the HelixLLM project to full completion: zero broken modules, zero disabled features, zero dead code, maximum test coverage, complete documentation, video course scripts, a Hugo-based project website, integrated security scanning, and hardened concurrency/performance patterns.

### Guiding Constraints

- **Rock-solid safety** — every change must be non-error-prone and must NOT break any existing working functionality
- **Containers submodule** — all container lifecycle operations go through `digital.vasic.containers` (no raw Docker/Podman CLI calls)
- **GitSpec constitution** — all work respects GitSpec constraints and all CLAUDE.md / AGENTS.md files
- **Go 1.26.1** — current toolchain, all code uses idiomatic Go patterns
- **85% coverage threshold** — current minimum; target is theoretical maximum (95%+)
- **Existing test types** — unit, integration, E2E, stress, chaos, security, benchmark, automation, use-case, challenge banks

---

## 2. Current State Audit

### 2.1 Critical Issues

| # | Issue | Location | Severity |
|---|-------|----------|----------|
| 1 | Mutex without defer in InMemoryBroker.Publish | `internal/shared/messaging/messaging.go:135-142` | HIGH |
| 2 | Mutex without defer in LSP diagnostics handler | `internal/agents/tools/lsp.go:451-453` | HIGH |
| 3 | Mutex without defer in LSP call/readLoop | `internal/agents/tools/lsp.go:280-295, 353-382` | HIGH |
| 4 | `panic()` in production code (MustGet — documented Go pattern) | `submodules/Lazy/pkg/lazy/lazy.go:45` | MEDIUM |
| 5 | No `-race` flag in main Makefile test targets | `Makefile` | HIGH |
| 6 | 6 source files have zero test coverage | RPC (server, client, brain_proxy), store_factory, mcp_registry, ssh | HIGH |

### 2.2 Missing Infrastructure

| # | Gap | Impact |
|---|-----|--------|
| 7 | No Snyk configuration | No dependency vulnerability scanning |
| 8 | No SonarQube configuration | No SAST scanning |
| 9 | No `.golangci.yml` config file | Linter runs with defaults only |
| 10 | No root-level CI/CD pipeline | `.github/workflows/` missing from root |
| 11 | No govulncheck integration | Go vulnerability database not checked |
| 12 | No root-level AGENTS.md | 35 submodules have one; main project does not |
| 13 | No static website | Documentation only in markdown files |
| 14 | No video course materials | No scripts, outlines, or storyboards |
| 15 | No Mermaid diagrams in main docs | 23+ exist in submodules, zero in main docs/ |
| 16 | No SQL schema definitions | Database schemas undocumented |

### 2.3 Test Coverage Gaps

| # | Gap | Current State | Target |
|---|-----|---------------|--------|
| 17 | Overall coverage | 89.5% | 95%+ theoretical max |
| 18 | No Go benchmark functions | 0 benchmarks | Full benchmark suite |
| 19 | No E2E tests in main project | 0 (submodules have them) | Comprehensive E2E |
| 20 | No Go stress tests | YAML-only benchmarks | Go stress test suite |
| 21 | Limited challenge categories | 8 categories / 15 banks | Extended banks |

### 2.4 Dead Code / Unconnected Features

| # | Finding | Location |
|---|---------|----------|
| 22 | PostgreSQL storage stub (TODO comment) | `submodules/SkillRegistry/storage.go:100` |
| 23 | Deprecated model constants still exposed | `submodules/LLMProvider/pkg/providers/zen/zen.go:51-52` |
| 24 | TOON submodule appears to have minimal API surface | `submodules/TOON/` |
| 25 | Brain parameter accepted but unused in embeddings | `internal/gateway/openai.go` |
| 26 | Partial disk monitoring implementation | `submodules/Containers/pkg/monitor/system_internal_test.go` |
| 27 | Missing external modules referenced in submodule go.mod | `digital.vasic.models`, `digital.vasic.messaging` |

---

## 3. Architecture: 4 Parallel Tracks, 10 Phases

### 3.1 Track Definitions

| Track | Focus | Phases |
|-------|-------|--------|
| **Safety & Quality** | Mutex fixes, race conditions, memory leaks, security scanning, lazy loading, semaphores | 1, 3, 5 |
| **Testing** | Missing tests, race detection, benchmarks, stress tests, E2E, challenge banks, coverage max | 2, 6 |
| **Code Completion** | Dead code connection, missing modules, unfinished features, CI/CD | 4, 7 |
| **Documentation & Content** | Docs extension, diagrams, SQL schemas, video courses, Hugo website | 8, 9, 10 |

### 3.2 Phase Dependency Graph

```
Phase 1 (Safety) ──────────┬──> Phase 3 (Security Scanning)
                           |
Phase 2 (Testing) ─────────┼──> Phase 5 (Lazy/Semaphores) ──> Phase 6 (Monitoring Tests)
                           |
Phase 4 (Dead Code) ───────┼──> Phase 7 (CI/CD)
                           |
Phase 8 (Docs) ────────────┼──> Phase 9 (Video Courses) ──> Phase 10 (Website)
                           |
Phases 1-7 gate Phase 8    |
Phases 1-9 gate Phase 10   |
```

**Parallel starts:** Phases 1, 2, 4, and 8 can begin simultaneously.

---

## 4. Phase 1: Safety Hardening & Race Condition Elimination

**Track:** Safety & Quality
**Prerequisites:** None (first phase)

### 4.1 Fix All Mutex Patterns Without Defer

**4.1.1 — `internal/shared/messaging/messaging.go`**

InMemoryBroker.Publish (lines 135-142) uses `RLock()`/`RUnlock()` without defer. If a panic occurs between lock and unlock, the mutex is never released, causing a deadlock.

**Fix:** Restructure to use defer:
```go
func (b *InMemoryBroker) Publish(ctx context.Context, topic string, data []byte) error {
    b.mu.RLock()
    defer b.mu.RUnlock()
    // ... existing logic
}
```

**4.1.2 — `internal/agents/tools/lsp.go` handlePublishDiagnostics (lines 451-453)**

`diagMu.Lock()` without defer.

**Fix:** Convert to defer pattern:
```go
func (c *LSPClient) handlePublishDiagnostics(params json.RawMessage) {
    c.diagMu.Lock()
    defer c.diagMu.Unlock()
    // ... existing logic
}
```

**4.1.3 — `internal/agents/tools/lsp.go` call method (lines 280-295)**

Multiple `pendMu.Lock()`/`pendMu.Unlock()` pairs without defer across branching logic.

**Fix:** Restructure each critical section into a closure with defer, or use a helper method that acquires and releases the lock safely:
```go
// Register pending request
func (c *LSPClient) registerPending(id int64) chan json.RawMessage {
    c.pendMu.Lock()
    defer c.pendMu.Unlock()
    ch := make(chan json.RawMessage, 1)
    c.pending[id] = ch
    return ch
}

func (c *LSPClient) removePending(id int64) {
    c.pendMu.Lock()
    defer c.pendMu.Unlock()
    delete(c.pending, id)
}
```

**4.1.4 — `internal/agents/tools/lsp.go` readLoop (lines 353-382)**

Same pattern as call method — multiple lock/unlock pairs.

**Fix:** Same helper-method approach as 4.1.3.

### 4.2 Enable Race Detection

Add `-race` flag to all test Makefile targets:

```makefile
test-unit:
    go test -v -count=1 -race -coverprofile=coverage-unit.out ./internal/...

test-integration:
    go test -v -count=1 -race ./tests/integration/

test-e2e:
    go test -v -count=1 -race -tags=e2e ./tests/integration/...
```

Add dedicated race detection target matching submodule convention:

```makefile
test-race:
    GOMAXPROCS=$$(nproc) go test -count=1 -race -p 1 ./internal/... ./pkg/... ./tests/...
```

### 4.3 Document MustGet Panic Pattern

`submodules/Lazy/pkg/lazy/lazy.go:45` — `MustGet()` is a well-known Go pattern (`template.Must`, `regexp.MustCompile`). The panic is intentional.

**Action:** Add prominent doc comment warning:
```go
// MustGet returns the lazily-loaded value, panicking on error.
// This follows the Go "Must" convention (like template.Must).
// Use Get() instead if you need error handling.
// WARNING: Do NOT call MustGet in request handlers or goroutines
// where panics would crash the server. Use Get() there instead.
```

Audit all call sites of MustGet to ensure none are in request paths.

### 4.4 Goroutine Lifecycle Audit

Add `go.uber.org/goleak` to test dependencies.

Add goroutine leak detection to test main:
```go
// internal/testing/testmain_test.go
func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}
```

**Focus areas to audit:**
- `internal/shared/config/watcher.go` — config watcher goroutine (uses WaitGroup — verify clean shutdown)
- `internal/shared/events/events.go` — event bus cleanup loop
- `internal/shared/messaging/messaging.go` — Kafka consumer goroutines
- `internal/agents/tools/lsp.go` — LSP readLoop goroutine
- `internal/control/monitor.go` — health monitoring goroutine

### 4.5 Add `.golangci.yml` Configuration

Create `.golangci.yml` at project root:

```yaml
run:
  timeout: 5m
  go: "1.26"

linters:
  enable:
    - govet
    - errcheck
    - staticcheck
    - gosec
    - bodyclose
    - durationcheck
    - gocritic
    - gosimple
    - ineffassign
    - misspell
    - noctx
    - prealloc
    - unconvert
    - unparam
    - unused

linters-settings:
  gosec:
    severity: medium
    confidence: medium
  gocritic:
    enabled-tags:
      - diagnostic
      - performance
      - style
  errcheck:
    check-type-assertions: true

issues:
  exclude-rules:
    - path: _test\.go
      linters:
        - gosec
        - errcheck
```

### 4.6 Tests for Phase 1

**New test files:**
- `internal/shared/messaging/messaging_race_test.go` — concurrent Publish/Subscribe hammering
- `internal/agents/tools/lsp_race_test.go` — concurrent LSP call/readLoop operations
- Goroutine leak tests via goleak in all packages

**New challenge bank:**
- `challenges/banks/safety/concurrency.yaml` — concurrent API request hammering (100+ parallel requests)

---

## 5. Phase 2: Missing Test Coverage & Push to Maximum

**Track:** Testing
**Prerequisites:** None (parallel start with Phase 1)

### 5.1 Write Tests for 6 Untested Files

**5.1.1 — `internal/shared/rpc/server_test.go`**
- TestServerStart — verify server starts and accepts connections
- TestServerStop — verify graceful shutdown
- TestServerHandlerRegistration — register handler, call it, verify response
- TestServerRequestDispatch — send JSON-RPC request, verify correct handler called
- TestServerInvalidRequest — malformed JSON, missing method
- TestServerConcurrentRequests — parallel requests handled correctly

**5.1.2 — `internal/shared/rpc/client_test.go`**
- TestClientConnect — connect to test server
- TestClientCall — make RPC call, verify response
- TestClientCallTimeout — verify timeout handling
- TestClientCallError — server returns error, client propagates
- TestClientReconnect — connection drops, client recovers
- TestClientConcurrentCalls — parallel RPC calls

**5.1.3 — `internal/shared/rpc/brain_proxy_test.go`**
- TestBrainProxyComplete — proxy Complete call to remote brain
- TestBrainProxyCompleteStream — proxy streaming call
- TestBrainProxyModels — proxy Models listing
- TestBrainProxyName — verify proxy name
- TestBrainProxyAvailable — verify availability check
- TestBrainProxyError — remote brain returns error, proxy propagates

**5.1.4 — `internal/knowledge/store_factory_test.go`**
- TestStoreFactoryQdrant — config selects Qdrant store
- TestStoreFactoryPgvector — config selects pgvector store
- TestStoreFactoryMilvus — config selects Milvus store
- TestStoreFactoryPinecone — config selects Pinecone store
- TestStoreFactoryMemory — fallback to in-memory store
- TestStoreFactoryInvalidConfig — unknown store type returns error
- TestStoreFactoryUnavailableBackend — graceful fallback when backend unreachable

**5.1.5 — `internal/agents/mcp_registry_test.go`**
- TestMCPRegistryRegister — register MCP tool
- TestMCPRegistryDiscover — discover available tools
- TestMCPRegistryList — list all registered tools
- TestMCPRegistryRemove — remove tool by name
- TestMCPRegistryDuplicate — register same name twice
- TestMCPRegistryConcurrent — parallel register/discover

**5.1.6 — `internal/control/ssh_test.go`**
- TestSSHConnectionSetup — verify connection config
- TestSSHCommandExecution — execute command, capture output
- TestSSHKeyAuth — key-based authentication
- TestSSHTimeout — connection timeout handling
- TestSSHConcurrentCommands — parallel command execution

### 5.2 Add Go Benchmark Functions

**New benchmark files:**

| File | Benchmarks |
|------|-----------|
| `internal/brain/brain_benchmark_test.go` | BenchmarkProviderRouting, BenchmarkComplete, BenchmarkProviderSelection |
| `internal/gateway/gateway_benchmark_test.go` | BenchmarkHTTPHandler, BenchmarkStreaming, BenchmarkMiddlewareChain |
| `internal/knowledge/knowledge_benchmark_test.go` | BenchmarkChunking, BenchmarkEmbedding, BenchmarkVectorSearch, BenchmarkRAGPipeline |
| `internal/agents/agents_benchmark_test.go` | BenchmarkToolExecution, BenchmarkReActLoop, BenchmarkSessionManagement |
| `internal/server/server_benchmark_test.go` | BenchmarkCompression, BenchmarkRequestID, BenchmarkTLSHandshake |
| `internal/shared/events/events_benchmark_test.go` | BenchmarkEventPublish, BenchmarkEventSubscribe |

Add Makefile target:
```makefile
test-benchmark:
    go test -bench=. -benchmem -count=3 -run=^$$ ./internal/...
```

### 5.3 Add E2E Tests for Main Project

**Location:** `tests/e2e/`
**Build tag:** `//go:build e2e`

| File | Tests |
|------|-------|
| `full_mode_test.go` | TestFullModeBootstrap — boot full mode, verify all endpoints respond |
| `agent_workflow_test.go` | TestAgentChatWithToolCalls — complete agent conversation with tool execution |
| `rag_pipeline_test.go` | TestRAGIngestAndRetrieve — ingest document, query, verify retrieval |
| `distributed_mode_test.go` | TestDistributedGatewayBrain — boot gateway+brain separately, verify RPC |
| `streaming_test.go` | TestSSEStreaming — SSE end-to-end; TestWebSocketStreaming — WebSocket end-to-end |
| `auth_test.go` | TestAPIKeyAuth, TestJWTAuth — full authentication flows |

### 5.4 Add Stress Tests in Go

**Location:** `tests/stress/`
**Build tag:** `//go:build stress`

| File | Tests |
|------|-------|
| `concurrent_requests_test.go` | Test1000ConcurrentChatCompletions, Test500ConcurrentStreamingRequests |
| `memory_pressure_test.go` | TestLargeDocumentIngestion, TestMemoryStabilityUnderLoad |
| `connection_exhaustion_test.go` | TestMaxConnections, TestConnectionPoolRecovery |
| `rate_limit_test.go` | TestRateLimiterUnderExtremeLoad, TestRateLimiterFairness |

Add Makefile target (already exists but verify wiring):
```makefile
test-stress:
    go test -v -count=1 -tags=stress -timeout=10m ./tests/stress/...
```

### 5.5 Push Coverage Toward Theoretical Maximum

- Target every exported function in every package
- Add internal (white-box) test files for unexported functions where needed
- Add error path tests — verify every `if err != nil` branch is covered
- Goal: 95%+ overall coverage
- Update Makefile threshold:
```makefile
COVERAGE_THRESHOLD := 95
```

### 5.6 New Challenge Banks

| File | Category | Content |
|------|----------|---------|
| `challenges/banks/stress/concurrent.yaml` | stress | Parallel request flood (100, 500, 1000 concurrent) |
| `challenges/banks/stress/memory.yaml` | stress | Large payload stress (1MB, 10MB, 100MB inputs) |
| `challenges/banks/stress/connections.yaml` | stress | Connection pool exhaustion and recovery |
| `challenges/banks/e2e/full_workflow.yaml` | e2e | Complete user workflow end-to-end |
| `challenges/banks/e2e/agent_tools.yaml` | e2e | Agent tool calling chains |

---

## 6. Phase 3: Security Scanning Infrastructure

**Track:** Safety & Quality
**Prerequisites:** Phase 1 completed

### 6.1 Snyk Integration via Containers Submodule

Create `deploy/compose.security.yaml` with Snyk service definition.

Orchestrate via `digital.vasic.containers` ComposeOrchestrator:
```go
project := compose.ComposeProject{
    Name:    "helixllm-security",
    File:    "deploy/compose.security.yaml",
    Profile: "snyk",
}
orchestrator.Up(ctx, project)
```

Create `.snyk` policy file at root for accepted risk documentation.

Makefile target:
```makefile
scan-snyk:
    go run ./cmd/helixllm --scan=snyk
```

### 6.2 SonarQube Integration via Containers Submodule

Add `sonarqube` service to `deploy/compose.security.yaml` (SonarQube Community Edition).

Use Containers' `HealthChecker` (HTTP type) to wait for SonarQube readiness:
```go
checker := health.NewChecker()
target := health.HealthTarget{
    Name: "sonarqube",
    Type: health.HealthTypeHTTP,
    Host: "sonarqube",
    Port: 9000,
    Path: "/api/system/status",
}
checker.Check(ctx, target)
```

Create `sonar-project.properties`:
```properties
sonar.projectKey=helixllm
sonar.projectName=HelixLLM
sonar.sources=internal/,pkg/,cmd/
sonar.tests=internal/,tests/
sonar.test.inclusions=**/*_test.go
sonar.go.coverage.reportPaths=coverage-unit.out
sonar.qualitygate.wait=true
```

Makefile target:
```makefile
scan-sonar:
    go run ./cmd/helixllm --scan=sonar
```

### 6.3 govulncheck Integration

Install `govulncheck` and add Makefile target:
```makefile
scan-vuln:
    govulncheck ./...
```

### 6.4 Enhanced golangci-lint with gosec

Already configured in Phase 1's `.golangci.yml` with `gosec` enabled.

Dedicated SAST target:
```makefile
scan-sast:
    golangci-lint run --enable-only gosec ./...
```

### 6.5 Container Image Scanning with Trivy via Containers Submodule

Add Trivy as a service in `deploy/compose.security.yaml`.

Run via `ContainerRuntime.Exec()` or compose up:
```go
// Image scan
runtime.Exec(ctx, trivyContainerID, []string{"trivy", "image", "helixllm:dev"})

// Filesystem scan
runtime.Exec(ctx, trivyContainerID, []string{"trivy", "fs", "/project"})
```

Create `.trivy.yaml` configuration at root.

Makefile targets:
```makefile
scan-container:
    go run ./cmd/helixllm --scan=trivy-image

scan-fs:
    go run ./cmd/helixllm --scan=trivy-fs
```

### 6.6 Unified Scanning Meta-Targets

```makefile
scan-all: scan-vuln scan-sast scan-snyk scan-sonar scan-container scan-fs

scan-quick: scan-vuln scan-sast
```

### 6.7 Tests for Phase 3

- `challenges/banks/security/scanning.yaml` — validate scanning tools are accessible and return results
- Integration test: verify compose security services start via Containers submodule and respond to health checks
- Unit tests for any scanning result parsing code

---

## 7. Phase 4: Dead Code Elimination & Feature Completion

**Track:** Code Completion
**Prerequisites:** None (parallel start)

### 7.1 SkillRegistry PostgreSQL Storage

**Location:** `submodules/SkillRegistry/storage.go:100`

Implement PostgreSQL storage initialization using existing Database submodule (`digital.vasic.database`) patterns:
- Connection pool setup
- CRUD operations (Create, Read, Update, Delete skills)
- Migration SQL for skills table
- Graceful fallback to in-memory when PostgreSQL is unavailable

**Tests:**
- Unit tests with mock database
- Integration tests with real PostgreSQL (via Containers submodule)

### 7.2 Deprecated Model Constants

**Location:** `submodules/LLMProvider/pkg/providers/zen/zen.go:51-52`

- Add `// Deprecated:` doc comments following Go convention
- Log warning via structured logger when deprecated models are selected
- Tests: verify warning logged for deprecated model usage

### 7.3 TOON Submodule Audit

Investigate if TOON (Token-Oriented Object Notation) is a planned feature or empty placeholder:
- If planned feature: document purpose, add stub tests, mark as future work
- If feature-gated (`HELIX_FEATURE_TOON`): verify feature flag works, document in configuration
- If abandoned: remove from go.mod replace directives and .gitmodules

### 7.4 Brain Parameter in Embeddings

**Location:** `internal/gateway/openai.go`

Wire Brain provider for embedding generation when local embedding provider routes through the LLM backend:
- Connect Brain.Complete for embedding extraction when provider is `local`
- Tests: verify embeddings route through Brain when configured

### 7.5 Complete Disk Monitoring in Containers Submodule

**Location:** `submodules/Containers/pkg/monitor/`

Implement disk usage collection using OS-level syscalls:
- `syscall.Statfs` for Linux
- Cross-platform abstraction
- Tests: verify disk stats populated correctly

### 7.6 Resolve Missing External Module References

**`digital.vasic.models`** — verify existence, add as submodule or refactor dependency out:
- Check if Models module exists elsewhere in the filesystem
- If found: add as Git submodule under `submodules/Models/`
- If not found: create minimal Models module with required type definitions

**`digital.vasic.messaging`** — same investigation and resolution

**`digital.vasic.docprocessor`** and **`digital.vasic.visionengine`** — document as external dependencies:
- Add setup instructions in docs/manual/development.md
- Add to .env.example with paths
- Verify HelixQA builds when these are absent (graceful degradation)

### 7.7 Tests for Phase 4

- Unit tests for every reconnected feature
- Integration tests for PostgreSQL storage via Containers submodule
- `challenges/banks/regression/dead_code.yaml` — verify all previously-dead features are reachable via API

---

## 8. Phase 5: Lazy Loading, Semaphores & Non-Blocking Patterns

**Track:** Safety & Quality
**Prerequisites:** Phases 1 and 2 completed

### 8.1 Lazy Initialization for Heavy Components

Use `submodules/Lazy` (`digital.vasic.lazy`) Value[T] pattern AND Containers submodule's `LazyBooter` for infrastructure services:

**Application-level lazy init (Lazy submodule):**
- Brain providers: lazy-init LLM connections (don't connect until first request)
- Knowledge pipeline: lazy-init vector store and embedding provider connections
- Control plane: lazy-init SSH connections to cluster hosts
- Analytics: lazy-init ClickHouse connection

**Infrastructure-level lazy init (Containers submodule):**
- Use `lifecycle.LazyBooter` for vector DB (Qdrant), cache (Redis), broker (Kafka) containers
- Services start on first demand, not at system boot
- Configurable via `HELIX_LAZY_INFRA=true` (default: false for production, true for development)

**Tests:**
- Verify components initialize on first use, not at startup
- Verify infrastructure containers boot on demand when lazy mode enabled
- Benchmark startup time: eager vs. lazy initialization

### 8.2 Semaphore-Based Concurrency Limiting

**In-process semaphores (`golang.org/x/sync/semaphore`):**

| Semaphore | Config Variable | Default | Purpose |
|-----------|----------------|---------|---------|
| Brain requests | `HELIX_LLM_MAX_CONCURRENT` | 10 | Limit concurrent LLM requests per provider |
| Embedding requests | `HELIX_EMBEDDING_MAX_CONCURRENT` | 20 | Limit concurrent embedding API calls |
| Tool executions | `HELIX_AGENT_MAX_CONCURRENT_TOOLS` | 5 | Limit concurrent tool executions per agent |
| SSH connections | `HELIX_SSH_MAX_CONCURRENT` | 10 | Limit concurrent SSH connections to hosts |

**Container-level semaphores (Containers submodule `lifecycle.ConcurrencySemaphore`):**
- Limit concurrent users of infrastructure services
- Prevent resource exhaustion of vector DB, Redis, Kafka

**Tests:**
- Verify semaphore limits respected under concurrent load
- Verify requests queue (not fail) when semaphore is full
- Benchmark throughput at various semaphore limits

### 8.3 Non-Blocking Patterns

| Area | Change | Mechanism |
|------|--------|-----------|
| Health checks | Convert synchronous checks to async with cached results | Background goroutine refreshes every N seconds; endpoint returns cached result |
| Event bus publish | Non-blocking fire-and-forget with bounded queue | Buffered channel; drop oldest if full; log warning |
| Analytics collection | Verify buffering in ClickHouse collector | Already has graceful fallback — add bounded buffer |
| Streaming responses | Never block on slow clients | `select` with timeout; close connection on write timeout |
| Config watcher | Non-blocking reload notification | Channel-based notification, non-blocking send |

**Tests:**
- Verify no endpoint blocks longer than configurable timeout
- Verify slow client doesn't block other streaming clients
- Verify event bus doesn't block publisher when subscribers are slow

### 8.4 Connection Pooling Optimization

| Connection | Pool Strategy |
|------------|--------------|
| HTTP clients (OpenAI/Anthropic) | `http.Transport` with `MaxIdleConns`, `MaxIdleConnsPerHost`, `IdleConnTimeout` |
| gRPC connections (distributed mode) | gRPC connection pool with health-based selection |
| Redis | Connection pool via Redis client config (already in redis library) |
| Kafka producer | Async producer with batching |
| SSH connections | Use Containers submodule `remote.RemoteExecutor` connection pooling |

**Tests:**
- Benchmark connection reuse vs. fresh connections
- Verify pool recovery after connection drops

### 8.5 Memory Allocation Optimization

| Optimization | Location | Technique |
|-------------|----------|-----------|
| Pre-allocate slices | RAG chunk results, tool parameters | `make([]T, 0, expectedCap)` |
| Buffer reuse | Streaming responses, JSON encoding | `sync.Pool` for `bytes.Buffer` |
| String interning | Repeated header values, model names | Pre-defined constants |
| Reduce allocations in hot paths | Middleware chain, request parsing | Avoid `fmt.Sprintf` in hot paths |

**Tests:**
- `go test -benchmem` — verify allocation counts
- Before/after comparison for key benchmarks

### 8.6 Infrastructure Idle Shutdown (Containers Submodule)

Use Containers' `lifecycle.IdleShutdown` for infrastructure services in development mode:
- Qdrant: shut down after 30 minutes idle
- Redis: shut down after 30 minutes idle
- Kafka: shut down after 30 minutes idle

Configurable via `HELIX_IDLE_SHUTDOWN_MINUTES=30` (0 = disabled, for production).

**Tests:**
- Verify idle shutdown triggers after configured period
- Verify service restarts on next request (via LazyBooter)

### 8.7 Challenge Banks for Phase 5

| File | Content |
|------|---------|
| `challenges/banks/performance/responsiveness.yaml` | Sub-100ms health, sub-500ms chat response |
| `challenges/banks/performance/nonblocking.yaml` | Slow downstream doesn't block other requests |
| `challenges/banks/performance/lazy_start.yaml` | Verify lazy boot timing and correctness |

---

## 9. Phase 6: Monitoring, Metrics & Optimization Tests

**Track:** Testing
**Prerequisites:** Phases 2 and 5 completed

### 9.1 Monitoring-Driven Test Suite

**Location:** `tests/monitoring/`
**Build tag:** `//go:build monitoring`

| File | Tests |
|------|-------|
| `metrics_test.go` | Verify Prometheus metrics emitted for all operations (http_requests_total, llm_requests_total, rag_queries_total, agent_turns_total, cluster_hosts_healthy) |
| `tracing_test.go` | Verify OTEL spans created for request lifecycle (gateway → brain → knowledge → agents) |
| `logging_test.go` | Verify structured log fields present (request_id, method, path, status, duration) |
| `health_test.go` | Verify health endpoint aggregates all component statuses correctly |
| `container_metrics_test.go` | Verify Containers submodule `MetricsCollector` emits container-level Prometheus metrics |

### 9.2 Performance Baseline Tests

**Location:** `tests/performance/`
**Build tag:** `//go:build performance`

| File | Tests |
|------|-------|
| `baseline_test.go` | Establish p50/p95/p99 latency baselines per endpoint |
| `throughput_test.go` | Measure max requests/sec per endpoint |
| `memory_test.go` | Measure memory usage under sustained load (1 min, 5 min, 10 min) |
| `cpu_test.go` | Measure CPU utilization per operation type |

Store baselines in `tests/performance/baselines.json` — fail if regression > 10%.

Use Containers submodule `ResourceMonitor` and `ClusterSnapshot` for container-level metrics during performance tests.

### 9.3 Extended Challenge Banks

| File | Content |
|------|---------|
| `challenges/banks/benchmarks/p99_latency.yaml` | p99 latency assertions per endpoint type |
| `challenges/banks/benchmarks/sustained_load.yaml` | 10-minute sustained request load, verify no degradation |
| `challenges/banks/benchmarks/memory_growth.yaml` | Verify no memory growth over time (leak detection) |
| `challenges/banks/benchmarks/cold_start.yaml` | Measure time from binary start to first request served |

### 9.4 Chaos Engineering Extensions

All chaos operations use Containers submodule's `ContainerRuntime.Stop()` and `RemoteRuntime`:

| File | Content |
|------|---------|
| `challenges/banks/chaos/provider_failure.yaml` | LLM provider goes down mid-stream |
| `challenges/banks/chaos/vector_db_failure.yaml` | Qdrant unavailable during RAG query |
| `challenges/banks/chaos/redis_failure.yaml` | Cache layer unavailable |
| `challenges/banks/chaos/kafka_failure.yaml` | Message broker unavailable |

### 9.5 Integration Test Extensions

| File | Tests |
|------|-------|
| `tests/integration/distributed_test.go` | Gateway + brain in separate processes via RPC |
| `tests/integration/rag_pipeline_test.go` | Full ingest → embed → store → retrieve flow |
| `tests/integration/agent_tools_test.go` | Agent with multiple tool calls in sequence |
| `tests/integration/websocket_test.go` | WebSocket streaming end-to-end |
| `tests/integration/auth_test.go` | API key and JWT authentication flows |

---

## 10. Phase 7: CI/CD Pipeline & Automation

**Track:** Code Completion
**Prerequisites:** Phases 1, 3, 4 completed

### 10.1 GitHub Actions CI/CD Pipeline

**`.github/workflows/ci.yml`** — triggered on push/PR to main:

| Step | Command | Purpose |
|------|---------|---------|
| 1 | `make deps` | Initialize submodules and dependencies |
| 2 | `make fmt` | Verify formatting (fail if diff) |
| 3 | `make lint` | golangci-lint with `.golangci.yml` |
| 4 | `make test-unit` | Unit tests with `-race` flag |
| 5 | `make coverage` | Enforce 95%+ threshold |
| 6 | `make scan-vuln` | govulncheck |
| 7 | `make scan-sast` | gosec via golangci-lint |
| 8 | `make build` | Verify compilation |

**`.github/workflows/security.yml`** — weekly schedule:

| Step | Command | Purpose |
|------|---------|---------|
| 1 | `make scan-snyk` | Snyk dependency scan (via Containers submodule) |
| 2 | `make scan-fs` | Trivy filesystem scan (via Containers submodule) |
| 3 | `make scan-container` | Container image scan (via Containers submodule) |

**`.github/workflows/release.yml`** — on tag push:

| Step | Command | Purpose |
|------|---------|---------|
| 1 | `make build` | Build binary |
| 2 | `make container` | Build container image (via Containers submodule) |
| 3 | `make container-push` | Push to registry |
| 4 | Generate changelog | From conventional commits |

### 10.2 Root-Level AGENTS.md

Create `AGENTS.md` at project root:
- Define agent collaboration rules for the HelixLLM project
- Document safe parallel changes vs. coordination-required changes
- Cross-reference all 35 submodule AGENTS.md files
- Define constraints for automated work (no sudo, no interactive processes, no destructive git operations)

### 10.3 Tests for Phase 7

- Validate CI workflow syntax with `actionlint`
- `challenges/banks/automation/ci_cd.yaml` — verify build pipeline stages are accessible

---

## 11. Phase 8: Documentation Completion

**Track:** Documentation & Content
**Prerequisites:** Phases 1-7 completed (all code changes finalized)

### 11.1 Extend All Existing User Guide Documents

| File | Extensions |
|------|-----------|
| `getting-started.md` | Platform-specific troubleshooting (Linux, macOS, WSL); GPU setup; first agent chat walkthrough |
| `configuration.md` | Document every `.env.example` variable with examples, valid values, and interactions |
| `api-reference.md` | curl examples for every endpoint; request/response JSON schemas; error code reference |
| `models.md` | Provider comparison table; local model download guide; model performance characteristics |
| `agents.md` | Tool creation tutorial; MCP integration step-by-step; multi-agent coordination guide |
| `rag-knowledge.md` | Performance tuning section; embedding provider comparison; chunking strategy guide |
| `multi-host-setup.md` | Step-by-step with screenshots; Containers submodule configuration; failover setup |
| `monitoring.md` | Grafana dashboard import guide; alert rule creation; custom metrics guide |
| `troubleshooting.md` | Extended error catalog; performance diagnosis; common misconfiguration patterns |

### 11.2 Extend All Existing Manual Documents

| File | Extensions |
|------|-----------|
| `architecture.md` | Mermaid diagrams for all request flows; distributed mode communication; data flow diagrams |
| `development.md` | Debugging guide (delve); profiling guide (pprof); contributing guide; IDE setup (VS Code, GoLand) |
| `testing.md` | Document all new test types (stress, E2E, monitoring, performance, chaos); challenge bank authoring guide |
| `security.md` | Snyk/SonarQube/Trivy scanning documentation; security scanning workflow; vulnerability triage process |
| `operations.md` | Scaling guide (horizontal and vertical); backup/restore procedures; disaster recovery playbook |
| `modules.md` | API signatures for all 43 submodules; usage examples; dependency graphs |

### 11.3 Create Mermaid Diagrams

**Location:** `docs/diagrams/`

| File | Content |
|------|---------|
| `architecture-overview.mmd` | Full system architecture (all 5 layers + shared) |
| `request-flow-chat.mmd` | Chat completion: HTTP → auth → brain → provider → response |
| `request-flow-agent.mmd` | Agent chat: HTTP → session → RAG → brain → tool calls → response |
| `request-flow-rag.mmd` | RAG: document → chunker → embedder → vector store → retrieval |
| `deployment-modes.mmd` | All 6 deployment modes (full, gateway, brain, knowledge, agents, control) |
| `distributed-architecture.mmd` | Multi-host RPC communication topology |
| `submodule-dependency-graph.mmd` | 43 submodule relationships (foundation → infrastructure → business → integration) |
| `security-layers.mmd` | TLS → auth middleware → rate limiter → security headers → PII detection |
| `container-lifecycle.mmd` | Containers submodule boot → health → monitor → idle shutdown cycle |
| `ci-cd-pipeline.mmd` | GitHub Actions workflow stages |

### 11.4 SQL Schema Documentation

**Location:** `docs/schemas/`

| File | Content |
|------|---------|
| `postgres.sql` | PostgreSQL schema definitions for all tables (skills, sessions, analytics, background tasks) |
| `migrations/001_initial.sql` | Initial schema migration |
| `migrations/002_skills.sql` | SkillRegistry table |
| `migrations/003_sessions.sql` | Agent conversation sessions |
| `migrations/004_analytics.sql` | ClickHouse analytics schema |
| `erd.mmd` | Entity relationship diagram in Mermaid |

### 11.5 Root AGENTS.md

Already created in Phase 7 — extend with full documentation of agent constraints.

### 11.6 Extended Module Documentation

Expand `docs/manual/modules.md` with:
- API signatures for every exported function in all 43 submodules
- Usage examples showing integration patterns
- Dependency graphs (Mermaid) per layer
- Version compatibility matrix

---

## 12. Phase 9: Video Course Scripts & Outlines

**Track:** Documentation & Content
**Prerequisites:** Phase 8 completed

### 12.1 Course Catalog

**Location:** `docs/courses/`

Each lesson file contains:
- Learning objectives (3-5 bullet points)
- Prerequisites
- Script outline (scene by scene with narration notes)
- Code examples (complete, runnable)
- Demo steps (exact commands to type)
- Key talking points
- Exercise suggestions
- Estimated duration

### 12.2 Course 1: Getting Started with HelixLLM (5 lessons)

| Lesson | File | Topic | Duration |
|--------|------|-------|----------|
| 1 | `01-getting-started/lesson-01-introduction.md` | What HelixLLM is, architecture overview, live demo | 15 min |
| 2 | `01-getting-started/lesson-02-installation.md` | Prerequisites, building from source, TLS certs, first run | 20 min |
| 3 | `01-getting-started/lesson-03-first-api-call.md` | curl to /v1/chat/completions, /v1/models, response anatomy | 15 min |
| 4 | `01-getting-started/lesson-04-configuration.md` | .env walkthrough, mode selection, provider setup | 20 min |
| 5 | `01-getting-started/lesson-05-local-llm.md` | llama.cpp setup, model download, local inference | 25 min |

### 12.3 Course 2: API Deep Dive (4 lessons)

| Lesson | File | Topic | Duration |
|--------|------|-------|----------|
| 1 | `02-api-deep-dive/lesson-01-openai-compat.md` | Full OpenAI API compatibility, function calling, streaming | 25 min |
| 2 | `02-api-deep-dive/lesson-02-anthropic-compat.md` | Anthropic Messages API, tool use, streaming | 25 min |
| 3 | `02-api-deep-dive/lesson-03-streaming.md` | SSE streaming, WebSocket streaming, error handling | 20 min |
| 4 | `02-api-deep-dive/lesson-04-embeddings.md` | Embedding generation, provider comparison, batch processing | 20 min |

### 12.4 Course 3: RAG Knowledge Pipeline (4 lessons)

| Lesson | File | Topic | Duration |
|--------|------|-------|----------|
| 1 | `03-rag-pipeline/lesson-01-ingestion.md` | Document upload, chunking strategies, file formats | 25 min |
| 2 | `03-rag-pipeline/lesson-02-vector-stores.md` | Qdrant, pgvector, Milvus, Pinecone setup and comparison | 30 min |
| 3 | `03-rag-pipeline/lesson-03-retrieval.md` | Semantic search, top-k tuning, re-ranking, hybrid search | 25 min |
| 4 | `03-rag-pipeline/lesson-04-integration.md` | RAG-augmented chat, knowledge-aware agents, context window | 25 min |

### 12.5 Course 4: Agent System (4 lessons)

| Lesson | File | Topic | Duration |
|--------|------|-------|----------|
| 1 | `04-agents/lesson-01-react-agents.md` | ReAct loop, tool calling basics, conversation sessions | 25 min |
| 2 | `04-agents/lesson-02-built-in-tools.md` | echo, time, knowledge_query tools, tool schema | 20 min |
| 3 | `04-agents/lesson-03-custom-tools.md` | Implementing Tool interface, registration, testing | 30 min |
| 4 | `04-agents/lesson-04-mcp-integration.md` | MCP protocol, external tool servers, registry | 25 min |

### 12.6 Course 5: Production Deployment (5 lessons)

| Lesson | File | Topic | Duration |
|--------|------|-------|----------|
| 1 | `05-production/lesson-01-containerization.md` | Containerfile, compose.yaml, GPU setup, volumes | 25 min |
| 2 | `05-production/lesson-02-multi-host.md` | SSH probing, scheduling strategies, distributed mode | 30 min |
| 3 | `05-production/lesson-03-monitoring.md` | Prometheus, Grafana, Loki, alerting, dashboards | 30 min |
| 4 | `05-production/lesson-04-security.md` | TLS, auth, rate limiting, scanning, hardening | 25 min |
| 5 | `05-production/lesson-05-operations.md` | Scaling, backup, disaster recovery, troubleshooting | 25 min |

### 12.7 Course 6: Development & Testing (4 lessons)

| Lesson | File | Topic | Duration |
|--------|------|-------|----------|
| 1 | `06-development/lesson-01-dev-setup.md` | Workspace, submodules, IDE config, debugging | 25 min |
| 2 | `06-development/lesson-02-testing.md` | Unit, integration, E2E, stress, benchmark tests | 30 min |
| 3 | `06-development/lesson-03-challenge-banks.md` | Writing YAML challenges, custom assertions, categories | 25 min |
| 4 | `06-development/lesson-04-ci-cd.md` | GitHub Actions pipeline, security scanning, releases | 20 min |

**Total:** 6 courses, 26 lessons, ~10 hours of estimated content.

---

## 13. Phase 10: Hugo Website

**Track:** Documentation & Content
**Prerequisites:** Phases 1-9 completed

### 13.1 Hugo Site Scaffolding

**Location:** `website/`

```
website/
├── config.toml                  # Hugo configuration
├── content/
│   ├── _index.md               # Landing page
│   ├── docs/                   # All documentation
│   │   ├── user-guide/         # Adapted from docs/user-guide/
│   │   └── manual/             # Adapted from docs/manual/
│   ├── api/                    # API reference
│   ├── courses/                # Video course catalog
│   ├── blog/                   # Changelog and releases
│   └── community/              # Contributing, links
├── static/
│   ├── css/                    # Custom styles
│   ├── images/                 # Logo, favicon, diagrams
│   └── js/                     # Custom scripts (Mermaid renderer)
├── layouts/                    # Custom templates
│   └── shortcodes/
│       ├── mermaid.html        # Mermaid diagram shortcode
│       └── api-example.html    # API example shortcode
└── themes/                     # Hugo theme (Book or Docsy)
```

**Hugo config.toml:**
```toml
baseURL = "https://helixllm.dev/"
title = "HelixLLM"
theme = "hugo-book"
enableGitInfo = true

[params]
  description = "Enterprise-grade distributed LLM system"
  github = "https://github.com/HelixDevelopment/HelixLLM"

[markup.goldmark.renderer]
  unsafe = true  # Allow raw HTML for Mermaid

[menu]
  [[menu.main]]
    name = "Docs"
    url = "/docs/"
    weight = 1
  [[menu.main]]
    name = "API"
    url = "/api/"
    weight = 2
  [[menu.main]]
    name = "Courses"
    url = "/courses/"
    weight = 3
  [[menu.main]]
    name = "Blog"
    url = "/blog/"
    weight = 4
```

### 13.2 Content Pages

| Page | Source | Content |
|------|--------|---------|
| `content/_index.md` | New | Landing page with feature highlights, quick start, architecture diagram |
| `content/docs/user-guide/*.md` | Adapted from `docs/user-guide/` | All 9 user guides with Hugo front matter |
| `content/docs/manual/*.md` | Adapted from `docs/manual/` | All 6 technical manuals with Hugo front matter |
| `content/api/_index.md` | New | Interactive API reference with examples |
| `content/courses/_index.md` | New | Course catalog linking to all 26 lessons |
| `content/courses/**/*.md` | Adapted from `docs/courses/` | All course lessons with Hugo front matter |
| `content/blog/changelog.md` | New | Release changelog from git tags |
| `content/community/_index.md` | New | Contributing guide, issue tracker, discussion links |

### 13.3 Build Integration

Makefile targets:
```makefile
website:
    cd website && hugo --minify

website-serve:
    cd website && hugo server --bind 0.0.0.0

website-deploy:
    cd website && hugo --minify --destination ../public
```

GitHub Actions: `.github/workflows/website.yml` — auto-deploy on changes to `docs/` or `website/`.

### 13.4 Content Migration

- Adapt all 33+ existing markdown docs to Hugo front matter format (title, weight, bookToC)
- Add navigation sidebar with hierarchical menu
- Include Mermaid diagrams rendered client-side via shortcode
- Cross-link between docs, API reference, and courses
- Add search via Hugo's built-in search or Pagefind

---

## 14. Deliverables Summary

### Files Created

| Phase | New Files | Categories |
|-------|-----------|-----------|
| 1 | 4 | `.golangci.yml`, 2 race test files, 1 challenge bank |
| 2 | 20+ | 6 test files, 6 benchmark files, 6 E2E tests, 4 stress tests, 5 challenge banks |
| 3 | 6 | `compose.security.yaml`, `sonar-project.properties`, `.snyk`, `.trivy.yaml`, 1 challenge bank, scanning Go code |
| 4 | 6+ | PostgreSQL storage, migration SQL, dead code tests, 1 challenge bank |
| 5 | 8+ | Lazy init wiring, semaphore wiring, non-blocking patterns, 3 challenge banks |
| 6 | 10+ | 5 monitoring tests, 4 performance tests, 4 challenge banks, 5 integration tests |
| 7 | 5 | 3 GitHub workflow files, AGENTS.md, 1 challenge bank |
| 8 | 20+ | 10 Mermaid diagrams, 6 SQL files, extended docs across 15 files |
| 9 | 27 | README.md + 26 lesson scripts |
| 10 | 50+ | Hugo config, 40+ content pages, templates, static assets |

**Total: ~160+ new files**

### Files Modified

| Phase | Modified Files | Changes |
|-------|---------------|---------|
| 1 | 4 | messaging.go, lsp.go, Makefile, lazy.go (doc comment) |
| 2 | 1 | Makefile (new targets) |
| 3 | 2 | Makefile, deploy/compose.yaml or new compose.security.yaml |
| 4 | 4+ | SkillRegistry storage.go, zen.go, openai.go, go.mod |
| 5 | 8+ | brain.go, knowledge pipeline, control, gateway, config |
| 6 | 1 | Makefile |
| 7 | 1 | Makefile |
| 8 | 15 | All existing docs files extended |
| 9 | 0 | All new files |
| 10 | 1 | Makefile |

### Test Coverage Targets

| Metric | Before | After |
|--------|--------|-------|
| Overall coverage | 89.5% | 95%+ |
| Files without tests | 6 | 0 |
| Go benchmark functions | 0 | 18+ |
| E2E test files | 0 | 6 |
| Stress test files | 0 | 4 |
| Monitoring test files | 0 | 5 |
| Performance test files | 0 | 4 |
| Challenge bank files | 15 | 30+ |
| Race detection | disabled | enabled globally |

### Security Scanning Coverage

| Scanner | Before | After |
|---------|--------|-------|
| golangci-lint (with config) | defaults | 16 linters configured |
| gosec (SAST) | disabled | enabled |
| govulncheck | not installed | integrated |
| Snyk | not installed | Compose service via Containers submodule |
| SonarQube | not installed | Compose service via Containers submodule |
| Trivy (container + fs) | not installed | Compose service via Containers submodule |

### Documentation Coverage

| Content Type | Before | After |
|-------------|--------|-------|
| User guide pages | 9 | 9 (all extended) |
| Manual pages | 6 | 6 (all extended) |
| Mermaid diagrams | 0 (main), 23+ (submodules) | 10 (main) + 23+ (submodules) |
| SQL schemas | 0 | 6 files |
| Video course lessons | 0 | 26 lessons (~10 hours) |
| Website pages | 0 | 40+ Hugo pages |
| AGENTS.md (root) | 0 | 1 |

---

## 15. Success Criteria

- [ ] All 26 audit items from Section 2 resolved
- [ ] Zero mutex operations without defer in production code
- [ ] Race detection enabled and passing on all test targets
- [ ] All 6 previously-untested files have comprehensive tests
- [ ] Coverage at 95%+ (up from 89.5%)
- [ ] All security scanners (Snyk, SonarQube, Trivy, govulncheck, gosec) integrated and passing
- [ ] All container operations go through Containers submodule
- [ ] Lazy loading configurable for all heavy components
- [ ] Semaphore limits configurable for all concurrent operations
- [ ] No blocking operations in request paths
- [ ] CI/CD pipeline running on GitHub Actions
- [ ] All 15 existing docs extended with new content
- [ ] 10 Mermaid diagrams in main docs
- [ ] 6 SQL schema files documented
- [ ] 26 video course lesson scripts complete
- [ ] Hugo website with 40+ pages buildable
- [ ] `make scan-all` passes clean
- [ ] `make test-all` passes with `-race`
- [ ] `make coverage` passes at 95%+ threshold
- [ ] Zero dead code, zero disabled features, zero broken modules
