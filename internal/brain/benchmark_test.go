package brain

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain/models"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

func BenchmarkComplexityAnalyzer(b *testing.B) {
	analyzer := NewComplexityAnalyzer()
	req := &types.InternalChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "analyze and refactor the authentication module"},
		},
		Tools: make([]types.InternalTool, 3),
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		analyzer.Analyze(req)
	}
}

func BenchmarkRegistryBestAvailable(b *testing.B) {
	reg := models.NewRegistry()
	cat := models.DefaultCatalog()
	for _, m := range cat.Models {
		reg.Add(models.RuntimeModel{Definition: m, Status: models.StatusLoaded})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reg.BestAvailable(models.TierFast)
	}
}
