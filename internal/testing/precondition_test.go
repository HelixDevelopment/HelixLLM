package testing

// The auth challenges declare `requires: [keyed_auth]`, which means that on
// the project's default open-access deployment they SKIP. A precondition that
// only ever skips would be a coverage deletion wearing a reason, so this file
// is the other half of the bargain: it stands up a REAL keyed gateway and
// runs those same shipped challenge files against it, unconditionally, on
// every `go test ./internal/testing/`.
//
// The banks are loaded from disk rather than restated here on purpose. If
// someone weakens `auth-empty-bearer-token` to expect 503, or drops the
// assertion, this test stops proving 401 and says so — a copy of the YAML
// would keep passing while the shipped challenge rotted.

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
)

// keyedGatewayAPIKey is the only key the test server accepts. Every token the
// auth challenges present differs from it, which is the point.
const keyedGatewayAPIKey = "test-keyed-deployment-key"

// newKeyedGateway starts a real gateway router with API keys configured —
// the same RegisterRoutes production main.go calls, with the same
// middleware.APIKeyAuth in front of /v1.
//
// Brain and ModelBrain stay nil: this test is about the auth layer, which
// runs BEFORE any handler, and a request that reaches a handler has already
// proved the thing under test (that it was not rejected 401).
func newKeyedGateway(t *testing.T) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	gateway.RegisterRoutes(r, gateway.RouterOptions{APIKeys: keyedGatewayAPIKey})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// authChallengeIDs are the shipped challenges that declare requires:
// [keyed_auth], with the bank each lives in.
var authChallengeIDs = map[string]string{
	"known-bug-regression/auth-empty-bearer-token":       "regression/known_bugs.yaml",
	"owasp-top10-security/auth-bypass-malformed-bearer":  "security/owasp.yaml",
	"Security Scanning Validation/invalid_auth_rejected": "security/scanning.yaml",
}

// TestKeyedAuthChallengesPassOnKeyedDeployment is the run the open-access
// skip promises exists. It proves three things at once: the product answers
// 401 for an empty or malformed Bearer token when keys are configured; the
// keyed_auth precondition probe opens the gate on such a deployment; and the
// shipped challenge assertions actually hold there.
func TestKeyedAuthChallengesPassOnKeyedDeployment(t *testing.T) {
	srv := newKeyedGateway(t)

	runner := NewRunner(srv.URL)
	for _, bank := range authChallengeIDs {
		require.NoError(t, runner.LoadBank(filepath.Join("..", "..", "challenges", "banks", bank)),
			"load shipped bank %s", bank)
	}

	results := runner.RunAll(context.Background())

	seen := map[string]ChallengeResult{}
	for _, res := range results {
		seen[res.ID] = res
	}

	for id := range authChallengeIDs {
		res, ok := seen[id]
		require.True(t, ok,
			"challenge %q was not found in the shipped banks — it was renamed or "+
				"removed, and the keyed-deployment coverage it carries went with it", id)

		require.NotEqual(t, StatusSkipped, res.Status,
			"challenge %q SKIPPED against a gateway that really does have API keys "+
				"configured: the keyed_auth precondition probe failed to detect a keyed "+
				"deployment, so on this path the challenge would never run anywhere. %s",
			id, firstSkipReason(res.Steps))

		require.Equal(t, StatusPassed, res.Status,
			"challenge %q failed against a real keyed gateway: %s", id, res.Error)

		require.Positive(t, res.Executed(),
			"challenge %q reported %q while executing zero steps", id, res.Status)
	}
}

// TestKeyedAuthPreconditionSkipsOnOpenAccessDeployment is the negative half:
// against a gateway with NO keys configured the same challenges must skip
// rather than fail, and the skip must name what is missing.
func TestKeyedAuthPreconditionSkipsOnOpenAccessDeployment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	gateway.RegisterRoutes(r, gateway.RouterOptions{APIKeys: ""}) // open access
	srv := httptest.NewServer(r)
	defer srv.Close()

	runner := NewRunner(srv.URL)
	for _, bank := range authChallengeIDs {
		require.NoError(t, runner.LoadBank(filepath.Join("..", "..", "challenges", "banks", bank)))
	}

	seen := map[string]ChallengeResult{}
	for _, res := range runner.RunAll(context.Background()) {
		seen[res.ID] = res
	}

	for id := range authChallengeIDs {
		res, ok := seen[id]
		require.True(t, ok, "challenge %q missing from the shipped banks", id)
		require.Equal(t, StatusSkipped, res.Status,
			"challenge %q did not skip on an open-access deployment; it %s. A 401 is "+
				"unreachable there, so anything but a skip is either a false failure or "+
				"an assertion that stopped testing auth", id, res.Status)

		reason := firstSkipReason(res.Steps)
		require.Contains(t, reason, "HELIX_AUTH_API_KEYS",
			"the skip reason for %q must name the setting that would make it run", id)
		require.Contains(t, reason, "returned 200, not 401",
			"the skip reason for %q must quote what the probe actually observed, "+
				"not merely assert the deployment mode", id)
	}
}

// TestUnknownPreconditionIsALoadError proves a typo cannot silently disable a
// challenge: an unrecognised precondition refuses to load rather than
// evaluating to "skip everything".
func TestUnknownPreconditionIsALoadError(t *testing.T) {
	path := writeBankFile(t, `
name: typo-bank
steps:
  - name: typo
    requires: [keyd_auth]
    method: GET
    path: /v1/models
    assertions:
      - type: status
        value: 200
`)
	err := NewRunner("http://127.0.0.1:1").LoadBank(path)
	require.Error(t, err, "an unknown precondition must refuse to load")
	require.Contains(t, err.Error(), "keyd_auth")
	require.Contains(t, err.Error(), "keyed_auth",
		"the load error must name the closed set so a typo is self-correcting")
}

// writeBankFile writes content to a throwaway bank file and returns its path.
func writeBankFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bank.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}
