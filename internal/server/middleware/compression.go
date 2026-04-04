package middleware

import (
	"bufio"
	"compress/gzip"
	"io"
	"net"
	"strings"
	"sync"

	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
)

// Compression returns a Gin middleware that compresses response bodies.
// Brotli ("br") is preferred when the client advertises it; gzip is the
// fallback. Requests that advertise neither encoding are served uncompressed.
func Compression() gin.HandlerFunc {
	return func(c *gin.Context) {
		accept := c.GetHeader("Accept-Encoding")

		switch {
		case strings.Contains(accept, "br"):
			brotliCompress(c)
		case strings.Contains(accept, "gzip"):
			gzipCompress(c)
		default:
			c.Next()
		}
	}
}

// ---------------------------------------------------------------------------
// Brotli
// ---------------------------------------------------------------------------

var brotliWriterPool = sync.Pool{
	New: func() interface{} {
		return brotli.NewWriter(io.Discard)
	},
}

type brotliResponseWriter struct {
	gin.ResponseWriter
	bw *brotli.Writer
}

func (w *brotliResponseWriter) Write(b []byte) (int, error) {
	return w.bw.Write(b)
}

func (w *brotliResponseWriter) WriteString(s string) (int, error) {
	return w.bw.Write([]byte(s))
}

func (w *brotliResponseWriter) WriteHeader(code int) {
	w.ResponseWriter.WriteHeader(code)
}

func brotliCompress(c *gin.Context) {
	bw := brotliWriterPool.Get().(*brotli.Writer)
	bw.Reset(c.Writer)
	defer func() {
		bw.Close()
		brotliWriterPool.Put(bw)
	}()

	c.Header("Content-Encoding", "br")
	c.Header("Vary", "Accept-Encoding")

	brw := &brotliResponseWriter{
		ResponseWriter: c.Writer,
		bw:             bw,
	}
	c.Writer = brw
	c.Next()
}

// ---------------------------------------------------------------------------
// Gzip
// ---------------------------------------------------------------------------

var gzipWriterPool = sync.Pool{
	New: func() interface{} {
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
		return w
	},
}

type gzipResponseWriter struct {
	gin.ResponseWriter
	gw *gzip.Writer
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	return w.gw.Write(b)
}

func (w *gzipResponseWriter) WriteString(s string) (int, error) {
	return w.gw.Write([]byte(s))
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	w.ResponseWriter.WriteHeader(code)
}

func (w *gzipResponseWriter) Status() int {
	return w.ResponseWriter.Status()
}

func (w *gzipResponseWriter) Written() bool {
	return w.ResponseWriter.Written()
}

func (w *gzipResponseWriter) WriteHeaderNow() {
	w.ResponseWriter.WriteHeaderNow()
}

func (w *gzipResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(interface {
		Hijack() (net.Conn, *bufio.ReadWriter, error)
	}); ok {
		return h.Hijack()
	}
	return nil, nil, nil
}

func gzipCompress(c *gin.Context) {
	gw := gzipWriterPool.Get().(*gzip.Writer)
	gw.Reset(c.Writer)
	defer func() {
		gw.Close()
		gzipWriterPool.Put(gw)
	}()

	c.Header("Content-Encoding", "gzip")
	c.Header("Vary", "Accept-Encoding")

	grw := &gzipResponseWriter{
		ResponseWriter: c.Writer,
		gw:             gw,
	}
	c.Writer = grw
	c.Next()
}
