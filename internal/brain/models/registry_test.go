package models_test

import (
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/brain/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper: load all DefaultCatalog models into the registry as StatusLoaded.
func loadAll(r *models.Registry) {
	catalog := models.DefaultCatalog()
	for _, def := range catalog.Models {
		r.Add(models.RuntimeModel{
			Definition: def,
			Status:     models.StatusUnloaded,
			FilePath:   "/models/" + def.Filename,
			Downloaded: true,
		})
		r.UpdateStatus(def.ID, models.StatusLoaded)
	}
}

// TestRegistry_AddAndGet verifies that a model added to the registry can be
// retrieved by ID and that its status is preserved.
func TestRegistry_AddAndGet(t *testing.T) {
	r := models.NewRegistry()

	catalog := models.DefaultCatalog()
	def, ok := catalog.Get("qwen2.5-coder-1.5b-instruct-q4_k_m")
	require.True(t, ok)

	r.Add(models.RuntimeModel{
		Definition: def,
		Status:     models.StatusUnloaded,
		FilePath:   "/models/qwen2.5-coder-1.5b-instruct-q4_k_m.gguf",
		Downloaded: false,
	})

	got, found := r.Get("qwen2.5-coder-1.5b-instruct-q4_k_m")
	require.True(t, found)
	assert.Equal(t, def.ID, got.Definition.ID)
	assert.Equal(t, models.StatusUnloaded, got.Status)
	assert.False(t, got.Downloaded)

	// Non-existent model returns false.
	_, found = r.Get("does-not-exist")
	assert.False(t, found)
}

// TestRegistry_BestAvailable_FastTier verifies that with all models loaded
// BestAvailable(TierFast) returns the 1.5B fast model.
func TestRegistry_BestAvailable_FastTier(t *testing.T) {
	r := models.NewRegistry()
	loadAll(r)

	rm, ok := r.BestAvailable(models.TierFast)
	require.True(t, ok)
	assert.Equal(t, "qwen2.5-coder-1.5b-instruct-q4_k_m", rm.Definition.ID)
	assert.Equal(t, models.TierFast, rm.Definition.Tier)
	assert.False(t, rm.Definition.IsEmbedding)
}

// TestRegistry_BestAvailable_FallbackToLower verifies that when only the
// fast-tier model is loaded, BestAvailable(TierPowerful) falls back to it.
func TestRegistry_BestAvailable_FallbackToLower(t *testing.T) {
	r := models.NewRegistry()

	catalog := models.DefaultCatalog()
	fastDef, ok := catalog.Get("qwen2.5-coder-1.5b-instruct-q4_k_m")
	require.True(t, ok)

	r.Add(models.RuntimeModel{
		Definition: fastDef,
		Status:     models.StatusUnloaded,
		FilePath:   "/models/fast.gguf",
		Downloaded: true,
	})
	r.UpdateStatus(fastDef.ID, models.StatusLoaded)

	rm, found := r.BestAvailable(models.TierPowerful)
	require.True(t, found, "should fall back to fast tier when powerful is absent")
	assert.Equal(t, "qwen2.5-coder-1.5b-instruct-q4_k_m", rm.Definition.ID)
}

// TestRegistry_BestAvailable_NoneLoaded verifies that an empty registry
// returns false.
func TestRegistry_BestAvailable_NoneLoaded(t *testing.T) {
	r := models.NewRegistry()

	_, ok := r.BestAvailable(models.TierFast)
	assert.False(t, ok)

	_, ok = r.BestAvailable(models.TierBalanced)
	assert.False(t, ok)

	_, ok = r.BestAvailable(models.TierPowerful)
	assert.False(t, ok)
}

// TestRegistry_UpdateStatus verifies that UpdateStatus transitions a model
// from unloaded to loaded and records a non-zero LoadedAt timestamp.
func TestRegistry_UpdateStatus(t *testing.T) {
	r := models.NewRegistry()

	catalog := models.DefaultCatalog()
	def, ok := catalog.Get("qwen2.5-coder-3b-instruct-q4_k_m")
	require.True(t, ok)

	r.Add(models.RuntimeModel{
		Definition: def,
		Status:     models.StatusUnloaded,
		FilePath:   "/models/balanced.gguf",
		Downloaded: true,
	})

	before := time.Now()
	r.UpdateStatus(def.ID, models.StatusLoaded)
	after := time.Now()

	got, found := r.Get(def.ID)
	require.True(t, found)
	assert.Equal(t, models.StatusLoaded, got.Status)
	assert.True(t, got.LoadedAt.After(before) || got.LoadedAt.Equal(before),
		"LoadedAt should be set after transition to loaded")
	assert.True(t, got.LoadedAt.Before(after) || got.LoadedAt.Equal(after))
}

// TestRegistry_LoadedModels verifies that LoadedModels returns only the
// subset of models whose Status is StatusLoaded.
func TestRegistry_LoadedModels(t *testing.T) {
	r := models.NewRegistry()

	catalog := models.DefaultCatalog()

	for _, def := range catalog.Models {
		r.Add(models.RuntimeModel{
			Definition: def,
			Status:     models.StatusUnloaded,
			FilePath:   "/models/" + def.Filename,
			Downloaded: true,
		})
	}

	// Load only the fast-tier model.
	r.UpdateStatus("qwen2.5-coder-1.5b-instruct-q4_k_m", models.StatusLoaded)

	loaded := r.LoadedModels()
	require.Len(t, loaded, 1)
	assert.Equal(t, "qwen2.5-coder-1.5b-instruct-q4_k_m", loaded[0].Definition.ID)
}
