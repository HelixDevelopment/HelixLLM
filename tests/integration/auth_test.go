package integration

import (
	"net/http"
	"strings"
	"testing"
)

func TestAuth_SecurityHeaders(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	resp, err := http.Get(ts.URL() + "/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	defer resp.Body.Close()

	headers := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":       "DENY",
	}
	for name, want := range headers {
		got := resp.Header.Get(name)
		if got != want {
			t.Errorf("header %s = %q, want %q", name, got, want)
		}
	}
}

func TestAuth_RequestIdGenerated(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	resp, err := http.Get(ts.URL() + "/internal/health")
	if err != nil {
		t.Fatalf("GET /internal/health: %v", err)
	}
	defer resp.Body.Close()

	reqID := resp.Header.Get("X-Request-ID")
	if reqID == "" {
		t.Error("X-Request-ID header not set")
	}
}

func TestAuth_ChatWithoutAuth(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	body := `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(ts.URL()+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 503 {
		t.Errorf("status = %d, want 200 or 503", resp.StatusCode)
	}
}
