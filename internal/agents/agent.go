package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/metrics"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

const defaultMaxTurns = 10

// AgentConfig holds the dependencies required to create an Agent.
type AgentConfig struct {
	// Brain is the LLM backend used for reasoning. Must not be nil.
	Brain *brain.Brain

	// Tools is the registry of available tools. May be nil (no tool calling).
	Tools *ToolRegistry

	// RAGHook is an optional function that augments the request with retrieved
	// context before the first Brain call.
	RAGHook func(*types.InternalChatRequest) *types.InternalChatRequest

	// MaxTurns caps the ReAct loop iterations. Defaults to 10 when zero.
	MaxTurns int

	// AuditLogger, when set, logs tool calls and LLM completions for audit
	// purposes. May be nil to disable audit logging.
	AuditLogger *AuditLogger
}

// Agent implements a ReAct (Reason-Act-Observe) loop: it sends messages to the
// Brain, checks for tool-call responses, executes tools, appends observations,
// and loops until the LLM returns a final answer or MaxTurns is exceeded.
type Agent struct {
	brain       *brain.Brain
	tools       *ToolRegistry
	ragHook     func(*types.InternalChatRequest) *types.InternalChatRequest
	maxTurns    int
	auditLogger *AuditLogger
}

// NewAgent creates an Agent from the provided config.
func NewAgent(cfg AgentConfig) *Agent {
	maxTurns := cfg.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxTurns
	}
	return &Agent{
		brain:       cfg.Brain,
		tools:       cfg.Tools,
		ragHook:     cfg.RAGHook,
		maxTurns:    maxTurns,
		auditLogger: cfg.AuditLogger,
	}
}

// Run executes the ReAct loop for the given conversation messages and returns
// the final assistant response.
func (a *Agent) Run(ctx context.Context, messages []types.InternalMessage) (*types.InternalChatResponse, error) {
	if a.brain == nil {
		return nil, fmt.Errorf("agent: brain must not be nil")
	}

	// Copy messages so we don't mutate the caller's slice.
	msgs := make([]types.InternalMessage, len(messages))
	copy(msgs, messages)

	// Build a system prompt describing available tools and the tool-call protocol.
	systemPrompt := a.buildSystemPrompt()
	if systemPrompt != "" {
		msgs = append([]types.InternalMessage{
			{Role: types.RoleSystem, Content: systemPrompt},
		}, msgs...)
	}

	req := &types.InternalChatRequest{
		Messages: msgs,
	}

	// Apply RAG hook if present (augments request with retrieved context).
	if a.ragHook != nil {
		req = a.ragHook(req)
	}

	var lastResp *types.InternalChatResponse

	for turn := 0; turn < a.maxTurns; turn++ {
		llmStart := time.Now()
		resp, err := a.brain.Complete(ctx, req)
		llmDuration := time.Since(llmStart)

		if a.auditLogger != nil {
			entry := AuditEntry{
				Timestamp: llmStart,
				EventType: "llm_completion",
				Duration:  llmDuration,
			}
			if err != nil {
				entry.Error = err.Error()
			}
			a.auditLogger.LogCompletion(entry)
		}

		if err != nil {
			return nil, fmt.Errorf("agent: brain.Complete (turn %d): %w", turn+1, err)
		}
		lastResp = resp

		content := resp.Message.Content

		// Check for a tool-call embedded in the assistant's response.
		tc, ok := extractToolCall(content)
		if !ok {
			// No tool call — LLM produced a final answer.
			return resp, nil
		}

		// Audit: log the tool call before execution.
		if a.auditLogger != nil {
			argsJSON, _ := json.Marshal(tc.Arguments)
			a.auditLogger.LogToolCall(AuditEntry{
				Timestamp: time.Now(),
				EventType: "tool_call",
				ToolName:  tc.Name,
				Arguments: string(argsJSON),
			})
		}

		// Execute the requested tool.
		var toolResult string
		if a.tools == nil {
			toolResult = fmt.Sprintf("error: no tools are registered (requested tool: %q)", tc.Name)
		} else {
			toolStart := time.Now()
			result, execErr := a.tools.Execute(ctx, tc.Name, tc.Arguments)
			toolDuration := time.Since(toolStart)
			metrics.TrackToolExecution(tc.Name, toolDuration, execErr == nil)

			// Audit: log the tool result after execution.
			if a.auditLogger != nil {
				entry := AuditEntry{
					Timestamp: toolStart,
					EventType: "tool_result",
					ToolName:  tc.Name,
					Duration:  toolDuration,
				}
				if execErr != nil {
					entry.Error = execErr.Error()
					entry.Result = ""
				} else {
					entry.Result = result
				}
				a.auditLogger.LogToolCall(entry)
			}

			if execErr != nil {
				toolResult = fmt.Sprintf("error: %s", execErr.Error())
			} else {
				toolResult = result
			}
		}

		// Append the assistant's tool-call message and the tool observation.
		req.Messages = append(req.Messages,
			resp.Message,
			types.InternalMessage{
				Role:    types.RoleTool,
				Content: toolResult,
				Name:    tc.Name,
			},
		)
	}

	// Max turns reached — return the last response.
	return lastResp, nil
}

// buildSystemPrompt constructs a system prompt that lists available tools and
// instructs the LLM how to invoke them.
func (a *Agent) buildSystemPrompt() string {
	if a.tools == nil {
		return ""
	}
	infos := a.tools.List()
	if len(infos) == 0 {
		return ""
	}

	toolsJSON, err := json.MarshalIndent(infos, "", "  ")
	if err != nil {
		// Fallback: list tool names only.
		names := make([]string, len(infos))
		for i, t := range infos {
			names[i] = t.Name
		}
		toolsJSON = []byte("[" + strings.Join(names, ", ") + "]")
	}

	return fmt.Sprintf(`You are a helpful assistant with access to the following tools:

%s

When you need to use a tool, respond with ONLY a JSON object in this exact format:
{"tool_call": {"name": "<tool_name>", "arguments": {<arguments>}}}

After receiving the tool result, continue reasoning and either use another tool or provide your final answer as plain text (not JSON).`, string(toolsJSON))
}

// toolCallRequest is the parsed form of a tool-call embedded in LLM output.
type toolCallRequest struct {
	Name      string
	Arguments map[string]interface{}
}

// extractToolCall attempts to parse a tool-call JSON object from the LLM
// content. Returns the parsed request and true if successful.
// Handles both raw JSON and markdown-fenced JSON (```json ... ```).
func extractToolCall(content string) (*toolCallRequest, bool) {
	content = strings.TrimSpace(content)

	// Strip markdown code fences if present — LLMs often wrap JSON
	// tool calls in ```json ... ``` blocks.
	if idx := strings.Index(content, "```"); idx >= 0 {
		// Find the opening fence
		start := idx + 3
		// Skip optional language tag (e.g., "json")
		if nl := strings.IndexByte(content[start:], '\n'); nl >= 0 {
			start += nl + 1
		}
		// Find closing fence
		if end := strings.Index(content[start:], "```"); end >= 0 {
			content = strings.TrimSpace(content[start : start+end])
		}
	}

	// Also try extracting JSON from surrounding prose: find first { to last }
	if !strings.HasPrefix(content, "{") {
		if braceStart := strings.Index(content, "{"); braceStart >= 0 {
			if braceEnd := strings.LastIndex(content, "}"); braceEnd > braceStart {
				content = content[braceStart : braceEnd+1]
			}
		}
	}

	var tc struct {
		ToolCall *struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		} `json:"tool_call"`
	}
	if err := json.Unmarshal([]byte(content), &tc); err != nil || tc.ToolCall == nil {
		return nil, false
	}
	if tc.ToolCall.Name == "" {
		return nil, false
	}
	return &toolCallRequest{
		Name:      tc.ToolCall.Name,
		Arguments: tc.ToolCall.Arguments,
	}, true
}
