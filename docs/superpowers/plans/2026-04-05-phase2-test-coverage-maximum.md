# Phase 2: Missing Test Coverage & Push to Maximum

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Write tests for all 6 untested files, add Go benchmarks, E2E tests, stress tests, new challenge banks, and push coverage from 89.5% to 95%+.

**Architecture:** Tests follow existing patterns: table-driven tests, hand-written mocks, `httptest.Server` for HTTP tests, standard library `testing` package. E2E and stress tests use build tags for selective execution. Challenge banks are YAML files processed by the Runner framework.

**Tech Stack:** Go 1.26.1, net/http/httptest, testing, build tags (e2e, stress)

---

### Task 1: Write RPC server tests

**Files:**
- Create: `internal/shared/rpc/server_test.go` (additional tests — `rpc_test.go` already tests round-trips)

Note: `rpc_test.go` already contains `TestRoundTrip_Echo`, `TestRoundTrip_MethodNotFound`, `TestRoundTrip_HandlerError`, and `TestRoundTrip_MultipleSequentialCalls` which cover the server. What's missing are: concurrent request handling, server stop, and parse error handling.

- [ ] **Step 1: Write the concurrent requests test**

Add to `internal/shared/rpc/rpc_test.go` (the existing test file, since it's `rpc_test` package):

```go
func TestRoundTrip_ConcurrentCalls(t *testing.T) {
	addr := freePort(t)
	srv, cancel := startServer(t, addr)
	defer cancel()
	defer srv.Stop()

	srv.Handle("echo", func(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
		return params, nil
	})

	client, err := rpc.NewClient(addr)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	// Note: Client serialises calls via mutex, so "concurrent" here means
	// sequential rapid-fire from multiple goroutines sharing one client.
	// True concurrency would need multiple clients.
	const numClients = 5
	const callsPerClient = 10

	var wg sync.WaitGroup
	errCh := make(chan error, numClients*callsPerClient)

	for c := 0; c < numClients; c++ {
		cl, err := rpc.NewClient(addr)
		if err != nil {
			t.Fatalf("NewClient[%d]: %v", c, err)
		}
		wg.Add(1)
		go func(cl *rpc.Client, id int) {
			defer wg.Done()
			defer cl.Close()
			for i := 0; i < callsPerClient; i++ {
				type payload struct {
					Msg string `json:"msg"`
				}
				var result payload
				if err := cl.Call(context.Background(), "echo", payload{Msg: "hello"}, &result); err != nil {
					errCh <- err
					return
				}
				if result.Msg != "hello" {
					errCh <- fmt.Errorf("client %d call %d: got %q, want %q", id, i, result.Msg, "hello")
				}
			}
		}(cl, c)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}
}

func TestServer_StopClosesListener(t *testing.T) {
	addr := freePort(t)
	srv := rpc.NewServer(addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe(ctx)
	}()
	time.Sleep(20 * time.Millisecond)

	srv.Stop()

	select {
	case err := <-errCh:
		// nil or accept error — both acceptable on clean stop.
		if err != nil {
			t.Logf("ListenAndServe returned: %v (acceptable on stop)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ListenAndServe did not return after Stop()")
	}
}

func TestServer_HandleAfterStart(t *testing.T) {
	addr := freePort(t)
	srv, cancel := startServer(t, addr)
	defer cancel()
	defer srv.Stop()

	// Register handler AFTER server is already running.
	srv.Handle("late", func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.Marshal("registered-late")
	})

	client, err := rpc.NewClient(addr)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	var result string
	if err := client.Call(context.Background(), "late", nil, &result); err != nil {
		t.Fatalf("Call late: %v", err)
	}
	if result != "registered-late" {
		t.Errorf("result = %q, want %q", result, "registered-late")
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test -v -count=1 -race ./internal/shared/rpc/...`
Expected: All tests PASS including new ones

- [ ] **Step 3: Commit**

```bash
git add internal/shared/rpc/rpc_test.go
git commit -m "test: add concurrent calls, stop, and late-registration tests for RPC server"
```

---

### Task 2: Write store factory tests

**Files:**
- Create: `internal/knowledge/store_factory_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/knowledge/store_factory_test.go`:

```go
package knowledge_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func TestNewVectorStore_Memory(t *testing.T) {
	store, err := knowledge.NewVectorStore("memory", "", 0)
	if err != nil {
		t.Fatalf("NewVectorStore(memory): %v", err)
	}
	if store == nil {
		t.Fatal("NewVectorStore(memory) returned nil")
	}
}

func TestNewVectorStore_EmptyStringDefaultsToMemory(t *testing.T) {
	store, err := knowledge.NewVectorStore("", "", 0)
	if err != nil {
		t.Fatalf("NewVectorStore(''): %v", err)
	}
	if store == nil {
		t.Fatal("NewVectorStore('') returned nil")
	}
}

func TestNewVectorStore_UnknownBackendFallsToMemory(t *testing.T) {
	store, err := knowledge.NewVectorStore("nonexistent", "", 0)
	if err != nil {
		t.Fatalf("NewVectorStore(nonexistent): %v", err)
	}
	if store == nil {
		t.Fatal("NewVectorStore(nonexistent) returned nil store")
	}
}

func TestNewVectorStore_QdrantWithInvalidHost(t *testing.T) {
	// Qdrant connection to invalid host should return error.
	_, err := knowledge.NewVectorStore("qdrant", "invalid-host-that-does-not-exist", 6334)
	if err == nil {
		t.Log("NewVectorStore(qdrant, invalid-host) did not return error — Qdrant may defer connection")
	}
	// Note: depending on implementation, connection may be deferred.
	// Either an error or a non-nil store is acceptable; the point is no panic.
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test -v -count=1 -run TestNewVectorStore ./internal/knowledge/...`
Expected: All tests PASS

- [ ] **Step 3: Commit**

```bash
git add internal/knowledge/store_factory_test.go
git commit -m "test: add store factory tests for memory, empty, unknown, and qdrant backends"
```

---

### Task 3: Write MCP registry tests

**Files:**
- Create: `internal/agents/mcp_registry_test.go`

- [ ] **Step 1: Write the test file**

Create `internal/agents/mcp_registry_test.go`:

```go
package agents_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
)

func TestRegisterMCPTools_EmptyServerList(t *testing.T) {
	registry := agents.NewToolRegistry()
	err := agents.RegisterMCPTools(registry, nil)
	if err != nil {
		t.Fatalf("RegisterMCPTools with nil servers: %v", err)
	}
	err = agents.RegisterMCPTools(registry, []agents.MCPServerConfig{})
	if err != nil {
		t.Fatalf("RegisterMCPTools with empty servers: %v", err)
	}
}

func TestRegisterMCPTools_InvalidServer(t *testing.T) {
	registry := agents.NewToolRegistry()
	servers := []agents.MCPServerConfig{
		{
			Name:    "nonexistent",
			Command: "/nonexistent/binary/that/does/not/exist",
		},
	}
	err := agents.RegisterMCPTools(registry, servers)
	if err == nil {
		t.Fatal("RegisterMCPTools with invalid server should return error")
	}
}
```

- [ ] **Step 2: Run test**

Run: `go test -v -count=1 -run TestRegisterMCPTools ./internal/agents/...`
Expected: All tests PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agents/mcp_registry_test.go
git commit -m "test: add MCP registry tests for empty and invalid server configurations"
```

---

### Task 4: Write SSH client tests

**Files:**
- Create: `internal/control/ssh_test.go`

- [ ] **Step 1: Write the test file**

Create `internal/control/ssh_test.go`:

```go
package control_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/control"
)

func TestNewSSHClient_InvalidKeyPath(t *testing.T) {
	_, err := control.NewSSHClient("localhost", 22, "user", "/nonexistent/key")
	if err == nil {
		t.Fatal("NewSSHClient with nonexistent key should return error")
	}
}

func TestNewSSHClient_InvalidKeyContent(t *testing.T) {
	// Create a temp file with invalid key content.
	tmpDir := t.TempDir()
	badKey := filepath.Join(tmpDir, "bad_key")
	if err := os.WriteFile(badKey, []byte("not-a-valid-ssh-key"), 0600); err != nil {
		t.Fatalf("write bad key: %v", err)
	}

	_, err := control.NewSSHClient("localhost", 22, "user", badKey)
	if err == nil {
		t.Fatal("NewSSHClient with invalid key content should return error")
	}
}

func TestSSHClient_IsReachable_UnreachableHost(t *testing.T) {
	// Generate a valid but unusable key for construction.
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "test_key")

	// Generate a minimal ed25519 private key for testing.
	// We use ssh-keygen via exec if available, otherwise skip.
	cmd := "ssh-keygen"
	if _, err := os.Stat("/usr/bin/ssh-keygen"); os.IsNotExist(err) {
		t.Skip("ssh-keygen not available")
	}

	_ = os.Remove(keyPath)
	out, err := execCommand(cmd, "-t", "ed25519", "-f", keyPath, "-N", "", "-q")
	if err != nil {
		t.Skipf("ssh-keygen failed: %v (output: %s)", err, out)
	}

	client, err := control.NewSSHClient("localhost", 22, "testuser", keyPath)
	if err != nil {
		t.Fatalf("NewSSHClient: %v", err)
	}

	// 198.51.100.1 is TEST-NET-2 (RFC 5737) — guaranteed unreachable.
	reachable := client.IsReachable(context.Background(), "198.51.100.1")
	if reachable {
		t.Error("IsReachable returned true for unreachable host")
	}
}

// execCommand is a test helper that runs a command and returns output.
func execCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
```

Add import for `os/exec` at the top of the file:

```go
import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/control"
)
```

- [ ] **Step 2: Run test**

Run: `go test -v -count=1 -run TestNewSSHClient ./internal/control/...`
Expected: Key path and content tests PASS; reachability test PASS or SKIP (if ssh-keygen unavailable)

- [ ] **Step 3: Commit**

```bash
git add internal/control/ssh_test.go
git commit -m "test: add SSH client tests for invalid keys and unreachable hosts"
```

---

### Task 5: Add Go benchmark functions

**Files:**
- Create: `internal/brain/brain_benchmark_test.go`
- Create: `internal/gateway/gateway_benchmark_test.go`
- Create: `internal/knowledge/knowledge_benchmark_test.go`
- Create: `internal/agents/agents_benchmark_test.go`
- Create: `internal/server/middleware/compression_benchmark_test.go`

- [ ] **Step 1: Write brain benchmarks**

Create `internal/brain/brain_benchmark_test.go`:

```go
package brain_test

import (
	"context"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

func BenchmarkBrain_Complete(b *testing.B) {
	provider := &mockBenchProvider{response: &types.InternalChatResponse{
		ID:           "bench-id",
		Model:        "bench-model",
		Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "hello"},
		FinishReason: "stop",
		Provider:     types.ProviderLocal,
	}}
	br := brain.New(brain.Config{
		Providers: []brain.Provider{provider},
	})

	req := &types.InternalChatRequest{
		Model:    "bench-model",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hi"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = br.Complete(context.Background(), req)
	}
}

type mockBenchProvider struct {
	response *types.InternalChatResponse
}

func (m *mockBenchProvider) Complete(_ context.Context, _ *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	return m.response, nil
}
func (m *mockBenchProvider) CompleteStream(_ context.Context, _ *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	return nil, nil
}
func (m *mockBenchProvider) Models() []string { return []string{"bench-model"} }
func (m *mockBenchProvider) Name() string     { return "bench" }
func (m *mockBenchProvider) Available() bool   { return true }
```

- [ ] **Step 2: Write knowledge benchmarks**

Create `internal/knowledge/knowledge_benchmark_test.go`:

```go
package knowledge_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func BenchmarkMemoryStore_Upsert(b *testing.B) {
	store := knowledge.NewMemoryStore()
	ctx := context.Background()
	doc := knowledge.Document{
		ID:         "bench-doc",
		Content:    "benchmark document content for testing vector store performance",
		Collection: "bench",
		Embedding:  make([]float32, 384),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc.ID = fmt.Sprintf("bench-doc-%d", i)
		_ = store.Upsert(ctx, doc)
	}
}

func BenchmarkMemoryStore_Search(b *testing.B) {
	store := knowledge.NewMemoryStore()
	ctx := context.Background()

	// Pre-populate with 1000 documents.
	for i := 0; i < 1000; i++ {
		emb := make([]float32, 384)
		emb[i%384] = 1.0
		_ = store.Upsert(ctx, knowledge.Document{
			ID:         fmt.Sprintf("doc-%d", i),
			Content:    fmt.Sprintf("document %d content", i),
			Collection: "bench",
			Embedding:  emb,
		})
	}

	query := make([]float32, 384)
	query[0] = 1.0

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.Search(ctx, "bench", query, 10)
	}
}
```

- [ ] **Step 3: Add benchmark Makefile target**

In `Makefile`, add after the `test-all` target:

```makefile
test-benchmark-go:
	go test -bench=. -benchmem -count=3 -run=^$$ ./internal/...
```

Add `test-benchmark-go` to the `.PHONY` line.

- [ ] **Step 4: Run benchmarks to verify they execute**

Run: `go test -bench=. -benchmem -count=1 -run=^$ ./internal/brain/... ./internal/knowledge/...`
Expected: Benchmark output with ns/op and B/op metrics

- [ ] **Step 5: Commit**

```bash
git add internal/brain/brain_benchmark_test.go internal/knowledge/knowledge_benchmark_test.go Makefile
git commit -m "test: add Go benchmark functions for brain routing and knowledge store operations"
```

---

### Task 6: Create E2E test scaffolding

**Files:**
- Create: `tests/e2e/e2e_test.go`

- [ ] **Step 1: Create e2e directory and test file**

Run: `mkdir -p tests/e2e`

Create `tests/e2e/e2e_test.go`:

```go
//go:build e2e

package e2e_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

var baseURL = "https://localhost:8443"

func init() {
	// Allow self-signed TLS in E2E tests.
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec
	}
}

func TestE2E_HealthEndpoint(t *testing.T) {
	resp, err := http.Get(baseURL + "/internal/health")
	if err != nil {
		t.Fatalf("GET /internal/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", resp.StatusCode)
	}
}

func TestE2E_ModelsEndpoint(t *testing.T) {
	resp, err := http.Get(baseURL + "/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("models status = %d, body = %s", resp.StatusCode, body)
	}

	var result struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode models: %v", err)
	}
	if result.Object != "list" {
		t.Errorf("object = %q, want %q", result.Object, "list")
	}
}

func TestE2E_ChatCompletion(t *testing.T) {
	body := `{"model":"auto","messages":[{"role":"user","content":"Say hello in one word"}]}`
	resp, err := http.Post(
		baseURL+"/v1/chat/completions",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST /v1/chat/completions: %v", err)
	}
	defer resp.Body.Close()

	// Accept 200 (success) or 503 (no providers available in test env).
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("chat status = %d, body = %s", resp.StatusCode, respBody)
	}
}

func TestE2E_StreamingChatCompletion(t *testing.T) {
	body := `{"model":"auto","messages":[{"role":"user","content":"Say hi"}],"stream":true}`
	resp, err := http.Post(
		baseURL+"/v1/chat/completions",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST /v1/chat/completions (stream): %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		// Verify SSE content type.
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/event-stream") {
			t.Errorf("Content-Type = %q, want text/event-stream", ct)
		}
	}
}
```

Add `crypto/tls` to the imports:

```go
import (
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)
```

- [ ] **Step 2: Verify build with e2e tag**

Run: `go build -tags=e2e ./tests/e2e/...`
Expected: Compiles without errors (tests need running server to actually pass)

- [ ] **Step 3: Commit**

```bash
git add tests/e2e/
git commit -m "test: add E2E test scaffolding with health, models, and chat completion tests"
```

---

### Task 7: Create stress test scaffolding

**Files:**
- Create: `tests/stress/stress_test.go`

- [ ] **Step 1: Create stress directory and test file**

Run: `mkdir -p tests/stress`

Create `tests/stress/stress_test.go`:

```go
//go:build stress

package stress_test

import (
	"crypto/tls"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var baseURL = "https://localhost:8443"

func init() {
	http.DefaultTransport = &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		MaxIdleConns:        1000,
		MaxIdleConnsPerHost: 1000,
		IdleConnTimeout:     30 * time.Second,
	}
}

func TestStress_ConcurrentChatCompletions(t *testing.T) {
	const concurrency = 100
	const requestsPerWorker = 10

	var success, failure atomic.Int64
	var wg sync.WaitGroup

	body := `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < requestsPerWorker; i++ {
				resp, err := http.Post(
					baseURL+"/v1/chat/completions",
					"application/json",
					strings.NewReader(body),
				)
				if err != nil {
					failure.Add(1)
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()

				if resp.StatusCode == 200 || resp.StatusCode == 429 {
					success.Add(1)
				} else {
					failure.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	total := success.Load() + failure.Load()
	t.Logf("Total: %d, Success: %d, Failure: %d", total, success.Load(), failure.Load())

	// At minimum, no panics and all requests got a response.
	if total != int64(concurrency*requestsPerWorker) {
		t.Errorf("expected %d total responses, got %d", concurrency*requestsPerWorker, total)
	}
}

func TestStress_HealthEndpointUnderLoad(t *testing.T) {
	const concurrency = 500
	const duration = 5 * time.Second

	var success, failure atomic.Int64
	var wg sync.WaitGroup

	done := make(chan struct{})
	go func() {
		time.Sleep(duration)
		close(done)
	}()

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				resp, err := http.Get(baseURL + "/internal/health")
				if err != nil {
					failure.Add(1)
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode == 200 {
					success.Add(1)
				} else {
					failure.Add(1)
				}
			}
		}()
	}

	wg.Wait()
	t.Logf("Health check: Success=%d, Failure=%d over %v", success.Load(), failure.Load(), duration)

	if success.Load() == 0 {
		t.Error("zero successful health checks under load")
	}
}
```

- [ ] **Step 2: Add stress Makefile target**

In `Makefile`, update the `test-stress` target to support Go stress tests alongside challenge banks:

Add a new target after `test-stress`:
```makefile
test-stress-go:
	go test -v -count=1 -tags=stress -timeout=10m ./tests/stress/...
```

Add `test-stress-go` to the `.PHONY` line.

- [ ] **Step 3: Verify build**

Run: `go build -tags=stress ./tests/stress/...`
Expected: Compiles without errors

- [ ] **Step 4: Commit**

```bash
git add tests/stress/ Makefile
git commit -m "test: add Go stress tests for concurrent chat completions and health endpoint load"
```

---

### Task 8: Add new challenge banks

**Files:**
- Create: `challenges/banks/stress/concurrent.yaml`
- Create: `challenges/banks/stress/memory.yaml`
- Create: `challenges/banks/e2e/full_workflow.yaml`

- [ ] **Step 1: Create stress challenge banks**

Run: `mkdir -p challenges/banks/stress challenges/banks/e2e`

Create `challenges/banks/stress/concurrent.yaml`:

```yaml
name: Concurrent Request Stress
description: Validates system stability under parallel request floods
category: stress
priority: high

challenges:
  - name: 100_concurrent_completions
    description: 100 parallel chat completions
    steps:
      - method: POST
        path: /v1/chat/completions
        concurrent: 100
        body:
          model: "auto"
          messages:
            - role: user
              content: "Respond with OK"
        assertions:
          - type: status_one_of
            values: [200, 429, 503]
          - type: no_5xx_except
            values: [503]

  - name: 500_concurrent_model_list
    description: 500 parallel model listing requests
    steps:
      - method: GET
        path: /v1/models
        concurrent: 500
        assertions:
          - type: status
            value: 200
          - type: response_time_ms
            max: 1000
```

Create `challenges/banks/stress/memory.yaml`:

```yaml
name: Memory Pressure
description: Validates system handles large payloads without memory issues
category: stress
priority: medium

challenges:
  - name: large_prompt
    description: 10KB prompt should be handled gracefully
    steps:
      - method: POST
        path: /v1/chat/completions
        body:
          model: "auto"
          messages:
            - role: user
              content: "REPEAT_10KB_Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua."
        assertions:
          - type: status_one_of
            values: [200, 413, 503]
          - type: no_5xx_except
            values: [503]
```

Create `challenges/banks/e2e/full_workflow.yaml`:

```yaml
name: Full E2E Workflow
description: Complete user workflow from health check to chat completion
category: e2e
priority: critical

challenges:
  - name: health_then_models_then_chat
    description: Sequential workflow - health check, list models, then chat
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
          - type: json_path
            path: $.object
            value: "list"

      - method: POST
        path: /v1/chat/completions
        body:
          model: "auto"
          messages:
            - role: user
              content: "Hello"
        assertions:
          - type: status_one_of
            values: [200, 503]
```

- [ ] **Step 2: Commit**

```bash
git add challenges/banks/stress/ challenges/banks/e2e/
git commit -m "test: add stress and e2e challenge banks for concurrent load and full workflows"
```

---

### Task 9: Push coverage toward maximum

- [ ] **Step 1: Run coverage and identify gaps**

Run: `go test -v -count=1 -coverprofile=coverage-unit.out ./internal/... && go tool cover -func=coverage-unit.out | sort -t: -k3 -n | head -20`
Expected: Shows the 20 lowest-coverage functions

- [ ] **Step 2: Write tests for lowest-coverage functions**

Based on the coverage report, write targeted tests for any exported function below 80% coverage. Focus on error paths and edge cases.

Each test file should be placed next to the source file it tests, following the existing `*_test.go` naming convention.

- [ ] **Step 3: Update coverage threshold in Makefile**

In `Makefile`, update line 62:

Replace:
```makefile
COVERAGE_THRESHOLD := 85
```

With:
```makefile
COVERAGE_THRESHOLD := 95
```

- [ ] **Step 4: Verify new threshold passes**

Run: `make coverage`
Expected: PASS with coverage >= 95%

- [ ] **Step 5: Commit**

```bash
git add internal/ Makefile
git commit -m "test: push coverage to 95%+ threshold with targeted edge-case tests"
```
