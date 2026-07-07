package a2a

import (
	"github.com/gin-gonic/gin"

	gwmw "github.com/HelixDevelopment/HelixLLM/internal/gateway/middleware"
)

// RouterOptions configures route registration. BasePath and BearerKeys are
// config-injected (CONST-045/046) by cmd/a2a-server/main.go from env vars —
// never hardcoded here.
type RouterOptions struct {
	// BasePath is the JSON-RPC dispatch path, e.g. "/a2a" (design §2.2 —
	// config-injectable, e.g. "/v1/a2a").
	BasePath string
	// BearerKeys reuses the EXACT existing gateway Bearer-auth middleware
	// (internal/gateway/middleware.APIKeyAuth, API_CONTRACT.md §3) so one
	// credential model spans the /v1 gateway and the A2A surface (design
	// §1.7 / §11.4.74 reuse-don't-reimplement). Empty = open-access (never
	// used in this proof — the harness always configures a real token so
	// the auth golden-bad path is genuinely exercised).
	BearerKeys string
}

// RegisterRoutes attaches the Agent Card (public) and JSON-RPC dispatch
// (Bearer-protected) routes to r.
func RegisterRoutes(r *gin.Engine, s *Server, opts RouterOptions) {
	basePath := opts.BasePath
	if basePath == "" {
		basePath = "/a2a"
	}

	// Public discovery — no auth (spec §1.2).
	r.GET("/.well-known/agent-card.json", s.AgentCardHandler)

	// Bearer-protected JSON-RPC dispatch (spec §1.5 / §1.7).
	grp := r.Group("")
	grp.Use(gwmw.APIKeyAuth(opts.BearerKeys))
	grp.POST(basePath, s.DispatchHandler)
}
