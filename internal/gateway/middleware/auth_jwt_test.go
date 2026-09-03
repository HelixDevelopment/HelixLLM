package middleware_test

import (
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/auth"
	"github.com/HelixDevelopment/HelixLLM/internal/gateway/middleware"
)

// Guards for the JWT credential at the middleware seam — the one place where
// the accepted-credential set is decided for /v1 AND for the
// /internal/{cluster,knowledge} + /v1/agents groups (cmd/helixllm/main.go
// wires this same constructor at all five sites).
//
// WHERE THE §11.4.115 RED BASELINE LIVES
// ======================================
//
// The defect these guards close could not be reproduced by a Go test compiled
// against the pre-fix tree: neither internal/auth nor APIKeyOrJWTAuth existed
// there, so a test naming them would not build. The RED baseline is therefore
// captured at the ARTIFACT level — docs/qa/jwt_auth_20260903/repro.sh driven
// against a binary built from pre-fix HEAD 8f7c38d, output in
// docs/qa/jwt_auth_20260903/RED_prefix_8f7c38d.txt, and it proved BOTH halves
// on the real HTTPS surface:
//
//	HELIX_AUTH_JWT_SECRET set, no API keys -> unauthenticated GET /v1/models
//	                                          returned 200. Wide open.
//	HELIX_AUTH_JWT_SECRET + API keys set   -> a validly-signed JWT returned
//	                                          401. The documented credential
//	                                          was refused.
//
// That is the stronger evidence anyway: it is the layer an operator touches.
// TestJWTSecretConfiguredProtectsTheSurface below carries the RED_MODE
// polarity switch for the in-process half.

const jwtTestSecret = "middleware-test-hs256-key-32bytes!"

func redMode() bool { return os.Getenv("RED_MODE") == "1" }

func testVerifier(t *testing.T) *auth.Verifier {
	t.Helper()
	v, err := auth.New(jwtTestSecret, time.Hour)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	return v
}

// serve runs one request through a router carrying mw and returns the status.
func serve(t *testing.T, mw gin.HandlerFunc, bearer string) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(mw)
	r.GET("/v1/models", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models", nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	r.ServeHTTP(w, req)
	return w.Code
}

// TestJWTSecretConfiguredProtectsTheSurface is the polarity-switched guard for
// the wiring half of the defect (§11.4.115).
//
//	RED_MODE=1 — build the middleware the way the PRE-FIX code did: a signing
//	             secret exists in the operator's environment but never reaches
//	             the middleware, so the surface stays open. Asserts the defect
//	             IS PRESENT (200 with no credential at all).
//	RED_MODE=0 — the default, and the wiring main.go now uses: the verifier is
//	             passed in. Asserts the defect is ABSENT (401).
//
// The switch selects the WIRING, and the assertion follows it — which is
// precisely the thing that was broken. Nothing else differs between the two
// branches.
func TestJWTSecretConfiguredProtectsTheSurface(t *testing.T) {
	verifier := testVerifier(t)

	if redMode() {
		// Pre-fix wiring: APIKeys empty, verifier never handed over.
		got := serve(t, middleware.APIKeyAuth(""), "")
		if got != 200 {
			t.Fatalf("RED_MODE=1 expected the pre-fix exposure (200 with no "+
				"credential while a JWT secret is configured), got %d. The "+
				"wiring may already be fixed — rerun with RED_MODE=0.", got)
		}
		t.Logf("RED reproduced in-process: secret configured, verifier not wired -> HTTP %d", got)
		return
	}

	// Post-fix wiring.
	if got := serve(t, middleware.APIKeyOrJWTAuth("", verifier), ""); got != 401 {
		t.Errorf("no credential with JWT enabled: status = %d, want 401. "+
			"Setting HELIX_AUTH_JWT_SECRET must close the surface, otherwise an "+
			"operator who sets it and ticks the hardening checklist is running "+
			"an open server.", got)
	}

	token, err := verifier.Issue("client")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if got := serve(t, middleware.APIKeyOrJWTAuth("", verifier), token); got != 200 {
		t.Errorf("valid JWT with JWT enabled: status = %d, want 200. The "+
			"credential the security manual documents must actually be accepted.", got)
	}
}

// TestEnforcementMatrix pins all four configuration states, so a later change
// cannot silently move one of them.
func TestEnforcementMatrix(t *testing.T) {
	verifier := testVerifier(t)
	token, err := verifier.Issue("client")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	cases := []struct {
		name     string
		keys     string
		verifier middleware.TokenVerifier
		bearer   string
		want     int
		why      string
	}{
		{
			name: "no credential configured, no credential sent",
			want: 200,
			why:  "open access is the shipped default; every live consumer relies on it",
		},
		{
			name: "no credential configured, JWT off, token sent anyway",
			// A stray Authorization header must not turn open access into a
			// rejection — the header is simply ignored.
			bearer: token, want: 200,
			why: "an unexpected header must not lock out a client on an open server",
		},
		{
			name: "keys configured, correct key",
			keys: "sk-live", bearer: "sk-live", want: 200,
			why: "the pre-existing API-key path must be untouched",
		},
		{
			name: "keys configured, wrong key",
			keys: "sk-live", bearer: "sk-wrong", want: 401,
			why: "the pre-existing API-key path must be untouched",
		},
		{
			name: "keys configured, JWT OFF, valid-looking token",
			keys: "sk-live", bearer: token, want: 401,
			why: "with no secret configured a token is just an unknown string; accepting it would be a forgery-free bypass",
		},
		{
			name: "keys configured, JWT ON, valid token",
			keys: "sk-live", verifier: verifier, bearer: token, want: 200,
			why: "both credentials are accepted when both are configured",
		},
		{
			name: "keys configured, JWT ON, correct key",
			keys: "sk-live", verifier: verifier, bearer: "sk-live", want: 200,
			why: "enabling JWT must not break the API-key clients",
		},
		{
			name: "keys configured, JWT ON, neither credential",
			keys: "sk-live", verifier: verifier, want: 401,
			why: "still closed",
		},
		{
			name:     "JWT ON only, valid token",
			verifier: verifier, bearer: token, want: 200,
			why: "a JWT-only deployment must work",
		},
		{
			name:     "JWT ON only, no credential",
			verifier: verifier, want: 401,
			why: "THE load-bearing row: a configured secret closes the surface even with no API keys",
		},
		{
			name:     "JWT ON only, garbage token",
			verifier: verifier, bearer: "not-a-token", want: 401,
			why: "malformed credentials are refused, not passed through",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := serve(t, middleware.APIKeyOrJWTAuth(tc.keys, tc.verifier), tc.bearer)
			if got != tc.want {
				t.Errorf("status = %d, want %d\n  why this row matters: %s", got, tc.want, tc.why)
			}
		})
	}
}

// TestForgedAndStaleTokensRejectedAtTheMiddleware re-runs the negative
// security cases at the HTTP layer.
//
// internal/auth already unit-tests each rejection, but a correct verifier
// wired into a middleware that ignores its error would still let every forgery
// through — the middleware is where the 401 actually has to happen. These
// cases prove the seam honours the verdict.
func TestForgedAndStaleTokensRejectedAtTheMiddleware(t *testing.T) {
	verifier := testVerifier(t)

	// A token signed with a different secret, minted by a second verifier that
	// is otherwise identical — the attacker who guessed everything but the key.
	other, err := auth.New("a-completely-different-32-byte-key!", time.Hour)
	if err != nil {
		t.Fatalf("auth.New(other): %v", err)
	}
	forged, err := other.Issue("attacker")
	if err != nil {
		t.Fatalf("Issue(forged): %v", err)
	}

	// An already-expired token: minted with a 1ns lifetime, then slept past.
	shortLived, err := auth.New(jwtTestSecret, time.Nanosecond)
	if err != nil {
		t.Fatalf("auth.New(shortLived): %v", err)
	}
	expired, err := shortLived.Issue("client")
	if err != nil {
		t.Fatalf("Issue(expired): %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	cases := []struct{ name, token string }{
		{"forged signature", forged},
		{"expired token", expired},
		{"empty bearer value", ""},
		{"header-shaped garbage", "Bearer"},
		{"truncated token", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhZG1pbiJ9"},
	}

	mw := middleware.APIKeyOrJWTAuth("", verifier)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := serve(t, mw, tc.token); got != 401 {
				t.Errorf("status = %d, want 401 — the middleware accepted a credential "+
					"the verifier rejects", got)
			}
		})
	}
}

// TestAuthContextIsPopulated proves the middleware records WHICH credential
// authenticated the request. The token-exchange handler reads exactly these
// keys to decide the subject it mints for, so an empty context there would
// mean minting tokens for an unidentified caller.
func TestAuthContextIsPopulated(t *testing.T) {
	verifier := testVerifier(t)
	token, err := verifier.Issue("client-beta")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	run := func(keys, bearer string) (scheme, subject, key string) {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.Use(middleware.APIKeyOrJWTAuth(keys, verifier))
		r.GET("/v1/models", func(c *gin.Context) {
			scheme = c.GetString(middleware.ContextKeyAuthScheme)
			subject = c.GetString(middleware.ContextKeyAuthSubject)
			key = c.GetString(middleware.ContextKeyAPIKey)
			c.String(200, "ok")
		})
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("setup: expected the request to authenticate, got %d", w.Code)
		}
		return
	}

	scheme, subject, key := run("", token)
	if scheme != middleware.SchemeJWT {
		t.Errorf("jwt request: scheme = %q, want %q", scheme, middleware.SchemeJWT)
	}
	if subject != "client-beta" {
		t.Errorf("jwt request: subject = %q, want the token's subject", subject)
	}
	if key != "" {
		t.Errorf("jwt request: api_key = %q, want empty — no API key was presented", key)
	}

	scheme, subject, key = run("sk-live", "sk-live")
	if scheme != middleware.SchemeAPIKey {
		t.Errorf("api-key request: scheme = %q, want %q", scheme, middleware.SchemeAPIKey)
	}
	if key != "sk-live" {
		t.Errorf("api-key request: api_key = %q, want the presented key", key)
	}
	if subject != "" {
		t.Errorf("api-key request: subject = %q, want empty — no token was presented", subject)
	}
}

// TestNilVerifierIsOpenAccessNotClosed pins the compatibility contract that
// keeps the four existing APIKeyAuth call sites and their tests working: a nil
// verifier means "JWT off", and with no keys that is open access, exactly as
// before this package knew about JWT.
func TestNilVerifierIsOpenAccessNotClosed(t *testing.T) {
	if got := serve(t, middleware.APIKeyOrJWTAuth("", nil), ""); got != 200 {
		t.Errorf("nil verifier + no keys: status = %d, want 200. A nil verifier "+
			"must not accidentally enable enforcement — every current deployment "+
			"of this server configures neither credential.", got)
	}

	// A *typed* nil is the value main.go actually passes when the secret is
	// unset (auth.New("") returns a nil *Verifier, not an untyped nil), so it
	// must behave identically. This is the classic Go nil-interface trap: a
	// non-nil interface holding a nil pointer.
	var typedNil *auth.Verifier
	if got := serve(t, middleware.APIKeyOrJWTAuth("", typedNil), ""); got != 200 {
		t.Errorf("typed-nil verifier + no keys: status = %d, want 200 — "+
			"auth.New(\"\") returns a nil *Verifier and that is what main.go passes", got)
	}
}
