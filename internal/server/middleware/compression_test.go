package middleware_test

import (
	"compress/gzip"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/server/middleware"
	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
)

func setupCompressionRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Compression())
	r.GET("/test", func(c *gin.Context) {
		data := strings.Repeat("Hello, World! ", 100)
		c.String(200, data)
	})
	return r
}

func TestBrotliCompression(t *testing.T) {
	r := setupCompressionRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "br, gzip")
	r.ServeHTTP(w, req)

	if w.Header().Get("Content-Encoding") != "br" {
		t.Errorf("Content-Encoding = %q, want %q", w.Header().Get("Content-Encoding"), "br")
	}
	reader := brotli.NewReader(w.Body)
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("brotli decode error: %v", err)
	}
	if !strings.Contains(string(body), "Hello, World!") {
		t.Error("decompressed body missing expected content")
	}
}

func TestGzipFallback(t *testing.T) {
	r := setupCompressionRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("Content-Encoding = %q, want %q", w.Header().Get("Content-Encoding"), "gzip")
	}
	reader, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("gzip reader error: %v", err)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("gzip decode error: %v", err)
	}
	if !strings.Contains(string(body), "Hello, World!") {
		t.Error("decompressed body missing expected content")
	}
}

func TestNoCompression(t *testing.T) {
	r := setupCompressionRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Header().Get("Content-Encoding") != "" {
		t.Errorf("Content-Encoding should be empty, got %q", w.Header().Get("Content-Encoding"))
	}
	if !strings.Contains(w.Body.String(), "Hello, World!") {
		t.Error("body missing expected content")
	}
}
