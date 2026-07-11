// Package middleware provides gateway-layer Gin middleware for HelixLLM.
package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

// RequireValidJSON returns a Gin middleware that rejects any POST/PUT/PATCH
// request declaring an "application/json" Content-Type whose body is not
// syntactically valid JSON, responding 400 with an OpenAI-compatible error
// body BEFORE the route handler -- and therefore before any downstream
// brain/provider proxy call to llama.cpp -- ever sees the request.
//
// Background (SEC-02, §11.4.115/§11.4.123/§11.4.146): a prior security
// probe wave (docs/qa/ext_security_mem_bench_20260711T150836Z/RESULTS.md)
// found that malformed JSON sent DIRECTLY to the raw llama.cpp server
// (bypassing HelixLLM entirely) surfaces as HTTP 500 -- llama.cpp's
// nlohmann::json parser reports parse errors via a generic 500, which is a
// documented upstream (not HelixLLM) HTTP-semantics defect, out of scope
// for this submodule (the coder process is a third-party binary).
//
// A live reproduction against THIS gateway (with both a nil Completer and a
// REAL Completer wired to the live coder backend -- see
// jsonvalidate_regression_test.go) found every v1 POST handler already
// rejects malformed JSON with 400 via gin's ShouldBindJSON, so no request
// with a JSON-syntax error ever reaches the coder through the facade. That
// protection was implicit and per-handler, though: a future handler that
// forgot to call ShouldBindJSON before touching the Completer would silently
// reopen the SEC-02 class. RequireValidJSON makes the guarantee explicit,
// centrally enforced at the router-group level, and independently
// regression-tested, closing that latent gap without changing behaviour for
// any currently-well-formed request.
func RequireValidJSON() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
		default:
			c.Next()
			return
		}

		if !strings.Contains(c.GetHeader("Content-Type"), "application/json") {
			c.Next()
			return
		}

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			// Not a JSON-syntax question -- let the handler's own body
			// reading path surface this failure as it already does.
			c.Next()
			return
		}
		// Restore the body so the handler's own ShouldBindJSON (or any
		// other body reader downstream) can still read it in full -- this
		// middleware only PEEKS at the bytes, it never consumes them.
		c.Request.Body = io.NopCloser(bytes.NewReader(body))

		if len(body) == 0 {
			// An empty body is not a JSON-syntax error this gate owns;
			// downstream ShouldBindJSON already turns it into a
			// consistent 400 ("EOF") today. Leave that path unchanged.
			c.Next()
			return
		}

		if !json.Valid(body) {
			c.AbortWithStatusJSON(http.StatusBadRequest, api.ErrorResponse{
				Error: api.ErrorDetail{
					Message: "invalid request body: malformed JSON",
					Type:    "invalid_request_error",
				},
			})
			return
		}

		c.Next()
	}
}
