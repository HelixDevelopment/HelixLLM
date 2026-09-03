package gateway_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/auth"
	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
)

const tokenRouteSecret = "gateway-test-hs256-signing-key32!"

// newTokenRouter assembles the REAL gateway router the way cmd/helixllm does,
// so these tests exercise the registered route and its middleware chain rather
// than the handler in isolation.
func newTokenRouter(t *testing.T, keys string, v *auth.Verifier) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	gateway.RegisterRoutes(r, gateway.RouterOptions{APIKeys: keys, JWT: v})
	return r
}

func postToken(t *testing.T, r *gin.Engine, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/auth/token", nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	r.ServeHTTP(w, req)
	return w
}

// TestIssueTokenExchangesAnAPIKeyForAUsableToken is the end-to-end path an
// operator follows: present the API key you already have, receive a token, use
// the token. The token is then driven back through the SAME router, so a token
// that parses but is not accepted by the live middleware would still fail here.
func TestIssueTokenExchangesAnAPIKeyForAUsableToken(t *testing.T) {
	v, err := auth.New(tokenRouteSecret, time.Hour)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	r := newTokenRouter(t, "sk-operator-key", v)

	w := postToken(t, r, "sk-operator-key")
	if w.Code != 200 {
		t.Fatalf("POST /v1/auth/token with a valid API key: status = %d, want 200; body=%s",
			w.Code, w.Body.String())
	}

	var resp gateway.TokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding token response: %v; body=%s", err, w.Body.String())
	}
	if resp.AccessToken == "" {
		t.Fatal("access_token is empty")
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("token_type = %q, want \"Bearer\"", resp.TokenType)
	}
	if resp.ExpiresIn != int((time.Hour).Seconds()) {
		t.Errorf("expires_in = %d, want %d", resp.ExpiresIn, int((time.Hour).Seconds()))
	}

	// The minted token must open the guarded surface.
	w2 := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+resp.AccessToken)
	r.ServeHTTP(w2, req)
	if w2.Code != 200 {
		t.Errorf("GET /v1/models with the freshly issued token: status = %d, want 200. "+
			"A token this server minted and will not accept is worse than no token at all.", w2.Code)
	}

	// The token's subject must NOT be the API key — the payload is base64, so
	// putting the long-lived key there would leak it to everything that
	// handles the token (guarded independently in internal/auth, re-checked
	// here on a token that actually travelled over HTTP).
	if strings.Contains(resp.AccessToken, "sk-operator-key") {
		t.Error("the issued token contains the API key it was exchanged for")
	}
	subject, err := v.Verify(resp.AccessToken)
	if err != nil {
		t.Fatalf("verifying the issued token: %v", err)
	}
	if subject != auth.SubjectForAPIKey("sk-operator-key") {
		t.Errorf("subject = %q, want the derived apikey digest", subject)
	}
}

// TestIssueTokenRefreshesFromAToken: a caller holding a valid token can obtain
// a fresh one without going back to the API key, and identity is preserved
// rather than being renamed to something anonymous.
func TestIssueTokenRefreshesFromAToken(t *testing.T) {
	v, err := auth.New(tokenRouteSecret, time.Hour)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	r := newTokenRouter(t, "", v)

	first, err := v.Issue("client-gamma")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	w := postToken(t, r, first)
	if w.Code != 200 {
		t.Fatalf("refresh: status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp gateway.TokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	subject, err := v.Verify(resp.AccessToken)
	if err != nil {
		t.Fatalf("verifying refreshed token: %v", err)
	}
	if subject != "client-gamma" {
		t.Errorf("refreshed subject = %q, want the original subject preserved", subject)
	}
}

// TestIssueTokenWhenJWTDisabled: the route exists but the capability is off.
// 501 (not 401) because the caller's credentials are not the problem and
// retrying with better ones will never help — this is the server telling an
// operator a switch is off, and the message names the switch.
func TestIssueTokenWhenJWTDisabled(t *testing.T) {
	r := newTokenRouter(t, "sk-operator-key", nil)

	w := postToken(t, r, "sk-operator-key")
	if w.Code != 501 {
		t.Fatalf("POST /v1/auth/token with JWT disabled: status = %d, want 501; body=%s",
			w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "HELIX_AUTH_JWT_SECRET") {
		t.Errorf("the 501 body does not name the variable to set: %s", w.Body.String())
	}
}

// TestIssueTokenRequiresAuthentication: the route sits inside the
// authenticated /v1 group, so an unauthenticated caller must never reach the
// minting code. If a future refactor moved the route out of the group, this
// fails — which is the point.
func TestIssueTokenRequiresAuthentication(t *testing.T) {
	v, err := auth.New(tokenRouteSecret, time.Hour)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}

	for _, keys := range []string{"", "sk-operator-key"} {
		r := newTokenRouter(t, keys, v)
		if w := postToken(t, r, ""); w.Code != 401 {
			t.Errorf("keys=%q: unauthenticated POST /v1/auth/token status = %d, want 401. "+
				"An unauthenticated caller must not be able to mint a credential.", keys, w.Code)
		}
		if w := postToken(t, r, "forged-or-unknown"); w.Code != 401 {
			t.Errorf("keys=%q: bad-credential POST /v1/auth/token status = %d, want 401", keys, w.Code)
		}
	}
}

// TestNoSecretConfiguredLeavesTheSurfaceExactlyAsItWas is the compatibility
// guard for the live consumers.
//
// The Claude Toolkit's provider verification and helix_code both call
// GET /v1/models against :8443 today with NO credential, and that deployment
// configures neither HELIX_AUTH_API_KEYS nor HELIX_AUTH_JWT_SECRET. Adding JWT
// must not have moved that: with both unset, the surface answers exactly as
// before.
func TestNoSecretConfiguredLeavesTheSurfaceExactlyAsItWas(t *testing.T) {
	r := newTokenRouter(t, "", nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models", nil)
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("unauthenticated GET /v1/models with no credential configured: "+
			"status = %d, want 200. This is the live :8443 configuration — a 401 here "+
			"means the Claude Toolkit and helix_code just broke.", w.Code)
	}
}
