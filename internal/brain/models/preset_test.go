package models_test

import (
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/brain/models"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/hardware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// consumer6GBProfile builds a 6 GB GPU profile with 16 CPU cores.
func consumer6GBProfile() *hardware.HardwareProfile {
	return &hardware.HardwareProfile{
		GPU:           hardware.GPUProfile{Available: true, VRAMTotal: 6 * 1024 * 1024 * 1024},
		CPU:           hardware.CPUProfile{Cores: 16},
		PresetProfile: "consumer_6gb",
	}
}

// cpuOnlyProfile builds a no-GPU profile with 8 CPU cores.
func cpuOnlyProfile() *hardware.HardwareProfile {
	return &hardware.HardwareProfile{
		GPU:           hardware.GPUProfile{Available: false, VRAMTotal: 0},
		CPU:           hardware.CPUProfile{Cores: 8},
		PresetProfile: "cpu_only",
	}
}

// TestGeneratePresets_Consumer6GB verifies the INI output for a 6 GB GPU host
// with 16 CPU cores and the consumer_6gb preset profile.
func TestGeneratePresets_Consumer6GB(t *testing.T) {
	defs := models.DefaultCatalog().Models
	profile := consumer6GBProfile()

	out, err := models.GeneratePresets(defs, profile)
	require.NoError(t, err)
	require.NotEmpty(t, out)

	// [global] section must be present.
	assert.Contains(t, out, "[global]")

	// GPU available → flash-attn = on.
	assert.Contains(t, out, "flash-attn = on")

	// InferenceThreads for 16 cores = 16-2 = 14.
	assert.Contains(t, out, "n-threads = 14")

	// BatchThreads for 16 cores = 16.
	assert.Contains(t, out, "n-threads-batch = 16")

	// Qwen 1.5B chat model section.
	assert.Contains(t, out, "[qwen2.5-coder-1.5b-instruct-q4_k_m]")

	// consumer_6gb preset: GPULayers=-1, ContextSize=4096.
	assert.Contains(t, out, "n-gpu-layers = -1")
	assert.Contains(t, out, "ctx-size = 4096")

	// RequiresJinja=true → chat-template = jinja.
	assert.Contains(t, out, "chat-template = jinja")

	// Embedding model section.
	assert.Contains(t, out, "[nomic-embed-text-v1.5-q4_k_m]")

	// Embedding model must NOT have ctx-size or chat-template.
	// We check this by splitting and inspecting the nomic section only.
	nomicSection := extractSection(out, "[nomic-embed-text-v1.5-q4_k_m]")
	assert.NotContains(t, nomicSection, "ctx-size")
	assert.NotContains(t, nomicSection, "chat-template")
}

// TestGeneratePresets_CPUOnly verifies CPU-only preset: no flash-attn,
// n-gpu-layers=0, ctx-size=2048, n-threads=6 (8 cores − 2).
func TestGeneratePresets_CPUOnly(t *testing.T) {
	defs := models.DefaultCatalog().Models
	profile := cpuOnlyProfile()

	out, err := models.GeneratePresets(defs, profile)
	require.NoError(t, err)

	// No GPU → no flash-attn line at all.
	assert.NotContains(t, out, "flash-attn")

	// cpu_only preset: GPULayers=0, ContextSize=2048.
	assert.Contains(t, out, "n-gpu-layers = 0")
	assert.Contains(t, out, "ctx-size = 2048")

	// InferenceThreads for 8 cores = 8-2 = 6.
	assert.Contains(t, out, "n-threads = 6")
}

// TestGeneratePresets_EmbeddingModel verifies that the nomic section carries
// "embedding = on" and does NOT contain ctx-size, n-batch, or chat-template.
func TestGeneratePresets_EmbeddingModel(t *testing.T) {
	defs := models.DefaultCatalog().Models
	profile := consumer6GBProfile()

	out, err := models.GeneratePresets(defs, profile)
	require.NoError(t, err)

	nomicSection := extractSection(out, "[nomic-embed-text-v1.5-q4_k_m]")
	require.NotEmpty(t, nomicSection, "nomic section must be present")

	assert.Contains(t, nomicSection, "embedding = on")
	assert.NotContains(t, nomicSection, "ctx-size")
	assert.NotContains(t, nomicSection, "n-batch")
	assert.NotContains(t, nomicSection, "chat-template")
}

// extractSection returns the lines from the given section header up to (but not
// including) the next section header or end-of-string.
func extractSection(ini, header string) string {
	lines := strings.Split(ini, "\n")
	var inSection bool
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == header {
			inSection = true
			result = append(result, line)
			continue
		}
		if inSection {
			if strings.HasPrefix(trimmed, "[") && trimmed != header {
				break
			}
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}
