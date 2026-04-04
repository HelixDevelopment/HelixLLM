package middleware_test

import (
	"net/http/httptest"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway/middleware"
	"github.com/gin-gonic/gin"
)

func TestNegotiationDefaultJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ContentNegotiation())
	r.GET("/test", func(c *gin.Context) {
		format := middleware.GetContentFormat(c)
		c.String(200, format)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Body.String() != "json" {
		t.Errorf("default format = %q, want json", w.Body.String())
	}
	if w.Header().Get("X-Content-Format") != "json" {
		t.Errorf("X-Content-Format = %q, want json", w.Header().Get("X-Content-Format"))
	}
}

func TestNegotiationTOON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ContentNegotiation())
	r.GET("/test", func(c *gin.Context) {
		format := middleware.GetContentFormat(c)
		c.String(200, format)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept", "application/toon")
	r.ServeHTTP(w, req)

	if w.Body.String() != "toon" {
		t.Errorf("toon format = %q, want toon", w.Body.String())
	}
	if w.Header().Get("X-Content-Format") != "toon" {
		t.Errorf("X-Content-Format = %q, want toon", w.Header().Get("X-Content-Format"))
	}
}

func TestNegotiationExplicitJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ContentNegotiation())
	r.GET("/test", func(c *gin.Context) {
		format := middleware.GetContentFormat(c)
		c.String(200, format)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept", "application/json")
	r.ServeHTTP(w, req)

	if w.Body.String() != "json" {
		t.Errorf("explicit json = %q, want json", w.Body.String())
	}
}
