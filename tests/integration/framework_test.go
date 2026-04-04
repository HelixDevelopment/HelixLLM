package integration

import (
	"testing"
)

func TestNewTestServer(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	if ts.Server == nil {
		t.Fatal("Server is nil")
	}
	if ts.Brain == nil {
		t.Fatal("Brain is nil")
	}
	if ts.Pipeline == nil {
		t.Fatal("Pipeline is nil")
	}
	if ts.URL() == "" {
		t.Fatal("URL() returned empty string")
	}
}

func TestFramework_HealthEndpoint(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	resp, err := ts.Get("/internal/health")
	if err != nil {
		t.Fatalf("GET /internal/health error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := ReadJSON(resp, &result); err != nil {
		t.Fatalf("ReadJSON error: %v", err)
	}
	if result["status"] != "healthy" {
		t.Errorf("status = %v, want healthy", result["status"])
	}
}
