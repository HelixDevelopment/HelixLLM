//go:build e2e

package e2e_test

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

var baseURL = "https://localhost:8443"

func init() {
	http.DefaultTransport = &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec
		},
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

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("chat status = %d, body = %s", resp.StatusCode, respBody)
	}
}
