package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// Step is a single action within a Plan.
type Step struct {
	ID           string   `json:"id"`
	Description  string   `json:"description"`
	Dependencies []string `json:"dependencies,omitempty"`
	Status       string   `json:"status"` // pending, completed
}

// Plan is an ordered decomposition of a high-level goal into Steps.
type Plan struct {
	Goal  string  `json:"goal"`
	Steps []*Step `json:"steps"`
}

// Planner decomposes complex goals into ordered Steps using an LLM.
type Planner struct {
	brain *brain.Brain
}

// NewPlanner creates a Planner backed by the given Brain.
func NewPlanner(b *brain.Brain) *Planner {
	return &Planner{brain: b}
}

const plannerSystemPrompt = `You are a task decomposition assistant.
Given a high-level goal, break it down into a concrete, ordered list of steps.

Respond ONLY with a valid JSON object matching this schema (no markdown, no extra text):
{
  "goal": "<the original goal>",
  "steps": [
    {
      "id": "step-1",
      "description": "<what to do>",
      "dependencies": [],
      "status": "pending"
    }
  ]
}

Rules:
- Each step must have a unique id of the form "step-N".
- Use the "dependencies" array to list step IDs that must complete before this step.
- Keep steps concrete and independently actionable.
- Return between 2 and 8 steps.`

// CreatePlan sends goal to the LLM and parses the JSON response into a Plan.
// If the LLM response cannot be parsed as JSON the raw text is wrapped in a
// single-step fallback plan so callers always receive a usable Plan.
func (p *Planner) CreatePlan(ctx context.Context, goal string) (*Plan, error) {
	if p.brain == nil {
		return nil, fmt.Errorf("planner: brain must not be nil")
	}

	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleSystem, Content: plannerSystemPrompt},
			{Role: types.RoleUser, Content: goal},
		},
	}

	resp, err := p.brain.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("planner: brain.Complete: %w", err)
	}

	content := strings.TrimSpace(resp.Message.Content)

	var plan Plan
	if jsonErr := json.Unmarshal([]byte(content), &plan); jsonErr != nil {
		// Fallback: wrap raw response in a single step.
		plan = Plan{
			Goal: goal,
			Steps: []*Step{
				{
					ID:          "step-1",
					Description: content,
					Status:      "pending",
				},
			},
		}
	}

	// Ensure all steps have IDs and pending status.
	for i, s := range plan.Steps {
		if s.ID == "" {
			s.ID = fmt.Sprintf("step-%d", i+1)
		}
		if s.Status == "" {
			s.Status = "pending"
		}
	}

	if plan.Goal == "" {
		plan.Goal = goal
	}

	return &plan, nil
}

// NextStep returns the first step whose status is "pending" and whose
// dependencies are all "completed". Returns nil when no step is ready.
func (p *Planner) NextStep(plan *Plan) *Step {
	if plan == nil {
		return nil
	}

	completed := make(map[string]bool)
	for _, s := range plan.Steps {
		if s.Status == "completed" {
			completed[s.ID] = true
		}
	}

	for _, s := range plan.Steps {
		if s.Status != "pending" {
			continue
		}
		ready := true
		for _, dep := range s.Dependencies {
			if !completed[dep] {
				ready = false
				break
			}
		}
		if ready {
			return s
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Coordinator integration: DecomposedExecute
// ---------------------------------------------------------------------------

// DecomposedExecute decomposes a complex goal into a Plan and then runs the
// Coordinator's Execute pipeline for each step in dependency order.
// Results from all steps are concatenated and returned as a CoordinatorResult
// whose FinalOutput summarises every step's verification output.
func (c *Coordinator) DecomposedExecute(ctx context.Context, goal string) (*CoordinatorResult, error) {
	planner := NewPlanner(c.brain)

	plan, err := planner.CreatePlan(ctx, goal)
	if err != nil {
		return nil, fmt.Errorf("coordinator: decomposed plan: %w", err)
	}

	combined := &CoordinatorResult{
		Tasks: make([]*Task, 0),
	}

	var outputs []string

	for {
		step := planner.NextStep(plan)
		if step == nil {
			break
		}

		step.Status = "in_progress"
		subResult, execErr := c.Execute(ctx, step.Description)
		combined.Tasks = append(combined.Tasks, subResult.Tasks...)
		combined.Duration += subResult.Duration

		if execErr != nil {
			step.Status = "pending" // mark as still pending so caller can inspect
			return combined, fmt.Errorf("coordinator: step %q failed: %w", step.ID, execErr)
		}

		step.Status = "completed"
		outputs = append(outputs, fmt.Sprintf("[%s] %s", step.ID, subResult.FinalOutput))
	}

	combined.FinalOutput = strings.Join(outputs, "\n")
	return combined, nil
}

