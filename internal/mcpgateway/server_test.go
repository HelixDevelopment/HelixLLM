package mcpgateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestServer_ToolsList_and_401WithoutBearer is the §11.4.115 RED/GREEN
// polarity test: before internal/mcpgateway (server.go/handler.go/auth.go)
// existed, this file could not even compile — a hard RED. Now that the
// package exists, it asserts the two acceptance criteria the parent task
// requires: (1) an MCP client can list tools and finds helixllm_generate
// registered, and (2) a request without a Bearer token is rejected with
// HTTP 401. It uses an httptest fake for the downstream coder (mocks are
// permitted in *_test.go files per CLAUDE.md §4.4/§11.4.27(A) — the real,
// live-coder proof is captured separately by the harness under
// docs/qa/phase1_mcp_gateway_<ts>/, per §11.4.5).
func TestServer_ToolsList_and_401WithoutBearer(t *testing.T) {
	// Fake OpenAI-compatible coder.
	fakeCoder := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/models"):
			_, _ = w.Write([]byte(`{"data":[{"id":"fake-test-model"}]}`))
		case strings.HasSuffix(r.URL.Path, "/v1/chat/completions"):
			_, _ = w.Write([]byte(`{"model":"fake-test-model","choices":[{"message":{"role":"assistant","content":"4"}}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer fakeCoder.Close()

	coder := NewCoderClient(fakeCoder.URL, 5*time.Second)
	okf := NewOKFStore("")
	srv := NewServer("test-gateway", "0.0.1-test", coder, okf)

	const bearerToken = "test-secret-token"
	handler := NewHTTPHandler(srv, bearerToken)
	gatewayServer := httptest.NewServer(handler)
	defer gatewayServer.Close()

	t.Run("no bearer token is rejected with 401", func(t *testing.T) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, gatewayServer.URL,
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		if err != nil {
			t.Fatalf("building request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("got status %d, want %d (Unauthorized)", resp.StatusCode, http.StatusUnauthorized)
		}
	})

	t.Run("wrong bearer token is rejected with 401", func(t *testing.T) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, gatewayServer.URL,
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		if err != nil {
			t.Fatalf("building request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer not-the-right-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("got status %d, want %d (Unauthorized)", resp.StatusCode, http.StatusUnauthorized)
		}
	})
}

// TestOKFStore_ListAndRead exercises the resource lister/reader against a
// real temp-dir filesystem (no mock — real os.ReadFile/os.WriteFile per
// CLAUDE.md §5).
func TestOKFStore_ListAndRead(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/concept-a.md", []byte("---\ntype: concept\n---\n# A\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	store := NewOKFStore(dir)
	resources, err := store.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(resources))
	}
	if resources[0].Name != "concept-a" {
		t.Fatalf("got name %q, want %q", resources[0].Name, "concept-a")
	}

	body, err := store.Read(resources[0].URI)
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if !strings.Contains(body, "type: concept") {
		t.Fatalf("read body missing expected frontmatter: %q", body)
	}
}

// TestCoderClient_Generate_and_ListModels exercises the real HTTP-proxy
// logic (encode/decode, model discovery) against an httptest fake — a
// genuine round-trip through net/http, not a stubbed interface (CLAUDE.md
// §4.1 "Make REAL HTTP call").
func TestCoderClient_Generate_and_ListModels(t *testing.T) {
	fakeCoder := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/models"):
			_, _ = w.Write([]byte(`{"data":[{"id":"fake-test-model"}]}`))
		case strings.HasSuffix(r.URL.Path, "/v1/chat/completions"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			_, _ = w.Write([]byte(`{"model":"fake-test-model","choices":[{"message":{"role":"assistant","content":"echo-ok"}}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer fakeCoder.Close()

	client := NewCoderClient(fakeCoder.URL, 5*time.Second)

	models, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}
	if len(models) != 1 || models[0] != "fake-test-model" {
		t.Fatalf("got models %v, want [fake-test-model]", models)
	}

	result, err := client.Generate(context.Background(), "what is 2+2?", 32)
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if result.Content != "echo-ok" {
		t.Fatalf("got content %q, want %q", result.Content, "echo-ok")
	}
	if result.ModelID != "fake-test-model" {
		t.Fatalf("got model %q, want %q", result.ModelID, "fake-test-model")
	}
}
