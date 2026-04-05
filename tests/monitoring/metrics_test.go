//go:build monitoring

package monitoring_test

import (
	"crypto/tls"
	"io"
	"net/http"
	"strings"
	"testing"
)

var baseURL = "https://localhost:8443"

func init() {
	http.DefaultTransport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	}
}

func TestMetrics_EndpointAccessible(t *testing.T) {
	resp, err := http.Get(baseURL + "/internal/metrics")
	if err != nil {
		t.Fatalf("GET /internal/metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	content := string(body)
	for _, metric := range []string{"http_requests_total", "go_goroutines", "go_memstats_alloc_bytes"} {
		if !strings.Contains(content, metric) {
			t.Errorf("metrics missing %q", metric)
		}
	}
}

func TestHealth_AggregatesComponents(t *testing.T) {
	resp, err := http.Get(baseURL + "/internal/health")
	if err != nil {
		t.Fatalf("GET /internal/health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("health status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestLogging_RequestIdHeader(t *testing.T) {
	resp, err := http.Get(baseURL + "/internal/health")
	if err != nil {
		t.Fatalf("GET /internal/health: %v", err)
	}
	defer resp.Body.Close()
	reqID := resp.Header.Get("X-Request-Id")
	if reqID == "" {
		t.Error("X-Request-Id header not set")
	}
}
