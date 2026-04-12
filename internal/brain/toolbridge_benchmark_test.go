package brain_test

import (
	"context"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// ---------------------------------------------------------------------------
// Benchmarks for the tool call bridge (JSON and XML parsing)
// ---------------------------------------------------------------------------

// helperBenchServer creates a mock server that returns the given content.
func helperBenchServer(content string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := api.ChatCompletionResponse{
			ID:    "bench-1",
			Model: "qwen2.5-coder-7b",
			Choices: []api.ChatCompletionChoice{
				{
					Index:        0,
					Message:      api.ChatMessage{Role: "assistant", Content: content},
					FinishReason: "stop",
				},
			},
			Usage: &api.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
}

func BenchmarkLlamaCpp_JSONToolCallBridge(b *testing.B) {
	content := `{"name": "bash", "arguments": {"command": "ls -la /tmp"}}`
	srv := helperBenchServer(content)
	defer srv.Close()

	p := brain.NewLlamaCppProvider(srv.URL, []string{"bench-model"})
	req := &types.InternalChatRequest{
		Model:    "bench-model",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "list files"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := p.Complete(context.Background(), req)
		if err != nil {
			b.Fatal(err)
		}
		if len(resp.Message.ToolCalls) == 0 {
			b.Fatal("expected tool calls")
		}
	}
}

func BenchmarkLlamaCpp_XMLToolCallBridge(b *testing.B) {
	content := `<function><name>bash</name><arguments>{"command":"ls -la /tmp"}</arguments></function>`
	srv := helperBenchServer(content)
	defer srv.Close()

	p := brain.NewLlamaCppProvider(srv.URL, []string{"bench-model"})
	req := &types.InternalChatRequest{
		Model:    "bench-model",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "list files"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := p.Complete(context.Background(), req)
		if err != nil {
			b.Fatal(err)
		}
		if len(resp.Message.ToolCalls) == 0 {
			b.Fatal("expected tool calls")
		}
	}
}

func BenchmarkLlamaCpp_MarkdownFencedJSON(b *testing.B) {
	content := "```json\n{\"name\": \"bash\", \"arguments\": {\"command\": \"pwd\"}}\n```"
	srv := helperBenchServer(content)
	defer srv.Close()

	p := brain.NewLlamaCppProvider(srv.URL, []string{"bench-model"})
	req := &types.InternalChatRequest{
		Model:    "bench-model",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "where am I"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := p.Complete(context.Background(), req)
		if err != nil {
			b.Fatal(err)
		}
		if len(resp.Message.ToolCalls) == 0 {
			b.Fatal("expected tool calls")
		}
	}
}

func BenchmarkLlamaCpp_NoToolCall_PlainText(b *testing.B) {
	content := "Hello! I'm happy to help you with your coding tasks."
	srv := helperBenchServer(content)
	defer srv.Close()

	p := brain.NewLlamaCppProvider(srv.URL, []string{"bench-model"})
	req := &types.InternalChatRequest{
		Model:    "bench-model",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hello"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := p.Complete(context.Background(), req)
		if err != nil {
			b.Fatal(err)
		}
		if resp.Message.Content == "" {
			b.Fatal("expected content")
		}
	}
}

// ---------------------------------------------------------------------------
// Stress: concurrent tool-call bridge requests
// ---------------------------------------------------------------------------

func TestStress_ConcurrentToolCallBridge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	content := `{"name": "read_file", "arguments": {"filePath": "/tmp/test.go"}}`
	srv := helperBenchServer(content)
	defer srv.Close()

	p := brain.NewLlamaCppProvider(srv.URL, []string{"stress-model"})
	concurrency := 50
	iterations := 20

	var wg sync.WaitGroup
	errors := make(chan error, concurrency*iterations)

	for c := 0; c < concurrency; c++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				req := &types.InternalChatRequest{
					Model: "stress-model",
					Messages: []types.InternalMessage{
						{Role: types.RoleUser, Content: fmt.Sprintf("request %d-%d", id, i)},
					},
				}
				resp, err := p.Complete(context.Background(), req)
				if err != nil {
					errors <- fmt.Errorf("goroutine %d iter %d: %w", id, i, err)
					continue
				}
				if len(resp.Message.ToolCalls) == 0 {
					errors <- fmt.Errorf("goroutine %d iter %d: no tool calls", id, i)
				}
			}
		}(c)
	}

	wg.Wait()
	close(errors)

	var errs []error
	for err := range errors {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		t.Fatalf("got %d errors out of %d requests; first: %v", len(errs), concurrency*iterations, errs[0])
	}
}

func TestStress_ConcurrentEmbeddingRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	// Use the integration test server which has the hash embedder.
	ts := newStressTestServer()
	defer ts.Close()

	concurrency := 30
	iterations := 10

	var wg sync.WaitGroup
	errors := make(chan error, concurrency*iterations)

	for c := 0; c < concurrency; c++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				body := fmt.Sprintf(`{"model":"test","input":"embedding request %d-%d"}`, id, i)
				req, err := http.NewRequest("POST", ts.URL+"/v1/embeddings",
					bytes.NewBufferString(body))
				if err != nil {
					errors <- err
					continue
				}
				req.Header.Set("Content-Type", "application/json")
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					errors <- fmt.Errorf("goroutine %d iter %d: %w", id, i, err)
					continue
				}
				resp.Body.Close()
				if resp.StatusCode != 200 {
					errors <- fmt.Errorf("goroutine %d iter %d: status %d", id, i, resp.StatusCode)
				}
			}
		}(c)
	}

	wg.Wait()
	close(errors)

	var errs []error
	for err := range errors {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		t.Fatalf("got %d errors; first: %v", len(errs), errs[0])
	}
}

// newStressTestServer creates a minimal test server for stress testing.
// Uses only the gateway layer with a hash embedder.
func newStressTestServer() *httptest.Server {
	// Import integration package indirectly — this test lives in brain_test.
	// Build a minimal Gin server with just /v1/embeddings.
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			http.Error(w, "not found", 404)
			return
		}
		// Mimic the hash embedder response.
		vec := make([]float64, 768)
		for i := range vec {
			vec[i] = float64(i%100) / 100.0
		}
		resp := api.EmbeddingResponse{
			Object: "list",
			Data: []api.EmbeddingData{
				{Object: "embedding", Embedding: vec, Index: 0},
			},
			Model: "hash",
			Usage: &api.Usage{PromptTokens: 1, TotalTokens: 1},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
}
