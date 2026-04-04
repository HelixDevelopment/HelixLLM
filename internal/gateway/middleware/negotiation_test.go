package middleware_test

import (
	"net/http/httptest"
	"strings"
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

	if w.Header().Get("X-Content-Format") != "toon" {
		t.Errorf("X-Content-Format = %q, want toon", w.Header().Get("X-Content-Format"))
	}
}

func TestNegotiationTOONContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ContentNegotiation())
	r.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"format": "toon", "ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept", "application/toon")
	r.ServeHTTP(w, req)

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/toon") {
		t.Errorf("Content-Type = %q, want application/toon", ct)
	}
	if w.Header().Get("X-Content-Format") != "toon" {
		t.Errorf("X-Content-Format = %q, want toon", w.Header().Get("X-Content-Format"))
	}
}

func TestNegotiationTOONBodyEncoded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ContentNegotiation())
	r.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"key": "value"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept", "application/toon")
	r.ServeHTTP(w, req)

	body := w.Body.String()
	if body == "" {
		t.Error("expected non-empty TOON-encoded body, got empty string")
	}
	// TOON currently falls back to compact JSON — verify the payload is present.
	if !strings.Contains(body, "value") {
		t.Errorf("TOON body = %q, expected to contain original payload", body)
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

func TestIsTOONRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ContentNegotiation())
	r.GET("/test", func(c *gin.Context) {
		if middleware.IsTOONRequest(c) {
			c.String(200, "toon")
		} else {
			c.String(200, "not-toon")
		}
	})

	// TOON Accept header
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept", "application/toon")
	r.ServeHTTP(w, req)
	if w.Body.String() != "toon" {
		t.Errorf("IsTOONRequest with toon Accept = %q, want toon", w.Body.String())
	}

	// Default (no Accept header)
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w2, req2)
	if w2.Body.String() != "not-toon" {
		t.Errorf("IsTOONRequest without toon Accept = %q, want not-toon", w2.Body.String())
	}
}
