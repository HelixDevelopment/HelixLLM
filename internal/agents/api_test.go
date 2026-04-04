package agents

import (
	"bytes"
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
