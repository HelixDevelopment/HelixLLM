// Package gateway provides gateway-layer handlers and utilities for HelixLLM.
package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/gin-gonic/gin"
)

var bufPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

// SSEWriter wraps a Gin context and writes Server-Sent Events in the OpenAI
// text/event-stream format.
type SSEWriter struct {
	c *gin.Context
}

// NewSSEWriter returns an SSEWriter backed by the given Gin context.
func NewSSEWriter(c *gin.Context) *SSEWriter {
	return &SSEWriter{c: c}
}

// WriteHeader sets the required SSE response headers and sends a 200 status.
// It must be called before any WriteEvent or WriteDone calls.
func (w *SSEWriter) WriteHeader() {
	w.c.Header("Content-Type", "text/event-stream")
	w.c.Header("Cache-Control", "no-cache")
	w.c.Header("Connection", "keep-alive")
	w.c.Header("X-Accel-Buffering", "no")
	w.c.Status(200)
}

// WriteEvent serialises data as JSON and writes a single SSE data line
// followed by the mandatory blank line separator. It flushes the response
// writer immediately so the client receives the chunk without buffering.
// A sync.Pool is used to reuse encoding buffers across calls.
func (w *SSEWriter) WriteEvent(data interface{}) error {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	if err := json.NewEncoder(buf).Encode(data); err != nil {
		return err
	}
	// Encode adds a trailing newline; trim it for SSE format.
	b := bytes.TrimRight(buf.Bytes(), "\n")
	fmt.Fprintf(w.c.Writer, "data: %s\n\n", b)
	w.c.Writer.Flush()
	return nil
}

// WriteDone writes the terminal SSE sentinel `data: [DONE]` and flushes.
func (w *SSEWriter) WriteDone() {
	fmt.Fprintf(w.c.Writer, "data: [DONE]\n\n")
	w.c.Writer.Flush()
}
