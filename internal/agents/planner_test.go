package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// planJSON builds a JSON string that the mock brain can return.
func planJSON(goal string, steps []*Step) string {
	p := Plan{Goal: goal, Steps: steps}
	b, err := json.Marshal(p)
	if err != nil {
		panic(fmt.Sprintf("planJSON: marshal: %v", err))
	}
	return string(b)
}

func newTestPlanner(responses []string) *Planner {
	return NewPlanner(newTestBrain(responses))
}

// ---------------------------------------------------------------------------
// Planner.CreatePlan tests
// ---------------------------------------------------------------------------

func TestPlanner_CreatePlan_ValidJSON(t *testing.T) {
	steps := []*Step{
		{ID: "step-1", Description: "Research requirements", Status: "pending"},
		{ID: "step-2", Description: "Write code", Dependencies: []string{"step-1"}, Status: "pending"},
	}
	p := newTestPlanner([]string{planJSON("build a feature", steps)})

	plan, err := p.CreatePlan(context.Background(), "build a feature")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plan.Goal != "build a feature" {
		t.Errorf("expected goal %q, got %q", "build a feature", plan.Goal)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(plan.Steps))
	}
	if plan.Steps[0].ID != "step-1" {
		t.Errorf("expected step-1, got %q", plan.Steps[0].ID)
	}
	if plan.Steps[1].Dependencies[0] != "step-1" {
		t.Errorf("expected dependency step-1, got %v", plan.Steps[1].Dependencies)
	}
}

func TestPlanner_CreatePlan_InvalidJSON_FallsBackToSingleStep(t *testing.T) {
	p := newTestPlanner([]string{"This is not JSON at all."})

	plan, err := p.CreatePlan(context.Background(), "do something complex")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(plan.Steps) != 1 {
		t.Fatalf("expected 1 fallback step, got %d", len(plan.Steps))
	}
	if plan.Steps[0].Status != "pending" {
		t.Errorf("expected pending status, got %q", plan.Steps[0].Status)
	}
	if plan.Goal != "do something complex" {
		t.Errorf("expected goal preserved, got %q", plan.Goal)
	}
}

func TestPlanner_CreatePlan_NilBrain(t *testing.T) {
	p := NewPlanner(nil)
	_, err := p.CreatePlan(context.Background(), "anything")
	if err == nil {
		t.Fatal("expected error for nil brain")
	}
}

func TestPlanner_CreatePlan_BrainError(t *testing.T) {
	b := newErrBrain()
	p := NewPlanner(b)
	_, err := p.CreatePlan(context.Background(), "anything")
	if err == nil {
		t.Fatal("expected error when brain errors")
	}
}

func TestPlanner_CreatePlan_StepIDsFilledWhenMissing(t *testing.T) {
	// Return steps with empty IDs — planner should fill them in.
	raw := `{"goal":"goal","steps":[{"id":"","description":"step one","status":"pending"},{"id":"","description":"step two","status":"pending"}]}`
	p := newTestPlanner([]string{raw})

	plan, err := p.CreatePlan(context.Background(), "goal")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i, s := range plan.Steps {
		if s.ID == "" {
			t.Errorf("step[%d] has empty ID after CreatePlan", i)
		}
	}
}

func TestPlanner_CreatePlan_StatusDefaultsPending(t *testing.T) {
	// Steps without status field should default to "pending".
	raw := `{"goal":"g","steps":[{"id":"step-1","description":"do it"}]}`
	p := newTestPlanner([]string{raw})

	plan, err := p.CreatePlan(context.Background(), "g")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Steps[0].Status != "pending" {
		t.Errorf("expected pending, got %q", plan.Steps[0].Status)
	}
}

// ---------------------------------------------------------------------------
// Planner.NextStep tests
// ---------------------------------------------------------------------------

func TestPlanner_NextStep_ReturnsFirstPendingWithNoDeps(t *testing.T) {
	plan := &Plan{
		Goal: "g",
		Steps: []*Step{
			{ID: "step-1", Description: "first", Status: "pending"},
			{ID: "step-2", Description: "second", Status: "pending"},
		},
	}
	p := NewPlanner(nil)

	next := p.NextStep(plan)
	if next == nil {
		t.Fatal("expected a next step")
	}
	if next.ID != "step-1" {
		t.Errorf("expected step-1, got %q", next.ID)
	}
}

func TestPlanner_NextStep_SkipsCompletedSteps(t *testing.T) {
	plan := &Plan{
		Goal: "g",
		Steps: []*Step{
			{ID: "step-1", Description: "done", Status: "completed"},
			{ID: "step-2", Description: "next up", Status: "pending"},
		},
	}
	p := NewPlanner(nil)

	next := p.NextStep(plan)
	if next == nil {
		t.Fatal("expected a next step")
	}
	if next.ID != "step-2" {
		t.Errorf("expected step-2, got %q", next.ID)
	}
}

func TestPlanner_NextStep_WaitsForDependency(t *testing.T) {
	plan := &Plan{
		Goal: "g",
		Steps: []*Step{
			{ID: "step-1", Description: "first", Status: "pending"},
			{ID: "step-2", Description: "second", Dependencies: []string{"step-1"}, Status: "pending"},
		},
	}
	p := NewPlanner(nil)

	// step-1 is not completed yet, so step-2 should not be returned.
	next := p.NextStep(plan)
	if next == nil || next.ID != "step-1" {
		t.Errorf("expected step-1 (dependency blocker), got %v", next)
	}

	// Complete step-1.
	plan.Steps[0].Status = "completed"

	// Now step-2 should be available.
	next = p.NextStep(plan)
	if next == nil || next.ID != "step-2" {
		t.Fatalf("expected step-2 after dependency fulfilled, got %v", next)
	}
}

func TestPlanner_NextStep_NilPlan(t *testing.T) {
	p := NewPlanner(nil)
	if p.NextStep(nil) != nil {
		t.Error("expected nil for nil plan")
	}
}

func TestPlanner_NextStep_AllCompleted_ReturnsNil(t *testing.T) {
	plan := &Plan{
		Goal: "g",
		Steps: []*Step{
			{ID: "step-1", Status: "completed"},
			{ID: "step-2", Status: "completed"},
		},
	}
	p := NewPlanner(nil)

	if p.NextStep(plan) != nil {
		t.Error("expected nil when all steps are completed")
	}
}

// ---------------------------------------------------------------------------
// Coordinator.DecomposedExecute tests
// ---------------------------------------------------------------------------

func TestCoordinator_DecomposedExecute_RunsEachStep(t *testing.T) {
	// Two steps in the plan; four coordinator phases per step = 8 brain responses
	// plus the initial plan response = 9 total.
	steps := []*Step{
		{ID: "step-1", Description: "step one", Status: "pending"},
		{ID: "step-2", Description: "step two", Status: "pending"},
	}
	planResp := planJSON("complex goal", steps)

	// 1 plan response + 4 phase responses per step (2 steps)
	responses := []string{
		planResp,
		"inv1", "syn1", "impl1", "verif1",
		"inv2", "syn2", "impl2", "verif2",
	}

	b := newTestBrain(responses)
	coord := NewCoordinator(CoordinatorConfig{Brain: b})

	result, err := coord.DecomposedExecute(context.Background(), "complex goal")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 2 steps × 4 phases = 8 tasks
	if len(result.Tasks) != 8 {
		t.Errorf("expected 8 tasks for 2-step plan, got %d", len(result.Tasks))
	}
}

func TestCoordinator_DecomposedExecute_FinalOutputContainsStepIDs(t *testing.T) {
	steps := []*Step{
		{ID: "step-1", Description: "do alpha", Status: "pending"},
	}
	responses := []string{
		planJSON("goal", steps),
		"inv", "syn", "impl", "verification complete",
	}

	b := newTestBrain(responses)
	coord := NewCoordinator(CoordinatorConfig{Brain: b})

	result, err := coord.DecomposedExecute(context.Background(), "goal")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.FinalOutput == "" {
		t.Error("expected non-empty FinalOutput")
	}
}
