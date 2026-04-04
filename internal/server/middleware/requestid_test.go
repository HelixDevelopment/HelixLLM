package middleware_test

import (
	"net/http/httptest"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func TestRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestID())
	r.GET("/test", func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		c.String(200, id)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	rid := w.Header().Get("X-Request-ID")
	if rid == "" {
		t.Error("X-Request-ID header not set in response")
	}
	if len(rid) < 20 {
		t.Errorf("X-Request-ID too short: %q", rid)
	}
}

func TestRequestIDPreserved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestID())
	r.GET("/test", func(c *gin.Context) {
		c.String(200, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "custom-id-123")
	r.ServeHTTP(w, req)

	if w.Header().Get("X-Request-ID") != "custom-id-123" {
		t.Error("existing X-Request-ID was overwritten")
	}
}
