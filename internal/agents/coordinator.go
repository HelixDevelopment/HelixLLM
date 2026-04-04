package agents

import (
	"context"
	"fmt"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
	"github.com/google/uuid"
)

// Phase represents a stage in the multi-agent execution pipeline.
type Phase string

const (
	PhaseInvestigation  Phase = "investigation"
	PhaseSynthesis      Phase = "synthesis"
	PhaseImplementation Phase = "implementation"
	PhaseVerification   Phase = "verification"
)

// AgentRole identifies the functional role a Worker plays.
type AgentRole string

const (
	RoleOrchestrator AgentRole = "orchestrator"
	RoleCoder        AgentRole = "coder"
	RoleResearcher   AgentRole = "researcher"
	RoleReviewer     AgentRole = "reviewer"
)

// Worker wraps an Agent with a named role.
type Worker struct {
	Role  AgentRole
	Agent *Agent
}

// Task is a unit of work assigned to a Worker during a specific Phase.
type Task struct {
	ID          string
	Description string
	Phase       Phase
	AssignedTo  AgentRole
	Status      string // pending, in_progress, completed, failed
	Result      string
}

// CoordinatorConfig holds the dependencies required to create a Coordinator.
type CoordinatorConfig struct {
	Brain   *brain.Brain
	Tools   *ToolRegistry
	RAGHook func(*types.InternalChatRequest) *types.InternalChatRequest
}

// Coordinator orchestrates multiple Worker agents through a fixed four-phase
// pipeline: Investigation → Synthesis → Implementation → Verification.
type Coordinator struct {
	brain   *brain.Brain
	tools   *ToolRegistry
	ragHook func(*types.InternalChatRequest) *types.InternalChatRequest
	workers map[AgentRole]*Worker
}

// NewCoordinator creates a Coordinator and pre-builds one Agent per role using
// the shared Brain, ToolRegistry, and RAG hook from cfg.
func NewCoordinator(cfg CoordinatorConfig) *Coordinator {
	c := &Coordinator{
		brain:   cfg.Brain,
		tools:   cfg.Tools,
		ragHook: cfg.RAGHook,
		workers: make(map[AgentRole]*Worker),
	}

	for _, role := range []AgentRole{RoleOrchestrator, RoleCoder, RoleResearcher, RoleReviewer} {
		c.workers[role] = &Worker{
			Role: role,
			Agent: NewAgent(AgentConfig{
				Brain:    cfg.Brain,
				Tools:    cfg.Tools,
				RAGHook:  cfg.RAGHook,
				MaxTurns: 10,
			}),
		}
	}

	return c
}

// CoordinatorResult collects every task that was executed and the final output.
type CoordinatorResult struct {
	Tasks      []*Task
	FinalOutput string
	Duration   time.Duration
}

// systemPromptForPhase returns a role-specific system prompt for the given phase.
func systemPromptForPhase(phase Phase, role AgentRole, task string) string {
	switch phase {
	case PhaseInvestigation:
		return fmt.Sprintf(
			"You are a researcher agent. Your job is to investigate the following task and gather all relevant context, facts, and constraints needed to complete it.\n\nTask: %s\n\nProvide a thorough investigation summary.", task)
	case PhaseSynthesis:
		return fmt.Sprintf(
			"You are an orchestrator agent. Based on the investigation findings below, synthesize an actionable plan.\n\nTask: %s\n\nProvide a clear, ordered plan of action.", task)
	case PhaseImplementation:
		return fmt.Sprintf(
			"You are a coder agent. Implement the plan provided. Produce concrete, working output.\n\nTask: %s\n\nDeliver the implementation.", task)
	case PhaseVerification:
		return fmt.Sprintf(
			"You are a reviewer agent. Verify that the implementation satisfies the original task requirements.\n\nOriginal task: %s\n\nProvide a verification report confirming correctness or listing issues.", task)
	default:
		return task
	}
}

// runPhase builds a task, runs the assigned worker's agent with a phase-specific
// system prompt, and returns the populated Task.
func (c *Coordinator) runPhase(ctx context.Context, phase Phase, role AgentRole, taskDesc string, prior string) (*Task, error) {
	t := &Task{
		ID:          uuid.NewString(),
		Description: taskDesc,
		Phase:       phase,
		AssignedTo:  role,
		Status:      "in_progress",
	}

	worker, ok := c.workers[role]
	if !ok {
		t.Status = "failed"
		t.Result = fmt.Sprintf("no worker registered for role %q", role)
		return t, fmt.Errorf("coordinator: no worker for role %q", role)
	}

	systemPrompt := systemPromptForPhase(phase, role, taskDesc)

	var msgs []types.InternalMessage
	msgs = append(msgs, types.InternalMessage{Role: types.RoleSystem, Content: systemPrompt})
	if prior != "" {
		msgs = append(msgs, types.InternalMessage{Role: types.RoleAssistant, Content: prior})
	}
	msgs = append(msgs, types.InternalMessage{Role: types.RoleUser, Content: taskDesc})

	resp, err := worker.Agent.Run(ctx, msgs)
	if err != nil {
		t.Status = "failed"
		t.Result = err.Error()
		return t, fmt.Errorf("coordinator: phase %s agent error: %w", phase, err)
	}

	t.Status = "completed"
	t.Result = resp.Message.Content
	return t, nil
}

// Execute runs the full four-phase pipeline for the given task description and
// returns a CoordinatorResult containing all tasks and the final output.
//
// Phases:
//  1. Investigation  – Researcher gathers context
//  2. Synthesis      – Orchestrator analyzes findings, creates plan
//  3. Implementation – Coder executes the plan
//  4. Verification   – Reviewer validates the results
func (c *Coordinator) Execute(ctx context.Context, task string) (*CoordinatorResult, error) {
	start := time.Now()
	result := &CoordinatorResult{Tasks: make([]*Task, 0, 4)}

	// Phase 1: Investigation
	t1, err := c.runPhase(ctx, PhaseInvestigation, RoleResearcher, task, "")
	result.Tasks = append(result.Tasks, t1)
	if err != nil {
		result.Duration = time.Since(start)
		return result, err
	}

	// Phase 2: Synthesis — pass investigation findings as prior context
	t2, err := c.runPhase(ctx, PhaseSynthesis, RoleOrchestrator, task, t1.Result)
	result.Tasks = append(result.Tasks, t2)
	if err != nil {
		result.Duration = time.Since(start)
		return result, err
	}

	// Phase 3: Implementation — pass synthesis plan as prior context
	t3, err := c.runPhase(ctx, PhaseImplementation, RoleCoder, task, t2.Result)
	result.Tasks = append(result.Tasks, t3)
	if err != nil {
		result.Duration = time.Since(start)
		return result, err
	}

	// Phase 4: Verification — pass implementation result as prior context
	t4, err := c.runPhase(ctx, PhaseVerification, RoleReviewer, task, t3.Result)
	result.Tasks = append(result.Tasks, t4)
	if err != nil {
		result.Duration = time.Since(start)
		return result, err
	}

	result.FinalOutput = t4.Result
	result.Duration = time.Since(start)
	return result, nil
}
