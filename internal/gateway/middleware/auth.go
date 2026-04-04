// Package middleware provides gateway-layer Gin middleware for HelixLLM.
package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

// contextKeyAPIKey is the Gin context key used to store the validated API key.
const contextKeyAPIKey = "api_key"

// APIKeyAuth returns a Gin middleware that validates Bearer API keys.
//
// If configuredKeys is empty the middleware runs in open-access mode: every
// request is allowed through without any authentication check.
//
// If configuredKeys is a non-empty comma-separated list, the middleware
// extracts the Bearer token from the Authorization header and checks it
// against each key in the list. On success the validated key is stored in
// the Gin context under "api_key". On failure a 401 response is returned
// using the OpenAI-compatible error JSON format.
func APIKeyAuth(configuredKeys string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Open-access mode: no keys configured, allow all requests.
		if configuredKeys == "" {
			c.Next()
			return
		}

		// Extract the Bearer token from the Authorization header.
		authHeader := c.GetHeader("Authorization")
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == "" || token == authHeader {
			// Header was absent or did not start with "Bearer ".
			abortUnauthorized(c, "missing or invalid Authorization header")
			return
		}

		// Validate the token against the configured key list.
		for _, key := range strings.Split(configuredKeys, ",") {
			if strings.TrimSpace(key) == token {
				c.Set(contextKeyAPIKey, token)
				c.Next()
				return
			}
		}

		abortUnauthorized(c, "invalid API key")
	}
}

// abortUnauthorized writes a 401 response in OpenAI error format and aborts
// the request chain.
func abortUnauthorized(c *gin.Context, message string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, api.ErrorResponse{
		Error: api.ErrorDetail{
			Message: message,
			Type:    "invalid_request_error",
		},
	})
}
