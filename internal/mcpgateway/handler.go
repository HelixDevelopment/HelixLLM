package mcpgateway

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewHTTPHandler wraps server in the go-sdk's stateless-first
// Streamable-HTTP transport (per MCP_OKF_GATEWAY_MEMO.md §3.2) and
// requires a bearer token on every request via the SDK's own
// auth.RequireBearerToken middleware — never a hand-rolled auth check
// (§11.4.10). bearerToken must be non-empty; the caller (main) is
// responsible for sourcing it from an environment variable and refusing
// to start the gateway if it is unset.
func NewHTTPHandler(server *mcp.Server, bearerToken string) http.Handler {
	streamable := mcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			Stateless: true,
		},
	)

	verifier := NewStaticBearerVerifier(bearerToken)
	return auth.RequireBearerToken(verifier, nil)(streamable)
}
