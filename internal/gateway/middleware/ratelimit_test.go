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

func TestRateLimitClientIP_RemoteAddrWithPort(t *testing.T) {
	// Test that the rate limiter correctly extracts the client IP from RemoteAddr
	// when ClientIP() returns empty (by using a non-trusted proxy address).
	// In practice Gin's ClientIP() always returns something, so we exercise the
	// rate limit with an explicit RemoteAddr that has a port.
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RateLimit(100))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.5:54321"
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestRateLimitClientIP_RemoteAddrNoPort(t *testing.T) {
	// Test rate limiter with a RemoteAddr that has no port (edge case).
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RateLimit(100))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.6" // no port
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestRateLimitNegativeDisabled(t *testing.T) {
	// A negative maxPerMinute should also disable the rate limiter.
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RateLimit(-1))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}
