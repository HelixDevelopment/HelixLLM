// Package auth implements HelixLLM's JWT credential: minting signed tokens
// and verifying them.
//
// WHY THIS PACKAGE EXISTS
// =======================
//
// `Auth.JWTSecret` (env HELIX_AUTH_JWT_SECRET) was declared in
// internal/shared/config/config.go and read by NOTHING. The security manual
// nevertheless told operators to set it, said "when set, the system can issue
// and validate JWT tokens for session-based access", rated it "High — token
// signing key", and put "Set a strong HELIX_AUTH_JWT_SECRET" on the production
// hardening checklist. An operator could follow that checklist, tick the box,
// and believe the server was protected while HELIX_AUTH_API_KEYS sat empty and
// every /v1 route answered anyone who could reach the port. The documentation
// written to prevent that outcome was producing it.
//
// The documentation was corrected first (it now reads NOT IMPLEMENTED). This
// package is the other half: the capability the manual described now exists,
// so the promise can be made true rather than merely withdrawn.
//
// WHAT IT IS AND IS NOT
// =====================
//
// HelixLLM authenticates CLIENTS, not users. It has no user table, no
// password store, and no session store — so this is deliberately NOT a port of
// helix_code's AuthService, which is user-shaped (Register/Login/VerifySession
// against an AuthRepository). What is matched from that sibling, because a
// second divergent JWT design in one product would be worse than none:
//
//   - HS256 over a shared secret (helix_code internal/auth/auth.go:325).
//   - The signing method is checked inside the key function and rejected if it
//     is not HMAC (auth.go:334-336) — the `alg=none` / algorithm-confusion
//     defence.
//   - Every claim read out of a parsed token goes through a CHECKED type
//     assertion. helix_code learned this the hard way and says so in a
//     comment (auth.go:355-362): "an unchecked assertion here would PANIC on
//     attacker-controlled input and crash the process". Here that is
//     structural instead of manual — claims parse into a typed
//     jwt.RegisteredClaims, so there is no `interface{}` to assert on.
//   - A 24h default token lifetime (auth.go:78, TokenExpiry).
//
// Deliberate divergences, each for a stated reason:
//
//   - golang-jwt/jwt/v5 v5.3.1, not the sibling's v4.5.2. v5 is ALREADY in
//     this module's dependency graph (pulled by the MCP go-sdk, go.sum:63), so
//     using it adds no new dependency, and its parser takes an explicit
//     algorithm allowlist — which makes the `alg=none` rejection a property of
//     the parser rather than of a hand-written check that a later edit could
//     drop.
//   - Registered claims (iss/aud/sub/exp/iat/nbf) instead of the sibling's
//     user_id/username/email. There is no user to name. `sub` carries the
//     client principal; iss and aud are pinned and VERIFIED so a token minted
//     for some other service that happens to share a secret is not accepted
//     here.
//   - `sub` never contains the API key it was exchanged for. It carries a
//     truncated SHA-256 of it (see SubjectForAPIKey), because a token travels
//     further than the key that bought it and may be logged by proxies.
package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// Issuer and Audience are pinned and both VERIFIED on every parse. A token
	// signed with the right secret but minted for a different service is
	// rejected — sharing a secret between services must not mean sharing
	// credentials between them.
	Issuer   = "helixllm"
	Audience = "helixllm"

	// MinSecretBytes is the shortest signing secret this package will accept.
	//
	// RFC 7518 (JSON Web Algorithms) §3.2: "A key of the same size as the hash
	// output (for instance, 256 bits for "HS256") or larger MUST be used with
	// this algorithm." 256 bits is 32 bytes. A shorter key is not a weaker
	// configuration to be warned about; it is outside the algorithm's
	// specification, so it is refused.
	MinSecretBytes = 32

	// DefaultTTL matches helix_code's AuthConfig.TokenExpiry default.
	DefaultTTL = 24 * time.Hour
)

var (
	// ErrTokenInvalid is returned for every rejection that is not an expiry:
	// bad signature, wrong algorithm, wrong issuer or audience, malformed
	// input, missing required claim. Callers get one indistinguishable answer
	// on purpose — telling an unauthenticated caller WHICH part of their forged
	// token was wrong is an oracle for forging a better one.
	ErrTokenInvalid = errors.New("invalid token")

	// ErrTokenExpired is separated from ErrTokenInvalid because it is the one
	// rejection a LEGITIMATE client hits routinely, and it has a different
	// remedy: mint a new token rather than fix your credentials. It reveals
	// nothing an attacker does not already know, since exp is in the token's
	// own readable payload.
	ErrTokenExpired = errors.New("token expired")

	// ErrWeakSecret reports a signing secret shorter than MinSecretBytes.
	ErrWeakSecret = errors.New("jwt signing secret is too short")
)

// Verifier mints and verifies HelixLLM JWTs.
//
// A nil *Verifier is the valid, meaningful "JWT auth is not enabled" value —
// the state produced by an unset HELIX_AUTH_JWT_SECRET, which the
// configuration manual documents as the off-switch. Every method is
// nil-receiver safe, so callers need no nil check before asking Enabled().
// That pattern is lifted from helix_code's AuthService.dbAvailable
// (internal/auth/auth.go:120-127), which exists because its server boots with
// db=nil on a documented path and every method had to survive it.
type Verifier struct {
	secret []byte
	ttl    time.Duration
	parser *jwt.Parser
}

// New builds a Verifier from a signing secret.
//
// An EMPTY secret returns (nil, nil): JWT auth is off, which is a supported
// configuration and not an error. Callers distinguish the two states with
// Enabled(), never by comparing to nil.
//
// A non-empty secret shorter than MinSecretBytes returns ErrWeakSecret. This
// refuses at construction rather than degrading quietly, matching how this
// codebase already treats a credential that is present but unusable: an
// unexpanded "${HELIX_AUTH_JWT_SECRET}" and a whitespace-only secret are both
// hard refusals at config-validation time (internal/shared/config/
// placeholder.go, requiredsecret.go). A four-character HMAC key belongs in the
// same class — it looks configured and protects nothing.
//
// ttl <= 0 falls back to DefaultTTL rather than minting tokens that are
// already expired.
func New(secret string, ttl time.Duration) (*Verifier, error) {
	if secret == "" {
		return nil, nil
	}
	if len(secret) < MinSecretBytes {
		return nil, fmt.Errorf(
			"%w: got %d bytes, need at least %d (RFC 7518 §3.2 requires an "+
				"HS256 key at least as large as the hash output, 256 bits)",
			ErrWeakSecret, len(secret), MinSecretBytes)
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Verifier{
		secret: []byte(secret),
		ttl:    ttl,
		// Every check that can be delegated to the parser IS delegated to the
		// parser, so none of them can be lost in a later edit to a validation
		// function:
		//
		//   WithValidMethods      — the algorithm allowlist. "none" and any
		//                           asymmetric alg are rejected before the key
		//                           function is consulted at all.
		//   WithIssuer/WithAudience — reject a token minted for another service.
		//   WithExpirationRequired  — a token with NO exp claim is rejected.
		//                           Without this, an omitted exp parses as "no
		//                           expiry constraint", i.e. a token valid
		//                           forever.
		//   WithIssuedAt            — reject a token issued in the future.
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
			jwt.WithIssuer(Issuer),
			jwt.WithAudience(Audience),
			jwt.WithExpirationRequired(),
			jwt.WithIssuedAt(),
		),
	}, nil
}

// Enabled reports whether JWT authentication is configured. Nil-receiver safe.
func (v *Verifier) Enabled() bool { return v != nil && len(v.secret) > 0 }

// TTL is the lifetime Issue stamps on new tokens.
func (v *Verifier) TTL() time.Duration {
	if !v.Enabled() {
		return 0
	}
	return v.ttl
}

// Issue mints a signed token for subject, valid for TTL from now.
//
// subject names the authenticated principal and is echoed back by Verify. It
// MUST NOT be a secret: a JWT payload is base64, not encrypted, and is
// readable by anything that handles the token.
func (v *Verifier) Issue(subject string) (string, error) {
	if !v.Enabled() {
		return "", ErrTokenInvalid
	}
	if subject == "" {
		return "", fmt.Errorf("issue token: empty subject")
	}
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    Issuer,
		Subject:   subject,
		Audience:  jwt.ClaimStrings{Audience},
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(v.ttl)),
	})
	signed, err := tok.SignedString(v.secret)
	if err != nil {
		// Deliberately does not wrap err: SignedString's error can quote the
		// key material it failed on, and this string reaches request logs.
		return "", fmt.Errorf("signing token failed")
	}
	return signed, nil
}

// Verify checks a token and returns its subject.
//
// The returned error is ErrTokenExpired or ErrTokenInvalid and NEVER contains
// the token, any part of it, or the signing secret — this value is written to
// 401 response bodies and to logs.
func (v *Verifier) Verify(tokenString string) (string, error) {
	if !v.Enabled() {
		return "", ErrTokenInvalid
	}

	var claims jwt.RegisteredClaims
	_, err := v.parser.ParseWithClaims(tokenString, &claims,
		func(t *jwt.Token) (interface{}, error) {
			// Belt and braces with WithValidMethods above. The sibling relies
			// on this check alone (helix_code internal/auth/auth.go:334-336);
			// keeping it means the algorithm-confusion defence survives even
			// if the parser options are ever refactored away.
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrTokenSignatureInvalid
			}
			return v.secret, nil
		})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return "", ErrTokenExpired
		}
		return "", ErrTokenInvalid
	}

	// A validly-signed token with no subject names no principal, so it cannot
	// authenticate one. Rejected rather than treated as an anonymous pass.
	if claims.Subject == "" {
		return "", ErrTokenInvalid
	}
	return claims.Subject, nil
}

// SubjectForAPIKey derives a stable, non-reversible principal name for an API
// key, for use as a token subject.
//
// The key itself must never become the subject. A JWT payload is base64 —
// readable by every proxy, log sink, and browser devtools panel the token
// passes through — so putting the long-lived API key in there would turn a
// short-lived credential into a carrier for the permanent one. The truncated
// digest is enough to tell two callers apart in an audit log and cannot be
// turned back into the key.
//
// 64 bits of digest is not collision-proof in the cryptographic sense and is
// not relied on for authentication: authentication already happened, this
// value only labels who passed it.
func SubjectForAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "apikey:" + hex.EncodeToString(sum[:8])
}
