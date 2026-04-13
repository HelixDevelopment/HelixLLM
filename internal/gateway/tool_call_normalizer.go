package gateway

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// NormalizeToolCalls post-processes a response to convert various tool-call
// formats (Qwen3 XML, Qwen2.5 XML, JSON-in-content) into standard OpenAI
// tool_calls. It also strips <think>...</think> blocks from Qwen3 thinking mode.
//
// This runs at the Gateway layer so it applies to ALL providers that flow
// through the FallbackChain (Chutes, OpenRouter, HuggingFace, etc.), not only
// the OpenAI provider which has its own postProcess.
func NormalizeToolCalls(resp *types.InternalChatResponse) {
	if resp == nil {
		return
	}

	// Strip <think>...</think> blocks first — Qwen3 in extended-thinking mode
	// wraps its chain-of-thought in these tags before the actual response.
	content := stripThinkTags(resp.Message.Content)
	resp.Message.Content = content

	// If already has native tool_calls from the provider, nothing to normalize.
	if len(resp.Message.ToolCalls) > 0 {
		return
	}

	// Try Qwen3 <tool_call>{"name":"...","arguments":{...}}</tool_call> format.
	if strings.Contains(content, "<tool_call>") {
		if tc := parseQwen3ToolCall(content); tc != nil {
			resp.Message.ToolCalls = append(resp.Message.ToolCalls, *tc)
			resp.Message.Content = ""
			resp.FinishReason = "tool_calls"
			return
		}
	}

	// Try Qwen2.5 / llama.cpp <function>...</function> format.
	if strings.Contains(content, "<function>") {
		if tc := parseGatewayXMLFunctionCall(content); tc != nil {
			resp.Message.ToolCalls = append(resp.Message.ToolCalls, *tc)
			resp.Message.Content = ""
			resp.FinishReason = "tool_calls"
			return
		}
	}

	// Try JSON-in-content format: {"name": "...", "arguments": {...}}
	if strings.Contains(content, `"name"`) && strings.Contains(content, `"arguments"`) {
		if tc := parseGatewayJSONInContent(content); tc != nil {
			resp.Message.ToolCalls = append(resp.Message.ToolCalls, *tc)
			resp.Message.Content = ""
			resp.FinishReason = "tool_calls"
		}
	}
}

// NormalizeStreamChunk strips <think>...</think> tags from a streaming chunk's
// content. Full tool-call normalization is not applied to individual chunks
// because the Gateway forces non-streaming for all tool-capable requests
// (see "Forcing non-stream for tool-capable request" log line in openai.go).
func NormalizeStreamChunk(chunk *types.StreamChunk) {
	if chunk == nil {
		return
	}
	chunk.Content = stripThinkTags(chunk.Content)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// stripThinkTags removes all <think>...</think> blocks from content and
// trims surrounding whitespace. Handles multiple blocks in sequence.
func stripThinkTags(content string) string {
	for {
		start := strings.Index(content, "<think>")
		if start < 0 {
			break
		}
		end := strings.Index(content, "</think>")
		if end < 0 {
			// Unclosed tag — strip everything from <think> to end of string.
			content = strings.TrimSpace(content[:start])
			break
		}
		content = content[:start] + content[end+len("</think>"):]
	}
	return strings.TrimSpace(content)
}

// parseQwen3ToolCall parses Qwen3's XML tool-call format:
//
//	<tool_call>{"name":"edit","arguments":{"filePath":"...","changes":{...}}}</tool_call>
//
// The JSON inside may have "arguments" as a JSON object or as a JSON string.
func parseQwen3ToolCall(content string) *types.InternalToolCall {
	start := strings.Index(content, "<tool_call>")
	end := strings.Index(content, "</tool_call>")
	if start < 0 || end < 0 || end <= start {
		return nil
	}

	jsonStr := strings.TrimSpace(content[start+len("<tool_call>") : end])
	if jsonStr == "" {
		return nil
	}

	// Decode into a struct that accepts arguments as raw JSON so we can
	// handle both object and string forms without a second unmarshal step.
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &call); err != nil || call.Name == "" {
		return nil
	}

	args := deriveArgsString(call.Arguments)

	return &types.InternalToolCall{
		ID:   fmt.Sprintf("call_%s", call.Name),
		Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{
			Name:      call.Name,
			Arguments: args,
		},
	}
}

// deriveArgsString converts a raw JSON value into the string that should be
// placed in InternalToolCall.Function.Arguments.
//
//   - JSON object → marshal back to compact JSON string
//   - JSON string → unquote to get the inner string value
//   - Anything else → use raw bytes as-is
func deriveArgsString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	// String value — unquote it.
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
	}
	// Object or array — return compact JSON.
	var obj interface{}
	if json.Unmarshal(raw, &obj) == nil {
		if b, err := json.Marshal(obj); err == nil {
			return string(b)
		}
	}
	return string(raw)
}

// parseGatewayXMLFunctionCall parses Qwen2.5 / llama.cpp XML format:
//
//	<function><name>tool_name</name><arguments>{"key":"value"}</arguments></function>
//
// This mirrors parseXMLToolCall in brain/openai_provider.go but lives in the
// gateway layer so it covers all providers (not just OpenAI).
func parseGatewayXMLFunctionCall(content string) *types.InternalToolCall {
	fnStart := strings.Index(content, "<function>")
	fnEnd := strings.Index(content, "</function>")
	if fnStart < 0 || fnEnd < 0 || fnEnd <= fnStart {
		return nil
	}
	inner := content[fnStart+len("<function>") : fnEnd]

	nameStart := strings.Index(inner, "<name>")
	nameEnd := strings.Index(inner, "</name>")
	if nameStart < 0 || nameEnd < 0 || nameEnd <= nameStart {
		return nil
	}
	name := strings.TrimSpace(inner[nameStart+len("<name>") : nameEnd])
	if name == "" {
		return nil
	}

	args := "{}"
	argsStart := strings.Index(inner, "<arguments>")
	argsEnd := strings.Index(inner, "</arguments>")
	if argsStart >= 0 && argsEnd > argsStart {
		args = strings.TrimSpace(inner[argsStart+len("<arguments>") : argsEnd])
	}

	return &types.InternalToolCall{
		ID:   fmt.Sprintf("call_%s", name),
		Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{
			Name:      name,
			Arguments: args,
		},
	}
}

// parseGatewayJSONInContent extracts a tool call from JSON-in-content format:
//
//	{"name": "tool_name", "arguments": {"key": "value"}}
//
// The JSON may be wrapped in markdown code fences.
// This mirrors parseJSONToolCall in brain/openai_provider.go but lives in the
// gateway layer so it covers all providers.
func parseGatewayJSONInContent(content string) *types.InternalToolCall {
	// Strip markdown fences.
	cleaned := content
	if idx := strings.Index(cleaned, "```"); idx >= 0 {
		start := idx + 3
		if nl := strings.IndexByte(cleaned[start:], '\n'); nl >= 0 {
			start += nl + 1
		}
		if end := strings.Index(cleaned[start:], "```"); end >= 0 {
			cleaned = strings.TrimSpace(cleaned[start : start+end])
		}
	}

	// Find the JSON object bounds.
	if !strings.HasPrefix(cleaned, "{") {
		if brace := strings.Index(cleaned, "{"); brace >= 0 {
			if end := strings.LastIndex(cleaned, "}"); end > brace {
				cleaned = cleaned[brace : end+1]
			}
		}
	}

	var tc struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(cleaned), &tc); err != nil || tc.Name == "" {
		return nil
	}

	args := deriveArgsString(tc.Arguments)

	return &types.InternalToolCall{
		ID:   fmt.Sprintf("call_%s", tc.Name),
		Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{
			Name:      tc.Name,
			Arguments: args,
		},
	}
}
