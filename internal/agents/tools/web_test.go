package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// WebSearchTool
// ---------------------------------------------------------------------------

func TestWebSearchTool_Name(t *testing.T) {
	tool := NewWebSearchTool()
	if tool.Name() != "web_search" {
		t.Errorf("expected web_search, got %s", tool.Name())
	}
}

func TestWebSearchTool_Execute(t *testing.T) {
	tool := NewWebSearchTool()

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"query": "golang concurrency patterns",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "not available in local mode") {
		t.Errorf("expected placeholder message, got: %s", out)
	}
	if !strings.Contains(out, "golang concurrency patterns") {
		t.Error("expected query to be echoed in output")
	}
}

func TestWebSearchTool_NilArgs(t *testing.T) {
	tool := NewWebSearchTool()

	_, err := tool.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil args")
	}
}

func TestWebSearchTool_MissingQuery(t *testing.T) {
	tool := NewWebSearchTool()

	_, err := tool.Execute(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Error("expected error for missing query")
	}
}

// ---------------------------------------------------------------------------
// FetchURLTool
// ---------------------------------------------------------------------------

func TestFetchURLTool_Name(t *testing.T) {
	tool := NewFetchURLTool()
	if tool.Name() != "fetch_url" {
		t.Errorf("expected fetch_url, got %s", tool.Name())
	}
}

func TestFetchURLTool_Execute(t *testing.T) {
	// Start a local test server.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("hello from test server"))
	}))
	defer ts.Close()

	tool := NewFetchURLTool()
	ctx := context.Background()

	out, err := tool.Execute(ctx, map[string]interface{}{
		"url": ts.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "hello from test server") {
		t.Errorf("expected response body, got: %s", out)
	}
}

func TestFetchURLTool_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	tool := NewFetchURLTool()

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"url": ts.URL,
	})
	if err == nil {
		t.Error("expected error for 404 response")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 in error, got: %v", err)
	}
}

func TestFetchURLTool_Truncation(t *testing.T) {
	// Serve a response larger than 10KB.
	bigBody := strings.Repeat("x", 20000)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(bigBody))
	}))
	defer ts.Close()

	tool := NewFetchURLTool()

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"url": ts.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "truncated") {
		t.Error("expected truncation marker in output")
	}
}

func TestFetchURLTool_InvalidURL(t *testing.T) {
	tool := NewFetchURLTool()

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"url": "not-a-valid-url",
	})
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestFetchURLTool_NilArgs(t *testing.T) {
	tool := NewFetchURLTool()

	_, err := tool.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil args")
	}
}
