package gateway

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/auth"
	gwmw "github.com/HelixDevelopment/HelixLLM/internal/gateway/middleware"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

// TokenResponse is the body of a successful POST /v1/auth/token.
//
// Field names follow RFC 6749 §5.1 (the OAuth 2.0 access-token response) so
// existing HTTP clients and token-caching libraries can consume it without a
// bespoke decoder. `expires_in` is seconds, as that RFC specifies.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// HandleIssueToken handles POST /v1/auth/token — exchanging the credential the
// caller already authenticated with for a short-lived JWT.
//
// It is registered INSIDE the authenticated /v1 group, so reaching this
// handler at all means the caller already presented a valid credential. It
// therefore mints a token for whoever that was rather than performing a
// second, independent authentication — there is no user store here to check a
// password against. This mirrors helix_code's refresh path, which likewise
// verifies the presented credential and re-issues from it
// (helix_code internal/server/handlers.go:301-321).
//
// BOOTSTRAP, STATED PLAINLY: a caller needs a credential to obtain a token
// here, so this route cannot hand out a FIRST credential. A deployment that
// configures HELIX_AUTH_JWT_SECRET and leaves HELIX_AUTH_API_KEYS empty has no
// in-band way to get its first token and must mint tokens out of band with the
// signing secret — the ordinary machine-to-machine arrangement. docs/manual/
// security.md carries the recipe. Configure at least one API key if you want
// the exchange to work over HTTP.
func HandleIssueToken(v *auth.Verifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Not enabled is 501, not 401: the caller's credentials are not the
		// problem and retrying with better ones will never help. This is the
		// server telling an operator that a capability is switched off.
		if !v.Enabled() {
			c.JSON(http.StatusNotImplemented, api.ErrorResponse{
				Error: api.ErrorDetail{
					Message: "JWT authentication is not enabled on this server; " +
						"set HELIX_AUTH_JWT_SECRET (at least 32 bytes) to enable it",
					Type: "invalid_request_error",
				},
			})
			return
		}

		subject := subjectFor(c)
		if subject == "" {
			// Only reachable if this route is ever re-registered outside the
			// authenticated group. Fail closed instead of minting a token for
			// an unidentified caller.
			c.JSON(http.StatusUnauthorized, api.ErrorResponse{
				Error: api.ErrorDetail{
					Message: "authentication required to issue a token",
					Type:    "invalid_request_error",
				},
			})
			return
		}

		token, err := v.Issue(subject)
		if err != nil {
			// err is not echoed: signing errors can quote key material.
			c.JSON(http.StatusInternalServerError, api.ErrorResponse{
				Error: api.ErrorDetail{
					Message: "could not issue token",
					Type:    "server_error",
				},
			})
			return
		}

		c.JSON(http.StatusOK, TokenResponse{
			AccessToken: token,
			TokenType:   "Bearer",
			ExpiresIn:   int(v.TTL().Seconds()),
		})
	}
}

// subjectFor names the principal that authenticated this request.
//
// For an API key it is the truncated digest from auth.SubjectForAPIKey — never
// the key itself, since the subject is readable in the minted token's payload.
// For a JWT it is the subject already carried by the presented token, so a
// refresh preserves identity instead of renaming the caller.
func subjectFor(c *gin.Context) string {
	switch c.GetString(gwmw.ContextKeyAuthScheme) {
	case gwmw.SchemeAPIKey:
		if key := c.GetString(gwmw.ContextKeyAPIKey); key != "" {
			return auth.SubjectForAPIKey(key)
		}
	case gwmw.SchemeJWT:
		return c.GetString(gwmw.ContextKeyAuthSubject)
	}
	return ""
}
