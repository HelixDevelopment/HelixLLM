package gateway

// Package-internal unit tests for the unexported anthropicToInternal /
// internalToAnthropic / anthropicToolChoiceToInternal conversion functions.
//
// ANTHROPIC-WIRE-DROPS-TOOLS regression guard (§11.4.135, V&V wave-2,
// 2026-07-11): the Anthropic /v1/messages facade declared tools/tool_choice
// in its schema (api.MessageRequest.Tools / ToolChoice) but silently
// dropped them on the way into the internal request, and silently dropped
// resp.Message.ToolCalls on the way back out — the OpenAI wire did tools
// end-to-end, the Anthropic wire did not. These tests exercise the exact
// conversion functions the fix touched. The full HTTP-facade round trip
// lives in anthropic_test.go (package gateway_test); the live-coder proof
// lives in anthropic_tools_live_test.go.

import (
	"encoding/json"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

func TestAnthropicToInternal_ToolsAndToolChoice_PassThrough(t *testing.T) {
	temp := 0.2
	req := &api.MessageRequest{
		Model:       "claude-opus-4-5",
		Messages:    []api.AnthropicMessage{{Role: "user", Content: "What's the weather in Paris?"}},
		MaxTokens:   512,
		Temperature: &temp,
		Tools: []api.AnthropicTool{
			{
				Name:        "get_weather",
				Description: "Get the current weather for a city",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"city": map[string]interface{}{"type": "string"},
					},
					"required": []interface{}{"city"},
				},
			},
		},
		ToolChoice: map[string]interface{}{"type": "any"},
	}

	internal := anthropicToInternal(req)

	if len(internal.Tools) != 1 {
		t.Fatalf("Tools dropped: got %d internal tools, want 1 (ANTHROPIC-WIRE-DROPS-TOOLS regression)", len(internal.Tools))
	}
	if internal.Tools[0].Type != "function" {
		t.Errorf("internal.Tools[0].Type = %q, want %q", internal.Tools[0].Type, "function")
	}
	fn, ok := internal.Tools[0].Function.(map[string]interface{})
	if !ok {
		t.Fatalf("internal.Tools[0].Function is %T, want map[string]interface{}", internal.Tools[0].Function)
	}
	if fn["name"] != "get_weather" {
		t.Errorf("internal.Tools[0].Function[name] = %v, want get_weather", fn["name"])
	}

	if internal.ToolChoice == nil {
		t.Fatal("ToolChoice dropped: internal.ToolChoice is nil, want the converted Anthropic any->required choice")
	}
	if internal.ToolChoice != "required" {
		t.Errorf("internal.ToolChoice = %v, want %q (Anthropic {type:any} -> OpenAI required)", internal.ToolChoice, "required")
	}
}

func TestAnthropicToInternal_NoTools_NilPassthrough(t *testing.T) {
	// Regression guard: a request with no Tools/ToolChoice must produce
	// nil Tools/ToolChoice on the internal request, exactly as before this
	// fix — no spurious empty-slice/empty-string artefacts.
	req := &api.MessageRequest{
		Model:     "claude-opus-4-5",
		Messages:  []api.AnthropicMessage{{Role: "user", Content: "Hello"}},
		MaxTokens: 64,
	}
	internal := anthropicToInternal(req)
	if internal.Tools != nil {
		t.Errorf("internal.Tools = %v, want nil for a tool-less request", internal.Tools)
	}
	if internal.ToolChoice != nil {
		t.Errorf("internal.ToolChoice = %v, want nil for a tool-less request", internal.ToolChoice)
	}
}

func TestAnthropicToolChoiceToInternal_AllVariants(t *testing.T) {
	cases := []struct {
		name  string
		input interface{}
		want  interface{}
	}{
		{"auto", map[string]interface{}{"type": "auto"}, "auto"},
		{"any->required", map[string]interface{}{"type": "any"}, "required"},
		{"none", map[string]interface{}{"type": "none"}, "none"},
		{"tool->function", map[string]interface{}{"type": "tool", "name": "get_weather"},
			map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "get_weather"}}},
		{"unknown-type-defaults-auto", map[string]interface{}{"type": "something_else"}, "auto"},
		{"already-openai-shaped-string", "required", "required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := anthropicToolChoiceToInternal(tc.input)
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(tc.want)
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("got %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestAnthropicToInternal_ToolResultContentBlock_MapsToToolRole(t *testing.T) {
	req := &api.MessageRequest{
		Model: "claude-opus-4-5",
		Messages: []api.AnthropicMessage{
			{Role: "user", Content: "What's the weather?"},
			{Role: "assistant", Content: []interface{}{
				map[string]interface{}{
					"type": "tool_use", "id": "toolu_01", "name": "get_weather",
					"input": map[string]interface{}{"city": "Paris"},
				},
			}},
			{Role: "user", Content: []interface{}{
				map[string]interface{}{
					"type": "tool_result", "tool_use_id": "toolu_01", "content": "18C, cloudy",
				},
			}},
		},
		MaxTokens: 512,
	}

	internal := anthropicToInternal(req)

	var assistantMsg, toolMsg *types.InternalMessage
	for i := range internal.Messages {
		m := &internal.Messages[i]
		if m.Role == types.RoleAssistant {
			assistantMsg = m
		}
		if m.Role == types.RoleTool {
			toolMsg = m
		}
	}
	if assistantMsg == nil || len(assistantMsg.ToolCalls) != 1 {
		t.Fatalf("expected one assistant message carrying 1 ToolCall, got %+v", assistantMsg)
	}
	if assistantMsg.ToolCalls[0].ID != "toolu_01" || assistantMsg.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("unexpected tool_use conversion: %+v", assistantMsg.ToolCalls[0])
	}
	if toolMsg == nil {
		t.Fatal("expected a RoleTool message from the tool_result content block, found none")
	}
	if toolMsg.ToolCallID != "toolu_01" {
		t.Errorf("toolMsg.ToolCallID = %q, want toolu_01", toolMsg.ToolCallID)
	}
	if toolMsg.Content != "18C, cloudy" {
		t.Errorf("toolMsg.Content = %q, want %q", toolMsg.Content, "18C, cloudy")
	}
}

func TestAnthropicToInternal_PlainStringContent_Unchanged(t *testing.T) {
	// Regression guard: the common case (bare string message content) must
	// still produce exactly one InternalMessage per AnthropicMessage.
	req := &api.MessageRequest{
		Model:     "claude-opus-4-5",
		System:    "You are helpful.",
		Messages:  []api.AnthropicMessage{{Role: "user", Content: "Hello"}},
		MaxTokens: 64,
	}
	internal := anthropicToInternal(req)
	if len(internal.Messages) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d: %+v", len(internal.Messages), internal.Messages)
	}
	if internal.Messages[0].Role != types.RoleSystem || internal.Messages[0].Content != "You are helpful." {
		t.Errorf("unexpected system message: %+v", internal.Messages[0])
	}
	if internal.Messages[1].Role != types.RoleUser || internal.Messages[1].Content != "Hello" {
		t.Errorf("unexpected user message: %+v", internal.Messages[1])
	}
}

func TestInternalToAnthropic_ToolUse_EmitsToolUseBlockAndStopReason(t *testing.T) {
	resp := &types.InternalChatResponse{
		ID:    "resp-1",
		Model: "claude-opus-4-5",
		Message: types.InternalMessage{
			Role: types.RoleAssistant,
			ToolCalls: []types.InternalToolCall{
				{
					ID:   "toolu_01",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "get_weather", Arguments: `{"city":"Paris"}`},
				},
			},
		},
		FinishReason: "tool_calls",
	}

	msg := internalToAnthropic(resp, "fallback-id", "fallback-model")

	if msg.StopReason == nil || *msg.StopReason != "tool_use" {
		t.Fatalf("StopReason = %v, want tool_use (ANTHROPIC-WIRE-DROPS-TOOLS regression: tool calls must surface as stop_reason=tool_use)", msg.StopReason)
	}
	var toolUseBlock *api.ContentBlock
	for i := range msg.Content {
		if msg.Content[i].Type == "tool_use" {
			toolUseBlock = &msg.Content[i]
		}
	}
	if toolUseBlock == nil {
		t.Fatalf("no tool_use content block found in response Content=%+v (ANTHROPIC-WIRE-DROPS-TOOLS regression)", msg.Content)
	}
	if toolUseBlock.ID != "toolu_01" || toolUseBlock.Name != "get_weather" {
		t.Errorf("unexpected tool_use block: %+v", toolUseBlock)
	}
	input, ok := toolUseBlock.Input.(map[string]interface{})
	if !ok {
		t.Fatalf("tool_use block Input is %T, want map[string]interface{}", toolUseBlock.Input)
	}
	if input["city"] != "Paris" {
		t.Errorf("tool_use block Input[city] = %v, want Paris", input["city"])
	}
}

func TestInternalToAnthropic_PlainText_Unchanged(t *testing.T) {
	// Regression guard: a plain-text response (no tool calls) must still
	// produce exactly one text content block and stop_reason=end_turn,
	// byte-identical to pre-fix behaviour.
	resp := &types.InternalChatResponse{
		Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "Hello there"},
		FinishReason: "stop",
	}
	msg := internalToAnthropic(resp, "id", "model")
	if len(msg.Content) != 1 || msg.Content[0].Type != "text" || msg.Content[0].Text != "Hello there" {
		t.Errorf("unexpected content blocks for plain-text response: %+v", msg.Content)
	}
	if msg.StopReason == nil || *msg.StopReason != "end_turn" {
		t.Errorf("StopReason = %v, want end_turn", msg.StopReason)
	}
}

func TestInternalToAnthropic_EmptyResponse_Unchanged(t *testing.T) {
	// Regression guard: an empty response (no content, no tool calls) must
	// still produce exactly one blank text block, matching the pre-fix
	// unconditional {"type":"text","text":resp.Message.Content} shape.
	resp := &types.InternalChatResponse{Message: types.InternalMessage{Role: types.RoleAssistant}}
	msg := internalToAnthropic(resp, "id", "model")
	if len(msg.Content) != 1 || msg.Content[0].Type != "text" || msg.Content[0].Text != "" {
		t.Errorf("unexpected content blocks for empty response: %+v", msg.Content)
	}
}

func TestAnthropicStopReason_MapsAllKnownValues(t *testing.T) {
	cases := []struct {
		finishReason string
		hasToolCalls bool
		want         string
	}{
		{"", false, "end_turn"},
		{"stop", false, "end_turn"},
		{"length", false, "max_tokens"},
		{"tool_calls", false, "tool_use"},
		{"stop", true, "tool_use"},
		{"end_turn", false, "end_turn"},
		{"stop_sequence", false, "stop_sequence"},
		{"anything_else_unrecognised", false, "end_turn"},
	}
	for _, tc := range cases {
		got := anthropicStopReason(tc.finishReason, tc.hasToolCalls)
		if got != tc.want {
			t.Errorf("anthropicStopReason(%q, %v) = %q, want %q", tc.finishReason, tc.hasToolCalls, got, tc.want)
		}
	}
}
