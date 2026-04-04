package middleware_test

import (
	"net/http/httptest"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway/middleware"
	"github.com/gin-gonic/gin"
)

func TestRateLimitAllowed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RateLimit(100)) // 100 req/min
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestRateLimitExceeded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RateLimit(2)) // 2 req/min
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		r.ServeHTTP(w, req)

		if i < 2 && w.Code != 200 {
			t.Errorf("request %d: status = %d, want 200", i, w.Code)
		}
		if i == 2 && w.Code != 429 {
			t.Errorf("request %d: status = %d, want 429", i, w.Code)
		}
	}
}

func TestRateLimitZeroDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RateLimit(0)) // disabled
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	for i := 0; i < 100; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Errorf("request %d should pass when disabled", i)
			break
		}
	}
}
