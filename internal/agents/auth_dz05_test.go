package agents

// DZ-05 regression guard — /v1/agents/* MUST require auth.
// §11.4.115 polarity switch (RED_MODE, default "1"): RED_MODE=1 reproduces the
// pre-fix exposure (agent control reachable unauthenticated because it is
// registered on a separate r.Group that does not inherit the gateway /v1
// APIKeyAuth), RED_MODE=0 is the standing GREEN guard (unauth -> 401, valid
// Bearer -> not 401).

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"

	gwmw "github.com/HelixDevelopment/HelixLLM/internal/gateway/middleware"
)

func TestDZ05_AgentsRequireAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const key = "sk-dz05-agents"
	red := os.Getenv("RED_MODE") != "0"

	r := gin.New()
	agent := NewAgent(AgentConfig{})
	convCtx := NewConversationContext(10)
	if red {
		RegisterAgentRoutes(r, agent, convCtx)
	} else {
		RegisterAgentRoutes(r, agent, convCtx, gwmw.APIKeyAuth(key))
	}

	unauth := httptest.NewRecorder()
	r.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/v1/agents/tools", nil))

	if red {
		if unauth.Code == http.StatusUnauthorized {
			t.Fatalf("RED expected exposure (unauth reachable) but got 401")
		}
		t.Logf("DZ-05 RED reproduced: unauth GET /v1/agents/tools -> HTTP %d", unauth.Code)
		return
	}

	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("GREEN expected 401 for unauthenticated request, got %d", unauth.Code)
	}
	authed := httptest.NewRecorder()
	authReq := httptest.NewRequest(http.MethodGet, "/v1/agents/tools", nil)
	authReq.Header.Set("Authorization", "Bearer "+key)
	r.ServeHTTP(authed, authReq)
	if authed.Code == http.StatusUnauthorized {
		t.Fatalf("GREEN authenticated request wrongly rejected with 401")
	}
	t.Logf("DZ-05 GREEN: unauth -> 401, auth -> HTTP %d", authed.Code)
}
