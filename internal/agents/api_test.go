package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newTestRouter(agent *Agent, convCtx *ConversationContext) *gin.Engine {
	r := gin.New()
	RegisterAgentRoutes(r, agent, convCtx)
	return r
}

// newExtrasRouter wires the full set of routes including coordinator, planner,
// and memory manager.
func newExtrasRouter(
	agent *Agent,
	convCtx *ConversationContext,
	coord *Coordinator,
	planner *Planner,
	memMgr *MemoryManager,
) *gin.Engine {
	r := gin.New()
	RegisterAgentRoutesWithExtras(r, agent, convCtx, coord, planner, memMgr)
	return r
}

func postChat(t *testing.T, r *gin.Engine, body AgentChatRequest) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/agents/chat", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func postJSON(t *testing.T, r *gin.Engine, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// Silence the unused import of context for test helpers that need it.
var _ = context.Background

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestAgentAPI_ChatWithoutSession(t *testing.T) {
	b := newTestBrain([]string{"Hello from agent!"})
	agent := NewAgent(AgentConfig{Brain: b})
	convCtx := NewConversationContext(50)
	r := newTestRouter(agent, convCtx)

	w := postChat(t, r, AgentChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "hi"},
		},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp AgentChatResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Response.Message.Content != "Hello from agent!" {
		t.Errorf("unexpected content: %q", resp.Response.Message.Content)
	}
	if resp.SessionID != "" {
		t.Errorf("expected empty session_id, got %q", resp.SessionID)
	}
}

func TestAgentAPI_ChatWithSession_HistoryPreserved(t *testing.T) {
	// Two turns: first returns "Turn 1 answer", second returns "Turn 2 answer".
	b := newTestBrain([]string{"Turn 1 answer", "Turn 2 answer"})
	agent := NewAgent(AgentConfig{Brain: b})
	convCtx := NewConversationContext(50)
	r := newTestRouter(agent, convCtx)

	// First turn.
	w1 := postChat(t, r, AgentChatRequest{
		SessionID: "sess-abc",
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "first question"},
		},
	})
	if w1.Code != http.StatusOK {
		t.Fatalf("turn 1: expected 200, got %d: %s", w1.Code, w1.Body.String())
	}

	// Verify session was saved.
	history := convCtx.Get("sess-abc")
	if len(history) != 2 { // user msg + assistant msg
		t.Fatalf("expected 2 messages in history after turn 1, got %d", len(history))
	}

	// Second turn — history should be prepended.
	w2 := postChat(t, r, AgentChatRequest{
		SessionID: "sess-abc",
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "second question"},
		},
	})
	if w2.Code != http.StatusOK {
		t.Fatalf("turn 2: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	var resp2 AgentChatResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("unmarshal turn 2 response: %v", err)
	}
	if resp2.Response.Message.Content != "Turn 2 answer" {
		t.Errorf("unexpected content: %q", resp2.Response.Message.Content)
	}
	if resp2.SessionID != "sess-abc" {
		t.Errorf("expected session_id %q, got %q", "sess-abc", resp2.SessionID)
	}

	// History should now have 4 messages.
	history2 := convCtx.Get("sess-abc")
	if len(history2) != 4 {
		t.Errorf("expected 4 messages in history after turn 2, got %d", len(history2))
	}
}

func TestAgentAPI_ListTools(t *testing.T) {
	b := newTestBrain([]string{"ok"})
	reg := newTestRegistry() // has "echo" tool
	agent := NewAgent(AgentConfig{Brain: b, Tools: reg})
	convCtx := NewConversationContext(50)
	r := newTestRouter(agent, convCtx)

	req := httptest.NewRequest(http.MethodGet, "/v1/agents/tools", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		Tools []ToolInfo `json:"tools"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal tools response: %v", err)
	}
	if len(body.Tools) == 0 {
		t.Fatal("expected at least one tool in response")
	}
	if body.Tools[0].Name != "echo" {
		t.Errorf("expected tool %q, got %q", "echo", body.Tools[0].Name)
	}
}

func TestAgentAPI_ListTools_NoRegistry(t *testing.T) {
	b := newTestBrain([]string{"ok"})
	agent := NewAgent(AgentConfig{Brain: b}) // no tools
	r := newTestRouter(agent, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/agents/tools", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body struct {
		Tools []ToolInfo `json:"tools"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Tools == nil {
		t.Error("expected non-nil (empty) tools array")
	}
	if len(body.Tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(body.Tools))
	}
}

func TestAgentAPI_InvalidRequest_EmptyBody(t *testing.T) {
	b := newTestBrain([]string{"ok"})
	agent := NewAgent(AgentConfig{Brain: b})
	r := newTestRouter(agent, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/agents/chat", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAgentAPI_BrainError_Returns500(t *testing.T) {
	// Use errBrainProvider (defined in agent_test.go, same package) to force
	// agent.Run to return an error, which should produce a 500 response.
	b := brain.New(brain.Config{DefaultProvider: "err"})
	b.RegisterProvider("err", &errBrainProvider{})
	agent := NewAgent(AgentConfig{Brain: b})
	r := newTestRouter(agent, nil)

	w := postChat(t, r, AgentChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "fail"},
		},
	})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAgentAPI_InvalidRequest_MalformedJSON(t *testing.T) {
	b := newTestBrain([]string{"ok"})
	agent := NewAgent(AgentConfig{Brain: b})
	r := newTestRouter(agent, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/agents/chat", bytes.NewReader([]byte(`not-json`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Coordinate endpoint tests
// ---------------------------------------------------------------------------

func TestAgentAPI_Coordinate_Success(t *testing.T) {
	b := newTestBrain(phaseResponses("inv", "syn", "impl", "done"))
	agent := NewAgent(AgentConfig{Brain: b})
	coord := NewCoordinator(CoordinatorConfig{Brain: b})
	r := newExtrasRouter(agent, nil, coord, nil, nil)

	w := postJSON(t, r, "/v1/agents/coordinate", CoordinateRequest{Task: "build something"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp CoordinateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Tasks) != 4 {
		t.Errorf("expected 4 tasks, got %d", len(resp.Tasks))
	}
	if resp.FinalOutput != "done" {
		t.Errorf("expected FinalOutput 'done', got %q", resp.FinalOutput)
	}
}

func TestAgentAPI_Coordinate_NilCoordinator_Returns501(t *testing.T) {
	b := newTestBrain([]string{"ok"})
	agent := NewAgent(AgentConfig{Brain: b})
	r := newExtrasRouter(agent, nil, nil, nil, nil)

	w := postJSON(t, r, "/v1/agents/coordinate", CoordinateRequest{Task: "task"})
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", w.Code)
	}
}

func TestAgentAPI_Coordinate_EmptyTask_Returns400(t *testing.T) {
	b := newTestBrain([]string{"ok"})
	agent := NewAgent(AgentConfig{Brain: b})
	coord := NewCoordinator(CoordinatorConfig{Brain: b})
	r := newExtrasRouter(agent, nil, coord, nil, nil)

	w := postJSON(t, r, "/v1/agents/coordinate", CoordinateRequest{Task: ""})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Plan endpoint tests
// ---------------------------------------------------------------------------

func TestAgentAPI_Plan_Success(t *testing.T) {
	steps := []*Step{
		{ID: "step-1", Description: "Research", Status: "pending"},
		{ID: "step-2", Description: "Implement", Dependencies: []string{"step-1"}, Status: "pending"},
	}
	b := newTestBrain([]string{planJSON("build a thing", steps)})
	agent := NewAgent(AgentConfig{Brain: b})
	planner := NewPlanner(b)
	r := newExtrasRouter(agent, nil, nil, planner, nil)

	w := postJSON(t, r, "/v1/agents/plan", PlanRequest{Goal: "build a thing"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var plan Plan
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if plan.Goal != "build a thing" {
		t.Errorf("expected goal 'build a thing', got %q", plan.Goal)
	}
	if len(plan.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(plan.Steps))
	}
}

func TestAgentAPI_Plan_NilPlanner_Returns501(t *testing.T) {
	b := newTestBrain([]string{"ok"})
	agent := NewAgent(AgentConfig{Brain: b})
	r := newExtrasRouter(agent, nil, nil, nil, nil)

	w := postJSON(t, r, "/v1/agents/plan", PlanRequest{Goal: "something"})
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", w.Code)
	}
}

func TestAgentAPI_Plan_EmptyGoal_Returns400(t *testing.T) {
	b := newTestBrain([]string{"ok"})
	agent := NewAgent(AgentConfig{Brain: b})
	planner := NewPlanner(b)
	r := newExtrasRouter(agent, nil, nil, planner, nil)

	w := postJSON(t, r, "/v1/agents/plan", PlanRequest{Goal: ""})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Memory remember endpoint tests
// ---------------------------------------------------------------------------

func TestAgentAPI_MemoryRemember_Success(t *testing.T) {
	b := newTestBrain([]string{"ok"})
	agent := NewAgent(AgentConfig{Brain: b})
	convCtx := NewConversationContext(50)
	memMgr := NewMemoryManager(convCtx, nil)
	r := newExtrasRouter(agent, convCtx, nil, nil, memMgr)

	w := postJSON(t, r, "/v1/agents/memory/remember", MemoryRememberRequest{
		SessionID:  "sess1",
		Content:    "user prefers dark mode",
		Importance: 0.8,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAgentAPI_MemoryRemember_NilManager_Returns501(t *testing.T) {
	b := newTestBrain([]string{"ok"})
	agent := NewAgent(AgentConfig{Brain: b})
	r := newExtrasRouter(agent, nil, nil, nil, nil)

	w := postJSON(t, r, "/v1/agents/memory/remember", MemoryRememberRequest{
		SessionID: "s", Content: "x", Importance: 0.5,
	})
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", w.Code)
	}
}

func TestAgentAPI_MemoryRemember_EmptyContent_Returns400(t *testing.T) {
	b := newTestBrain([]string{"ok"})
	agent := NewAgent(AgentConfig{Brain: b})
	convCtx := NewConversationContext(50)
	memMgr := NewMemoryManager(convCtx, nil)
	r := newExtrasRouter(agent, convCtx, nil, nil, memMgr)

	w := postJSON(t, r, "/v1/agents/memory/remember", MemoryRememberRequest{
		SessionID: "sess1", Content: "", Importance: 0.5,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAgentAPI_MemoryRemember_EmptySession_Returns400(t *testing.T) {
	b := newTestBrain([]string{"ok"})
	agent := NewAgent(AgentConfig{Brain: b})
	convCtx := NewConversationContext(50)
	memMgr := NewMemoryManager(convCtx, nil)
	r := newExtrasRouter(agent, convCtx, nil, nil, memMgr)

	w := postJSON(t, r, "/v1/agents/memory/remember", MemoryRememberRequest{
		SessionID: "", Content: "something", Importance: 0.5,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Memory recall endpoint tests
// ---------------------------------------------------------------------------

func TestAgentAPI_MemoryRecall_Success(t *testing.T) {
	b := newTestBrain([]string{"ok"})
	agent := NewAgent(AgentConfig{Brain: b})
	convCtx := NewConversationContext(50)
	memMgr := NewMemoryManager(convCtx, nil)
	r := newExtrasRouter(agent, convCtx, nil, nil, memMgr)

	// Store something first via the manager directly.
	memMgr.Remember(nil, "sess1", "deployment uses blue-green strategy", 0.8) //nolint:staticcheck

	w := postJSON(t, r, "/v1/agents/memory/recall", MemoryRecallRequest{
		SessionID: "sess1",
		Query:     "deployment strategy",
		TopK:      5,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp MemoryRecallResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Memories) == 0 {
		t.Error("expected at least one recalled memory")
	}
}

func TestAgentAPI_MemoryRecall_NilManager_Returns501(t *testing.T) {
	b := newTestBrain([]string{"ok"})
	agent := NewAgent(AgentConfig{Brain: b})
	r := newExtrasRouter(agent, nil, nil, nil, nil)

	w := postJSON(t, r, "/v1/agents/memory/recall", MemoryRecallRequest{
		SessionID: "s", Query: "q",
	})
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", w.Code)
	}
}

func TestAgentAPI_MemoryRecall_EmptyQuery_Returns400(t *testing.T) {
	b := newTestBrain([]string{"ok"})
	agent := NewAgent(AgentConfig{Brain: b})
	convCtx := NewConversationContext(50)
	memMgr := NewMemoryManager(convCtx, nil)
	r := newExtrasRouter(agent, convCtx, nil, nil, memMgr)

	w := postJSON(t, r, "/v1/agents/memory/recall", MemoryRecallRequest{
		SessionID: "sess1", Query: "",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAgentAPI_MemoryRecall_EmptySession_Returns400(t *testing.T) {
	b := newTestBrain([]string{"ok"})
	agent := NewAgent(AgentConfig{Brain: b})
	convCtx := NewConversationContext(50)
	memMgr := NewMemoryManager(convCtx, nil)
	r := newExtrasRouter(agent, convCtx, nil, nil, memMgr)

	w := postJSON(t, r, "/v1/agents/memory/recall", MemoryRecallRequest{
		SessionID: "", Query: "something",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAgentAPI_MemoryRecall_ReturnsEmptyArrayNotNull(t *testing.T) {
	b := newTestBrain([]string{"ok"})
	agent := NewAgent(AgentConfig{Brain: b})
	convCtx := NewConversationContext(50)
	memMgr := NewMemoryManager(convCtx, nil)
	r := newExtrasRouter(agent, convCtx, nil, nil, memMgr)

	w := postJSON(t, r, "/v1/agents/memory/recall", MemoryRecallRequest{
		SessionID: "empty-sess", Query: "nothing stored here",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp MemoryRecallResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Memories == nil {
		t.Error("expected non-nil (empty) memories array, got null")
	}
}
