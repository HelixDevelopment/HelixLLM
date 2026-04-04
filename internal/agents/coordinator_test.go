package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// phaseResponses returns four canned strings — one per phase — for mock brain.
func phaseResponses(investigation, synthesis, implementation, verification string) []string {
	return []string{investigation, synthesis, implementation, verification}
}

func newTestCoordinator(responses []string) *Coordinator {
	b := newTestBrain(responses)
	return NewCoordinator(CoordinatorConfig{Brain: b})
}

// newErrCoordinator builds a Coordinator whose Brain always returns an error.
func newErrCoordinator() *Coordinator {
	return NewCoordinator(CoordinatorConfig{Brain: newErrBrain()})
}

// newErrBrain builds a Brain wired with the always-failing errBrainProvider.
func newErrBrain() *brain.Brain {
	b := brain.New(brain.Config{DefaultProvider: "err"})
	b.RegisterProvider("err", &errBrainProvider{})
	return b
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestCoordinator_Execute_FourPhasesRun(t *testing.T) {
	coord := newTestCoordinator(phaseResponses(
		"Investigation: found relevant context",
		"Synthesis: here is the plan",
		"Implementation: code written",
		"Verification: all checks pass",
	))

	result, err := coord.Execute(context.Background(), "build a feature")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Tasks) != 4 {
		t.Fatalf("expected 4 tasks, got %d", len(result.Tasks))
	}
}

func TestCoordinator_Execute_PhaseOrder(t *testing.T) {
	coord := newTestCoordinator(phaseResponses("inv", "syn", "impl", "verif"))

	result, err := coord.Execute(context.Background(), "task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedPhases := []Phase{
		PhaseInvestigation,
		PhaseSynthesis,
		PhaseImplementation,
		PhaseVerification,
	}
	for i, task := range result.Tasks {
		if task.Phase != expectedPhases[i] {
			t.Errorf("task[%d]: expected phase %q, got %q", i, expectedPhases[i], task.Phase)
		}
	}
}

func TestCoordinator_Execute_RoleAssignment(t *testing.T) {
	coord := newTestCoordinator(phaseResponses("a", "b", "c", "d"))

	result, err := coord.Execute(context.Background(), "task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedRoles := []AgentRole{
		RoleResearcher,
		RoleOrchestrator,
		RoleCoder,
		RoleReviewer,
	}
	for i, task := range result.Tasks {
		if task.AssignedTo != expectedRoles[i] {
			t.Errorf("task[%d]: expected role %q, got %q", i, expectedRoles[i], task.AssignedTo)
		}
	}
}

func TestCoordinator_Execute_TaskStatusCompleted(t *testing.T) {
	coord := newTestCoordinator(phaseResponses("a", "b", "c", "d"))

	result, err := coord.Execute(context.Background(), "task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, task := range result.Tasks {
		if task.Status != "completed" {
			t.Errorf("task phase %q: expected status completed, got %q", task.Phase, task.Status)
		}
	}
}

func TestCoordinator_Execute_ResultCollection(t *testing.T) {
	coord := newTestCoordinator(phaseResponses(
		"inv result", "syn result", "impl result", "verif result",
	))

	result, err := coord.Execute(context.Background(), "task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"inv result", "syn result", "impl result", "verif result"}
	for i, task := range result.Tasks {
		if task.Result != expected[i] {
			t.Errorf("task[%d] result: expected %q, got %q", i, expected[i], task.Result)
		}
	}
}

func TestCoordinator_Execute_FinalOutputIsVerificationResult(t *testing.T) {
	coord := newTestCoordinator(phaseResponses("a", "b", "c", "verification done"))

	result, err := coord.Execute(context.Background(), "task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.FinalOutput != "verification done" {
		t.Errorf("expected FinalOutput %q, got %q", "verification done", result.FinalOutput)
	}
}

func TestCoordinator_Execute_DurationSet(t *testing.T) {
	coord := newTestCoordinator(phaseResponses("a", "b", "c", "d"))

	result, err := coord.Execute(context.Background(), "task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Duration <= 0 {
		t.Error("expected positive Duration in result")
	}
}

func TestCoordinator_Execute_BrainError_PhaseFailure(t *testing.T) {
	coord := newErrCoordinator()

	result, err := coord.Execute(context.Background(), "task")
	if err == nil {
		t.Fatal("expected error when brain always errors")
	}
	if len(result.Tasks) == 0 {
		t.Fatal("expected at least one task recorded on failure")
	}
	if result.Tasks[0].Status != "failed" {
		t.Errorf("expected first task status 'failed', got %q", result.Tasks[0].Status)
	}
}

func TestCoordinator_Execute_TaskIDsAreUnique(t *testing.T) {
	coord := newTestCoordinator(phaseResponses("a", "b", "c", "d"))

	result, err := coord.Execute(context.Background(), "task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	seen := make(map[string]bool)
	for _, task := range result.Tasks {
		if seen[task.ID] {
			t.Errorf("duplicate task ID %q", task.ID)
		}
		seen[task.ID] = true
	}
}

func TestCoordinator_Execute_TaskDescriptionSet(t *testing.T) {
	coord := newTestCoordinator(phaseResponses("a", "b", "c", "d"))
	description := "implement authentication module"

	result, err := coord.Execute(context.Background(), description)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, task := range result.Tasks {
		if !strings.Contains(task.Description, description) {
			t.Errorf("task.Description should contain the original task, got %q", task.Description)
		}
	}
}
