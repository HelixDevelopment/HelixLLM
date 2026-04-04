package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

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

func TestAltSvcHeaderPort0_NoHeader(t *testing.T) {
	checker := health.NewChecker()
	srv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    0,
		Checker: checker,
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/health", nil)
	srv.Handler().ServeHTTP(w, req)

	altSvc := w.Header().Get("Alt-Svc")
	if altSvc != "" {
		t.Errorf("Alt-Svc header should not be set for port 0, got %q", altSvc)
	}
}

func TestRouterReturnsEngine(t *testing.T) {
	checker := health.NewChecker()
	srv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    0,
		Checker: checker,
	})

	router := srv.Router()
	if router == nil {
		t.Fatal("Router() returned nil")
	}

	// Register a custom route on the router.
	router.GET("/test-custom", func(c *gin.Context) {
		c.String(200, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test-custom", nil)
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("custom route status = %d, want 200", w.Code)
	}
}

func TestHealthEndpointUnhealthy(t *testing.T) {
	checker := health.NewChecker()
	checker.Register("failing", func(ctx context.Context) error {
		return fmt.Errorf("database unreachable")
	})
	srv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    0,
		Checker: checker,
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/health", nil)
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 503 {
		t.Errorf("status = %d, want 503", w.Code)
	}
	var report map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatalf("json decode error: %v", err)
	}
	if report["status"] == "healthy" {
		t.Error("status should not be healthy when a check fails")
	}
}

func TestListenAndServe_MissingTLS(t *testing.T) {
	checker := health.NewChecker()
	srv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    0,
		Checker: checker,
	})

	err := srv.ListenAndServe(context.Background())
	if err == nil {
		t.Fatal("expected error when TLSCert and TLSKey are empty")
	}
}

func TestRequestIDMiddleware(t *testing.T) {
	checker := health.NewChecker()
	srv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    0,
		Checker: checker,
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/health", nil)
	srv.Handler().ServeHTTP(w, req)

	reqID := w.Header().Get("X-Request-ID")
	if reqID == "" {
		t.Error("X-Request-ID header not set by middleware")
	}
}
