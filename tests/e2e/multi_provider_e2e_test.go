// Package e2e_test holds end-to-end tests for HelixLLM.
// Tests in this file send real HTTP requests to a running HelixLLM instance.
// They are skipped unless HELIX_LLM_URL is set.
package e2e_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestMultiProvider_E2E_ChatCompletion sends a real chat completion request
// to the running HelixLLM server and verifies a valid response is returned.
// Skipped unless HELIX_LLM_URL is set (e.g. "https://localhost:8443").
func TestMultiProvider_E2E_ChatCompletion(t *testing.T) {
	baseURL := os.Getenv("HELIX_LLM_URL")
	if baseURL == "" {
		t.Skip("HELIX_LLM_URL not set — skipping multi-provider E2E test")  // SKIP-OK: #legacy-untriaged
	}

	body := `{"model":"auto","messages":[{"role":"user","content":"Reply with exactly one word: ready"}],"max_tokens":10}`

	resp, err := http.Post(
		baseURL+"/v1/chat/completions",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST /v1/chat/completions: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result.Choices) == 0 {
		t.Fatal("expected at least one choice in response")
	}
	if result.Choices[0].Message.Content == "" {
		t.Errorf("expected non-empty message content")
	}
	t.Logf("response content: %q", result.Choices[0].Message.Content)
}
