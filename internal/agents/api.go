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

// CoordinateRequest is the request body for POST /v1/agents/coordinate.
type CoordinateRequest struct {
	Task string `json:"task"`
}

// CoordinateResponse is the response body for POST /v1/agents/coordinate.
type CoordinateResponse struct {
	Tasks       []*Task `json:"tasks"`
	FinalOutput string  `json:"final_output"`
}

// PlanRequest is the request body for POST /v1/agents/plan.
type PlanRequest struct {
	Goal string `json:"goal"`
}

// MemoryRememberRequest is the request body for POST /v1/agents/memory/remember.
type MemoryRememberRequest struct {
	SessionID  string  `json:"session_id"`
	Content    string  `json:"content"`
	Importance float64 `json:"importance"`
}

// MemoryRecallRequest is the request body for POST /v1/agents/memory/recall.
type MemoryRecallRequest struct {
	SessionID string `json:"session_id"`
	Query     string `json:"query"`
	TopK      int    `json:"top_k,omitempty"`
}

// MemoryRecallResponse is the response body for POST /v1/agents/memory/recall.
type MemoryRecallResponse struct {
	Memories []MemoryEntry `json:"memories"`
}

// RegisterAgentRoutes wires the agents API endpoints into the Gin router:
//
//	POST /v1/agents/chat              — run the agent loop (with optional session tracking)
//	GET  /v1/agents/tools             — list available tools
//	POST /v1/agents/coordinate        — run multi-phase coordinator
//	POST /v1/agents/plan              — decompose a goal into a plan
//	POST /v1/agents/memory/remember   — store a memory
//	POST /v1/agents/memory/recall     — recall memories
func RegisterAgentRoutes(r *gin.Engine, agent *Agent, ctx *ConversationContext) {
	RegisterAgentRoutesWithExtras(r, agent, ctx, nil, nil, nil)
}

// ToolExecuteRequest is the request body for POST /v1/agents/tools/execute.
type ToolExecuteRequest struct {
	Tool   string                 `json:"tool" binding:"required"`
	Params map[string]interface{} `json:"params"`
}

// ToolExecuteResponse is the response body for POST /v1/agents/tools/execute.
type ToolExecuteResponse struct {
	Result string `json:"result"`
	Error  string `json:"error,omitempty"`
}

// RegisterAgentRoutesWithExtras wires all agent routes including the optional
// Coordinator and MemoryManager.  Either may be nil, in which case the
// corresponding endpoints return 501 Not Implemented.
func RegisterAgentRoutesWithExtras(
	r *gin.Engine,
	agent *Agent,
	convCtx *ConversationContext,
	coordinator *Coordinator,
	planner *Planner,
	memMgr *MemoryManager,
) {
	v1 := r.Group("/v1/agents")
	v1.POST("/chat", agentChatHandler(agent, convCtx))
	v1.GET("/tools", agentToolsHandler(agent))
	v1.POST("/tools/execute", toolExecuteHandler(agent))
	v1.POST("/coordinate", coordinateHandler(coordinator))
	v1.POST("/plan", planHandler(planner))
	v1.POST("/memory/remember", memoryRememberHandler(memMgr))
	v1.POST("/memory/recall", memoryRecallHandler(memMgr))
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

// coordinateHandler returns a Gin handler for POST /v1/agents/coordinate.
func coordinateHandler(coord *Coordinator) gin.HandlerFunc {
	return func(c *gin.Context) {
		if coord == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "coordinator not configured"})
			return
		}

		var req CoordinateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if req.Task == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "task must not be empty"})
			return
		}

		result, err := coord.Execute(c.Request.Context(), req.Task)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, CoordinateResponse{
			Tasks:       result.Tasks,
			FinalOutput: result.FinalOutput,
		})
	}
}

// planHandler returns a Gin handler for POST /v1/agents/plan.
func planHandler(planner *Planner) gin.HandlerFunc {
	return func(c *gin.Context) {
		if planner == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "planner not configured"})
			return
		}

		var req PlanRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if req.Goal == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "goal must not be empty"})
			return
		}

		plan, err := planner.CreatePlan(c.Request.Context(), req.Goal)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, plan)
	}
}

// memoryRememberHandler returns a Gin handler for POST /v1/agents/memory/remember.
func memoryRememberHandler(memMgr *MemoryManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if memMgr == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "memory manager not configured"})
			return
		}

		var req MemoryRememberRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if req.Content == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "content must not be empty"})
			return
		}
		if req.SessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id must not be empty"})
			return
		}

		memMgr.Remember(c.Request.Context(), req.SessionID, req.Content, req.Importance)
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}

// memoryRecallHandler returns a Gin handler for POST /v1/agents/memory/recall.
func memoryRecallHandler(memMgr *MemoryManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if memMgr == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "memory manager not configured"})
			return
		}

		var req MemoryRecallRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if req.Query == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "query must not be empty"})
			return
		}
		if req.SessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id must not be empty"})
			return
		}

		topK := req.TopK
		if topK <= 0 {
			topK = 10
		}

		memories := memMgr.Recall(c.Request.Context(), req.SessionID, req.Query, topK)
		if memories == nil {
			memories = []MemoryEntry{}
		}

		c.JSON(http.StatusOK, MemoryRecallResponse{Memories: memories})
	}
}

// toolExecuteHandler returns a Gin handler for POST /v1/agents/tools/execute.
func toolExecuteHandler(agent *Agent) gin.HandlerFunc {
	return func(c *gin.Context) {
		if agent.tools == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "tool registry not configured"})
			return
		}

		var req ToolExecuteRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		tool, ok := agent.tools.Get(req.Tool)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "tool not found: " + req.Tool})
			return
		}

		result, err := tool.Execute(c.Request.Context(), req.Params)
		if err != nil {
			c.JSON(http.StatusInternalServerError, ToolExecuteResponse{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, ToolExecuteResponse{Result: result})
	}
}
