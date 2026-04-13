// Package gateway provides gateway-layer handlers and utilities for HelixLLM.
package gateway

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	gorillaws "github.com/gorilla/websocket"

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

		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				// Client disconnected or read error — exit the loop.
				break
			}

			var req types.InternalChatRequest
			if err := json.Unmarshal(raw, &req); err != nil {
				_ = conn.WriteJSON(map[string]string{
					"error": "invalid request: " + err.Error(),
				})
				continue
			}

			if b != nil {
				resp, err := b.Complete(context.Background(), &req)
				if err != nil {
					_ = conn.WriteJSON(map[string]string{"error": err.Error()})
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
