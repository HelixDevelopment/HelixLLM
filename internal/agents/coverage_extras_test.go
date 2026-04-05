package agents

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// systemPromptForPhase — default (unknown) phase
// ---------------------------------------------------------------------------

func TestSystemPromptForPhase_UnknownPhase(t *testing.T) {
	prompt := systemPromptForPhase("unknown-phase", RoleCoder, "some task")
	if prompt != "some task" {
		t.Errorf("expected task passthrough for unknown phase, got %q", prompt)
	}
}

// ---------------------------------------------------------------------------
// coordinateHandler — malformed JSON
// ---------------------------------------------------------------------------

func TestAgentAPI_Coordinate_MalformedJSON_Returns400(t *testing.T) {
	b := newTestBrain([]string{"ok"})
	agent := NewAgent(AgentConfig{Brain: b})
	coord := NewCoordinator(CoordinatorConfig{Brain: b})
	r := newExtrasRouter(agent, nil, coord, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/agents/coordinate",
		bytes.NewReader([]byte(`not-json`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// planHandler — malformed JSON
// ---------------------------------------------------------------------------

func TestAgentAPI_Plan_MalformedJSON_Returns400(t *testing.T) {
	b := newTestBrain([]string{"ok"})
	agent := NewAgent(AgentConfig{Brain: b})
	planner := NewPlanner(b)
	r := newExtrasRouter(agent, nil, nil, planner, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/agents/plan",
		bytes.NewReader([]byte(`{invalid`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// memoryRememberHandler — malformed JSON
// ---------------------------------------------------------------------------

func TestAgentAPI_MemoryRemember_MalformedJSON_Returns400(t *testing.T) {
	b := newTestBrain([]string{"ok"})
	agent := NewAgent(AgentConfig{Brain: b})
	convCtx := NewConversationContext(50)
	memMgr := NewMemoryManager(convCtx, nil)
	r := newExtrasRouter(agent, convCtx, nil, nil, memMgr)

	req := httptest.NewRequest(http.MethodPost, "/v1/agents/memory/remember",
		bytes.NewReader([]byte(`not-json`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// memoryRecallHandler — malformed JSON
// ---------------------------------------------------------------------------

func TestAgentAPI_MemoryRecall_MalformedJSON_Returns400(t *testing.T) {
	b := newTestBrain([]string{"ok"})
	agent := NewAgent(AgentConfig{Brain: b})
	convCtx := NewConversationContext(50)
	memMgr := NewMemoryManager(convCtx, nil)
	r := newExtrasRouter(agent, convCtx, nil, nil, memMgr)

	req := httptest.NewRequest(http.MethodPost, "/v1/agents/memory/recall",
		bytes.NewReader([]byte(`not-json`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// coordinateHandler — brain error propagation (coord.Execute fails)
// ---------------------------------------------------------------------------

func TestAgentAPI_Coordinate_BrainError_Returns500(t *testing.T) {
	b := newErrBrain()
	agent := NewAgent(AgentConfig{Brain: b})
	coord := NewCoordinator(CoordinatorConfig{Brain: b})
	r := newExtrasRouter(agent, nil, coord, nil, nil)

	w := postJSON(t, r, "/v1/agents/coordinate", CoordinateRequest{Task: "fail"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// planHandler — brain error propagation (planner.CreatePlan fails)
// ---------------------------------------------------------------------------

func TestAgentAPI_Plan_BrainError_Returns500(t *testing.T) {
	b := newErrBrain()
	agent := NewAgent(AgentConfig{Brain: b})
	planner := NewPlanner(b)
	r := newExtrasRouter(agent, nil, nil, planner, nil)

	w := postJSON(t, r, "/v1/agents/plan", PlanRequest{Goal: "fail"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}
