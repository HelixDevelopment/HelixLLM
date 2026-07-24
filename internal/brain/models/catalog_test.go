package models_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultCatalog_HasSixModels(t *testing.T) {
	c := models.DefaultCatalog()
	require.NotNil(t, c)
	assert.Len(t, c.Models, 6)
}

func TestDefaultCatalog_HasFastTier(t *testing.T) {
	c := models.DefaultCatalog()
	fast := c.ByTier(models.TierFast)
	require.Len(t, fast, 1)

	m := fast[0]
	assert.Equal(t, "qwen2.5-coder-1.5b-instruct-q4_k_m", m.ID)
	assert.True(t, m.SupportsTools)
	assert.True(t, m.RequiresJinja)
	assert.Equal(t, "Apache-2.0", m.License)
	assert.Equal(t, models.TierFast, m.Tier)
	assert.False(t, m.IsEmbedding)
}

func TestDefaultCatalog_HasBalancedTier(t *testing.T) {
	c := models.DefaultCatalog()
	balanced := c.ByTier(models.TierBalanced)
	require.Len(t, balanced, 1)
	assert.Equal(t, "qwen2.5-coder-3b-instruct-q4_k_m", balanced[0].ID)
}

func TestDefaultCatalog_HasEmbeddingModel(t *testing.T) {
	c := models.DefaultCatalog()
	embeddings := c.EmbeddingModels()
	require.Len(t, embeddings, 1)

	m := embeddings[0]
	assert.Equal(t, "nomic-embed-text-v1.5-q4_k_m", m.ID)
	assert.Equal(t, 768, m.EmbeddingDims)
	assert.True(t, m.IsEmbedding)
}

func TestCatalog_FilterByVRAM(t *testing.T) {
	c := models.DefaultCatalog()
	// 3GB VRAM = 3 * 1024 * 1024 * 1024
	maxVRAM := int64(3 * 1024 * 1024 * 1024)
	filtered := c.FilterByVRAM(maxVRAM)

	// Should include 1.5B (~1GB), 3B (~2GB), embed (~90MB) but NOT 8B (~5GB)
	assert.Len(t, filtered, 3)

	ids := make(map[string]bool)
	for _, m := range filtered {
		ids[m.ID] = true
	}
	assert.True(t, ids["qwen2.5-coder-1.5b-instruct-q4_k_m"])
	assert.True(t, ids["qwen2.5-coder-3b-instruct-q4_k_m"])
	assert.True(t, ids["nomic-embed-text-v1.5-q4_k_m"])
	assert.False(t, ids["functionary-small-v3.2-q4_k_m"])
}

func TestCatalog_GetByID(t *testing.T) {
	c := models.DefaultCatalog()

	m, ok := c.Get("qwen2.5-coder-1.5b-instruct-q4_k_m")
	assert.True(t, ok)
	assert.Equal(t, "qwen2.5-coder-1.5b-instruct-q4_k_m", m.ID)

	_, ok = c.Get("nonexistent-model")
	assert.False(t, ok)
}
