package middleware

import (
	"bytes"
	"testing"

	"github.com/gin-gonic/gin"
	"net/http/httptest"
)

// TestToonResponseWriter_WriteString_Direct calls WriteString on the unexported
// toonResponseWriter directly to ensure the method is covered.
func TestToonResponseWriter_WriteString_Direct(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	buf := &bytes.Buffer{}
	tw := &toonResponseWriter{
		ResponseWriter: c.Writer,
		buf:            buf,
	}

	n, err := tw.WriteString("hello toon")
	if err != nil {
		t.Fatalf("WriteString error: %v", err)
	}
	if n == 0 {
		t.Error("expected non-zero bytes written")
	}
	if buf.String() != "hello toon" {
		t.Errorf("buf = %q, want %q", buf.String(), "hello toon")
	}
}
