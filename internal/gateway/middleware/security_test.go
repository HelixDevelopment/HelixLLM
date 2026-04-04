package middleware_test

import (
	"net/http/httptest"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway/middleware"
	"github.com/gin-gonic/gin"
)

func TestSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.SecurityHeaders())
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	headers := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"X-XSS-Protection":       "1; mode=block",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}

	for name, want := range headers {
		got := w.Header().Get(name)
		if got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}

	if w.Header().Get("Strict-Transport-Security") == "" {
		t.Error("HSTS header not set")
	}
	if w.Header().Get("Content-Security-Policy") == "" {
		t.Error("CSP header not set")
	}
}
