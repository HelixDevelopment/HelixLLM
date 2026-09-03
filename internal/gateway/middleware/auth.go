// Package middleware provides gateway-layer Gin middleware for HelixLLM.
package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

// Gin context keys set by the auth middleware once a request is authenticated.
const (
	// ContextKeyAPIKey holds the validated API key, when the request
	// authenticated with one. Exported so handlers that need to know which
	// key called them (e.g. the token-exchange handler) can read it without
	// re-parsing the Authorization header.
	ContextKeyAPIKey = "api_key"

	// ContextKeyAuthScheme is SchemeAPIKey or SchemeJWT.
	ContextKeyAuthScheme = "auth_scheme"

	// ContextKeyAuthSubject holds the JWT subject, when the request
	// authenticated with a token.
	ContextKeyAuthSubject = "auth_subject"
)

// Authentication schemes reported in ContextKeyAuthScheme.
const (
	SchemeAPIKey = "api_key"
	SchemeJWT    = "jwt"
)

// TokenVerifier is the JWT contract the auth middleware needs.
//
// An interface rather than *auth.Verifier so this package keeps no dependency
// on the JWT implementation and tests can drive the middleware with a stub
// verifier. internal/auth.Verifier satisfies it, and a nil *auth.Verifier
// (the "JWT disabled" value) satisfies it too — its Enabled() is
// nil-receiver safe.
type TokenVerifier interface {
	// Enabled reports whether JWT auth is configured.
	Enabled() bool
	// Verify checks a token and returns its subject.
	Verify(token string) (string, error)
}

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
//
// This is APIKeyOrJWTAuth with JWT disabled. It is kept as its own name
// because it is what most of this codebase's own tests construct, and because
// "API keys only" remains the shipped default.
func APIKeyAuth(configuredKeys string) gin.HandlerFunc {
	return APIKeyOrJWTAuth(configuredKeys, nil)
}

// APIKeyOrJWTAuth returns a Gin middleware that accepts EITHER a configured
// API key or a valid JWT in the Authorization Bearer header.
//
// WHY BOTH CREDENTIALS ON ONE SURFACE, RATHER THAN JWT REPLACING KEYS
// ===================================================================
//
// The routes behind this middleware are OpenAI- and Anthropic-wire-compatible
// (/v1/chat/completions, /v1/models, /v1/messages, ...). Genuine SDK clients
// pointed at this server send an API key, because that is what the wire
// protocol they implement specifies — they have no way to obtain or present
// this server's JWT. helix_code hit exactly this and wrote it down: its
// wireFacadeAuthMiddleware is a deliberately separate check from its
// JWT authMiddleware because "wiring authMiddleware()/VerifyJWTWithDB here
// would reject every genuine client and defeat the wire-compatibility this
// facade exists to provide" (helix_code internal/server/server.go:573-581).
//
// So JWT is ADDITIVE here, not a replacement: an API key keeps working
// forever, and a JWT is accepted as a second, short-lived credential for
// callers that can hold one.
//
// ENFORCEMENT — WHICH CONFIGURATION PROTECTS WHAT
// ===============================================
//
//	keys="",  jwt off  -> open access. Every request passes. Unchanged from
//	                      before JWT existed, so no live consumer is affected
//	                      by a deployment that sets neither variable — which
//	                      is every shipped config in this repo (.env.example
//	                      ships both blank).
//	keys=set, jwt off  -> API key required. Unchanged.
//	keys="",  jwt on   -> A CREDENTIAL IS REQUIRED. The only one that can
//	                      succeed is a JWT.
//	keys=set, jwt on   -> either credential is accepted.
//
// The third row is the load-bearing decision, and it is the reason this
// middleware exists rather than a JWT-only one bolted alongside. Setting a
// signing secret is an operator saying "authenticate this server". If that act
// left the surface open — because API keys happened to be unset — the result
// would be the precise trap that made the old documentation a defect: the
// operator sets HELIX_AUTH_JWT_SECRET, ticks the hardening checklist, and the
// server answers everyone. Enabling JWT therefore closes the door, and
// cmd/helixllm announces which mode is active at startup so the answer is
// never inferred from silence.
func APIKeyOrJWTAuth(configuredKeys string, verifier TokenVerifier) gin.HandlerFunc {
	jwtEnabled := verifier != nil && verifier.Enabled()

	return func(c *gin.Context) {
		// Open-access mode: no credential of any kind is configured.
		if configuredKeys == "" && !jwtEnabled {
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
		//
		// Compared in constant time. The previous `==` leaked key length and
		// matching-prefix length through timing; ConstantTimeCompare is
		// behaviourally identical (it too reports false for differing lengths)
		// so this changes no verdict, only the timing side channel.
		if configuredKeys != "" {
			for _, key := range strings.Split(configuredKeys, ",") {
				key = strings.TrimSpace(key)
				if key == "" {
					continue
				}
				if subtle.ConstantTimeCompare([]byte(key), []byte(token)) == 1 {
					c.Set(ContextKeyAPIKey, token)
					c.Set(ContextKeyAuthScheme, SchemeAPIKey)
					c.Next()
					return
				}
			}
		}

		// Not a configured API key — try it as a JWT.
		if jwtEnabled {
			subject, err := verifier.Verify(token)
			if err == nil {
				c.Set(ContextKeyAuthSubject, subject)
				c.Set(ContextKeyAuthScheme, SchemeJWT)
				c.Next()
				return
			}
			// err is deliberately not surfaced. internal/auth.Verify already
			// collapses every non-expiry rejection into one sentinel so a
			// forger learns nothing about WHICH check failed; repeating the
			// distinction in the response body would undo that.
			abortUnauthorized(c, "invalid credentials")
			return
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
