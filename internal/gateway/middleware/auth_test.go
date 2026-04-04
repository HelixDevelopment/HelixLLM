package middleware_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway/middleware"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/gin-gonic/gin"
)

func TestAuthNoKeysConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.APIKeyAuth("")) // empty = open access
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("open access: status = %d, want 200", w.Code)
	}
}

func TestAuthValidKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.APIKeyAuth("sk-test123,sk-other456"))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer sk-test123")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("valid key: status = %d, want 200", w.Code)
	}
}

func TestAuthInvalidKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.APIKeyAuth("sk-test123"))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer sk-wrong")
	r.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("invalid key: status = %d, want 401", w.Code)
	}

	var errResp api.ErrorResponse
	json.Unmarshal(w.Body.Bytes(), &errResp) //nolint:errcheck
	if errResp.Error.Type != "invalid_request_error" {
		t.Errorf("error type = %q, want invalid_request_error", errResp.Error.Type)
	}
}

func TestAuthMissingHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.APIKeyAuth("sk-test123"))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("missing header: status = %d, want 401", w.Code)
	}
}
