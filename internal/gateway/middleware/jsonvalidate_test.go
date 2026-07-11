package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestRequireValidJSON_MalformedBody_RejectsBeforeNextHandler proves the
// middleware, IN ISOLATION (no downstream handler protection involved),
// rejects malformed JSON with 400 and never invokes the next handler --
// i.e. the guard genuinely blocks the request rather than merely logging.
func TestRequireValidJSON_MalformedBody_RejectsBeforeNextHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name string
		body string
	}{
		{"truncated_json", `{"model": "x", "messages": [{"role":"user","content":"trunc`},
		{"unquoted_key_trailing_comma", `{model: "x", max_tokens: 8,}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nextCalled := false
			r := gin.New()
			r.Use(RequireValidJSON())
			r.POST("/probe", func(c *gin.Context) {
				nextCalled = true
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodPost, "/probe", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			if nextCalled {
				t.Fatal("next handler was called for malformed JSON — middleware did not actually block the request")
			}
			if !bytes.Contains(w.Body.Bytes(), []byte("invalid_request_error")) {
				t.Fatalf("body missing OpenAI-compat error type: %s", w.Body.String())
			}
		})
	}
}

// TestRequireValidJSON_WellFormedBody_PassesThrough proves well-formed
// requests are entirely unaffected: the next handler runs and receives the
// original body bytes (the middleware only peeks, never consumes).
func TestRequireValidJSON_WellFormedBody_PassesThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := `{"model": "x", "messages": [{"role":"user","content":"hello"}]}`
	var handlerSawBody string
	r := gin.New()
	r.Use(RequireValidJSON())
	r.POST("/probe", func(c *gin.Context) {
		raw, _ := io.ReadAll(c.Request.Body)
		handlerSawBody = string(raw)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/probe", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if handlerSawBody != body {
		t.Fatalf("handler saw body %q, want %q — middleware consumed/corrupted the body", handlerSawBody, body)
	}
}

// TestRequireValidJSON_NonJSONContentType_PassesThrough proves the gate is
// scoped to declared JSON bodies only (e.g. multipart/form uploads on other
// future endpoints are not affected).
func TestRequireValidJSON_NonJSONContentType_PassesThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)

	nextCalled := false
	r := gin.New()
	r.Use(RequireValidJSON())
	r.POST("/probe", func(c *gin.Context) {
		nextCalled = true
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/probe", bytes.NewBufferString("not json at all {{{"))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if !nextCalled {
		t.Fatal("next handler was not called for a non-JSON Content-Type request")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// TestRequireValidJSON_GETRequest_PassesThrough proves body-less methods
// (GET /v1/models, GET /v1/hardware) are never touched by this gate.
func TestRequireValidJSON_GETRequest_PassesThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)

	nextCalled := false
	r := gin.New()
	r.Use(RequireValidJSON())
	r.GET("/probe", func(c *gin.Context) {
		nextCalled = true
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if !nextCalled {
		t.Fatal("next handler was not called for a GET request")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}
