package server_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/server"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/health"
)

func TestNewServer(t *testing.T) {
	checker := health.NewChecker()
	srv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    0,
		Checker: checker,
	})
	if srv == nil {
		t.Fatal("New() returned nil")
	}
}

func TestHealthEndpoint(t *testing.T) {
	checker := health.NewChecker()
	checker.Register("test", func(ctx context.Context) error { return nil })
	srv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    0,
		Checker: checker,
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/health", nil)
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var report map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatalf("json decode error: %v", err)
	}
	if report["status"] != "healthy" {
		t.Errorf("status = %v, want healthy", report["status"])
	}
}

func TestAltSvcHeader(t *testing.T) {
	checker := health.NewChecker()
	srv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    8443,
		Checker: checker,
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/health", nil)
	srv.Handler().ServeHTTP(w, req)

	altSvc := w.Header().Get("Alt-Svc")
	if altSvc == "" {
		t.Error("Alt-Svc header not set")
	}
}
