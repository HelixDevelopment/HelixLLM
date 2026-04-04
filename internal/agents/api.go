package agents

import (
	"net/http"

	"github.com/HelixDevelopment/HelixLLM/pkg/types"
	"github.com/gin-gonic/gin"
)

// AgentChatRequest is the request body for POST /v1/agents/chat.
type AgentChatRequest struct {
	// SessionID is optional. When provided, prior conversation history is
	// prepended to the request and the exchange is saved for future turns.
	SessionID string `json:"session_id,omitempty"`

	// Messages contains the new messages for this turn (required).
	Messages []types.InternalMessage `json:"messages"`

	// Model is an optional hint for provider/model selection.
	Model string `json:"model,omitempty"`
}

// AgentChatResponse is the response body for POST /v1/agents/chat.
type AgentChatResponse struct {
	SessionID string                     `json:"session_id"`
	Response  types.InternalChatResponse `json:"response"`
}

// RegisterAgentRoutes wires the agents API endpoints into the Gin router:
//
//	POST /v1/agents/chat  — run the agent loop (with optional session tracking)
//	GET  /v1/agents/tools — list available tools
func RegisterAgentRoutes(r *gin.Engine, agent *Agent, ctx *ConversationContext) {
	v1 := r.Group("/v1/agents")
	v1.POST("/chat", agentChatHandler(agent, ctx))
	v1.GET("/tools", agentToolsHandler(agent))
}

// agentChatHandler returns a Gin handler for POST /v1/agents/chat.
func agentChatHandler(agent *Agent, convCtx *ConversationContext) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req AgentChatRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if len(req.Messages) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "messages must not be empty"})
			return
		}

		// Build the full message list: history (if any) + new messages.
		var messages []types.InternalMessage
		if req.SessionID != "" && convCtx != nil {
			history := convCtx.Get(req.SessionID)
			messages = append(messages, history...)
		}
		messages = append(messages, req.Messages...)

		// Run the agent loop.
		resp, err := agent.Run(c.Request.Context(), messages)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Persist new messages and the assistant response to the session.
		if req.SessionID != "" && convCtx != nil {
			convCtx.AddMultiple(req.SessionID, req.Messages)
			convCtx.Add(req.SessionID, resp.Message)
		}

		c.JSON(http.StatusOK, AgentChatResponse{
			SessionID: req.SessionID,
			Response:  *resp,
		})
	}
}

// agentToolsHandler returns a Gin handler for GET /v1/agents/tools.
func agentToolsHandler(agent *Agent) gin.HandlerFunc {
	return func(c *gin.Context) {
		var tools []ToolInfo
		if agent.tools != nil {
			tools = agent.tools.List()
		}
		if tools == nil {
			tools = []ToolInfo{}
		}
		c.JSON(http.StatusOK, gin.H{"tools": tools})
	}
}
