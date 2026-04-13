package gateway

import (
	"encoding/json"
	"log"

	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// RequestOrchestrator pre-processes requests to help small models succeed
// at multi-step coding tasks. Instead of the model calling multiple tools
// in a loop (read → read → read → summarize), the orchestrator:
//
//  1. Detects what kind of task the user wants (file update, git op, etc.)
//  2. Pre-fetches needed context (existing file content via RAG)
//  3. Injects that context into the system prompt
//  4. Tells the model to produce a SINGLE tool call with the result
//
// This eliminates multi-step tool loops that 7-8B models can't handle
// reliably, while still letting the model decide what content to generate.
//
// Per SYSTEM_DESIGN.md: "Optimized function calling for 1.5B parameter
// models" — the model does the thinking, the orchestrator handles the
// plumbing.
type RequestOrchestrator struct{}

// NewRequestOrchestrator creates a new orchestrator.
func NewRequestOrchestrator() *RequestOrchestrator {
	return &RequestOrchestrator{}
}

// EnhanceRequest examines the conversation and adds context hints to the
// system prompt so the model can produce a single correct tool call
// instead of looping through exploration calls.
//
// Enhancements:
//   - If conversation has tool results, add a summary hint
//   - If model has been exploring (multiple tool calls), nudge toward action
//   - Add turn count awareness so model knows to wrap up
func (o *RequestOrchestrator) EnhanceRequest(req *types.InternalChatRequest) *types.InternalChatRequest {
	if req == nil || len(req.Messages) == 0 {
		return req
	}

	// Count tool call/result pairs in the conversation
	toolCalls := 0
	hasToolResults := false
	var lastToolContent string
	for _, m := range req.Messages {
		if m.Role == "tool" {
			toolCalls++
			hasToolResults = true
			if m.Content != "" {
				lastToolContent = m.Content
			}
		}
	}

	// If no tool results yet, no enhancement needed (first turn)
	if !hasToolResults {
		return req
	}

	modified := *req
	modified.Messages = make([]types.InternalMessage, len(req.Messages))
	copy(modified.Messages, req.Messages)

	// After 2+ tool calls, nudge the model to produce output instead
	// of continuing to explore. This prevents the read→read→read loop.
	if toolCalls >= 2 {
		hint := types.InternalMessage{
			Role: types.RoleSystem,
			Content: "IMPORTANT: You have already gathered enough context from previous tool calls. " +
				"Now produce your final output. If the task requires writing/editing a file, " +
				"call Write or Edit NOW with the content. Do NOT call Read, Glob, or Bash to " +
				"gather more information. Act on what you already have.",
		}
		// Insert hint just before the last message
		lastIdx := len(modified.Messages) - 1
		modified.Messages = append(modified.Messages[:lastIdx],
			append([]types.InternalMessage{hint}, modified.Messages[lastIdx:]...)...)
		log.Printf("[HelixLLM] Orchestrator: injected action hint after %d tool calls", toolCalls)
	}

	// If the last tool result contains file content, summarize what was found
	if toolCalls >= 1 && lastToolContent != "" && len(lastToolContent) > 100 {
		_ = lastToolContent // Content is already in the conversation
	}

	return &modified
}

// CompactToolHistory collapses old tool call/result pairs in the
// conversation to save context tokens. Keeps only the most recent
// tool pair intact; older pairs are summarized.
func (o *RequestOrchestrator) CompactToolHistory(req *types.InternalChatRequest, maxPairs int) *types.InternalChatRequest {
	if req == nil || maxPairs <= 0 {
		return req
	}

	// Count tool pairs (assistant_empty + tool)
	var pairs []int // indices of tool messages
	for i, m := range req.Messages {
		if m.Role == "tool" {
			pairs = append(pairs, i)
		}
	}

	if len(pairs) <= maxPairs {
		return req // within limit
	}

	// Keep only the last maxPairs tool results; drop older ones
	modified := *req
	keepFrom := pairs[len(pairs)-maxPairs]
	var compacted []types.InternalMessage

	// Keep system messages and user messages before the tool history
	for i, m := range req.Messages {
		if i < keepFrom {
			if m.Role == types.RoleSystem || m.Role == types.RoleUser {
				compacted = append(compacted, m)
			}
			// Drop old assistant(empty) and tool messages
		} else {
			compacted = append(compacted, m)
		}
	}

	modified.Messages = compacted
	log.Printf("[HelixLLM] Orchestrator: compacted %d tool pairs to %d", len(pairs), maxPairs)
	return &modified
}

// ForceActionOnToolResult checks if the last message is a tool result
// and ensures tool_choice allows the model to respond with text
// (to break loops) while still being able to call action tools.
// Returns the recommended tool_choice value.
func (o *RequestOrchestrator) RecommendToolChoice(msgs []types.InternalMessage) string {
	if len(msgs) == 0 {
		return "required"
	}

	lastRole := msgs[len(msgs)-1].Role

	// Count consecutive tool results at the end
	toolResultCount := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "tool" {
			toolResultCount++
		} else if msgs[i].Role == types.RoleAssistant && msgs[i].Content == "" {
			continue // empty assistant = tool call
		} else {
			break
		}
	}

	// After tool results, use auto to let model break the loop
	if lastRole == "tool" {
		return "auto"
	}

	// After many tool calls (4+), use auto to avoid forced looping
	if toolResultCount >= 4 {
		return "auto"
	}

	return "required"
}

// SanitizeToolResult cleans up tool result messages to prevent
// llama.cpp template errors.
func SanitizeToolResult(content interface{}) string {
	switch v := content.(type) {
	case string:
		if v == "" {
			return "(no output)"
		}
		return v
	case nil:
		return "(no output)"
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "(error reading result)"
		}
		return string(b)
	}
}
