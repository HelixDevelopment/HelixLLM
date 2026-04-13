package gateway_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrchestrator_EnhanceRequest_NilRequest(t *testing.T) {
	o := gateway.NewRequestOrchestrator()
	assert.Nil(t, o.EnhanceRequest(nil))
}

func TestOrchestrator_EnhanceRequest_NoToolResults(t *testing.T) {
	o := gateway.NewRequestOrchestrator()
	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleSystem, Content: "system"},
			{Role: types.RoleUser, Content: "hello"},
		},
	}
	result := o.EnhanceRequest(req)
	assert.Equal(t, 2, len(result.Messages), "no enhancement without tool results")
}

func TestOrchestrator_EnhanceRequest_OneToolCall(t *testing.T) {
	o := gateway.NewRequestOrchestrator()
	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "read file"},
			{Role: types.RoleAssistant, Content: ""},
			{Role: "tool", Content: "file content"},
		},
	}
	result := o.EnhanceRequest(req)
	// 1 tool call doesn't trigger the hint (needs 2+)
	assert.Equal(t, 3, len(result.Messages))
}

func TestOrchestrator_EnhanceRequest_TwoToolCalls_InjectsHint(t *testing.T) {
	o := gateway.NewRequestOrchestrator()
	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "update file"},
			{Role: types.RoleAssistant, Content: ""},
			{Role: "tool", Content: "content1"},
			{Role: types.RoleAssistant, Content: ""},
			{Role: "tool", Content: "content2"},
			{Role: types.RoleUser, Content: "now write it"},
		},
	}
	result := o.EnhanceRequest(req)
	// Should inject a system hint before the last user message
	assert.Greater(t, len(result.Messages), len(req.Messages))
	// Find the hint
	found := false
	for _, m := range result.Messages {
		if m.Role == types.RoleSystem && m.Content != "system" {
			found = true
			assert.Contains(t, m.Content, "enough context")
			assert.Contains(t, m.Content, "Write or Edit NOW")
		}
	}
	assert.True(t, found, "should inject action hint")
}

func TestOrchestrator_CompactToolHistory_NilRequest(t *testing.T) {
	o := gateway.NewRequestOrchestrator()
	assert.Nil(t, o.CompactToolHistory(nil, 2))
}

func TestOrchestrator_CompactToolHistory_WithinLimit(t *testing.T) {
	o := gateway.NewRequestOrchestrator()
	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "do something"},
			{Role: types.RoleAssistant, Content: ""},
			{Role: "tool", Content: "result"},
		},
	}
	result := o.CompactToolHistory(req, 2)
	assert.Equal(t, 3, len(result.Messages), "within limit — no compaction")
}

func TestOrchestrator_CompactToolHistory_DropOldPairs(t *testing.T) {
	o := gateway.NewRequestOrchestrator()
	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleSystem, Content: "system"},
			{Role: types.RoleUser, Content: "do something"},
			{Role: types.RoleAssistant, Content: ""},
			{Role: "tool", Content: "old result 1"},
			{Role: types.RoleAssistant, Content: ""},
			{Role: "tool", Content: "old result 2"},
			{Role: types.RoleAssistant, Content: ""},
			{Role: "tool", Content: "recent result"},
			{Role: types.RoleUser, Content: "now write"},
		},
	}
	result := o.CompactToolHistory(req, 1) // keep only last 1 tool pair
	// Should keep: system, user, recent tool pair, last user
	assert.Less(t, len(result.Messages), len(req.Messages))
	// System and user messages preserved
	hasSystem := false
	hasUser := false
	for _, m := range result.Messages {
		if m.Role == types.RoleSystem {
			hasSystem = true
		}
		if m.Role == types.RoleUser {
			hasUser = true
		}
	}
	assert.True(t, hasSystem)
	assert.True(t, hasUser)
}

func TestOrchestrator_RecommendToolChoice_EmptyMessages(t *testing.T) {
	o := gateway.NewRequestOrchestrator()
	assert.Equal(t, "required", o.RecommendToolChoice(nil))
}

func TestOrchestrator_RecommendToolChoice_UserMessage(t *testing.T) {
	o := gateway.NewRequestOrchestrator()
	msgs := []types.InternalMessage{
		{Role: types.RoleUser, Content: "hello"},
	}
	assert.Equal(t, "required", o.RecommendToolChoice(msgs))
}

func TestOrchestrator_RecommendToolChoice_AfterToolResult(t *testing.T) {
	o := gateway.NewRequestOrchestrator()
	msgs := []types.InternalMessage{
		{Role: types.RoleUser, Content: "read file"},
		{Role: types.RoleAssistant, Content: ""},
		{Role: "tool", Content: "file content"},
	}
	assert.Equal(t, "auto", o.RecommendToolChoice(msgs))
}

func TestOrchestrator_RecommendToolChoice_ManyToolCalls(t *testing.T) {
	o := gateway.NewRequestOrchestrator()
	msgs := []types.InternalMessage{
		{Role: types.RoleUser, Content: "do stuff"},
		{Role: types.RoleAssistant, Content: ""},
		{Role: "tool", Content: "r1"},
		{Role: types.RoleAssistant, Content: ""},
		{Role: "tool", Content: "r2"},
		{Role: types.RoleAssistant, Content: ""},
		{Role: "tool", Content: "r3"},
		{Role: types.RoleAssistant, Content: ""},
		{Role: "tool", Content: "r4"},
	}
	assert.Equal(t, "auto", o.RecommendToolChoice(msgs))
}

func TestSanitizeToolResult_EmptyString(t *testing.T) {
	assert.Equal(t, "(no output)", gateway.SanitizeToolResult(""))
}

func TestSanitizeToolResult_Nil(t *testing.T) {
	assert.Equal(t, "(no output)", gateway.SanitizeToolResult(nil))
}

func TestSanitizeToolResult_ValidString(t *testing.T) {
	assert.Equal(t, "hello", gateway.SanitizeToolResult("hello"))
}

func TestSanitizeToolResult_NonString(t *testing.T) {
	result := gateway.SanitizeToolResult(42)
	assert.Equal(t, "42", result)
}

func TestOrchestrator_FullWorkflow(t *testing.T) {
	o := gateway.NewRequestOrchestrator()
	// Simulate: user asks /init, model has already read file
	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleSystem, Content: "system prompt"},
			{Role: types.RoleUser, Content: "Create or update AGENTS.md"},
			{Role: types.RoleAssistant, Content: ""},
			{Role: "tool", Content: "existing AGENTS.md content here"},
			{Role: types.RoleAssistant, Content: ""},
			{Role: "tool", Content: "more file listings"},
			{Role: types.RoleUser, Content: "now write the updated version"},
		},
	}

	// Compact
	compacted := o.CompactToolHistory(req, 2)
	require.NotNil(t, compacted)

	// Enhance
	enhanced := o.EnhanceRequest(compacted)
	require.NotNil(t, enhanced)

	// Should have action hint
	hasHint := false
	for _, m := range enhanced.Messages {
		if m.Role == types.RoleSystem && m.Content != "system prompt" {
			hasHint = true
		}
	}
	assert.True(t, hasHint, "should have action hint after 2+ tool calls")

	// Tool choice should be required (last msg is user)
	choice := o.RecommendToolChoice(enhanced.Messages)
	assert.Equal(t, "required", choice)
}
