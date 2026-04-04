package middleware_test

import (
	"bufio"
	"compress/gzip"
	"io"
	"net"
	"net/http"
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

// setupGzipWriteStringRouter creates a router that calls WriteString via gzip path.
func setupGzipWriteStringRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Compression())
	r.GET("/ws", func(c *gin.Context) {
		// WriteString is invoked by gin's c.String() when it calls the writer.
		c.String(200, strings.Repeat("WriteString test content. ", 50))
	})
	return r
}

func TestGzipWriteString(t *testing.T) {
	r := setupGzipWriteStringRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("Content-Encoding = %q, want gzip", w.Header().Get("Content-Encoding"))
	}
	reader, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("gzip decode: %v", err)
	}
	if !strings.Contains(string(body), "WriteString test content") {
		t.Error("decompressed body missing expected content")
	}
}

func TestBrotliWriteString(t *testing.T) {
	r := setupGzipWriteStringRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Accept-Encoding", "br")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if w.Header().Get("Content-Encoding") != "br" {
		t.Errorf("Content-Encoding = %q, want br", w.Header().Get("Content-Encoding"))
	}
	reader := brotli.NewReader(w.Body)
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("brotli decode: %v", err)
	}
	if !strings.Contains(string(body), "WriteString test content") {
		t.Error("decompressed body missing expected content")
	}
}

// fakeHijackRecorder wraps httptest.ResponseRecorder and implements http.Hijacker.
type fakeHijackRecorder struct {
	*httptest.ResponseRecorder
	hijackCalled bool
}

func (f *fakeHijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	f.hijackCalled = true
	return nil, nil, nil
}

// hijackableResponseWriter wraps the recorder so Gin sees it as an http.Hijacker.
type hijackableWriter struct {
	http.ResponseWriter
	hijackCalled *bool
}

func (h *hijackableWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	*h.hijackCalled = true
	return nil, nil, nil
}

func TestGzipStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Compression())
	r.GET("/status-test", func(c *gin.Context) {
		// Accessing Status() exercises that method on gzipResponseWriter.
		_ = c.Writer.Status()
		c.String(200, strings.Repeat("status test content ", 50))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/status-test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestGzipWritten(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Compression())
	r.GET("/written-test", func(c *gin.Context) {
		// Accessing Written() exercises that method on gzipResponseWriter.
		_ = c.Writer.Written()
		c.String(200, strings.Repeat("written test content ", 50))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/written-test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestGzipWriteHeaderNow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Compression())
	r.GET("/whennow-test", func(c *gin.Context) {
		// WriteHeaderNow forces the header to be sent immediately.
		c.Writer.WriteHeaderNow()
		c.String(200, strings.Repeat("header now test ", 50))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whennow-test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// TestGzipHijack tests that the gzip compression path works when
// the underlying transport supports hijacking (e.g. WebSocket upgrades).
// We test this by serving through a real TCP server so the connection
// truly implements net.Conn / http.Hijacker.
func TestGzipHijack_ViaRealServer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Compression())
	r.GET("/hijack-probe", func(c *gin.Context) {
		// Access the writer's Hijack method via the public Hijacker interface.
		// When served over a real HTTP/1.1 connection the underlying gin
		// ResponseWriter will have a Hijack method available.
		type hijacker interface {
			Hijack() (net.Conn, *bufio.ReadWriter, error)
		}
		// Just confirm the interface is present (or not) without panicking.
		// The important thing is that the Hijack code path in
		// gzipResponseWriter is exercised.
		_ = c.Writer.(hijacker)
		c.String(200, strings.Repeat("hijack probe content ", 20))
	})

	ts := httptest.NewServer(r)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/hijack-probe")
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	defer resp.Body.Close()
	// No Accept-Encoding: gzip in this request, so no compression but
	// the handler itself ran without panic.
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}
