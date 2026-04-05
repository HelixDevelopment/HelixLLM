# Phase 1: Safety Hardening & Race Condition Elimination

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate all concurrency safety issues — defer-less mutexes, missing race detection, potential goroutine leaks — and establish static analysis configuration.

**Architecture:** Fix mutex patterns using helper-method extraction (lock in helper with defer, logic outside lock). Enable `-race` in all test targets. Add `goleak` for goroutine leak detection. Add `.golangci.yml` with security-focused linters.

**Tech Stack:** Go 1.26.1, golang.org/x/sync, go.uber.org/goleak, golangci-lint

---

### Task 1: Fix InMemoryBroker.Publish mutex pattern

**Files:**
- Modify: `internal/shared/messaging/messaging.go:134-148`
- Test: `internal/shared/messaging/messaging_test.go` (existing — verify still passes)

- [ ] **Step 1: Run existing tests to establish baseline**

Run: `go test -v -count=1 ./internal/shared/messaging/...`
Expected: All tests PASS

- [ ] **Step 2: Extract getHandlers helper with defer**

In `internal/shared/messaging/messaging.go`, replace the `Publish` method of `InMemoryBroker` (lines 134-148) with a safe pattern that uses defer inside a helper:

Replace this block:
```go
// Publish delivers data to all handlers registered for topic.
func (b *InMemoryBroker) Publish(topic string, data []byte) error {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return fmt.Errorf("broker closed")
	}
	handlers := make([]func([]byte), len(b.handlers[topic]))
	copy(handlers, b.handlers[topic])
	b.mu.RUnlock()

	for _, h := range handlers {
		h(data)
	}
	return nil
}
```

With:
```go
// Publish delivers data to all handlers registered for topic.
func (b *InMemoryBroker) Publish(topic string, data []byte) error {
	handlers, err := b.handlersForTopic(topic)
	if err != nil {
		return err
	}
	for _, h := range handlers {
		h(data)
	}
	return nil
}

// handlersForTopic returns a snapshot of handlers for topic under read lock.
// Callers invoke handlers outside the lock to avoid re-entrant deadlocks.
func (b *InMemoryBroker) handlersForTopic(topic string) ([]func([]byte), error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return nil, fmt.Errorf("broker closed")
	}
	handlers := make([]func([]byte), len(b.handlers[topic]))
	copy(handlers, b.handlers[topic])
	return handlers, nil
}
```

- [ ] **Step 3: Run existing tests to verify no regression**

Run: `go test -v -count=1 ./internal/shared/messaging/...`
Expected: All tests PASS (identical results to Step 1)

- [ ] **Step 4: Commit**

```bash
git add internal/shared/messaging/messaging.go
git commit -m "fix: extract handlersForTopic with defer to prevent mutex leak in InMemoryBroker.Publish"
```

---

### Task 2: Fix DistributedBus.Subscribe mutex pattern

**Files:**
- Modify: `internal/shared/messaging/messaging.go:88-92`

- [ ] **Step 1: Extract checkAndSetSubscription helper with defer**

In `internal/shared/messaging/messaging.go`, replace the lock/unlock block in `Subscribe` (lines 88-92) with a helper:

Replace this block:
```go
func (d *DistributedBus) Subscribe(topic events.Topic) {
	d.mu.Lock()
	alreadySubscribed := d.topics[string(topic)]
	d.topics[string(topic)] = true
	d.mu.Unlock()

	if alreadySubscribed {
		return
	}
```

With:
```go
func (d *DistributedBus) Subscribe(topic events.Topic) {
	if d.markSubscribed(topic) {
		return
	}
```

And add the helper method after `Close()`:
```go
// markSubscribed records topic as subscribed and returns true if it was already
// subscribed. The write lock is held only during the map operation via defer.
func (d *DistributedBus) markSubscribed(topic events.Topic) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	already := d.topics[string(topic)]
	d.topics[string(topic)] = true
	return already
}
```

- [ ] **Step 2: Run existing tests**

Run: `go test -v -count=1 ./internal/shared/messaging/...`
Expected: All tests PASS

- [ ] **Step 3: Commit**

```bash
git add internal/shared/messaging/messaging.go
git commit -m "fix: extract markSubscribed with defer to prevent mutex leak in DistributedBus.Subscribe"
```

---

### Task 3: Fix LSP call method mutex pattern

**Files:**
- Modify: `internal/agents/tools/lsp.go:268-304`

- [ ] **Step 1: Run existing tests to establish baseline**

Run: `go test -v -count=1 ./internal/agents/tools/...`
Expected: All tests PASS

- [ ] **Step 2: Add pending-map helper methods**

Add these helper methods to `LSPClient` in `internal/agents/tools/lsp.go`, right before the `readLoop` method (before line 346):

```go
// registerPending creates a buffered channel for a pending request and stores
// it in the pending map. The lock is held via defer for panic safety.
func (c *LSPClient) registerPending(id int64) chan json.RawMessage {
	c.pendMu.Lock()
	defer c.pendMu.Unlock()
	ch := make(chan json.RawMessage, 1)
	c.pending[id] = ch
	return ch
}

// removePending deletes a pending request from the map. Safe to call if the
// entry was already removed (e.g. by readLoop dispatching the response).
func (c *LSPClient) removePending(id int64) {
	c.pendMu.Lock()
	defer c.pendMu.Unlock()
	delete(c.pending, id)
}
```

- [ ] **Step 3: Rewrite call method to use helpers**

Replace the `call` method body (lines 268-304) with:

```go
func (c *LSPClient) call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := c.nextID.Add(1)

	req := lspRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	ch := c.registerPending(id)

	if err := c.writeRequest(req); err != nil {
		c.removePending(id)
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.removePending(id)
		return nil, ctx.Err()
	case raw := <-ch:
		return raw, nil
	case <-c.done:
		return nil, fmt.Errorf("lsp: server closed")
	}
}
```

- [ ] **Step 4: Run existing tests**

Run: `go test -v -count=1 ./internal/agents/tools/...`
Expected: All tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agents/tools/lsp.go
git commit -m "fix: extract registerPending/removePending with defer to prevent mutex leak in LSP call"
```

---

### Task 4: Fix LSP readLoop and handlePublishDiagnostics mutex patterns

**Files:**
- Modify: `internal/agents/tools/lsp.go:346-401, 442-454`

- [ ] **Step 1: Add dispatchPending and closeAllPending helpers**

Add these helpers next to `registerPending` and `removePending` (added in Task 3):

```go
// dispatchPending delivers result to the pending channel for id and removes it
// from the map. Returns false if id was not found (already collected or timed out).
func (c *LSPClient) dispatchPending(id int64) (chan json.RawMessage, bool) {
	c.pendMu.Lock()
	defer c.pendMu.Unlock()
	ch, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	return ch, ok
}

// drainAllPending sends nil to every pending caller and clears the map.
// Used when the reader goroutine exits (EOF / server crash).
func (c *LSPClient) drainAllPending() {
	c.pendMu.Lock()
	defer c.pendMu.Unlock()
	for _, ch := range c.pending {
		ch <- nil
	}
	c.pending = make(map[int64]chan json.RawMessage)
}
```

- [ ] **Step 2: Rewrite readLoop to use helpers**

Replace the `readLoop` method (lines 346-401) with:

```go
func (c *LSPClient) readLoop() {
	defer close(c.done)

	for {
		body, err := c.readFrame()
		if err != nil {
			c.drainAllPending()
			return
		}

		var peek struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(body, &peek); err != nil {
			continue
		}

		if peek.ID != nil {
			var resp lspResponse
			if err := json.Unmarshal(body, &resp); err != nil {
				continue
			}
			ch, ok := c.dispatchPending(resp.ID)
			if ok {
				if resp.Error != nil {
					errMsg, _ := json.Marshal(map[string]string{"_lspError": resp.Error.Error()})
					ch <- errMsg
				} else {
					ch <- resp.Result
				}
			}
		} else if peek.Method == "textDocument/publishDiagnostics" {
			var notif lspNotification
			if err := json.Unmarshal(body, &notif); err != nil {
				continue
			}
			c.handlePublishDiagnostics(notif.Params)
		}
	}
}
```

- [ ] **Step 3: Fix handlePublishDiagnostics to use defer**

Replace the `handlePublishDiagnostics` method (lines 442-454) with:

```go
func (c *LSPClient) handlePublishDiagnostics(raw json.RawMessage) {
	var params struct {
		URI         string       `json:"uri"`
		Diagnostics []Diagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return
	}
	c.diagMu.Lock()
	defer c.diagMu.Unlock()
	c.diagnostics[params.URI] = params.Diagnostics
}
```

- [ ] **Step 4: Run existing tests**

Run: `go test -v -count=1 ./internal/agents/tools/...`
Expected: All tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agents/tools/lsp.go
git commit -m "fix: extract dispatchPending/drainAllPending with defer for readLoop mutex safety"
```

---

### Task 5: Write concurrency stress tests for fixed code

**Files:**
- Create: `internal/shared/messaging/messaging_race_test.go`
- Create: `internal/agents/tools/lsp_race_test.go`

- [ ] **Step 1: Write messaging concurrency test**

Create `internal/shared/messaging/messaging_race_test.go`:

```go
package messaging_test

import (
	"sync"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/messaging"
)

func TestInMemoryBroker_ConcurrentPublishSubscribe(t *testing.T) {
	broker := messaging.NewInMemoryBroker()
	defer broker.Close()

	const goroutines = 50
	const messagesPerGoroutine = 100

	var received sync.Map
	var totalReceived int64
	var mu sync.Mutex

	// Subscribe to topic before publishing.
	if err := broker.Subscribe("stress", func(data []byte) {
		mu.Lock()
		totalReceived++
		mu.Unlock()
		received.Store(string(data), true)
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Hammer the broker from many goroutines simultaneously.
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < messagesPerGoroutine; i++ {
				data := []byte("msg")
				_ = broker.Publish("stress", data)
			}
		}(g)
	}

	wg.Wait()

	mu.Lock()
	got := totalReceived
	mu.Unlock()

	want := int64(goroutines * messagesPerGoroutine)
	if got != want {
		t.Errorf("received %d messages, want %d", got, want)
	}
}

func TestInMemoryBroker_ConcurrentCloseWhilePublishing(t *testing.T) {
	broker := messaging.NewInMemoryBroker()

	if err := broker.Subscribe("topic", func(data []byte) {}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	var wg sync.WaitGroup

	// Publishers.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = broker.Publish("topic", []byte("data"))
			}
		}()
	}

	// Close in the middle of publishing.
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = broker.Close()
	}()

	wg.Wait()
	// No panic, no deadlock — test passes if it completes.
}
```

- [ ] **Step 2: Run messaging race test with -race**

Run: `go test -v -count=1 -race ./internal/shared/messaging/...`
Expected: All tests PASS with no race conditions detected

- [ ] **Step 3: Write LSP concurrency test**

Create `internal/agents/tools/lsp_race_test.go`:

```go
package tools_test

import (
	"sync"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/agents/tools"
)

func TestLSPRegistry_ConcurrentAccess(t *testing.T) {
	registry := tools.NewLSPRegistry()

	var wg sync.WaitGroup

	// Concurrent registration.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_ = registry.Languages()
		}(i)
	}

	wg.Wait()
	// No panic, no deadlock — test passes if it completes.
}
```

- [ ] **Step 4: Run LSP race test with -race**

Run: `go test -v -count=1 -race ./internal/agents/tools/...`
Expected: All tests PASS with no race conditions detected

- [ ] **Step 5: Commit**

```bash
git add internal/shared/messaging/messaging_race_test.go internal/agents/tools/lsp_race_test.go
git commit -m "test: add concurrency stress tests for messaging broker and LSP registry"
```

---

### Task 6: Enable race detection in Makefile

**Files:**
- Modify: `Makefile:24-31`

- [ ] **Step 1: Add -race to test targets and add test-race target**

In `Makefile`, replace the test section (lines 23-31) with:

```makefile
# ── Test ─────────────────────────────────────────────────
test-unit:
	go test -v -count=1 -race -coverprofile=coverage-unit.out ./internal/...

test-integration:
	go test -v -count=1 -race ./tests/integration/

test-e2e:
	go test -v -count=1 -race -tags=e2e ./tests/integration/...

test-race:
	GOMAXPROCS=$$(nproc) go test -count=1 -race -p 1 ./internal/... ./pkg/... ./tests/...
```

Also add `test-race` to the `.PHONY` line at the top of the Makefile.

- [ ] **Step 2: Run test-unit with race detection**

Run: `make test-unit`
Expected: All tests PASS with `-race` enabled (no race conditions detected)

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "feat: enable race detection in all test targets, add test-race target"
```

---

### Task 7: Document MustGet panic pattern in Lazy submodule

**Files:**
- Modify: `submodules/Lazy/pkg/lazy/lazy.go:41-48`

- [ ] **Step 1: Read current doc comment**

Run: `go test -v -count=1 ./submodules/Lazy/...`
Expected: All existing tests PASS

- [ ] **Step 2: Expand MustGet doc comment**

In `submodules/Lazy/pkg/lazy/lazy.go`, replace lines 41-48:

```go
// MustGet returns the lazily-loaded value, panicking on error.
func (v *Value[T]) MustGet() T {
```

With:

```go
// MustGet returns the lazily-loaded value, panicking on error.
// This follows the Go "Must" convention (see template.Must, regexp.MustCompile).
// Use Get() instead in request handlers or goroutines where panics would
// crash the server — MustGet is intended for package-level initialisation only.
func (v *Value[T]) MustGet() T {
```

- [ ] **Step 3: Run tests**

Run: `go test -v -count=1 ./submodules/Lazy/...`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add submodules/Lazy/pkg/lazy/lazy.go
git commit -m "docs: clarify MustGet panic convention and safe usage in Lazy submodule"
```

---

### Task 8: Add .golangci.yml configuration

**Files:**
- Create: `.golangci.yml`

- [ ] **Step 1: Create .golangci.yml**

Create `.golangci.yml` at project root:

```yaml
run:
  timeout: 5m
  modules-download-mode: readonly

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
    - path: mock_
      linters:
        - unused
```

- [ ] **Step 2: Run lint with new config**

Run: `golangci-lint run ./...`
Expected: Lint passes (or only pre-existing warnings unrelated to our changes)

- [ ] **Step 3: Commit**

```bash
git add .golangci.yml
git commit -m "feat: add .golangci.yml with security-focused linter configuration"
```

---

### Task 9: Add concurrency challenge bank

**Files:**
- Create: `challenges/banks/safety/concurrency.yaml`

- [ ] **Step 1: Create safety challenge bank directory**

Run: `mkdir -p challenges/banks/safety`

- [ ] **Step 2: Create concurrency challenge bank**

Create `challenges/banks/safety/concurrency.yaml`:

```yaml
name: Concurrency Safety
description: Validates the system remains stable under concurrent request pressure
category: safety
priority: high

challenges:
  - name: concurrent_chat_completions
    description: 100 parallel chat completion requests should all succeed or return proper errors
    steps:
      - method: POST
        path: /v1/chat/completions
        concurrent: 100
        body:
          model: "auto"
          messages:
            - role: user
              content: "Say hello"
        assertions:
          - type: status_one_of
            values: [200, 429]
          - type: no_5xx

  - name: concurrent_model_listing
    description: 50 parallel model list requests under concurrent chat load
    steps:
      - method: GET
        path: /v1/models
        concurrent: 50
        assertions:
          - type: status
            value: 200
          - type: response_time_ms
            max: 500

  - name: concurrent_health_checks
    description: Health endpoint remains responsive under load
    steps:
      - method: GET
        path: /internal/health
        concurrent: 200
        assertions:
          - type: status
            value: 200
          - type: response_time_ms
            max: 100
```

- [ ] **Step 3: Commit**

```bash
git add challenges/banks/safety/concurrency.yaml
git commit -m "test: add concurrency safety challenge bank"
```

---

### Task 10: Final verification

- [ ] **Step 1: Run full test suite with race detection**

Run: `make test-unit`
Expected: All tests PASS with `-race` flag

- [ ] **Step 2: Run lint**

Run: `make lint`
Expected: Lint passes

- [ ] **Step 3: Run integration tests**

Run: `make test-integration`
Expected: All integration tests PASS with `-race` flag

- [ ] **Step 4: Verify no regressions**

Run: `make coverage`
Expected: Coverage meets or exceeds 85% threshold (should be at or above 89.5% baseline)
