package middleware

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
)

// TestGzipResponseWriter_WriteString calls WriteString directly on the
// unexported gzipResponseWriter.
func TestGzipResponseWriter_WriteString(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	var buf bytes.Buffer
	gw, err := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	if err != nil {
		t.Fatalf("gzip.NewWriterLevel: %v", err)
	}

	grw := &gzipResponseWriter{
		ResponseWriter: c.Writer,
		gw:             gw,
	}

	n, err := grw.WriteString("hello gzip")
	if err != nil {
		t.Fatalf("WriteString error: %v", err)
	}
	if n == 0 {
		t.Error("expected non-zero bytes written")
	}

	gw.Close()
	r, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read decompressed: %v", err)
	}
	if string(out) != "hello gzip" {
		t.Errorf("decompressed = %q, want %q", string(out), "hello gzip")
	}
}

// TestBrotliResponseWriter_WriteString calls WriteString directly on the
// unexported brotliResponseWriter.
func TestBrotliResponseWriter_WriteString(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	var buf bytes.Buffer
	bw := brotli.NewWriter(&buf)

	brw := &brotliResponseWriter{
		ResponseWriter: c.Writer,
		bw:             bw,
	}

	n, err := brw.WriteString("hello brotli")
	if err != nil {
		t.Fatalf("WriteString error: %v", err)
	}
	if n == 0 {
		t.Error("expected non-zero bytes written")
	}

	bw.Close()
	out, err := io.ReadAll(brotli.NewReader(&buf))
	if err != nil {
		t.Fatalf("brotli decode: %v", err)
	}
	if string(out) != "hello brotli" {
		t.Errorf("decompressed = %q, want %q", string(out), "hello brotli")
	}
}

// hijackableResponseWriter is a gin.ResponseWriter that implements http.Hijacker,
// allowing the hijack-capable branch in gzipResponseWriter.Hijack to be exercised.
type hijackableResponseWriter struct {
	gin.ResponseWriter
	hijackCalled bool
}

func (h *hijackableResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijackCalled = true
	return nil, nil, nil
}

// TestGzipResponseWriter_Hijack_Hijackable exercises the hijacker branch in
// gzipResponseWriter.Hijack — when the embedded ResponseWriter implements
// http.Hijacker, the call must be delegated to it.
func TestGzipResponseWriter_Hijack_Hijackable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	var buf bytes.Buffer
	gw, _ := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)

	hrw := &hijackableResponseWriter{ResponseWriter: c.Writer}
	grw := &gzipResponseWriter{
		ResponseWriter: hrw,
		gw:             gw,
	}

	conn, rw, err := grw.Hijack()
	if err != nil {
		t.Errorf("unexpected Hijack error: %v", err)
	}
	if conn != nil {
		t.Errorf("expected nil conn, got: %v", conn)
	}
	if rw != nil {
		t.Errorf("expected nil ReadWriter, got: %v", rw)
	}
	if !hrw.hijackCalled {
		t.Error("expected underlying Hijack to be called")
	}
}

// Ensure minimalResponseWriter satisfies gin.ResponseWriter for the Pusher check.
var _ http.Pusher = (*nopPusher)(nil)

type nopPusher struct{}

func (n *nopPusher) Push(_ string, _ *http.PushOptions) error { return nil }
