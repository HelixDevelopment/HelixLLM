package integration

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/brain/models"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/hardware"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComplexity_To_Registry_Routing(t *testing.T) {
	reg := models.NewRegistry()
	cat := models.DefaultCatalog()
	for _, m := range cat.ChatModels() {
		if m.Tier == models.TierFast || m.Tier == models.TierBalanced {
			reg.Add(models.RuntimeModel{
				Definition: m,
				Status:     models.StatusLoaded,
				Downloaded: true,
			})
		}
	}

	analyzer := brain.NewComplexityAnalyzer()

	// Simple request -> fast model
	simpleReq := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "list files"},
		},
	}
	result := analyzer.Analyze(simpleReq)
	best, ok := reg.BestAvailable(result.TargetTier)
	require.True(t, ok)
	assert.Equal(t, "qwen2.5-coder-1.5b-instruct-q4_k_m", best.Definition.ID)

	// Moderate-to-complex request -> balanced model (powerful not loaded)
	// Score breakdown: 4 tools (+2), keywords "analyze" (+1), "compare" (+1),
	// "refactor" (+1) = 5 -> moderate -> TierBalanced.
	// With powerful tier unloaded, BestAvailable falls back to balanced.
	complexReq := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "analyze and refactor the authentication module, compare implementations thoroughly"},
		},
		Tools: make([]types.InternalTool, 4),
	}
	result2 := analyzer.Analyze(complexReq)
	assert.True(t, result2.Score >= 5, "expected score >= 5, got %d", result2.Score)
	best2, ok := reg.BestAvailable(result2.TargetTier)
	require.True(t, ok)
	assert.Equal(t, "qwen2.5-coder-3b-instruct-q4_k_m", best2.Definition.ID)
}

func TestPresetGeneration_MatchesProfile(t *testing.T) {
	profile := &hardware.HardwareProfile{
		GPU:           hardware.GPUProfile{Available: true, VRAMTotal: 6 * 1024 * 1024 * 1024},
		CPU:           hardware.CPUProfile{Cores: 16},
		PresetProfile: "consumer_6gb",
	}
	cat := models.DefaultCatalog()
	filtered := cat.FilterByVRAM(6 * 1024 * 1024 * 1024)
	ini, err := models.GeneratePresets(filtered, profile)
	require.NoError(t, err)
	assert.Contains(t, ini, "n-gpu-layers = -1")
	assert.Contains(t, ini, "ctx-size = 4096")
	assert.Contains(t, ini, "flash-attn = on")
	assert.Contains(t, ini, "chat-template = jinja")
	assert.Contains(t, ini, "embedding = on")
}

func TestCatalog_VRAMFiltering_Consistency(t *testing.T) {
	cat := models.DefaultCatalog()

	// 1GB should only include embedding (90MB)
	tiny := cat.FilterByVRAM(1 * 1024 * 1024 * 1024)
	for _, m := range tiny {
		assert.LessOrEqual(t, m.VRAMRequired, int64(1*1024*1024*1024))
	}

	// 6GB should include everything except 8B
	medium := cat.FilterByVRAM(6 * 1024 * 1024 * 1024)
	assert.GreaterOrEqual(t, len(medium), 3)
}
