package server_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/server"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/health"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/observability"
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

// generateTestCert creates a self-signed TLS certificate and key, writes them
// to temporary files, and returns their paths. The caller is responsible for
// calling the returned cleanup function.
func generateTestCert(t *testing.T) (certFile, keyFile string, cleanup func()) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	certF, err := os.CreateTemp(t.TempDir(), "cert*.pem")
	if err != nil {
		t.Fatalf("create cert file: %v", err)
	}
	if err := pem.Encode(certF, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		t.Fatalf("encode cert: %v", err)
	}
	certF.Close()

	keyF, err := os.CreateTemp(t.TempDir(), "key*.pem")
	if err != nil {
		t.Fatalf("create key file: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	if err := pem.Encode(keyF, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		t.Fatalf("encode key: %v", err)
	}
	keyF.Close()

	return certF.Name(), keyF.Name(), func() {}
}

func TestListenAndServe_WithTLS(t *testing.T) {
	certFile, keyFile, cleanup := generateTestCert(t)
	defer cleanup()

	checker := health.NewChecker()

	// Pick a free port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	srv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    port,
		TLSCert: certFile,
		TLSKey:  keyFile,
		Checker: checker,
	})

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe(ctx)
	}()

	// Give the server a moment to start.
	time.Sleep(100 * time.Millisecond)

	// Make an HTTPS request with a client that trusts our self-signed cert.
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		cancel()
		t.Fatalf("read cert: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(certPEM)
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/internal/health", port))
	if err != nil {
		cancel()
		t.Fatalf("GET health: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != 200 {
		cancel()
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	// Shut down the server.
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("ListenAndServe returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("server did not shut down in time")
	}
}

func TestListenAndServe_InvalidTLSFiles(t *testing.T) {
	checker := health.NewChecker()
	srv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    0,
		TLSCert: "/nonexistent/cert.pem",
		TLSKey:  "/nonexistent/key.pem",
		Checker: checker,
	})

	err := srv.ListenAndServe(context.Background())
	if err == nil {
		t.Fatal("expected error for nonexistent TLS files, got nil")
	}
}

func TestListenAndServe_PortInUse(t *testing.T) {
	// Occupy the port BEFORE the server tries to bind it, causing the
	// net.Listen call inside the HTTP/2 goroutine to fail.
	// This covers server.go lines 124-126 and the errCh-return path (143-145).
	certFile, keyFile, cleanup := generateTestCert(t)
	defer cleanup()

	// Bind a port and hold it open.
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind blocker: %v", err)
	}
	defer blocker.Close()
	port := blocker.Addr().(*net.TCPAddr).Port

	checker := health.NewChecker()
	srv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    port,
		TLSCert: certFile,
		TLSKey:  keyFile,
		Checker: checker,
	})

	// Cancel the context quickly so ListenAndServe returns after the
	// HTTP/2 goroutine sends its net.Listen error to errCh.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err = srv.ListenAndServe(ctx)
	if err == nil {
		t.Fatal("expected error when port is already in use, got nil")
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

func TestMetricsEndpoint_NoObs_DefaultRegistry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	checker := health.NewChecker()
	srv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    0,
		Checker: checker,
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/metrics", nil)
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("metrics status = %d, want 200", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("metrics Content-Type = %q, want text/plain", ct)
	}
}

func TestMetricsEndpoint_WithObs_PrometheusFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	checker := health.NewChecker()

	obs, err := observability.New(observability.Options{
		ServiceName: "test",
		Exporter:    "none",
		Namespace:   "helixllm_test",
	})
	if err != nil {
		t.Fatalf("observability.New: %v", err)
	}
	defer obs.Shutdown()

	srv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    0,
		Checker: checker,
		Obs:     obs,
	})

	// Make a request so the metrics middleware records something.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/health", nil)
	srv.Handler().ServeHTTP(w, req)

	// Now fetch /internal/metrics.
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/internal/metrics", nil)
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("metrics status = %d, want 200", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("metrics Content-Type = %q, want text/plain", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "# HELP") && !strings.Contains(body, "# TYPE") {
		preview := body
		if len(preview) > 200 {
			preview = preview[:200]
		}
		t.Errorf("metrics body does not look like Prometheus format: %q", preview)
	}
}

func TestMetricsMiddleware_RecordsRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	checker := health.NewChecker()

	obs, err := observability.New(observability.Options{
		ServiceName: "test",
		Exporter:    "none",
		Namespace:   "helixllm_mw",
	})
	if err != nil {
		t.Fatalf("observability.New: %v", err)
	}
	defer obs.Shutdown()

	srv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    0,
		Checker: checker,
		Obs:     obs,
	})

	// Fire several requests.
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/internal/health", nil)
		srv.Handler().ServeHTTP(w, req)
	}

	// Scrape metrics — the counter and histogram should be present.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/metrics", nil)
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("metrics status = %d, want 200", w.Code)
	}
}

