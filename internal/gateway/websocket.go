// Package gateway provides gateway-layer handlers and utilities for HelixLLM.
package gateway

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	gorillaws "github.com/gorilla/websocket"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/i18n"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// wsUpgrader upgrades HTTP connections to WebSocket. CheckOrigin allows all
// origins; tighten this in production via configuration if needed.
var wsUpgrader = gorillaws.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// HandleWebSocket returns a Gin handler that upgrades the HTTP connection to
// WebSocket, then loops reading JSON-encoded InternalChatRequest messages from
// the client and writing back InternalChatResponse (or error) objects.
//
// The Completer is optional: when nil the handler returns a fallback response so
// that the endpoint remains functional in development / test setups.
func HandleWebSocket(b Completer) gin.HandlerFunc {
	return func(c *gin.Context) {
		conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			// Upgrade already wrote an HTTP error response.
			return
		}
		defer conn.Close()

		// Negotiate the response language once from the upgrade
		// request's Accept-Language header; the WebSocket protocol
		// carries no per-frame language hint.
		lang := langFromContext(c)

		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				// Client disconnected or read error — exit the loop.
				break
			}

			var req types.InternalChatRequest
			if err := json.Unmarshal(raw, &req); err != nil {
				_ = conn.WriteJSON(map[string]string{
					"error": gatewayTranslator.T(lang, i18n.KeyGatewayInvalidRequest,
						map[string]string{"detail": err.Error()}),
				})
				continue
			}

			if b != nil {
				resp, err := b.Complete(context.Background(), &req)
				if err != nil {
					// Redact, then log. This frame used to carry
					// err.Error() verbatim, which named the backend's
					// address to any WebSocket client. See
					// upstream_error.go.
					log.Printf("[HelixLLM] upstream complete failed for WS %s: %s",
						c.Request.URL.Path, UpstreamErrorLogDetail(err))
					_ = conn.WriteJSON(map[string]string{
						"error": upstreamErrorTextForLang(lang, err),
					})
					continue
				}
				_ = conn.WriteJSON(resp)
			} else {
				// Fallback response when no Brain is wired.
				_ = conn.WriteJSON(map[string]string{"message": "ok (no brain)"})
			}
		}
	}
}
