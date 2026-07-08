package mcpgateway

import (
	"context"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

// NewStaticBearerVerifier returns an auth.TokenVerifier that accepts exactly
// one bearer token value, sourced by the caller from an environment
// variable (§11.4.10/CONST-045 — no hardcoded credentials in source).
//
// The go-sdk's auth.RequireBearerToken middleware rejects any TokenInfo
// whose Expiration is the zero value ("token missing expiration"), so this
// verifier stamps a long-lived expiration on every successful check; the
// real security boundary is the constant-time-independent equality check
// against the configured token, not a wall-clock expiry (this is a static
// shared-secret gateway credential, not an OAuth-issued token).
func NewStaticBearerVerifier(expectedToken string) auth.TokenVerifier {
	return func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		if expectedToken == "" || token != expectedToken {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{
			Expiration: time.Now().Add(24 * time.Hour),
		}, nil
	}
}
