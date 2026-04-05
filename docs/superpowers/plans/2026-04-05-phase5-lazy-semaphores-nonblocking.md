# Phase 5: Lazy Loading, Semaphores & Non-Blocking Patterns

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Introduce lazy initialization for heavy components, semaphore-based concurrency limits, non-blocking patterns for all request paths, and connection pool optimization — making the system responsive under any load.

**Architecture:** Application-level lazy init uses `digital.vasic.lazy` Value[T]. Infrastructure-level lazy init uses Containers submodule `lifecycle.LazyBooter`. In-process semaphores use `golang.org/x/sync/semaphore`. Non-blocking patterns use buffered channels with select/timeout. All limits are configurable via environment variables.

**Tech Stack:** Go 1.26.1, digital.vasic.lazy, digital.vasic.containers/lifecycle, golang.org/x/sync/semaphore, sync.Pool

---

### Task 1: Add semaphore dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add golang.org/x/sync dependency**

Run: `go get golang.org/x/sync`
Expected: Module added to go.mod

- [ ] **Step 2: Verify build**

Run: `go build ./cmd/helixllm`
Expected: Clean build

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add golang.org/x/sync for weighted semaphore support"
```

---

### Task 2: Add concurrency configuration variables

**Files:**
- Modify: `internal/shared/config/config.go`
- Modify: `.env.example`

- [ ] **Step 1: Read current config structure**

Run: `grep -n "type Config struct" internal/shared/config/config.go`
Expected: Find the Config struct definition

- [ ] **Step 2: Add concurrency limit fields to Config**

Add these fields to the Config struct in `internal/shared/config/config.go`:

```go
// Concurrency limits — 0 means unlimited.
LLMMaxConcurrent       int `env:"HELIX_LLM_MAX_CONCURRENT" default:"10"`
EmbeddingMaxConcurrent int `env:"HELIX_EMBEDDING_MAX_CONCURRENT" default:"20"`
AgentMaxConcurrentTools int `env:"HELIX_AGENT_MAX_CONCURRENT_TOOLS" default:"5"`
SSHMaxConcurrent       int `env:"HELIX_SSH_MAX_CONCURRENT" default:"10"`

// Lazy initialization — when true, infrastructure starts on first use.
LazyInfra bool `env:"HELIX_LAZY_INFRA" default:"false"`

// Idle shutdown — minutes of inactivity before stopping idle infra (0 = disabled).
IdleShutdownMinutes int `env:"HELIX_IDLE_SHUTDOWN_MINUTES" default:"0"`
```

- [ ] **Step 3: Add variables to .env.example**

Add to `.env.example` under a new `# ── Concurrency ──` section:

```bash
# ── Concurrency ─────────────────────────────────────────
# HELIX_LLM_MAX_CONCURRENT=10          # Max parallel LLM requests per provider (0=unlimited)
# HELIX_EMBEDDING_MAX_CONCURRENT=20    # Max parallel embedding API calls (0=unlimited)
# HELIX_AGENT_MAX_CONCURRENT_TOOLS=5   # Max parallel tool executions per agent (0=unlimited)
# HELIX_SSH_MAX_CONCURRENT=10          # Max parallel SSH connections to hosts (0=unlimited)
# HELIX_LAZY_INFRA=false               # Start infra services on first use (dev mode)
# HELIX_IDLE_SHUTDOWN_MINUTES=0        # Stop idle infra after N minutes (0=disabled)
```

- [ ] **Step 4: Run config tests**

Run: `go test -v -count=1 ./internal/shared/config/...`
Expected: All tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/shared/config/config.go .env.example
git commit -m "feat: add concurrency limit and lazy infrastructure config variables"
```

---

### Task 3: Add semaphore to Brain provider routing

**Files:**
- Modify: `internal/brain/brain.go`
- Create: `internal/brain/brain_semaphore_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/brain/brain_semaphore_test.go`:

```go
package brain_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

func TestBrain_ConcurrencyLimit(t *testing.T) {
	var concurrent atomic.Int64
	var maxSeen atomic.Int64

	provider := &slowProvider{
		delay: 50 * time.Millisecond,
		onCall: func() {
			cur := concurrent.Add(1)
			for {
				old := maxSeen.Load()
				if cur <= old || maxSeen.CompareAndSwap(old, cur) {
					break
				}
			}
		},
		onReturn: func() {
			concurrent.Add(-1)
		},
	}

	br := brain.New(brain.Config{
		Providers:      []brain.Provider{provider},
		MaxConcurrent:  3,
	})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := &types.InternalChatRequest{
				Model:    "slow-model",
				Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
			}
			_, _ = br.Complete(context.Background(), req)
		}()
	}

	wg.Wait()

	if maxSeen.Load() > 3 {
		t.Errorf("max concurrent = %d, want <= 3", maxSeen.Load())
	}
}

type slowProvider struct {
	delay    time.Duration
	onCall   func()
	onReturn func()
}

func (p *slowProvider) Complete(_ context.Context, _ *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	if p.onCall != nil {
		p.onCall()
	}
	time.Sleep(p.delay)
	if p.onReturn != nil {
		p.onReturn()
	}
	return &types.InternalChatResponse{
		ID:      "slow",
		Model:   "slow-model",
		Message: types.InternalMessage{Role: types.RoleAssistant, Content: "ok"},
	}, nil
}

func (p *slowProvider) CompleteStream(_ context.Context, _ *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	return nil, nil
}
func (p *slowProvider) Models() []string { return []string{"slow-model"} }
func (p *slowProvider) Name() string     { return "slow" }
func (p *slowProvider) Available() bool   { return true }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v -count=1 -run TestBrain_ConcurrencyLimit ./internal/brain/...`
Expected: FAIL (MaxConcurrent field doesn't exist yet or isn't enforced)

- [ ] **Step 3: Add semaphore to Brain**

Read `internal/brain/brain.go` to find the Brain struct and Complete method. Add a `*semaphore.Weighted` field and acquire/release around Complete calls:

In the Brain struct, add:
```go
import "golang.org/x/sync/semaphore"

// In Brain struct:
sem *semaphore.Weighted
```

In the `New` function, initialize the semaphore:
```go
var sem *semaphore.Weighted
if cfg.MaxConcurrent > 0 {
    sem = semaphore.NewWeighted(int64(cfg.MaxConcurrent))
}
```

In the `Complete` method, wrap the provider call:
```go
if b.sem != nil {
    if err := b.sem.Acquire(ctx, 1); err != nil {
        return nil, fmt.Errorf("brain: acquire semaphore: %w", err)
    }
    defer b.sem.Release(1)
}
```

Add `MaxConcurrent int` to the `Config` struct.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v -count=1 -run TestBrain_ConcurrencyLimit ./internal/brain/...`
Expected: PASS

- [ ] **Step 5: Run all brain tests**

Run: `go test -v -count=1 -race ./internal/brain/...`
Expected: All tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/brain/brain.go internal/brain/brain_semaphore_test.go
git commit -m "feat: add weighted semaphore concurrency limit to Brain provider routing"
```

---

### Task 4: Add lazy initialization for Brain providers

**Files:**
- Modify: `internal/brain/brain.go`

- [ ] **Step 1: Read current provider initialization**

Run: `grep -n "func New\|func.*Complete\|providers" internal/brain/brain.go | head -20`
Expected: Understand how providers are initialized and used

- [ ] **Step 2: Wrap provider connections in lazy init**

Use `digital.vasic.lazy` Value[T] to defer provider connections. The providers are already passed as interfaces, so the lazy wrapping happens at the call site in `cmd/helixllm/main.go` where providers are constructed.

In `cmd/helixllm/main.go`, find where LLM providers are created (e.g., `llamacpp.New`, `openai.New`) and wrap expensive ones:

```go
import "digital.vasic.lazy"

// Example for OpenAI provider (which requires network connection):
openaiProvider := lazy.New(func() (brain.Provider, error) {
    return brain.NewOpenAIProvider(cfg.LLMOpenAIKey, cfg.LLMOpenAIEndpoint)
})
```

Then in Brain, accept `lazy.Value[Provider]` or call `.Get()` before use.

**Note:** The exact implementation depends on whether providers do eager or lazy connection. Read the provider constructors to determine the right approach. If constructors are already lightweight (just storing config), lazy init adds no value and should be skipped.

- [ ] **Step 3: Run all tests**

Run: `go test -v -count=1 -race ./internal/brain/... ./cmd/...`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/brain/ cmd/helixllm/
git commit -m "feat: add lazy initialization for Brain provider connections"
```

---

### Task 5: Add non-blocking health checks with caching

**Files:**
- Modify: `internal/shared/health/health.go`
- Create: `internal/shared/health/health_cache_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/shared/health/health_cache_test.go`:

```go
package health_test

import (
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/health"
)

func TestHealthChecker_CachedResult(t *testing.T) {
	callCount := 0
	checker := health.NewChecker(health.WithCacheDuration(100 * time.Millisecond))
	checker.Register("test", func() health.Status {
		callCount++
		return health.Status{OK: true, Message: "up"}
	})

	// First call should invoke the check function.
	result1 := checker.Check()
	if !result1.OK {
		t.Fatal("first check should be OK")
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}

	// Immediate second call should use cache (same result, no new call).
	result2 := checker.Check()
	if !result2.OK {
		t.Fatal("cached check should be OK")
	}
	if callCount != 1 {
		t.Errorf("expected still 1 call (cached), got %d", callCount)
	}

	// After cache expires, should invoke again.
	time.Sleep(150 * time.Millisecond)
	result3 := checker.Check()
	if !result3.OK {
		t.Fatal("post-cache check should be OK")
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls after cache expiry, got %d", callCount)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v -count=1 -run TestHealthChecker_CachedResult ./internal/shared/health/...`
Expected: FAIL (WithCacheDuration option doesn't exist yet)

- [ ] **Step 3: Add caching to health checker**

Read `internal/shared/health/health.go` and add a cache layer:
- Add a `cacheDuration` field and `lastResult`/`lastCheck` fields to the checker struct
- Add `WithCacheDuration` option function
- In `Check()`, return cached result if `time.Since(lastCheck) < cacheDuration`
- Use `sync.RWMutex` with defer to protect the cache

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v -count=1 -run TestHealthChecker_CachedResult ./internal/shared/health/...`
Expected: PASS

- [ ] **Step 5: Run all health tests**

Run: `go test -v -count=1 -race ./internal/shared/health/...`
Expected: All tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/shared/health/
git commit -m "feat: add cached health checks to avoid blocking on frequent health requests"
```

---

### Task 6: Add sync.Pool for buffer reuse in streaming

**Files:**
- Modify: `internal/gateway/streaming.go`
- Create: `internal/gateway/streaming_pool_test.go`

- [ ] **Step 1: Read current streaming implementation**

Run: `grep -n "bytes.Buffer\|json.NewEncoder\|sync.Pool" internal/gateway/streaming.go`
Expected: Identify allocation patterns in streaming code

- [ ] **Step 2: Add buffer pool**

Add a package-level `sync.Pool` for `bytes.Buffer` reuse:

```go
var bufPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}
```

In streaming hot paths, replace `new(bytes.Buffer)` or `&bytes.Buffer{}` with:
```go
buf := bufPool.Get().(*bytes.Buffer)
buf.Reset()
defer bufPool.Put(buf)
```

- [ ] **Step 3: Run tests**

Run: `go test -v -count=1 -race ./internal/gateway/...`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/gateway/streaming.go
git commit -m "perf: add sync.Pool buffer reuse for streaming response encoding"
```

---

### Task 7: Add performance challenge banks

**Files:**
- Create: `challenges/banks/performance/responsiveness.yaml`
- Create: `challenges/banks/performance/nonblocking.yaml`

- [ ] **Step 1: Create performance challenge directory**

Run: `mkdir -p challenges/banks/performance`

- [ ] **Step 2: Create responsiveness challenge bank**

Create `challenges/banks/performance/responsiveness.yaml`:

```yaml
name: Responsiveness
description: Validates response time SLOs for all endpoint types
category: performance
priority: high

challenges:
  - name: health_sub_100ms
    description: Health endpoint must respond in under 100ms
    steps:
      - method: GET
        path: /internal/health
        assertions:
          - type: status
            value: 200
          - type: response_time_ms
            max: 100

  - name: models_sub_200ms
    description: Models listing must respond in under 200ms
    steps:
      - method: GET
        path: /v1/models
        assertions:
          - type: status
            value: 200
          - type: response_time_ms
            max: 200
```

Create `challenges/banks/performance/nonblocking.yaml`:

```yaml
name: Non-Blocking Behavior
description: Validates that slow operations do not block fast ones
category: performance
priority: high

challenges:
  - name: health_during_chat
    description: Health endpoint stays fast even when chat is processing
    steps:
      - method: POST
        path: /v1/chat/completions
        async: true
        body:
          model: "auto"
          messages:
            - role: user
              content: "Write a very long essay about the history of computing"

      - method: GET
        path: /internal/health
        assertions:
          - type: status
            value: 200
          - type: response_time_ms
            max: 100
```

- [ ] **Step 3: Commit**

```bash
git add challenges/banks/performance/
git commit -m "test: add performance responsiveness and non-blocking challenge banks"
```

---

### Task 8: Final verification

- [ ] **Step 1: Run full test suite**

Run: `make test-unit`
Expected: All tests PASS with race detection

- [ ] **Step 2: Run benchmarks to compare before/after**

Run: `go test -bench=. -benchmem -count=3 -run=^$ ./internal/brain/... ./internal/knowledge/...`
Expected: Benchmark results show allocation improvements

- [ ] **Step 3: Verify build**

Run: `make build`
Expected: Clean build
