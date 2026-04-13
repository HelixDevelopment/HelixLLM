package gateway

import (
	"encoding/json"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// ---------------------------------------------------------------------------
// stripThinkTags
// ---------------------------------------------------------------------------

func TestStripThinkTags_Empty(t *testing.T) {
	if got := stripThinkTags(""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestStripThinkTags_NoTags(t *testing.T) {
	in := "plain content"
	if got := stripThinkTags(in); got != in {
		t.Errorf("got %q, want %q", got, in)
	}
}

func TestStripThinkTags_SingleBlock(t *testing.T) {
	in := "<think>\nsome reasoning\n</think>\n\nhello"
	want := "hello"
	if got := stripThinkTags(in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripThinkTags_EmptyBlock(t *testing.T) {
	in := "<think>\n</think>\n\n<tool_call>{}</tool_call>"
	want := "<tool_call>{}</tool_call>"
	if got := stripThinkTags(in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripThinkTags_MultipleBlocks(t *testing.T) {
	in := "<think>first</think> middle <think>second</think> end"
	want := "middle  end" // two spaces where blocks were
	// The function just removes the blocks; extra internal whitespace is kept.
	got := stripThinkTags(in)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripThinkTags_UnclosedTag(t *testing.T) {
	in := "before<think>unclosed content"
	want := "before"
	if got := stripThinkTags(in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// parseQwen3ToolCall
// ---------------------------------------------------------------------------

func TestParseQwen3ToolCall_BasicObject(t *testing.T) {
	content := `<think>
</think>

<tool_call>
{"name": "edit", "arguments": {"filePath": "main.go", "old": "foo", "new": "bar"}}
</tool_call>`

	tc := parseQwen3ToolCall(content)
	if tc == nil {
		t.Fatal("expected non-nil tool call")
	}
	if tc.Function.Name != "edit" {
		t.Errorf("name = %q, want edit", tc.Function.Name)
	}
	if tc.Type != "function" {
		t.Errorf("type = %q, want function", tc.Type)
	}
	// Arguments must be a valid JSON object string.
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Errorf("arguments not valid JSON: %v — got %q", err, tc.Function.Arguments)
	}
	if args["filePath"] != "main.go" {
		t.Errorf("filePath = %v, want main.go", args["filePath"])
	}
}

func TestParseQwen3ToolCall_ArgumentsAsString(t *testing.T) {
	// Some models serialise arguments as a JSON string rather than an object.
	argsStr := `{"command":"ls -la"}`
	// Properly JSON-encode the payload so "arguments" is a quoted string.
	inner, _ := json.Marshal(map[string]interface{}{
		"name":      "bash",
		"arguments": argsStr,
	})
	content := "<tool_call>" + string(inner) + "</tool_call>"

	tc := parseQwen3ToolCall(content)
	if tc == nil {
		t.Fatal("expected non-nil tool call")
	}
	if tc.Function.Name != "bash" {
		t.Errorf("name = %q, want bash", tc.Function.Name)
	}
	// Arguments should be the unquoted inner string.
	if tc.Function.Arguments != argsStr {
		t.Errorf("arguments = %q, want %q", tc.Function.Arguments, argsStr)
	}
}

func TestParseQwen3ToolCall_MissingName(t *testing.T) {
	content := `<tool_call>{"arguments":{"a":"b"}}</tool_call>`
	if tc := parseQwen3ToolCall(content); tc != nil {
		t.Errorf("expected nil for missing name, got %+v", tc)
	}
}

func TestParseQwen3ToolCall_InvalidJSON(t *testing.T) {
	content := `<tool_call>not json</tool_call>`
	if tc := parseQwen3ToolCall(content); tc != nil {
		t.Errorf("expected nil for invalid JSON, got %+v", tc)
	}
}

func TestParseQwen3ToolCall_MissingTags(t *testing.T) {
	if tc := parseQwen3ToolCall("no tags here"); tc != nil {
		t.Errorf("expected nil, got %+v", tc)
	}
}

func TestParseQwen3ToolCall_IDPrefix(t *testing.T) {
	content := `<tool_call>{"name":"read","arguments":{"filePath":"a.go"}}</tool_call>`
	tc := parseQwen3ToolCall(content)
	if tc == nil {
		t.Fatal("expected non-nil")
	}
	if tc.ID != "call_read" {
		t.Errorf("ID = %q, want call_read", tc.ID)
	}
}

// ---------------------------------------------------------------------------
// parseGatewayXMLFunctionCall (Qwen2.5 / llama.cpp format)
// ---------------------------------------------------------------------------

func TestParseGatewayXMLFunctionCall_Basic(t *testing.T) {
	content := `<function><name>bash</name><arguments>{"command":"ls"}</arguments></function>`
	tc := parseGatewayXMLFunctionCall(content)
	if tc == nil {
		t.Fatal("expected non-nil")
	}
	if tc.Function.Name != "bash" {
		t.Errorf("name = %q, want bash", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"command":"ls"}` {
		t.Errorf("arguments = %q", tc.Function.Arguments)
	}
}

func TestParseGatewayXMLFunctionCall_NoArguments(t *testing.T) {
	content := `<function><name>list_dir</name></function>`
	tc := parseGatewayXMLFunctionCall(content)
	if tc == nil {
		t.Fatal("expected non-nil even without arguments")
	}
	if tc.Function.Arguments != "{}" {
		t.Errorf("arguments = %q, want {}", tc.Function.Arguments)
	}
}

func TestParseGatewayXMLFunctionCall_MissingTags(t *testing.T) {
	if tc := parseGatewayXMLFunctionCall("no xml"); tc != nil {
		t.Errorf("expected nil, got %+v", tc)
	}
}

func TestParseGatewayXMLFunctionCall_EmptyName(t *testing.T) {
	content := `<function><name></name><arguments>{"a":"b"}</arguments></function>`
	if tc := parseGatewayXMLFunctionCall(content); tc != nil {
		t.Errorf("expected nil for empty name, got %+v", tc)
	}
}

// ---------------------------------------------------------------------------
// parseGatewayJSONInContent
// ---------------------------------------------------------------------------

func TestParseGatewayJSONInContent_Bare(t *testing.T) {
	content := `{"name":"glob","arguments":{"pattern":"*.go"}}`
	tc := parseGatewayJSONInContent(content)
	if tc == nil {
		t.Fatal("expected non-nil")
	}
	if tc.Function.Name != "glob" {
		t.Errorf("name = %q, want glob", tc.Function.Name)
	}
}

func TestParseGatewayJSONInContent_MarkdownFence(t *testing.T) {
	content := "```json\n{\"name\":\"read\",\"arguments\":{\"filePath\":\"x.go\"}}\n```"
	tc := parseGatewayJSONInContent(content)
	if tc == nil {
		t.Fatal("expected non-nil for fenced JSON")
	}
	if tc.Function.Name != "read" {
		t.Errorf("name = %q, want read", tc.Function.Name)
	}
}

func TestParseGatewayJSONInContent_MissingName(t *testing.T) {
	content := `{"arguments":{"a":"b"}}`
	if tc := parseGatewayJSONInContent(content); tc != nil {
		t.Errorf("expected nil for missing name, got %+v", tc)
	}
}

func TestParseGatewayJSONInContent_InvalidJSON(t *testing.T) {
	content := `not json at all`
	if tc := parseGatewayJSONInContent(content); tc != nil {
		t.Errorf("expected nil, got %+v", tc)
	}
}

// ---------------------------------------------------------------------------
// NormalizeToolCalls — end-to-end
// ---------------------------------------------------------------------------

func TestNormalizeToolCalls_Nil(t *testing.T) {
	// Must not panic.
	NormalizeToolCalls(nil)
}

func TestNormalizeToolCalls_AlreadyNative(t *testing.T) {
	resp := &types.InternalChatResponse{
		Message: types.InternalMessage{
			Role:    types.RoleAssistant,
			Content: "",
			ToolCalls: []types.InternalToolCall{
				{
					ID:   "call_existing",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "bash", Arguments: `{"command":"ls"}`},
				},
			},
		},
		FinishReason: "tool_calls",
	}

	NormalizeToolCalls(resp)

	// Nothing should change.
	if len(resp.Message.ToolCalls) != 1 {
		t.Errorf("tool_calls count = %d, want 1", len(resp.Message.ToolCalls))
	}
	if resp.Message.ToolCalls[0].ID != "call_existing" {
		t.Errorf("ID changed unexpectedly")
	}
}

func TestNormalizeToolCalls_Qwen3XMLWithThink(t *testing.T) {
	// Exact format observed from Qwen3-32B-TEE on Chutes.
	resp := &types.InternalChatResponse{
		Message: types.InternalMessage{
			Role: types.RoleAssistant,
			Content: `<think>
</think>

<tool_call>
{"name": "edit", "arguments": {"filePath": "main.go", "old": "foo", "new": "bar"}}
</tool_call>`,
		},
		FinishReason: "stop",
	}

	NormalizeToolCalls(resp)

	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls count = %d, want 1", len(resp.Message.ToolCalls))
	}
	tc := resp.Message.ToolCalls[0]
	if tc.Function.Name != "edit" {
		t.Errorf("name = %q, want edit", tc.Function.Name)
	}
	if resp.Message.Content != "" {
		t.Errorf("content should be cleared, got %q", resp.Message.Content)
	}
	if resp.FinishReason != "tool_calls" {
		t.Errorf("finish_reason = %q, want tool_calls", resp.FinishReason)
	}
	// Arguments must be valid JSON.
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Errorf("arguments not valid JSON: %v", err)
	}
}

func TestNormalizeToolCalls_Qwen25XML(t *testing.T) {
	resp := &types.InternalChatResponse{
		Message: types.InternalMessage{
			Role:    types.RoleAssistant,
			Content: `<function><name>bash</name><arguments>{"command":"pwd"}</arguments></function>`,
		},
		FinishReason: "stop",
	}

	NormalizeToolCalls(resp)

	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls count = %d, want 1", len(resp.Message.ToolCalls))
	}
	if resp.Message.ToolCalls[0].Function.Name != "bash" {
		t.Errorf("name = %q, want bash", resp.Message.ToolCalls[0].Function.Name)
	}
	if resp.FinishReason != "tool_calls" {
		t.Errorf("finish_reason = %q, want tool_calls", resp.FinishReason)
	}
}

func TestNormalizeToolCalls_JSONInContent(t *testing.T) {
	resp := &types.InternalChatResponse{
		Message: types.InternalMessage{
			Role:    types.RoleAssistant,
			Content: `{"name":"glob","arguments":{"pattern":"*.go"}}`,
		},
		FinishReason: "stop",
	}

	NormalizeToolCalls(resp)

	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls count = %d, want 1", len(resp.Message.ToolCalls))
	}
	if resp.Message.ToolCalls[0].Function.Name != "glob" {
		t.Errorf("name = %q, want glob", resp.Message.ToolCalls[0].Function.Name)
	}
}

func TestNormalizeToolCalls_PlainText(t *testing.T) {
	// Plain text responses must be left unchanged.
	resp := &types.InternalChatResponse{
		Message: types.InternalMessage{
			Role:    types.RoleAssistant,
			Content: "Here is a summary of the codebase.",
		},
		FinishReason: "stop",
	}

	NormalizeToolCalls(resp)

	if len(resp.Message.ToolCalls) != 0 {
		t.Errorf("expected no tool_calls, got %d", len(resp.Message.ToolCalls))
	}
	if resp.Message.Content != "Here is a summary of the codebase." {
		t.Errorf("content changed unexpectedly: %q", resp.Message.Content)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("finish_reason changed to %q", resp.FinishReason)
	}
}

func TestNormalizeToolCalls_ThinkOnlyStripped(t *testing.T) {
	// Response that has a <think> block but no tool call — content should be
	// stripped of the think block but no tool_calls should be created.
	resp := &types.InternalChatResponse{
		Message: types.InternalMessage{
			Role:    types.RoleAssistant,
			Content: "<think>Let me reason...</think>\n\nHere is my answer.",
		},
		FinishReason: "stop",
	}

	NormalizeToolCalls(resp)

	if len(resp.Message.ToolCalls) != 0 {
		t.Errorf("expected no tool_calls, got %d", len(resp.Message.ToolCalls))
	}
	if resp.Message.Content != "Here is my answer." {
		t.Errorf("content = %q, want %q", resp.Message.Content, "Here is my answer.")
	}
}

// ---------------------------------------------------------------------------
// NormalizeStreamChunk
// ---------------------------------------------------------------------------

func TestNormalizeStreamChunk_Nil(t *testing.T) {
	// Must not panic.
	NormalizeStreamChunk(nil)
}

func TestNormalizeStreamChunk_StripsThink(t *testing.T) {
	chunk := &types.StreamChunk{
		Content: "<think>reasoning</think>answer",
	}
	NormalizeStreamChunk(chunk)
	if chunk.Content != "answer" {
		t.Errorf("content = %q, want answer", chunk.Content)
	}
}

func TestNormalizeStreamChunk_NoTags(t *testing.T) {
	chunk := &types.StreamChunk{Content: "plain"}
	NormalizeStreamChunk(chunk)
	if chunk.Content != "plain" {
		t.Errorf("content changed to %q", chunk.Content)
	}
}
