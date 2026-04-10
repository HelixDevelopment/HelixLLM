package brain

import (
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain/models"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComplexityAnalyzer_SimpleToolCall(t *testing.T) {
	a := NewComplexityAnalyzer()
	require.NotNil(t, a)

	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "read file main.go"},
		},
		Tools: []types.InternalTool{
			{Type: "function"},
		},
	}

	result := a.Analyze(req)

	assert.Equal(t, ComplexitySimple, result.Level)
	assert.Equal(t, models.TierFast, result.TargetTier)
	assert.False(t, result.ModelOverride)
	assert.GreaterOrEqual(t, result.Score, 0)
	assert.LessOrEqual(t, result.Score, 2)
}

func TestComplexityAnalyzer_ComplexMultiTool(t *testing.T) {
	a := NewComplexityAnalyzer()
	require.NotNil(t, a)

	// Build a long message with complexity keywords
	longMsg := strings.Repeat("analyze refactor compare ", 100) // > 2000 chars, has keywords

	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: longMsg},
		},
		Tools: []types.InternalTool{
			{Type: "function"},
			{Type: "function"},
			{Type: "function"},
			{Type: "function"},
			{Type: "function"},
		},
	}

	result := a.Analyze(req)

	assert.Equal(t, ComplexityComplex, result.Level)
	assert.Equal(t, models.TierPowerful, result.TargetTier)
	assert.False(t, result.ModelOverride)
	assert.GreaterOrEqual(t, result.Score, 6)
}

func TestComplexityAnalyzer_ModerateRequest(t *testing.T) {
	a := NewComplexityAnalyzer()
	require.NotNil(t, a)

	// 600+ char message with "explain", 2 tools → +1 (tool 1-3) + +1 (500-2000 chars) + +1 (keyword) = 3 → moderate
	longMsg := "explain " + strings.Repeat("x", 600)

	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: longMsg},
		},
		Tools: []types.InternalTool{
			{Type: "function"},
			{Type: "function"},
		},
	}

	result := a.Analyze(req)

	assert.Equal(t, ComplexityModerate, result.Level)
	assert.Equal(t, models.TierBalanced, result.TargetTier)
	assert.False(t, result.ModelOverride)
	assert.GreaterOrEqual(t, result.Score, 3)
	assert.LessOrEqual(t, result.Score, 5)
}

func TestComplexityAnalyzer_ExplicitModelOverride(t *testing.T) {
	a := NewComplexityAnalyzer()
	require.NotNil(t, a)

	req := &types.InternalChatRequest{
		Model: "functionary-small-v3.2-q4_k_m",
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "hello"},
		},
	}

	result := a.Analyze(req)

	assert.True(t, result.ModelOverride)
}

func TestComplexityAnalyzer_EmptyRequest(t *testing.T) {
	a := NewComplexityAnalyzer()
	require.NotNil(t, a)

	req := &types.InternalChatRequest{}

	result := a.Analyze(req)

	assert.Equal(t, ComplexitySimple, result.Level)
	assert.Equal(t, models.TierFast, result.TargetTier)
	assert.False(t, result.ModelOverride)
	assert.Equal(t, 0, result.Score)
}

func TestComplexityAnalyzer_LongConversation(t *testing.T) {
	a := NewComplexityAnalyzer()
	require.NotNil(t, a)

	// 8 messages (> 5) → +1
	msgs := make([]types.InternalMessage, 8)
	for i := range msgs {
		role := types.RoleUser
		if i%2 == 1 {
			role = types.RoleAssistant
		}
		msgs[i] = types.InternalMessage{Role: role, Content: "short message"}
	}

	req := &types.InternalChatRequest{
		Messages: msgs,
	}

	result := a.Analyze(req)

	assert.GreaterOrEqual(t, result.Score, 1)
	assert.False(t, result.ModelOverride)
}
