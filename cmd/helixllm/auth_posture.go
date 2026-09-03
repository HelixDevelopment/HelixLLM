package main

import (
	"github.com/HelixDevelopment/HelixLLM/internal/auth"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/logging"
)

// authPosture is the human-readable answer to "what is protecting this server
// right now", derived from the two credential settings.
type authPosture struct {
	// Summary names the accepted credentials.
	Summary string
	// Open is true when no credential is configured and every gated route
	// answers any client that can reach the port.
	Open bool
	// Advice is a non-empty operator instruction when the posture warrants
	// one, and "" otherwise.
	Advice string
}

// describeAuthPosture classifies the configured credentials.
//
// Separate from the logging call so the classification is unit-testable
// without capturing log output, and so the four states are enumerated in one
// readable place rather than as nested ifs inside main().
func describeAuthPosture(apiKeys string, jwtEnabled bool) authPosture {
	keysConfigured := apiKeys != ""

	switch {
	case keysConfigured && jwtEnabled:
		return authPosture{
			Summary: "API key or JWT (HS256) required",
		}
	case keysConfigured:
		return authPosture{
			Summary: "API key required",
			Advice: "JWT auth is DISABLED (HELIX_AUTH_JWT_SECRET is unset); " +
				"set it to at least 32 bytes to accept short-lived tokens as well",
		}
	case jwtEnabled:
		return authPosture{
			Summary: "JWT (HS256) required",
			// Said out loud because POST /v1/auth/token cannot issue a FIRST
			// token: it authenticates the caller before minting. With no API
			// key there is nothing to exchange, so tokens must be minted out
			// of band. An operator who does not know that reads the 401s as a
			// broken server.
			Advice: "no API keys configured, so POST /v1/auth/token has no credential to " +
				"exchange — mint tokens out of band with the signing secret, or set " +
				"HELIX_AUTH_API_KEYS to enable the exchange endpoint",
		}
	default:
		return authPosture{
			Summary: "NONE — every /v1 and /internal route is open to any client that can reach this port",
			Open:    true,
			Advice: "set HELIX_AUTH_API_KEYS and/or HELIX_AUTH_JWT_SECRET to require a credential; " +
				"this server binds all interfaces by default, so an open posture is reachable " +
				"from the whole network segment, not just localhost",
		}
	}
}

// logAuthPosture states at startup which credentials the server accepts.
//
// WHY THIS IS LOGGED UNCONDITIONALLY
// ==================================
//
// The defect this whole change answers was an operator being unable to tell
// protected from unprotected: the security manual described a JWT capability
// that did not exist, so setting HELIX_AUTH_JWT_SECRET produced silence that
// looked like success. Silence is the failure mode. The server now says what
// it is doing on every boot, and says the open case at WARN so it stands out
// in an otherwise-quiet log rather than blending into info-level chatter.
//
// The secret and the key list are NEVER logged — not truncated, not hashed,
// not counted-and-sampled. Only whether each is configured.
func logAuthPosture(log logging.Logger, apiKeys string, verifier *auth.Verifier) {
	p := describeAuthPosture(apiKeys, verifier.Enabled())

	l := log.WithFields(map[string]interface{}{
		"api_keys_configured":  apiKeys != "",
		"jwt_enabled":          verifier.Enabled(),
		"accepted_credentials": p.Summary,
	})
	if p.Open {
		l.Warn("AUTHENTICATION IS NOT CONFIGURED")
	} else {
		l.Info("authentication configured")
	}
	if p.Advice != "" {
		log.Warn("auth: " + p.Advice)
	}
}
