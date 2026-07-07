package a2a

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Server holds the state a real (never simulated) A2A server needs: the
// composed Agent Card, the task store, and the downstream client that makes
// real HTTP calls to the live coder.
type Server struct {
	Card       AgentCard
	Store      *TaskStore
	Downstream *Downstream
	MaxTokens  int
}

// NewServer wires a Server. cardCfg and downstream are already resolved by
// the caller (cmd/a2a-server/main.go) from config-injected env vars.
func NewServer(card AgentCard, downstream *Downstream, maxTokens int) *Server {
	if maxTokens <= 0 {
		maxTokens = 512
	}
	return &Server{
		Card:       card,
		Store:      NewTaskStore(),
		Downstream: downstream,
		MaxTokens:  maxTokens,
	}
}

// AgentCardHandler serves the public discovery document. Unauthenticated by
// design (spec §1.2 — discovery is a public manifest).
func (s *Server) AgentCardHandler(c *gin.Context) {
	c.JSON(http.StatusOK, s.Card)
}

// DispatchHandler is the JSON-RPC 2.0 POST endpoint (spec §1.5). It is
// mounted behind Bearer auth (gwmw.APIKeyAuth) by the caller — an
// unauthenticated request never reaches this handler at all, which is the
// real (not simulated) rejection the golden-bad "unauthorized Task" fixture
// exercises end-to-end.
func (s *Server) DispatchHandler(c *gin.Context) {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		writeRPCError(c, http.StatusBadRequest, nil, RPCParseError, "failed to read request body")
		return
	}

	req, hasJSONRPC, hasMethod, hasID, parseErr := decodeRPCRequest(raw)
	if parseErr != nil {
		// Not even valid JSON.
		writeRPCError(c, http.StatusBadRequest, nil, RPCParseError, "invalid JSON: "+parseErr.Error())
		return
	}
	if !hasJSONRPC || req.JSONRPC != "2.0" || !hasMethod || req.Method == "" || !hasID {
		// Genuinely malformed per JSON-RPC 2.0 — missing/invalid envelope
		// fields. NEVER processed as if it were a real Task (this is the
		// golden-bad "malformed JSON-RPC" fixture's real server behaviour).
		writeRPCError(c, http.StatusBadRequest, req.ID, RPCInvalidRequest,
			"invalid request: jsonrpc/method/id must all be present and jsonrpc must equal \"2.0\"")
		return
	}

	switch req.Method {
	case "message/send":
		s.handleMessageSend(c, req)
	case "tasks/get":
		s.handleTasksGet(c, req)
	default:
		writeRPCError(c, http.StatusOK, req.ID, RPCMethodNotFound, "method not found: "+req.Method)
	}
}

func (s *Server) handleMessageSend(c *gin.Context, req RPCRequest) {
	msgRaw, ok := req.Params["message"]
	if !ok {
		writeRPCError(c, http.StatusOK, req.ID, RPCInvalidParams, "params.message is required")
		return
	}
	msg, ok := msgRaw.(map[string]any)
	if !ok {
		writeRPCError(c, http.StatusOK, req.ID, RPCInvalidParams, "params.message must be an object")
		return
	}
	prompt := firstTextPart(msg)
	if prompt == "" {
		writeRPCError(c, http.StatusOK, req.ID, RPCInvalidParams, "params.message.parts must contain a non-empty text part")
		return
	}

	userMsg := Message{Role: RoleUser, Parts: []Part{{Kind: PartKindText, Text: prompt}}}
	task := s.Store.New([]Message{userMsg})

	// submitted -> working (real, observable state transition).
	task.Status = TaskStatus{State: TaskStateWorking, Timestamp: now()}
	s.Store.Update(task)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()
	content, err := s.Downstream.Generate(ctx, prompt, s.MaxTokens)
	if err != nil {
		task.Status = TaskStatus{State: TaskStateFailed, Timestamp: now()}
		task.Error = err.Error()
		s.Store.Update(task)
		writeRPCResult(c, req.ID, task)
		return
	}

	agentMsg := Message{Role: RoleAgent, Parts: []Part{{Kind: PartKindText, Text: content}}}
	task.History = append(task.History, agentMsg)
	task.Artifacts = []Artifact{{Name: "generated-code", Parts: []Part{{Kind: PartKindText, Text: content}}}}
	task.Status = TaskStatus{State: TaskStateCompleted, Timestamp: now()}
	s.Store.Update(task)

	writeRPCResult(c, req.ID, task)
}

func (s *Server) handleTasksGet(c *gin.Context, req RPCRequest) {
	idRaw, ok := req.Params["id"]
	if !ok {
		writeRPCError(c, http.StatusOK, req.ID, RPCInvalidParams, "params.id is required")
		return
	}
	id, ok := idRaw.(string)
	if !ok || id == "" {
		writeRPCError(c, http.StatusOK, req.ID, RPCInvalidParams, "params.id must be a non-empty string")
		return
	}
	task, found := s.Store.Get(id)
	if !found {
		writeRPCError(c, http.StatusOK, req.ID, RPCInvalidParams, "task not found: "+id)
		return
	}
	writeRPCResult(c, req.ID, task)
}

// firstTextPart extracts the first Part{kind:"text"} from a decoded
// params.message object (spec §1.4 — Part discriminated by "kind").
func firstTextPart(msg map[string]any) string {
	partsRaw, ok := msg["parts"]
	if !ok {
		return ""
	}
	parts, ok := partsRaw.([]any)
	if !ok {
		return ""
	}
	for _, pRaw := range parts {
		p, ok := pRaw.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := p["kind"].(string)
		if kind == PartKindText {
			if text, ok := p["text"].(string); ok {
				return text
			}
		}
	}
	return ""
}

func writeRPCResult(c *gin.Context, id any, result any) {
	c.JSON(http.StatusOK, RPCResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func writeRPCError(c *gin.Context, httpStatus int, id any, code int, message string) {
	c.JSON(httpStatus, RPCResponse{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: code, Message: message}})
}
