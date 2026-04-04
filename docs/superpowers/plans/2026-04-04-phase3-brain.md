# Phase 3: Brain Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Brain layer that provides LLM coordination between the Gateway (HTTP handlers) and actual LLM backends (llama.cpp local, OpenAI API, Anthropic API). Replace the stub/canned responses in the Gateway with real provider-backed completions. All providers use plain `net/http` to call their respective APIs; the vasic-digital LLMProvider module is added as a submodule but not wired in yet (future refinement).

**Architecture:** The Brain defines a `Provider` interface implemented by three backends (llama.cpp, OpenAI, Anthropic). A `Router` selects the best provider per request based on model name, explicit provider field, and fallback chains. The `Brain` service ties providers and router together, exposing `Complete`, `CompleteStream`, and `Models` methods. The Gateway handlers accept a `*Brain` and delegate to it instead of returning hardcoded responses.

**Tech Stack:** Go 1.26+, Gin Gonic, `net/http`, `net/http/httptest` (for testing), vasic-digital modules (LLMProvider, Optimization, Cache, Recovery -- added as submodules, not yet integrated)

**Spec Reference:** `docs/superpowers/specs/2026-04-04-helixllm-master-design.md` -- Section 6 (Brain Layer Design)

**Important notes:**
- Providers use `net/http` to call external APIs. They do NOT use the vasic-digital LLMProvider module directly -- that module's API may be complex and will be integrated as a later refinement.
- For unit tests, `httptest.NewServer` mocks the external APIs.
- Optimization (semantic cache), Cache, and Recovery (circuit breaker) modules from the spec will be integrated in a follow-up refinement task, not in the initial wiring.

---

## File Structure

```
helixllm/
  cmd/helixllm/
    main.go                                Updated to create Brain and pass to gateway
  internal/
    brain/
      provider.go                          Provider interface definition
      provider_test.go
      llamacpp.go                          llama.cpp provider (HTTP to local server)
      llamacpp_test.go
      openai_provider.go                   OpenAI API provider
      openai_provider_test.go
      anthropic_provider.go                Anthropic API provider
      anthropic_provider_test.go
      router.go                            Intelligent LLM router
      router_test.go
      brain.go                             Brain service (ties it all together)
      brain_test.go
    gateway/
      router.go                            Updated to accept Brain
      openai.go                            Updated to use Brain instead of stubs
      anthropic.go                         Updated to use Brain instead of stubs
  submodules/
    LLMProvider/                           digital.vasic.llmprovider (added, not yet wired)
    Optimization/                          digital.vasic.optimization (added, not yet wired)
    Cache/                                 digital.vasic.cache (added, not yet wired)
    Recovery/                              digital.vasic.recovery (added, not yet wired)
  go.mod                                   Updated with new submodules + replace directives
  go.sum
```

---

### Task 1: Add Phase 3 Git Submodules

**Files:**
- Modify: `.gitmodules`
- Modify: `go.mod`
- Create: `submodules/` entries for LLMProvider, Optimization, Cache, Recovery

- [ ] **Step 1: Add Brain layer submodules from vasic-digital (GitHub)**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
git submodule add git@github.com:vasic-digital/LLMProvider.git submodules/LLMProvider
git submodule add git@github.com:vasic-digital/Optimization.git submodules/Optimization
git submodule add git@github.com:vasic-digital/Cache.git submodules/Cache
git submodule add git@github.com:vasic-digital/Recovery.git submodules/Recovery
```

Expected: each submodule cloned into `submodules/`, `.gitmodules` updated with 4 new entries.

- [ ] **Step 2: Add replace directives to go.mod**

Add these `replace` directives to the existing `replace` block in `go.mod`:

```
replace (
    // ... existing Phase 1 + Phase 2 replacements ...
    digital.vasic.llmprovider => ./submodules/LLMProvider
    digital.vasic.optimization => ./submodules/Optimization
    digital.vasic.cache => ./submodules/Cache
    digital.vasic.recovery => ./submodules/Recovery
)
```

Also add to the `require` block:

```
require (
    // ... existing Phase 1 + Phase 2 requirements ...
    digital.vasic.llmprovider v0.0.0
    digital.vasic.optimization v0.0.0
    digital.vasic.cache v0.0.0
    digital.vasic.recovery v0.0.0
)
```

**Note:** These modules are added for availability but are NOT imported by any Go code in this phase. They will be integrated in a follow-up refinement task.

- [ ] **Step 3: Tidy modules**

```bash
go mod tidy
```

Expected: `go.sum` updated, all new dependencies resolved.

- [ ] **Step 4: Verify build**

```bash
go build ./...
```

Expected: builds successfully with all new submodules resolved.

- [ ] **Step 5: Commit**

```bash
git add .gitmodules submodules/ go.mod go.sum
git commit -m "feat: add Phase 3 Brain submodules (LLMProvider, Optimization, Cache, Recovery)"
```

---

### Task 2: Provider Interface

**Files:**
- Create: `internal/brain/provider.go`
- Create: `internal/brain/provider_test.go`

- [ ] **Step 1: Write failing tests for Provider interface**

Create `internal/brain/provider_test.go`:

```go
package brain_test

import (
	"context"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// mockProvider is a test double that implements Provider.
type mockProvider struct {
	name      string
	models    []string
	available bool
	response  *types.InternalChatResponse
	chunks    []types.StreamChunk
	err       error
}

func (m *mockProvider) Complete(_ context.Context, _ *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func (m *mockProvider) CompleteStream(_ context.Context, _ *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan types.StreamChunk, len(m.chunks))
	for _, c := range m.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (m *mockProvider) Models() []string  { return m.models }
func (m *mockProvider) Name() string      { return m.name }
func (m *mockProvider) Available() bool   { return m.available }

func TestProviderInterfaceSatisfied(t *testing.T) {
	// Verify that mockProvider satisfies the Provider interface at compile time.
	var _ brain.Provider = (*mockProvider)(nil)
}

func TestProviderComplete(t *testing.T) {
	p := &mockProvider{
		name:      "test",
		available: true,
		response: &types.InternalChatResponse{
			ID:           "test-1",
			Model:        "test-model",
			Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "Hello"},
			FinishReason: "stop",
			Provider:     types.ProviderLocal,
		},
	}

	resp, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "test-model",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.ID != "test-1" {
		t.Errorf("ID = %q, want %q", resp.ID, "test-1")
	}
	if resp.Message.Content != "Hello" {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "Hello")
	}
}

func TestProviderCompleteStream(t *testing.T) {
	p := &mockProvider{
		name:      "test",
		available: true,
		chunks: []types.StreamChunk{
			{Content: "Hello"},
			{Content: " world", FinishReason: "stop"},
		},
	}

	ch, err := p.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model:    "test-model",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("CompleteStream returned error: %v", err)
	}

	var chunks []types.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if chunks[0].Content != "Hello" {
		t.Errorf("chunks[0].Content = %q, want %q", chunks[0].Content, "Hello")
	}
	if chunks[1].FinishReason != "stop" {
		t.Errorf("chunks[1].FinishReason = %q, want %q", chunks[1].FinishReason, "stop")
	}
}

func TestProviderMetadata(t *testing.T) {
	p := &mockProvider{
		name:      "test-provider",
		models:    []string{"model-a", "model-b"},
		available: true,
	}

	if p.Name() != "test-provider" {
		t.Errorf("Name() = %q, want %q", p.Name(), "test-provider")
	}
	if len(p.Models()) != 2 {
		t.Errorf("Models() length = %d, want 2", len(p.Models()))
	}
	if !p.Available() {
		t.Error("Available() = false, want true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/brain/ -v
```

Expected: FAIL -- package `brain` does not exist yet.

- [ ] **Step 3: Implement Provider interface**

Create `internal/brain/provider.go`:

```go
// Package brain provides the LLM coordination layer for HelixLLM.
//
// It defines the Provider interface that all LLM backends implement,
// a Router that selects the best provider for each request, and the
// Brain service that ties everything together.
package brain

import (
	"context"

	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// Provider is the interface that all LLM backends must implement.
// Each provider wraps a specific LLM API (llama.cpp local, OpenAI, Anthropic)
// and translates between HelixLLM's internal types and the provider's API format.
type Provider interface {
	// Complete sends a chat completion request and returns the full response.
	Complete(ctx context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error)

	// CompleteStream sends a streaming chat completion request and returns a
	// channel that emits StreamChunks. The channel is closed when the stream
	// ends or an error occurs.
	CompleteStream(ctx context.Context, req *types.InternalChatRequest) (<-chan types.StreamChunk, error)

	// Models returns the list of model IDs this provider supports.
	Models() []string

	// Name returns the provider's display name (e.g. "llamacpp", "openai", "anthropic").
	Name() string

	// Available returns true if the provider is currently reachable and ready
	// to serve requests.
	Available() bool
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/brain/ -v -count=1
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/brain/provider.go internal/brain/provider_test.go
git commit -m "feat: define Brain Provider interface for LLM backends"
```

---

### Task 3: llama.cpp Provider

**Files:**
- Create: `internal/brain/llamacpp.go`
- Create: `internal/brain/llamacpp_test.go`

- [ ] **Step 1: Write failing tests for llama.cpp provider**

Create `internal/brain/llamacpp_test.go`:

```go
package brain_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

func TestLlamaCppProvider_Satisfies(t *testing.T) {
	var _ brain.Provider = (*brain.LlamaCppProvider)(nil)
}

func TestLlamaCppProvider_Name(t *testing.T) {
	p := brain.NewLlamaCppProvider("http://localhost:8080", []string{"llama-3.1-70b"})
	if p.Name() != "llamacpp" {
		t.Errorf("Name() = %q, want %q", p.Name(), "llamacpp")
	}
}

func TestLlamaCppProvider_Models(t *testing.T) {
	models := []string{"llama-3.1-70b", "llama-3.1-8b"}
	p := brain.NewLlamaCppProvider("http://localhost:8080", models)
	got := p.Models()
	if len(got) != 2 {
		t.Fatalf("Models() length = %d, want 2", len(got))
	}
	if got[0] != "llama-3.1-70b" {
		t.Errorf("Models()[0] = %q, want %q", got[0], "llama-3.1-70b")
	}
}

func TestLlamaCppProvider_Complete(t *testing.T) {
	// Mock llama.cpp's OpenAI-compatible /v1/chat/completions endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Decode request to verify it was sent correctly.
		var req api.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Model != "llama-3.1-70b" {
			t.Errorf("request model = %q, want %q", req.Model, "llama-3.1-70b")
		}

		resp := api.ChatCompletionResponse{
			ID:      "chatcmpl-local-001",
			Object:  "chat.completion",
			Created: 1700000000,
			Model:   "llama-3.1-70b",
			Choices: []api.ChatCompletionChoice{
				{
					Index: 0,
					Message: api.ChatMessage{
						Role:    "assistant",
						Content: "Hello from llama.cpp!",
					},
					FinishReason: "stop",
				},
			},
			Usage: &api.Usage{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := brain.NewLlamaCppProvider(srv.URL, []string{"llama-3.1-70b"})

	resp, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model: "llama-3.1-70b",
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.ID != "chatcmpl-local-001" {
		t.Errorf("ID = %q, want %q", resp.ID, "chatcmpl-local-001")
	}
	if resp.Message.Content != "Hello from llama.cpp!" {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "Hello from llama.cpp!")
	}
	if resp.Provider != types.ProviderLocal {
		t.Errorf("Provider = %q, want %q", resp.Provider, types.ProviderLocal)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("Usage.TotalTokens = %d, want 15", resp.Usage.TotalTokens)
	}
}

func TestLlamaCppProvider_CompleteStream(t *testing.T) {
	// Mock llama.cpp streaming endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req api.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if !req.Stream {
			t.Error("expected stream=true in request")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)

		tokens := []string{"Hello", " from", " llama.cpp!"}
		stopStr := "stop"
		for i, token := range tokens {
			chunk := api.ChatCompletionChunk{
				ID:      "chatcmpl-local-002",
				Object:  "chat.completion.chunk",
				Created: 1700000000,
				Model:   "llama-3.1-70b",
				Choices: []api.ChatCompletionChunkChoice{
					{
						Index: 0,
						Delta: api.ChatMessageDelta{
							Content: token,
						},
						FinishReason: func() *string {
							if i == len(tokens)-1 {
								return &stopStr
							}
							return nil
						}(),
					},
				},
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := brain.NewLlamaCppProvider(srv.URL, []string{"llama-3.1-70b"})

	ch, err := p.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model: "llama-3.1-70b",
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "Hello"},
		},
		Stream: true,
	})
	if err != nil {
		t.Fatalf("CompleteStream returned error: %v", err)
	}

	var chunks []types.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	if chunks[0].Content != "Hello" {
		t.Errorf("chunks[0].Content = %q, want %q", chunks[0].Content, "Hello")
	}
	if chunks[1].Content != " from" {
		t.Errorf("chunks[1].Content = %q, want %q", chunks[1].Content, " from")
	}
	if chunks[2].Content != " llama.cpp!" {
		t.Errorf("chunks[2].Content = %q, want %q", chunks[2].Content, " llama.cpp!")
	}
	if chunks[2].FinishReason != "stop" {
		t.Errorf("chunks[2].FinishReason = %q, want %q", chunks[2].FinishReason, "stop")
	}
}

func TestLlamaCppProvider_Available(t *testing.T) {
	// Available server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	p := brain.NewLlamaCppProvider(srv.URL, []string{"llama-3.1-70b"})
	if !p.Available() {
		t.Error("Available() = false, want true (server is up)")
	}

	// Unavailable server (closed).
	srv.Close()
	p2 := brain.NewLlamaCppProvider(srv.URL, []string{"llama-3.1-70b"})
	if p2.Available() {
		t.Error("Available() = true, want false (server is down)")
	}
}

func TestLlamaCppProvider_CompleteError(t *testing.T) {
	// Server that returns a 500 error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := brain.NewLlamaCppProvider(srv.URL, []string{"llama-3.1-70b"})

	_, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "llama-3.1-70b",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error from 500 response, got nil")
	}
}

func TestLlamaCppProvider_CompleteContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response; context should cancel before we respond.
		<-r.Context().Done()
	}))
	defer srv.Close()

	p := brain.NewLlamaCppProvider(srv.URL, []string{"llama-3.1-70b"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := p.Complete(ctx, &types.InternalChatRequest{
		Model:    "llama-3.1-70b",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/brain/ -v -run TestLlamaCpp
```

Expected: FAIL -- `brain.LlamaCppProvider` and `brain.NewLlamaCppProvider` do not exist.

- [ ] **Step 3: Implement llama.cpp provider**

Create `internal/brain/llamacpp.go`:

```go
package brain

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// LlamaCppProvider implements Provider by calling llama.cpp's OpenAI-compatible
// API at the configured base URL.
type LlamaCppProvider struct {
	baseURL string
	models  []string
	client  *http.Client
}

// NewLlamaCppProvider creates a new llama.cpp provider pointing at the given
// base URL (e.g. "http://localhost:8080"). The models slice lists model IDs
// that this llama.cpp instance serves.
func NewLlamaCppProvider(baseURL string, models []string) *LlamaCppProvider {
	return &LlamaCppProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		models:  models,
		client: &http.Client{
			Timeout: 5 * time.Minute, // LLM completions can be slow.
		},
	}
}

func (p *LlamaCppProvider) Name() string    { return "llamacpp" }
func (p *LlamaCppProvider) Models() []string { return p.models }

// Available checks whether the llama.cpp server is reachable by hitting /health.
func (p *LlamaCppProvider) Available() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Complete sends a non-streaming chat completion to llama.cpp and returns the
// response translated to HelixLLM's internal types.
func (p *LlamaCppProvider) Complete(ctx context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	apiReq := p.toAPIRequest(req)
	apiReq.Stream = false

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("llamacpp: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("llamacpp: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llamacpp: send request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llamacpp: unexpected status %d", httpResp.StatusCode)
	}

	var apiResp api.ChatCompletionResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("llamacpp: decode response: %w", err)
	}

	return p.fromAPIResponse(&apiResp), nil
}

// CompleteStream sends a streaming chat completion to llama.cpp and returns a
// channel of StreamChunks. The channel is closed when the stream ends.
func (p *LlamaCppProvider) CompleteStream(ctx context.Context, req *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	apiReq := p.toAPIRequest(req)
	apiReq.Stream = true

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("llamacpp: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("llamacpp: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llamacpp: send request: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		httpResp.Body.Close()
		return nil, fmt.Errorf("llamacpp: unexpected status %d", httpResp.StatusCode)
	}

	ch := make(chan types.StreamChunk, 64)
	go func() {
		defer close(ch)
		defer httpResp.Body.Close()
		p.readSSEStream(ctx, httpResp, ch)
	}()

	return ch, nil
}

// readSSEStream reads SSE lines from an HTTP response and sends StreamChunks
// on the channel.
func (p *LlamaCppProvider) readSSEStream(ctx context.Context, resp *http.Response, ch chan<- types.StreamChunk) {
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return
		}

		var chunk api.ChatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) > 0 {
			sc := types.StreamChunk{
				Content: chunk.Choices[0].Delta.Content,
			}
			if chunk.Choices[0].FinishReason != nil {
				sc.FinishReason = *chunk.Choices[0].FinishReason
			}
			ch <- sc
		}
	}
}

// toAPIRequest converts an InternalChatRequest to an OpenAI ChatCompletionRequest
// suitable for llama.cpp's API.
func (p *LlamaCppProvider) toAPIRequest(req *types.InternalChatRequest) api.ChatCompletionRequest {
	messages := make([]api.ChatMessage, len(req.Messages))
	for i, m := range req.Messages {
		messages[i] = api.ChatMessage{
			Role:    string(m.Role),
			Content: m.Content,
			Name:    m.Name,
		}
	}

	apiReq := api.ChatCompletionRequest{
		Model:    req.Model,
		Messages: messages,
	}
	if req.MaxTokens > 0 {
		mt := req.MaxTokens
		apiReq.MaxTokens = &mt
	}
	if req.Temperature > 0 {
		temp := req.Temperature
		apiReq.Temperature = &temp
	}
	return apiReq
}

// fromAPIResponse converts an OpenAI ChatCompletionResponse to an InternalChatResponse.
func (p *LlamaCppProvider) fromAPIResponse(resp *api.ChatCompletionResponse) *types.InternalChatResponse {
	result := &types.InternalChatResponse{
		ID:       resp.ID,
		Model:    resp.Model,
		Provider: types.ProviderLocal,
	}
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		// Extract content as string. The Content field is interface{} in the API
		// types; for llama.cpp it is always a string.
		content := ""
		switch v := choice.Message.Content.(type) {
		case string:
			content = v
		}
		result.Message = types.InternalMessage{
			Role:    types.Role(choice.Message.Role),
			Content: content,
		}
		result.FinishReason = choice.FinishReason
	}
	if resp.Usage != nil {
		result.Usage = types.InternalUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
	}
	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/brain/ -v -run TestLlamaCpp -count=1
```

Expected: all 7 LlamaCpp tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/brain/llamacpp.go internal/brain/llamacpp_test.go
git commit -m "feat: implement llama.cpp provider with OpenAI-compatible HTTP client"
```

---

### Task 4: OpenAI Provider

**Files:**
- Create: `internal/brain/openai_provider.go`
- Create: `internal/brain/openai_provider_test.go`

- [ ] **Step 1: Write failing tests for OpenAI provider**

Create `internal/brain/openai_provider_test.go`:

```go
package brain_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

func TestOpenAIProvider_Satisfies(t *testing.T) {
	var _ brain.Provider = (*brain.OpenAIProvider)(nil)
}

func TestOpenAIProvider_Name(t *testing.T) {
	p := brain.NewOpenAIProvider("sk-test", "https://api.openai.com")
	if p.Name() != "openai" {
		t.Errorf("Name() = %q, want %q", p.Name(), "openai")
	}
}

func TestOpenAIProvider_Models(t *testing.T) {
	p := brain.NewOpenAIProvider("sk-test", "https://api.openai.com")
	models := p.Models()
	if len(models) == 0 {
		t.Fatal("Models() returned empty slice")
	}
	// Should contain at least gpt-4o.
	found := false
	for _, m := range models {
		if m == "gpt-4o" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Models() does not contain gpt-4o: %v", models)
	}
}

func TestOpenAIProvider_Complete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		// Verify Authorization header.
		auth := r.Header.Get("Authorization")
		if auth != "Bearer sk-test-key" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer sk-test-key")
		}

		var req api.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		resp := api.ChatCompletionResponse{
			ID:      "chatcmpl-openai-001",
			Object:  "chat.completion",
			Created: 1700000000,
			Model:   req.Model,
			Choices: []api.ChatCompletionChoice{
				{
					Index: 0,
					Message: api.ChatMessage{
						Role:    "assistant",
						Content: "Hello from OpenAI!",
					},
					FinishReason: "stop",
				},
			},
			Usage: &api.Usage{
				PromptTokens:     12,
				CompletionTokens: 4,
				TotalTokens:      16,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := brain.NewOpenAIProvider("sk-test-key", srv.URL)

	resp, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model: "gpt-4o",
		Messages: []types.InternalMessage{
			{Role: types.RoleSystem, Content: "You are helpful."},
			{Role: types.RoleUser, Content: "Hello"},
		},
		Temperature: 0.7,
		MaxTokens:   100,
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.ID != "chatcmpl-openai-001" {
		t.Errorf("ID = %q, want %q", resp.ID, "chatcmpl-openai-001")
	}
	if resp.Message.Content != "Hello from OpenAI!" {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "Hello from OpenAI!")
	}
	if resp.Provider != types.ProviderOpenAI {
		t.Errorf("Provider = %q, want %q", resp.Provider, types.ProviderOpenAI)
	}
}

func TestOpenAIProvider_CompleteStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)

		tokens := []string{"Hello", " from", " OpenAI!"}
		stopStr := "stop"
		for i, token := range tokens {
			chunk := api.ChatCompletionChunk{
				ID:      "chatcmpl-openai-002",
				Object:  "chat.completion.chunk",
				Created: 1700000000,
				Model:   "gpt-4o",
				Choices: []api.ChatCompletionChunkChoice{
					{
						Index: 0,
						Delta: api.ChatMessageDelta{Content: token},
						FinishReason: func() *string {
							if i == len(tokens)-1 {
								return &stopStr
							}
							return nil
						}(),
					},
				},
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := brain.NewOpenAIProvider("sk-test-key", srv.URL)

	ch, err := p.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model:    "gpt-4o",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hello"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("CompleteStream returned error: %v", err)
	}

	var chunks []types.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	if chunks[0].Content != "Hello" {
		t.Errorf("chunks[0].Content = %q, want %q", chunks[0].Content, "Hello")
	}
	if chunks[2].FinishReason != "stop" {
		t.Errorf("chunks[2].FinishReason = %q, want %q", chunks[2].FinishReason, "stop")
	}
}

func TestOpenAIProvider_Available_NoKey(t *testing.T) {
	p := brain.NewOpenAIProvider("", "https://api.openai.com")
	if p.Available() {
		t.Error("Available() = true, want false (no API key)")
	}
}

func TestOpenAIProvider_Available_WithKey(t *testing.T) {
	p := brain.NewOpenAIProvider("sk-test", "https://api.openai.com")
	if !p.Available() {
		t.Error("Available() = false, want true (has API key)")
	}
}

func TestOpenAIProvider_CompleteError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: "rate limited",
				Type:    "rate_limit_error",
			},
		})
	}))
	defer srv.Close()

	p := brain.NewOpenAIProvider("sk-test", srv.URL)

	_, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "gpt-4o",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error from 429 response, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/brain/ -v -run TestOpenAI
```

Expected: FAIL -- `brain.OpenAIProvider` and `brain.NewOpenAIProvider` do not exist.

- [ ] **Step 3: Implement OpenAI provider**

Create `internal/brain/openai_provider.go`:

```go
package brain

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// openAIModels is the set of models supported via the OpenAI API.
var openAIModels = []string{
	"gpt-4o",
	"gpt-4o-mini",
	"gpt-4-turbo",
	"gpt-4",
	"gpt-3.5-turbo",
	"o1",
	"o1-mini",
	"o1-preview",
	"o3",
	"o3-mini",
}

// OpenAIProvider implements Provider by calling the OpenAI API.
type OpenAIProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewOpenAIProvider creates a new OpenAI provider with the given API key and
// base URL. For the real OpenAI API, use "https://api.openai.com" as baseURL.
func NewOpenAIProvider(apiKey, baseURL string) *OpenAIProvider {
	return &OpenAIProvider{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

func (p *OpenAIProvider) Name() string    { return "openai" }
func (p *OpenAIProvider) Models() []string { return openAIModels }

// Available returns true if the provider has a configured API key.
// We do not make a network call here -- if the key is set, we assume
// the provider is available.
func (p *OpenAIProvider) Available() bool {
	return p.apiKey != ""
}

// Complete sends a non-streaming chat completion to OpenAI.
func (p *OpenAIProvider) Complete(ctx context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	apiReq := p.toAPIRequest(req)
	apiReq.Stream = false

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("openai: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: send request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai: unexpected status %d", httpResp.StatusCode)
	}

	var apiResp api.ChatCompletionResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("openai: decode response: %w", err)
	}

	return p.fromAPIResponse(&apiResp), nil
}

// CompleteStream sends a streaming chat completion to OpenAI.
func (p *OpenAIProvider) CompleteStream(ctx context.Context, req *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	apiReq := p.toAPIRequest(req)
	apiReq.Stream = true

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("openai: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: send request: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		httpResp.Body.Close()
		return nil, fmt.Errorf("openai: unexpected status %d", httpResp.StatusCode)
	}

	ch := make(chan types.StreamChunk, 64)
	go func() {
		defer close(ch)
		defer httpResp.Body.Close()
		p.readSSEStream(ctx, httpResp, ch)
	}()

	return ch, nil
}

// readSSEStream reads SSE lines from the OpenAI streaming response.
func (p *OpenAIProvider) readSSEStream(ctx context.Context, resp *http.Response, ch chan<- types.StreamChunk) {
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return
		}

		var chunk api.ChatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) > 0 {
			sc := types.StreamChunk{
				Content: chunk.Choices[0].Delta.Content,
			}
			if chunk.Choices[0].FinishReason != nil {
				sc.FinishReason = *chunk.Choices[0].FinishReason
			}
			ch <- sc
		}
	}
}

// toAPIRequest converts an InternalChatRequest to an OpenAI ChatCompletionRequest.
func (p *OpenAIProvider) toAPIRequest(req *types.InternalChatRequest) api.ChatCompletionRequest {
	messages := make([]api.ChatMessage, len(req.Messages))
	for i, m := range req.Messages {
		messages[i] = api.ChatMessage{
			Role:    string(m.Role),
			Content: m.Content,
			Name:    m.Name,
		}
	}

	apiReq := api.ChatCompletionRequest{
		Model:    req.Model,
		Messages: messages,
	}
	if req.MaxTokens > 0 {
		mt := req.MaxTokens
		apiReq.MaxTokens = &mt
	}
	if req.Temperature > 0 {
		temp := req.Temperature
		apiReq.Temperature = &temp
	}
	return apiReq
}

// fromAPIResponse converts an OpenAI ChatCompletionResponse to an InternalChatResponse.
func (p *OpenAIProvider) fromAPIResponse(resp *api.ChatCompletionResponse) *types.InternalChatResponse {
	result := &types.InternalChatResponse{
		ID:       resp.ID,
		Model:    resp.Model,
		Provider: types.ProviderOpenAI,
	}
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		content := ""
		switch v := choice.Message.Content.(type) {
		case string:
			content = v
		}
		result.Message = types.InternalMessage{
			Role:    types.Role(choice.Message.Role),
			Content: content,
		}
		result.FinishReason = choice.FinishReason
	}
	if resp.Usage != nil {
		result.Usage = types.InternalUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
	}
	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/brain/ -v -run TestOpenAI -count=1
```

Expected: all 7 OpenAI tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/brain/openai_provider.go internal/brain/openai_provider_test.go
git commit -m "feat: implement OpenAI provider with API key auth and SSE streaming"
```

---

### Task 5: Anthropic Provider

**Files:**
- Create: `internal/brain/anthropic_provider.go`
- Create: `internal/brain/anthropic_provider_test.go`

- [ ] **Step 1: Write failing tests for Anthropic provider**

Create `internal/brain/anthropic_provider_test.go`:

```go
package brain_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

func TestAnthropicProvider_Satisfies(t *testing.T) {
	var _ brain.Provider = (*brain.AnthropicProvider)(nil)
}

func TestAnthropicProvider_Name(t *testing.T) {
	p := brain.NewAnthropicProvider("sk-ant-test", "https://api.anthropic.com")
	if p.Name() != "anthropic" {
		t.Errorf("Name() = %q, want %q", p.Name(), "anthropic")
	}
}

func TestAnthropicProvider_Models(t *testing.T) {
	p := brain.NewAnthropicProvider("sk-ant-test", "https://api.anthropic.com")
	models := p.Models()
	if len(models) == 0 {
		t.Fatal("Models() returned empty slice")
	}
	found := false
	for _, m := range models {
		if m == "claude-sonnet-4-20250514" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Models() does not contain claude-sonnet-4-20250514: %v", models)
	}
}

func TestAnthropicProvider_Complete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		// Verify headers.
		if r.Header.Get("x-api-key") != "sk-ant-test-key" {
			t.Errorf("x-api-key = %q, want %q", r.Header.Get("x-api-key"), "sk-ant-test-key")
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("anthropic-version = %q, want %q", r.Header.Get("anthropic-version"), "2023-06-01")
		}

		var req api.MessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		stopReason := "end_turn"
		resp := api.MessageResponse{
			ID:   "msg-anthropic-001",
			Type: "message",
			Role: "assistant",
			Content: []api.ContentBlock{
				{Type: "text", Text: "Hello from Anthropic!"},
			},
			Model:      req.Model,
			StopReason: &stopReason,
			Usage: api.AnthropicUsage{
				InputTokens:  15,
				OutputTokens: 5,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := brain.NewAnthropicProvider("sk-ant-test-key", srv.URL)

	resp, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "Hello"},
		},
		MaxTokens: 1024,
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.ID != "msg-anthropic-001" {
		t.Errorf("ID = %q, want %q", resp.ID, "msg-anthropic-001")
	}
	if resp.Message.Content != "Hello from Anthropic!" {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "Hello from Anthropic!")
	}
	if resp.Provider != types.ProviderAnthropic {
		t.Errorf("Provider = %q, want %q", resp.Provider, types.ProviderAnthropic)
	}
	if resp.Usage.PromptTokens != 15 {
		t.Errorf("Usage.PromptTokens = %d, want 15", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 5 {
		t.Errorf("Usage.CompletionTokens = %d, want 5", resp.Usage.CompletionTokens)
	}
}

func TestAnthropicProvider_Complete_WithSystemPrompt(t *testing.T) {
	var receivedSystem string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req api.MessageRequest
		json.NewDecoder(r.Body).Decode(&req)
		receivedSystem = req.System

		stopReason := "end_turn"
		resp := api.MessageResponse{
			ID:         "msg-002",
			Type:       "message",
			Role:       "assistant",
			Content:    []api.ContentBlock{{Type: "text", Text: "OK"}},
			Model:      req.Model,
			StopReason: &stopReason,
			Usage:      api.AnthropicUsage{InputTokens: 10, OutputTokens: 1},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := brain.NewAnthropicProvider("sk-ant-test", srv.URL)

	// When a system message is the first message, it should be extracted into
	// the Anthropic system field.
	_, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []types.InternalMessage{
			{Role: types.RoleSystem, Content: "You are a helpful assistant."},
			{Role: types.RoleUser, Content: "Hello"},
		},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if receivedSystem != "You are a helpful assistant." {
		t.Errorf("system = %q, want %q", receivedSystem, "You are a helpful assistant.")
	}
}

func TestAnthropicProvider_CompleteStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)

		// message_start
		idx := 0
		msgStart, _ := json.Marshal(api.MessageStreamEvent{
			Type: "message_start",
			Message: &api.MessageResponse{
				ID: "msg-003", Type: "message", Role: "assistant",
				Content: []api.ContentBlock{}, Model: "claude-sonnet-4-20250514",
				Usage: api.AnthropicUsage{InputTokens: 10, OutputTokens: 0},
			},
		})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", msgStart)
		flusher.Flush()

		// content_block_start
		cbStart, _ := json.Marshal(api.MessageStreamEvent{
			Type: "content_block_start", Index: &idx,
			ContentBlock: &api.ContentBlock{Type: "text", Text: ""},
		})
		fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", cbStart)
		flusher.Flush()

		// content_block_delta events
		tokens := []string{"Hello", " from", " Anthropic!"}
		for _, token := range tokens {
			delta, _ := json.Marshal(api.MessageStreamEvent{
				Type: "content_block_delta", Index: &idx,
				Delta: &api.StreamDelta{Type: "text_delta", Text: token},
			})
			fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", delta)
			flusher.Flush()
		}

		// content_block_stop
		cbStop, _ := json.Marshal(api.MessageStreamEvent{
			Type: "content_block_stop", Index: &idx,
		})
		fmt.Fprintf(w, "event: content_block_stop\ndata: %s\n\n", cbStop)
		flusher.Flush()

		// message_delta (stop reason)
		stopReason := "end_turn"
		msgDelta, _ := json.Marshal(api.MessageStreamEvent{
			Type:  "message_delta",
			Delta: &api.StreamDelta{Type: "message_delta", StopReason: &stopReason},
			Usage: &api.AnthropicUsage{OutputTokens: 4},
		})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", msgDelta)
		flusher.Flush()

		// message_stop
		msgStop, _ := json.Marshal(map[string]string{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", msgStop)
		flusher.Flush()
	}))
	defer srv.Close()

	p := brain.NewAnthropicProvider("sk-ant-test-key", srv.URL)

	ch, err := p.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model:     "claude-sonnet-4-20250514",
		Messages:  []types.InternalMessage{{Role: types.RoleUser, Content: "Hello"}},
		MaxTokens: 100,
		Stream:    true,
	})
	if err != nil {
		t.Fatalf("CompleteStream returned error: %v", err)
	}

	var chunks []types.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	if chunks[0].Content != "Hello" {
		t.Errorf("chunks[0].Content = %q, want %q", chunks[0].Content, "Hello")
	}
	if chunks[2].Content != " Anthropic!" {
		t.Errorf("chunks[2].Content = %q, want %q", chunks[2].Content, " Anthropic!")
	}
	// The last text chunk may or may not carry FinishReason depending on
	// whether we attach it from message_delta. We accept both.
}

func TestAnthropicProvider_Available_NoKey(t *testing.T) {
	p := brain.NewAnthropicProvider("", "https://api.anthropic.com")
	if p.Available() {
		t.Error("Available() = true, want false (no API key)")
	}
}

func TestAnthropicProvider_Available_WithKey(t *testing.T) {
	p := brain.NewAnthropicProvider("sk-ant-test", "https://api.anthropic.com")
	if !p.Available() {
		t.Error("Available() = false, want true (has API key)")
	}
}

func TestAnthropicProvider_CompleteError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"bad"}}`))
	}))
	defer srv.Close()

	p := brain.NewAnthropicProvider("sk-ant-test", srv.URL)

	_, err := p.Complete(context.Background(), &types.InternalChatRequest{
		Model:     "claude-sonnet-4-20250514",
		Messages:  []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
		MaxTokens: 100,
	})
	if err == nil {
		t.Fatal("expected error from 400 response, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/brain/ -v -run TestAnthropic
```

Expected: FAIL -- `brain.AnthropicProvider` and `brain.NewAnthropicProvider` do not exist.

- [ ] **Step 3: Implement Anthropic provider**

Create `internal/brain/anthropic_provider.go`:

```go
package brain

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

const anthropicAPIVersion = "2023-06-01"

// anthropicModels is the set of models supported via the Anthropic API.
var anthropicModels = []string{
	"claude-sonnet-4-20250514",
	"claude-opus-4-20250514",
	"claude-3-5-sonnet-20241022",
	"claude-3-5-haiku-20241022",
	"claude-3-opus-20240229",
	"claude-3-haiku-20240307",
}

// AnthropicProvider implements Provider by calling the Anthropic Messages API.
type AnthropicProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewAnthropicProvider creates a new Anthropic provider with the given API key
// and base URL. For the real API, use "https://api.anthropic.com" as baseURL.
func NewAnthropicProvider(apiKey, baseURL string) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

func (p *AnthropicProvider) Name() string    { return "anthropic" }
func (p *AnthropicProvider) Models() []string { return anthropicModels }

// Available returns true if the provider has a configured API key.
func (p *AnthropicProvider) Available() bool {
	return p.apiKey != ""
}

// Complete sends a non-streaming messages request to Anthropic.
func (p *AnthropicProvider) Complete(ctx context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	apiReq := p.toAPIRequest(req)
	apiReq.Stream = false

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/messages", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("anthropic: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: send request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic: unexpected status %d", httpResp.StatusCode)
	}

	var apiResp api.MessageResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("anthropic: decode response: %w", err)
	}

	return p.fromAPIResponse(&apiResp), nil
}

// CompleteStream sends a streaming messages request to Anthropic.
func (p *AnthropicProvider) CompleteStream(ctx context.Context, req *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	apiReq := p.toAPIRequest(req)
	apiReq.Stream = true

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/messages", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("anthropic: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
	httpReq.Header.Set("Accept", "text/event-stream")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: send request: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		httpResp.Body.Close()
		return nil, fmt.Errorf("anthropic: unexpected status %d", httpResp.StatusCode)
	}

	ch := make(chan types.StreamChunk, 64)
	go func() {
		defer close(ch)
		defer httpResp.Body.Close()
		p.readSSEStream(ctx, httpResp, ch)
	}()

	return ch, nil
}

// readSSEStream reads Anthropic SSE events. Anthropic uses named events
// ("event: <type>\ndata: <json>\n\n"). We extract text from content_block_delta
// events and finish reason from message_delta events.
func (p *AnthropicProvider) readSSEStream(ctx context.Context, resp *http.Response, ch chan<- types.StreamChunk) {
	scanner := bufio.NewScanner(resp.Body)
	var currentEventType string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			currentEventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		switch currentEventType {
		case "content_block_delta":
			var event api.MessageStreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}
			if event.Delta != nil && event.Delta.Text != "" {
				ch <- types.StreamChunk{
					Content: event.Delta.Text,
				}
			}

		case "message_delta":
			var event api.MessageStreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}
			if event.Delta != nil && event.Delta.StopReason != nil {
				ch <- types.StreamChunk{
					FinishReason: *event.Delta.StopReason,
				}
			}

		case "message_stop":
			return
		}

		currentEventType = ""
	}
}

// toAPIRequest converts an InternalChatRequest to an Anthropic MessageRequest.
// The Anthropic API requires the system prompt as a separate field, not as a
// message. If the first message has role "system", it is extracted into the
// System field.
func (p *AnthropicProvider) toAPIRequest(req *types.InternalChatRequest) api.MessageRequest {
	var system string
	startIdx := 0

	if len(req.Messages) > 0 && req.Messages[0].Role == types.RoleSystem {
		system = req.Messages[0].Content
		startIdx = 1
	}

	messages := make([]api.AnthropicMessage, 0, len(req.Messages)-startIdx)
	for _, m := range req.Messages[startIdx:] {
		messages = append(messages, api.AnthropicMessage{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}

	apiReq := api.MessageRequest{
		Model:     req.Model,
		Messages:  messages,
		MaxTokens: req.MaxTokens,
		System:    system,
	}
	if apiReq.MaxTokens == 0 {
		apiReq.MaxTokens = 4096 // Anthropic requires max_tokens.
	}
	if req.Temperature > 0 {
		temp := req.Temperature
		apiReq.Temperature = &temp
	}
	return apiReq
}

// fromAPIResponse converts an Anthropic MessageResponse to an InternalChatResponse.
func (p *AnthropicProvider) fromAPIResponse(resp *api.MessageResponse) *types.InternalChatResponse {
	result := &types.InternalChatResponse{
		ID:       resp.ID,
		Model:    resp.Model,
		Provider: types.ProviderAnthropic,
		Usage: types.InternalUsage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}

	// Concatenate all text content blocks.
	var content strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			content.WriteString(block.Text)
		}
	}
	result.Message = types.InternalMessage{
		Role:    types.RoleAssistant,
		Content: content.String(),
	}

	if resp.StopReason != nil {
		result.FinishReason = *resp.StopReason
	}

	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/brain/ -v -run TestAnthropic -count=1
```

Expected: all 8 Anthropic tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/brain/anthropic_provider.go internal/brain/anthropic_provider_test.go
git commit -m "feat: implement Anthropic provider with Messages API and SSE streaming"
```

---

### Task 6: Router

**Files:**
- Create: `internal/brain/router.go`
- Create: `internal/brain/router_test.go`

- [ ] **Step 1: Write failing tests for Router**

Create `internal/brain/router_test.go`:

```go
package brain_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

func newTestProviders() map[string]brain.Provider {
	return map[string]brain.Provider{
		"llamacpp": &mockProvider{
			name:      "llamacpp",
			models:    []string{"llama-3.1-70b", "llama-3.1-8b"},
			available: true,
		},
		"openai": &mockProvider{
			name:      "openai",
			models:    []string{"gpt-4o", "gpt-4o-mini", "gpt-3.5-turbo"},
			available: true,
		},
		"anthropic": &mockProvider{
			name:      "anthropic",
			models:    []string{"claude-sonnet-4-20250514", "claude-3-5-sonnet-20241022"},
			available: true,
		},
	}
}

func TestRouter_RouteByModelName_Llama(t *testing.T) {
	providers := newTestProviders()
	r := brain.NewRouter(providers, "local")

	req := &types.InternalChatRequest{Model: "llama-3.1-70b"}
	p, err := r.Route(req)
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	if p.Name() != "llamacpp" {
		t.Errorf("Name() = %q, want %q", p.Name(), "llamacpp")
	}
}

func TestRouter_RouteByModelName_GPT(t *testing.T) {
	providers := newTestProviders()
	r := brain.NewRouter(providers, "local")

	req := &types.InternalChatRequest{Model: "gpt-4o"}
	p, err := r.Route(req)
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("Name() = %q, want %q", p.Name(), "openai")
	}
}

func TestRouter_RouteByModelName_Claude(t *testing.T) {
	providers := newTestProviders()
	r := brain.NewRouter(providers, "local")

	req := &types.InternalChatRequest{Model: "claude-sonnet-4-20250514"}
	p, err := r.Route(req)
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	if p.Name() != "anthropic" {
		t.Errorf("Name() = %q, want %q", p.Name(), "anthropic")
	}
}

func TestRouter_RouteByExplicitProvider(t *testing.T) {
	providers := newTestProviders()
	r := brain.NewRouter(providers, "local")

	// Even though model name says "gpt-*", explicit provider overrides.
	req := &types.InternalChatRequest{
		Model:    "gpt-4o",
		Provider: types.ProviderAnthropic,
	}
	p, err := r.Route(req)
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	if p.Name() != "anthropic" {
		t.Errorf("Name() = %q, want %q", p.Name(), "anthropic")
	}
}

func TestRouter_RouteDefaultProvider(t *testing.T) {
	providers := newTestProviders()
	r := brain.NewRouter(providers, "local")

	// Unknown model, no explicit provider -- should fall back to default.
	req := &types.InternalChatRequest{Model: "unknown-model-xyz"}
	p, err := r.Route(req)
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	if p.Name() != "llamacpp" {
		t.Errorf("Name() = %q, want %q (default provider)", p.Name(), "llamacpp")
	}
}

func TestRouter_RouteDefaultProvider_OpenAI(t *testing.T) {
	providers := newTestProviders()
	r := brain.NewRouter(providers, "openai")

	req := &types.InternalChatRequest{Model: "some-random-model"}
	p, err := r.Route(req)
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("Name() = %q, want %q (default provider)", p.Name(), "openai")
	}
}

func TestRouter_FallbackWhenPrimaryUnavailable(t *testing.T) {
	providers := map[string]brain.Provider{
		"llamacpp": &mockProvider{
			name:      "llamacpp",
			models:    []string{"llama-3.1-70b"},
			available: false, // Primary is down.
		},
		"openai": &mockProvider{
			name:      "openai",
			models:    []string{"gpt-4o"},
			available: true,
		},
	}
	r := brain.NewRouter(providers, "local")

	// Request for a llama model, but llamacpp is unavailable.
	req := &types.InternalChatRequest{Model: "llama-3.1-70b"}
	p, err := r.Route(req)
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	// Should fall back to any available provider.
	if !p.Available() {
		t.Error("fell back to unavailable provider")
	}
}

func TestRouter_NoProvidersAvailable(t *testing.T) {
	providers := map[string]brain.Provider{
		"llamacpp": &mockProvider{
			name:      "llamacpp",
			models:    []string{"llama-3.1-70b"},
			available: false,
		},
		"openai": &mockProvider{
			name:      "openai",
			models:    []string{"gpt-4o"},
			available: false,
		},
	}
	r := brain.NewRouter(providers, "local")

	req := &types.InternalChatRequest{Model: "gpt-4o"}
	_, err := r.Route(req)
	if err == nil {
		t.Fatal("expected error when no providers are available, got nil")
	}
}

func TestRouter_AllModels(t *testing.T) {
	providers := newTestProviders()
	r := brain.NewRouter(providers, "local")

	models := r.AllModels()
	// Should contain models from all three providers.
	if len(models) < 7 {
		t.Errorf("AllModels() returned %d models, want at least 7", len(models))
	}

	// Check that each model has the correct owner.
	modelMap := make(map[string]string)
	for _, m := range models {
		modelMap[m.ID] = m.OwnedBy
	}
	if owner, ok := modelMap["llama-3.1-70b"]; !ok || owner != "llamacpp" {
		t.Errorf("llama-3.1-70b owner = %q, want %q", owner, "llamacpp")
	}
	if owner, ok := modelMap["gpt-4o"]; !ok || owner != "openai" {
		t.Errorf("gpt-4o owner = %q, want %q", owner, "openai")
	}
	if owner, ok := modelMap["claude-sonnet-4-20250514"]; !ok || owner != "anthropic" {
		t.Errorf("claude-sonnet-4-20250514 owner = %q, want %q", owner, "anthropic")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/brain/ -v -run TestRouter
```

Expected: FAIL -- `brain.Router` and `brain.NewRouter` do not exist.

- [ ] **Step 3: Implement Router**

Create `internal/brain/router.go`:

```go
package brain

import (
	"fmt"
	"strings"
	"time"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// providerMapping maps types.Provider enum values to provider name keys.
var providerMapping = map[types.Provider]string{
	types.ProviderLocal:     "llamacpp",
	types.ProviderOpenAI:    "openai",
	types.ProviderAnthropic: "anthropic",
}

// Router selects the best LLM provider for a given request.
//
// Selection order:
//  1. If InternalChatRequest.Provider is set, use that provider (if available).
//  2. Route by model name prefix (gpt-* -> openai, claude-* -> anthropic,
//     llama-* -> llamacpp).
//  3. Fall back to the configured default provider.
//  4. If the selected provider is unavailable, try any available provider.
type Router struct {
	providers       map[string]Provider
	defaultProvider string // "llamacpp", "openai", or "anthropic"
}

// NewRouter creates a new Router with the given providers and default provider.
// The defaultProviderType is the types.Provider string value ("local", "openai", "anthropic").
func NewRouter(providers map[string]Provider, defaultProviderType string) *Router {
	defaultName := "llamacpp"
	switch types.Provider(defaultProviderType) {
	case types.ProviderOpenAI:
		defaultName = "openai"
	case types.ProviderAnthropic:
		defaultName = "anthropic"
	case types.ProviderLocal:
		defaultName = "llamacpp"
	}

	return &Router{
		providers:       providers,
		defaultProvider: defaultName,
	}
}

// Route selects the best provider for the given request.
func (r *Router) Route(req *types.InternalChatRequest) (Provider, error) {
	// 1. Explicit provider field.
	if req.Provider != "" {
		if name, ok := providerMapping[req.Provider]; ok {
			if p, exists := r.providers[name]; exists && p.Available() {
				return p, nil
			}
		}
	}

	// 2. Route by model name prefix.
	if target := r.routeByModelName(req.Model); target != "" {
		if p, exists := r.providers[target]; exists && p.Available() {
			return p, nil
		}
	}

	// 3. Default provider.
	if p, exists := r.providers[r.defaultProvider]; exists && p.Available() {
		return p, nil
	}

	// 4. Fallback: any available provider.
	for _, p := range r.providers {
		if p.Available() {
			return p, nil
		}
	}

	return nil, fmt.Errorf("no available LLM providers")
}

// routeByModelName returns the provider name for a given model, or "" if
// the model name does not match any known prefix pattern.
func (r *Router) routeByModelName(model string) string {
	lower := strings.ToLower(model)

	switch {
	case strings.HasPrefix(lower, "gpt-"),
		strings.HasPrefix(lower, "o1"),
		strings.HasPrefix(lower, "o3"):
		return "openai"

	case strings.HasPrefix(lower, "claude-"):
		return "anthropic"

	case strings.HasPrefix(lower, "llama-"),
		strings.HasPrefix(lower, "mistral-"),
		strings.HasPrefix(lower, "codellama-"),
		strings.HasPrefix(lower, "phi-"),
		strings.HasPrefix(lower, "gemma-"),
		strings.HasPrefix(lower, "qwen-"):
		return "llamacpp"
	}

	return ""
}

// AllModels aggregates the model lists from all available providers and returns
// them as api.Model objects suitable for the /v1/models endpoint.
func (r *Router) AllModels() []api.Model {
	now := time.Now().Unix()
	var models []api.Model

	for _, p := range r.providers {
		if !p.Available() {
			continue
		}
		for _, m := range p.Models() {
			models = append(models, api.Model{
				ID:      m,
				Object:  "model",
				Created: now,
				OwnedBy: p.Name(),
			})
		}
	}

	return models
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/brain/ -v -run TestRouter -count=1
```

Expected: all 9 Router tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/brain/router.go internal/brain/router_test.go
git commit -m "feat: implement Brain Router with model-prefix routing and fallback"
```

---

### Task 7: Brain Service

**Files:**
- Create: `internal/brain/brain.go`
- Create: `internal/brain/brain_test.go`

- [ ] **Step 1: Write failing tests for Brain service**

Create `internal/brain/brain_test.go`:

```go
package brain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/config"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

func TestBrain_New(t *testing.T) {
	cfg := config.LLMConfig{
		LocalModel:      "llama-3.1-70b",
		LocalRPCPort:    8080,
		DefaultProvider: "local",
	}
	b := brain.New(cfg)
	if b == nil {
		t.Fatal("New returned nil")
	}
}

func TestBrain_Models(t *testing.T) {
	cfg := config.LLMConfig{
		LocalModel:      "llama-3.1-70b",
		LocalRPCPort:    8080,
		DefaultProvider: "local",
	}
	b := brain.New(cfg)
	models := b.Models()
	// Should at least include the local model.
	if len(models) == 0 {
		t.Error("Models() returned empty list")
	}
}

func TestBrain_Complete_WithMockProviders(t *testing.T) {
	providers := map[string]brain.Provider{
		"llamacpp": &mockProvider{
			name:      "llamacpp",
			models:    []string{"llama-3.1-70b"},
			available: true,
			response: &types.InternalChatResponse{
				ID:           "test-resp-1",
				Model:        "llama-3.1-70b",
				Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "Hello from mock!"},
				FinishReason: "stop",
				Provider:     types.ProviderLocal,
			},
		},
		"openai": &mockProvider{
			name:      "openai",
			models:    []string{"gpt-4o"},
			available: true,
			response: &types.InternalChatResponse{
				ID:           "test-resp-2",
				Model:        "gpt-4o",
				Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "Hello from OpenAI!"},
				FinishReason: "stop",
				Provider:     types.ProviderOpenAI,
			},
		},
	}

	b := brain.NewWithProviders(providers, "local")

	// Route to llamacpp by model name.
	resp, err := b.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "llama-3.1-70b",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.Provider != types.ProviderLocal {
		t.Errorf("Provider = %q, want %q", resp.Provider, types.ProviderLocal)
	}
	if resp.Message.Content != "Hello from mock!" {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "Hello from mock!")
	}

	// Route to openai by model name.
	resp, err = b.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "gpt-4o",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.Provider != types.ProviderOpenAI {
		t.Errorf("Provider = %q, want %q", resp.Provider, types.ProviderOpenAI)
	}
}

func TestBrain_CompleteStream_WithMockProviders(t *testing.T) {
	providers := map[string]brain.Provider{
		"llamacpp": &mockProvider{
			name:      "llamacpp",
			models:    []string{"llama-3.1-70b"},
			available: true,
			chunks: []types.StreamChunk{
				{Content: "Hello"},
				{Content: " world", FinishReason: "stop"},
			},
		},
	}

	b := brain.NewWithProviders(providers, "local")

	ch, err := b.CompleteStream(context.Background(), &types.InternalChatRequest{
		Model:    "llama-3.1-70b",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("CompleteStream returned error: %v", err)
	}

	var chunks []types.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if chunks[0].Content != "Hello" {
		t.Errorf("chunks[0].Content = %q, want %q", chunks[0].Content, "Hello")
	}
}

func TestBrain_Complete_NoProviders(t *testing.T) {
	providers := map[string]brain.Provider{
		"llamacpp": &mockProvider{
			name:      "llamacpp",
			models:    []string{"llama-3.1-70b"},
			available: false,
		},
	}

	b := brain.NewWithProviders(providers, "local")

	_, err := b.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "llama-3.1-70b",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error when no providers are available, got nil")
	}
}

func TestBrain_Complete_ProviderError(t *testing.T) {
	providers := map[string]brain.Provider{
		"llamacpp": &mockProvider{
			name:      "llamacpp",
			models:    []string{"llama-3.1-70b"},
			available: true,
			err:       errors.New("connection refused"),
		},
	}

	b := brain.NewWithProviders(providers, "local")

	_, err := b.Complete(context.Background(), &types.InternalChatRequest{
		Model:    "llama-3.1-70b",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error from provider, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/brain/ -v -run TestBrain
```

Expected: FAIL -- `brain.Brain`, `brain.New`, and `brain.NewWithProviders` do not exist.

- [ ] **Step 3: Implement Brain service**

Create `internal/brain/brain.go`:

```go
package brain

import (
	"context"
	"fmt"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/config"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// Brain is the central LLM coordination service. It manages providers,
// routes requests to the appropriate backend, and translates between
// HelixLLM's internal types and provider-specific formats.
type Brain struct {
	router    *Router
	providers map[string]Provider
}

// New creates a Brain from configuration. It initializes all configured
// providers (llama.cpp local, OpenAI, Anthropic) and sets up the router.
func New(cfg config.LLMConfig) *Brain {
	providers := make(map[string]Provider)

	// Local (llama.cpp) provider -- always registered; Available() checks health.
	localURL := fmt.Sprintf("http://localhost:%d", cfg.LocalRPCPort)
	localModels := []string{cfg.LocalModel}
	providers["llamacpp"] = NewLlamaCppProvider(localURL, localModels)

	// OpenAI provider -- registered if API key is configured.
	if cfg.OpenAIKey != "" {
		providers["openai"] = NewOpenAIProvider(cfg.OpenAIKey, "https://api.openai.com")
	}

	// Anthropic provider -- registered if API key is configured.
	if cfg.AnthropicKey != "" {
		providers["anthropic"] = NewAnthropicProvider(cfg.AnthropicKey, "https://api.anthropic.com")
	}

	return &Brain{
		router:    NewRouter(providers, cfg.DefaultProvider),
		providers: providers,
	}
}

// NewWithProviders creates a Brain with explicit providers, for testing.
func NewWithProviders(providers map[string]Provider, defaultProvider string) *Brain {
	return &Brain{
		router:    NewRouter(providers, defaultProvider),
		providers: providers,
	}
}

// Complete routes the request to the appropriate provider and returns the
// full response.
func (b *Brain) Complete(ctx context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	provider, err := b.router.Route(req)
	if err != nil {
		return nil, fmt.Errorf("brain: %w", err)
	}
	return provider.Complete(ctx, req)
}

// CompleteStream routes the request to the appropriate provider and returns a
// channel of streaming chunks.
func (b *Brain) CompleteStream(ctx context.Context, req *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	provider, err := b.router.Route(req)
	if err != nil {
		return nil, fmt.Errorf("brain: %w", err)
	}
	return provider.CompleteStream(ctx, req)
}

// Models returns all available models from all registered providers.
func (b *Brain) Models() []api.Model {
	return b.router.AllModels()
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/brain/ -v -run TestBrain -count=1
```

Expected: all 6 Brain tests PASS.

- [ ] **Step 5: Run all brain package tests to verify no regressions**

```bash
go test ./internal/brain/ -v -count=1
```

Expected: all tests in the brain package PASS (Provider, LlamaCpp, OpenAI, Anthropic, Router, Brain tests).

- [ ] **Step 6: Commit**

```bash
git add internal/brain/brain.go internal/brain/brain_test.go
git commit -m "feat: implement Brain service tying providers and router together"
```

---

### Task 8: Wire Brain into Gateway

**Files:**
- Modify: `internal/gateway/router.go`
- Modify: `internal/gateway/openai.go`
- Modify: `internal/gateway/anthropic.go`
- Modify: `cmd/helixllm/main.go`
- Create: `internal/gateway/openai_brain_test.go` (integration-level test)
- Create: `internal/gateway/anthropic_brain_test.go` (integration-level test)

This task updates the Gateway to accept a `*Brain` and use it for real LLM completions. When Brain is `nil`, the handlers fall back to stub/canned responses (preserving backward compatibility for tests and development).

- [ ] **Step 1: Write failing tests for Brain-wired gateway**

Create `internal/gateway/openai_brain_test.go`:

```go
package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// stubProvider is a test provider returning fixed responses.
type stubProvider struct {
	name      string
	models    []string
	available bool
}

func (s *stubProvider) Name() string    { return s.name }
func (s *stubProvider) Models() []string { return s.models }
func (s *stubProvider) Available() bool  { return s.available }

func (s *stubProvider) Complete(_ context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	return &types.InternalChatResponse{
		ID:           "chatcmpl-brain-001",
		Model:        req.Model,
		Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "Brain response!"},
		FinishReason: "stop",
		Provider:     types.ProviderLocal,
		Usage:        types.InternalUsage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
	}, nil
}

func (s *stubProvider) CompleteStream(_ context.Context, _ *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	ch := make(chan types.StreamChunk, 3)
	ch <- types.StreamChunk{Content: "Brain"}
	ch <- types.StreamChunk{Content: " response!"}
	ch <- types.StreamChunk{Content: "", FinishReason: "stop"}
	close(ch)
	return ch, nil
}

func setupBrainRouter(t *testing.T) (*gin.Engine, *brain.Brain) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	providers := map[string]brain.Provider{
		"llamacpp": &stubProvider{
			name:      "llamacpp",
			models:    []string{"llama-3.1-70b"},
			available: true,
		},
	}

	b := brain.NewWithProviders(providers, "local")

	r := gin.New()
	gateway.RegisterRoutes(r, gateway.RouterOptions{}, b)

	return r, b
}

func TestChatCompletions_WithBrain(t *testing.T) {
	router, _ := setupBrainRouter(t)

	body, _ := json.Marshal(api.ChatCompletionRequest{
		Model:    "llama-3.1-70b",
		Messages: []api.ChatMessage{{Role: "user", Content: "Hello"}},
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp api.ChatCompletionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if resp.ID != "chatcmpl-brain-001" {
		t.Errorf("ID = %q, want %q", resp.ID, "chatcmpl-brain-001")
	}
	// The response should contain Brain's answer, not the stub "Hello! I'm HelixLLM."
	if len(resp.Choices) == 0 {
		t.Fatal("no choices in response")
	}

	content := ""
	switch v := resp.Choices[0].Message.Content.(type) {
	case string:
		content = v
	}
	if content != "Brain response!" {
		t.Errorf("Content = %q, want %q", content, "Brain response!")
	}
}

func TestListModels_WithBrain(t *testing.T) {
	router, _ := setupBrainRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp api.ModelList
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(resp.Data) == 0 {
		t.Error("expected at least one model from Brain")
	}
	// Should contain the Brain's model, not the hardcoded list.
	found := false
	for _, m := range resp.Data {
		if m.ID == "llama-3.1-70b" && m.OwnedBy == "llamacpp" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected llama-3.1-70b owned by llamacpp in model list: %+v", resp.Data)
	}
}
```

Create `internal/gateway/anthropic_brain_test.go`:

```go
package gateway_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

func TestMessages_WithBrain(t *testing.T) {
	router, _ := setupBrainRouter(t)

	body, _ := json.Marshal(api.MessageRequest{
		Model:     "llama-3.1-70b",
		MaxTokens: 100,
		Messages:  []api.AnthropicMessage{{Role: "user", Content: "Hello"}},
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp api.MessageResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if resp.ID != "chatcmpl-brain-001" {
		t.Errorf("ID = %q, want %q", resp.ID, "chatcmpl-brain-001")
	}
	if len(resp.Content) == 0 || resp.Content[0].Text != "Brain response!" {
		t.Errorf("Content = %+v, want text 'Brain response!'", resp.Content)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/gateway/ -v -run "WithBrain"
```

Expected: FAIL -- `RegisterRoutes` does not accept a `*brain.Brain` argument yet.

- [ ] **Step 3: Update `internal/gateway/router.go`**

Modify `internal/gateway/router.go` to accept an optional `*brain.Brain`:

```go
package gateway

import (
	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	gwmw "github.com/HelixDevelopment/HelixLLM/internal/gateway/middleware"
)

// RouterOptions configures the gateway middleware applied to all /v1 routes.
type RouterOptions struct {
	// APIKeys is a comma-separated list of valid Bearer tokens.
	// Empty string means open-access (no authentication required).
	APIKeys string
	// RateLimit is the maximum number of requests per minute per IP.
	// 0 disables rate limiting.
	RateLimit int
}

// RegisterRoutes attaches all gateway endpoint handlers and middleware to r
// under the /v1 prefix. If b is non-nil, handlers delegate to the Brain for
// real LLM completions; otherwise they return canned stub responses.
func RegisterRoutes(r *gin.Engine, opts RouterOptions, b *brain.Brain) {
	v1 := r.Group("/v1")
	v1.Use(gwmw.APIKeyAuth(opts.APIKeys))
	v1.Use(gwmw.RateLimit(opts.RateLimit))
	v1.Use(gwmw.SecurityHeaders())
	v1.Use(gwmw.ContentNegotiation())

	h := &handlers{brain: b}

	// OpenAI-compatible endpoints
	v1.POST("/chat/completions", h.HandleChatCompletions)
	v1.POST("/completions", HandleCompletions)
	v1.GET("/models", h.HandleListModels)
	v1.GET("/models/:id", h.HandleGetModel)
	v1.POST("/embeddings", HandleEmbeddings)

	// Anthropic-compatible endpoints
	v1.POST("/messages", h.HandleMessages)
}

// handlers groups gateway handlers that need access to the Brain.
type handlers struct {
	brain *brain.Brain
}
```

- [ ] **Step 4: Update `internal/gateway/openai.go`**

Modify `internal/gateway/openai.go` to use Brain when available:

```go
package gateway

import (
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// hardcodedModels is the static model list returned when Brain is nil (stub mode).
var hardcodedModels = []api.Model{
	{ID: "llama-3.1-70b", Object: "model", Created: 1700000000, OwnedBy: "helix"},
	{ID: "gpt-4o", Object: "model", Created: 1700000001, OwnedBy: "helix"},
	{ID: "claude-sonnet-4-20250514", Object: "model", Created: 1700000002, OwnedBy: "helix"},
}

// randomID generates a short random hex suffix for synthetic IDs.
func randomID() string {
	return fmt.Sprintf("%08x", rand.Uint32()) //nolint:gosec // stub, not security-sensitive
}

// HandleChatCompletions handles POST /v1/chat/completions.
//
// When Brain is configured, delegates to the Brain for real LLM completions.
// When Brain is nil (stub mode), returns canned responses.
func (h *handlers) HandleChatCompletions(c *gin.Context) {
	var req api.ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: fmt.Sprintf("invalid request body: %v", err),
				Type:    "invalid_request_error",
			},
		})
		return
	}

	model := req.Model
	if model == "" {
		model = "llama-3.1-70b"
	}

	// If Brain is available, use it.
	if h.brain != nil {
		h.handleChatWithBrain(c, &req, model)
		return
	}

	// Stub mode: return canned response.
	id := "chatcmpl-helix-" + randomID()

	if req.Stream {
		streamChatCompletions(c, id, model)
		return
	}

	c.JSON(http.StatusOK, api.ChatCompletionResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []api.ChatCompletionChoice{
			{
				Index: 0,
				Message: api.ChatMessage{
					Role:    "assistant",
					Content: "Hello! I'm HelixLLM.",
				},
				FinishReason: "stop",
			},
		},
		Usage: &api.Usage{
			PromptTokens:     10,
			CompletionTokens: 6,
			TotalTokens:      16,
		},
	})
}

// handleChatWithBrain uses the Brain to complete a chat request.
func (h *handlers) handleChatWithBrain(c *gin.Context, req *api.ChatCompletionRequest, model string) {
	internalReq := toInternalRequest(req, model)

	if req.Stream {
		h.streamChatWithBrain(c, internalReq, model)
		return
	}

	resp, err := h.brain.Complete(c.Request.Context(), internalReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: fmt.Sprintf("completion failed: %v", err),
				Type:    "server_error",
			},
		})
		return
	}

	c.JSON(http.StatusOK, api.ChatCompletionResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   resp.Model,
		Choices: []api.ChatCompletionChoice{
			{
				Index: 0,
				Message: api.ChatMessage{
					Role:    string(resp.Message.Role),
					Content: resp.Message.Content,
				},
				FinishReason: resp.FinishReason,
			},
		},
		Usage: &api.Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	})
}

// streamChatWithBrain streams chat completions from the Brain via SSE.
func (h *handlers) streamChatWithBrain(c *gin.Context, req *types.InternalChatRequest, model string) {
	ch, err := h.brain.CompleteStream(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: fmt.Sprintf("stream failed: %v", err),
				Type:    "server_error",
			},
		})
		return
	}

	w := NewSSEWriter(c)
	w.WriteHeader()

	id := "chatcmpl-helix-" + randomID()
	created := time.Now().Unix()

	for chunk := range ch {
		var finishReason *string
		if chunk.FinishReason != "" {
			fr := chunk.FinishReason
			finishReason = &fr
		}

		sseChunk := api.ChatCompletionChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []api.ChatCompletionChunkChoice{
				{
					Index: 0,
					Delta: api.ChatMessageDelta{
						Content: chunk.Content,
					},
					FinishReason: finishReason,
				},
			},
		}
		w.WriteEvent(sseChunk) //nolint:errcheck
	}

	w.WriteDone()
}

// toInternalRequest converts an OpenAI ChatCompletionRequest to an InternalChatRequest.
func toInternalRequest(req *api.ChatCompletionRequest, model string) *types.InternalChatRequest {
	messages := make([]types.InternalMessage, len(req.Messages))
	for i, m := range req.Messages {
		content := ""
		switch v := m.Content.(type) {
		case string:
			content = v
		}
		messages[i] = types.InternalMessage{
			Role:    types.Role(m.Role),
			Content: content,
			Name:    m.Name,
		}
	}

	ir := &types.InternalChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   req.Stream,
	}
	if req.MaxTokens != nil {
		ir.MaxTokens = *req.MaxTokens
	}
	if req.Temperature != nil {
		ir.Temperature = *req.Temperature
	}
	return ir
}

// streamChatCompletions writes 3 SSE chunks followed by [DONE] (stub mode).
func streamChatCompletions(c *gin.Context, id, model string) {
	w := NewSSEWriter(c)
	w.WriteHeader()

	created := time.Now().Unix()
	tokens := []string{"Hello", "! I'm", " HelixLLM."}

	stopStr := "stop"
	for i, token := range tokens {
		chunk := api.ChatCompletionChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []api.ChatCompletionChunkChoice{
				{
					Index: 0,
					Delta: api.ChatMessageDelta{
						Content: token,
					},
					FinishReason: func() *string {
						if i == len(tokens)-1 {
							return &stopStr
						}
						return nil
					}(),
				},
			},
		}
		w.WriteEvent(chunk) //nolint:errcheck
	}

	w.WriteDone()
}

// HandleCompletions handles POST /v1/completions (stub).
func HandleCompletions(c *gin.Context) {
	var req api.CompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: fmt.Sprintf("invalid request body: %v", err),
				Type:    "invalid_request_error",
			},
		})
		return
	}

	model := req.Model
	if model == "" {
		model = "llama-3.1-70b"
	}

	c.JSON(http.StatusOK, api.CompletionResponse{
		ID:      "cmpl-helix-" + randomID(),
		Object:  "text_completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []api.CompletionChoice{
			{
				Text:         "Hello! I'm HelixLLM.",
				Index:        0,
				FinishReason: "stop",
			},
		},
		Usage: &api.Usage{
			PromptTokens:     10,
			CompletionTokens: 6,
			TotalTokens:      16,
		},
	})
}

// HandleListModels handles GET /v1/models.
func (h *handlers) HandleListModels(c *gin.Context) {
	if h.brain != nil {
		models := h.brain.Models()
		c.JSON(http.StatusOK, api.ModelList{
			Object: "list",
			Data:   models,
		})
		return
	}

	c.JSON(http.StatusOK, api.ModelList{
		Object: "list",
		Data:   hardcodedModels,
	})
}

// HandleGetModel handles GET /v1/models/:id.
func (h *handlers) HandleGetModel(c *gin.Context) {
	id := c.Param("id")

	if h.brain != nil {
		models := h.brain.Models()
		for _, m := range models {
			if m.ID == id {
				c.JSON(http.StatusOK, m)
				return
			}
		}
	} else {
		for _, m := range hardcodedModels {
			if m.ID == id {
				c.JSON(http.StatusOK, m)
				return
			}
		}
	}

	c.JSON(http.StatusNotFound, api.ErrorResponse{
		Error: api.ErrorDetail{
			Message: fmt.Sprintf("model %q not found", id),
			Type:    "invalid_request_error",
		},
	})
}

// HandleEmbeddings handles POST /v1/embeddings.
// Returns a single zero-vector embedding of dimension 1536 (ada-002 default).
func HandleEmbeddings(c *gin.Context) {
	var req api.EmbeddingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: fmt.Sprintf("invalid request body: %v", err),
				Type:    "invalid_request_error",
			},
		})
		return
	}

	model := req.Model
	if model == "" {
		model = "text-embedding-ada-002"
	}

	const dim = 1536
	zeroVec := make([]float64, dim)

	c.JSON(http.StatusOK, api.EmbeddingResponse{
		Object: "list",
		Data: []api.EmbeddingData{
			{
				Object:    "embedding",
				Embedding: zeroVec,
				Index:     0,
			},
		},
		Model: model,
		Usage: &api.Usage{
			PromptTokens: 1,
			TotalTokens:  1,
		},
	})
}
```

- [ ] **Step 5: Update `internal/gateway/anthropic.go`**

Modify `internal/gateway/anthropic.go` to use Brain when available:

```go
package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// anthropicSSEWriter writes Anthropic-format SSE events.
type anthropicSSEWriter struct {
	c *gin.Context
}

func newAnthropicSSEWriter(c *gin.Context) *anthropicSSEWriter {
	return &anthropicSSEWriter{c: c}
}

func (w *anthropicSSEWriter) writeHeader() {
	w.c.Header("Content-Type", "text/event-stream")
	w.c.Header("Cache-Control", "no-cache")
	w.c.Header("Connection", "keep-alive")
	w.c.Header("X-Accel-Buffering", "no")
	w.c.Status(200)
}

func (w *anthropicSSEWriter) writeEvent(eventType string, data interface{}) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	fmt.Fprintf(w.c.Writer, "event: %s\ndata: %s\n\n", eventType, jsonBytes)
	w.c.Writer.Flush()
	return nil
}

// HandleMessages handles POST /v1/messages (Anthropic-compatible).
//
// When Brain is configured, delegates to the Brain for real completions and
// translates the response to Anthropic format. When Brain is nil, returns
// canned stub responses.
func (h *handlers) HandleMessages(c *gin.Context) {
	var req api.MessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: fmt.Sprintf("invalid request body: %v", err),
				Type:    "invalid_request_error",
			},
		})
		return
	}

	model := req.Model
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	// If Brain is available, use it.
	if h.brain != nil {
		h.handleMessagesWithBrain(c, &req, model)
		return
	}

	// Stub mode.
	id := "msg-helix-" + randomID()

	if req.Stream {
		streamMessages(c, id, model)
		return
	}

	stopReason := "end_turn"
	c.JSON(http.StatusOK, api.MessageResponse{
		ID:   id,
		Type: "message",
		Role: "assistant",
		Content: []api.ContentBlock{
			{Type: "text", Text: "Hello! I'm HelixLLM."},
		},
		Model:      model,
		StopReason: &stopReason,
		Usage: api.AnthropicUsage{
			InputTokens:  10,
			OutputTokens: 6,
		},
	})
}

// handleMessagesWithBrain delegates to Brain and translates to Anthropic format.
func (h *handlers) handleMessagesWithBrain(c *gin.Context, req *api.MessageRequest, model string) {
	internalReq := anthropicToInternalRequest(req, model)

	if req.Stream {
		h.streamMessagesWithBrain(c, internalReq, model)
		return
	}

	resp, err := h.brain.Complete(c.Request.Context(), internalReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: fmt.Sprintf("completion failed: %v", err),
				Type:    "server_error",
			},
		})
		return
	}

	stopReason := resp.FinishReason
	if stopReason == "stop" {
		stopReason = "end_turn"
	}

	c.JSON(http.StatusOK, api.MessageResponse{
		ID:   resp.ID,
		Type: "message",
		Role: "assistant",
		Content: []api.ContentBlock{
			{Type: "text", Text: resp.Message.Content},
		},
		Model:      resp.Model,
		StopReason: &stopReason,
		Usage: api.AnthropicUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	})
}

// streamMessagesWithBrain streams an Anthropic-formatted SSE response from Brain.
func (h *handlers) streamMessagesWithBrain(c *gin.Context, req *types.InternalChatRequest, model string) {
	ch, err := h.brain.CompleteStream(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: fmt.Sprintf("stream failed: %v", err),
				Type:    "server_error",
			},
		})
		return
	}

	w := newAnthropicSSEWriter(c)
	w.writeHeader()

	id := "msg-helix-" + randomID()
	idx := 0

	// message_start
	w.writeEvent("message_start", api.MessageStreamEvent{ //nolint:errcheck
		Type: "message_start",
		Message: &api.MessageResponse{
			ID: id, Type: "message", Role: "assistant",
			Content: []api.ContentBlock{}, Model: model,
			Usage: api.AnthropicUsage{InputTokens: 0, OutputTokens: 0},
		},
	})

	// content_block_start
	w.writeEvent("content_block_start", api.MessageStreamEvent{ //nolint:errcheck
		Type: "content_block_start", Index: &idx,
		ContentBlock: &api.ContentBlock{Type: "text", Text: ""},
	})

	// Stream content_block_delta from Brain chunks.
	for chunk := range ch {
		if chunk.Content != "" {
			w.writeEvent("content_block_delta", api.MessageStreamEvent{ //nolint:errcheck
				Type: "content_block_delta", Index: &idx,
				Delta: &api.StreamDelta{Type: "text_delta", Text: chunk.Content},
			})
		}

		if chunk.FinishReason != "" {
			stopReason := chunk.FinishReason
			if stopReason == "stop" {
				stopReason = "end_turn"
			}
			// content_block_stop
			w.writeEvent("content_block_stop", api.MessageStreamEvent{ //nolint:errcheck
				Type: "content_block_stop", Index: &idx,
			})
			// message_delta
			w.writeEvent("message_delta", api.MessageStreamEvent{ //nolint:errcheck
				Type:  "message_delta",
				Delta: &api.StreamDelta{Type: "message_delta", StopReason: &stopReason},
				Usage: &api.AnthropicUsage{OutputTokens: 0},
			})
		}
	}

	// message_stop
	w.writeEvent("message_stop", map[string]string{"type": "message_stop"}) //nolint:errcheck
}

// anthropicToInternalRequest converts an Anthropic MessageRequest to an InternalChatRequest.
func anthropicToInternalRequest(req *api.MessageRequest, model string) *types.InternalChatRequest {
	var messages []types.InternalMessage

	// Add system prompt as a system message if present.
	if req.System != "" {
		messages = append(messages, types.InternalMessage{
			Role:    types.RoleSystem,
			Content: req.System,
		})
	}

	for _, m := range req.Messages {
		content := ""
		switch v := m.Content.(type) {
		case string:
			content = v
		}
		messages = append(messages, types.InternalMessage{
			Role:    types.Role(m.Role),
			Content: content,
		})
	}

	ir := &types.InternalChatRequest{
		Model:     model,
		Messages:  messages,
		MaxTokens: req.MaxTokens,
		Stream:    req.Stream,
	}
	if req.Temperature != nil {
		ir.Temperature = *req.Temperature
	}
	return ir
}

// streamMessages writes the Anthropic SSE event sequence for a canned response (stub mode).
func streamMessages(c *gin.Context, id, model string) {
	w := newAnthropicSSEWriter(c)
	w.writeHeader()

	stopReason := "end_turn"

	w.writeEvent("message_start", api.MessageStreamEvent{ //nolint:errcheck
		Type: "message_start",
		Message: &api.MessageResponse{
			ID: id, Type: "message", Role: "assistant",
			Content: []api.ContentBlock{}, Model: model,
			StopReason: nil,
			Usage: api.AnthropicUsage{InputTokens: 10, OutputTokens: 0},
		},
	})

	idx := 0
	w.writeEvent("content_block_start", api.MessageStreamEvent{ //nolint:errcheck
		Type: "content_block_start", Index: &idx,
		ContentBlock: &api.ContentBlock{Type: "text", Text: ""},
	})

	tokens := []string{"Hello", "! I'm", " HelixLLM."}
	for _, token := range tokens {
		deltaText := token
		w.writeEvent("content_block_delta", api.MessageStreamEvent{ //nolint:errcheck
			Type: "content_block_delta", Index: &idx,
			Delta: &api.StreamDelta{Type: "text_delta", Text: deltaText},
		})
	}

	w.writeEvent("content_block_stop", api.MessageStreamEvent{ //nolint:errcheck
		Type: "content_block_stop", Index: &idx,
	})

	w.writeEvent("message_delta", api.MessageStreamEvent{ //nolint:errcheck
		Type: "message_delta",
		Delta: &api.StreamDelta{Type: "message_delta", StopReason: &stopReason},
		Usage: &api.AnthropicUsage{OutputTokens: 6},
	})

	w.writeEvent("message_stop", map[string]string{"type": "message_stop"}) //nolint:errcheck

	fmt.Fprintf(c.Writer, "event: ping\ndata: {\"type\":\"ping\"}\n\n")
	c.Writer.Flush()
}
```

- [ ] **Step 6: Update `cmd/helixllm/main.go`**

Modify `cmd/helixllm/main.go` to create a Brain and pass it to the gateway:

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/internal/mode"
	"github.com/HelixDevelopment/HelixLLM/internal/server"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/config"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/events"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/health"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/logging"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/observability"
)

func main() {
	modeFlag := flag.String("mode", "", "Operating mode (overrides HELIX_MODE env)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	// CLI flag overrides env
	if *modeFlag != "" {
		cfg.Mode = *modeFlag
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "invalid config: %v\n", err)
		os.Exit(1)
	}

	m, err := mode.Parse(cfg.Mode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	log := logging.New(cfg.Log.Level, cfg.Log.Format)
	bus := events.NewBus()
	defer bus.Close()

	obs, err := observability.New(observability.Options{
		ServiceName: "helixllm",
		Environment: "production",
		Exporter:    cfg.Log.OTELExporter,
	})
	if err != nil {
		log.Error(fmt.Sprintf("observability init failed: %v", err))
		os.Exit(1)
	}
	defer obs.Shutdown()

	checker := health.NewChecker()

	log.WithField("mode", m.String()).Info("starting HelixLLM")

	srv := server.New(server.Options{
		Host:    cfg.Server.Host,
		Port:    cfg.Server.Port,
		TLSCert: cfg.Server.TLSCert,
		TLSKey:  cfg.Server.TLSKey,
		Checker: checker,
	})

	// Initialize Brain (LLM coordination layer)
	b := brain.New(cfg.LLM)
	log.Info("brain initialized")

	// Register gateway routes (OpenAI + Anthropic compatible endpoints)
	gateway.RegisterRoutes(srv.Router(), gateway.RouterOptions{
		APIKeys:   cfg.Auth.APIKeys,
		RateLimit: 0, // TODO: add to config
	}, b)

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Info("shutting down...")
		bus.Publish(events.TopicServerStopped, "main", nil)
		cancel()
	}()

	bus.Publish(events.TopicServerStarted, "main", m.String())
	log.WithField("addr", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)).
		Info("server listening")

	if err := srv.ListenAndServe(ctx); err != nil {
		log.WithError(err).Error("server error")
		os.Exit(1)
	}
}
```

- [ ] **Step 7: Update any existing gateway tests that call RegisterRoutes**

Any tests in `internal/gateway/` that currently call `RegisterRoutes(r, opts)` need to be updated to `RegisterRoutes(r, opts, nil)` to use stub mode. Search for all callers:

```bash
grep -r "RegisterRoutes" internal/gateway/ --include="*.go"
```

Update each call site to pass `nil` as the third argument (Brain). For example, in existing test files:

```go
// Before:
gateway.RegisterRoutes(r, gateway.RouterOptions{})
// After:
gateway.RegisterRoutes(r, gateway.RouterOptions{}, nil)
```

Also update `cmd/helixllm/main.go` references if any other files call `RegisterRoutes`.

- [ ] **Step 8: Run all tests to verify everything passes**

```bash
go test ./internal/brain/ -v -count=1
go test ./internal/gateway/ -v -count=1
go build ./cmd/helixllm/
```

Expected: all brain tests PASS, all gateway tests PASS (both old stub tests and new Brain-wired tests), binary builds successfully.

- [ ] **Step 9: Commit**

```bash
git add internal/gateway/router.go internal/gateway/openai.go internal/gateway/anthropic.go \
       internal/gateway/openai_brain_test.go internal/gateway/anthropic_brain_test.go \
       cmd/helixllm/main.go
git commit -m "feat: wire Brain into Gateway, replacing stubs with real LLM provider routing"
```

---

## Summary

| Task | Files | Tests | Description |
|------|-------|-------|-------------|
| 1 | `.gitmodules`, `go.mod` | -- | Add LLMProvider, Optimization, Cache, Recovery submodules |
| 2 | `internal/brain/provider.go` | 4 | Define Provider interface |
| 3 | `internal/brain/llamacpp.go` | 7 | llama.cpp provider (OpenAI-compat HTTP) |
| 4 | `internal/brain/openai_provider.go` | 7 | OpenAI API provider |
| 5 | `internal/brain/anthropic_provider.go` | 8 | Anthropic Messages API provider |
| 6 | `internal/brain/router.go` | 9 | Intelligent LLM router |
| 7 | `internal/brain/brain.go` | 6 | Brain service (ties it all together) |
| 8 | `gateway/*.go`, `main.go` | 3+ | Wire Brain into Gateway, replace stubs |

**Total new files:** 12 (6 implementation + 6 test files, plus 2 gateway test files)
**Total modified files:** 4 (`gateway/router.go`, `gateway/openai.go`, `gateway/anthropic.go`, `cmd/helixllm/main.go`)
**Total tests:** ~44+

**Follow-up refinement tasks (not in this plan):**
- Integrate `digital.vasic.llmprovider` for unified adapter interface
- Wire `digital.vasic.optimization` semantic cache for near-duplicate prompt detection
- Wire `digital.vasic.cache` for response caching (Redis + in-memory)
- Wire `digital.vasic.recovery` for per-provider circuit breakers
- Add cost-based and latency-based routing strategies to Router
- Add capability-based routing (e.g. vision support detection)
