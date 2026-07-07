package control

// DZ-05 regression guard — cluster control plane MUST require auth.
//
// §11.4.115 polarity switch (RED_MODE, default "1"):
//   RED_MODE=1  reproduce-and-assert-defect-present: the sensitive
//               /internal/cluster group registered WITHOUT auth middleware
//               (the pre-fix wiring main.go used) is reachable unauthenticated
//               and does NOT return 401. Proves the exposure is real.
//   RED_MODE=0  standing GREEN guard: the group registered WITH the shared
//               gateway APIKeyAuth middleware rejects an unauthenticated request
//               (401) and accepts a valid-Bearer request (not 401).
//
// The bug-catcher IS the regression guard: flipping RED_MODE=0 asserts the
// defect is ABSENT on the fixed artifact.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"

	gwmw "github.com/HelixDevelopment/HelixLLM/internal/gateway/middleware"
)

func TestDZ05_ClusterControlRequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const key = "sk-dz05-cluster"
	red := os.Getenv("RED_MODE") != "0"

	r := gin.New()
	cp := NewControlPlane(ControlPlaneOptions{})
	if red {
		RegisterRoutes(r, cp) // pre-fix wiring: no auth middleware
	} else {
		RegisterRoutes(r, cp, gwmw.APIKeyAuth(key)) // fixed wiring: gated
	}

	// Unauthenticated request to the sensitive control-plane status endpoint.
	unauth := httptest.NewRecorder()
	r.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/internal/cluster/status", nil))

	if red {
		if unauth.Code == http.StatusUnauthorized {
			t.Fatalf("RED expected exposure (unauth reachable) but got 401")
		}
		t.Logf("DZ-05 RED reproduced: unauth GET /internal/cluster/status -> HTTP %d (reachable without a key)", unauth.Code)
		return
	}

	// GREEN: unauth MUST be rejected.
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("GREEN expected 401 for unauthenticated request, got %d", unauth.Code)
	}
	// GREEN: a valid Bearer key MUST pass the auth layer (not 401).
	authed := httptest.NewRecorder()
	authReq := httptest.NewRequest(http.MethodGet, "/internal/cluster/status", nil)
	authReq.Header.Set("Authorization", "Bearer "+key)
	r.ServeHTTP(authed, authReq)
	if authed.Code == http.StatusUnauthorized {
		t.Fatalf("GREEN authenticated request wrongly rejected with 401")
	}
	t.Logf("DZ-05 GREEN: unauth -> 401, auth(Bearer %s) -> HTTP %d", key, authed.Code)
}
