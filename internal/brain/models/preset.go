package models

import (
	"fmt"
	"strings"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/hardware"
)

// presetProfile holds the llama.cpp parameters for a named hardware tier.
type presetProfile struct {
	GPULayers   int
	ContextSize int
	BatchSize   int
}

// presetProfiles maps hardware tier names to llama.cpp tuning parameters.
var presetProfiles = map[string]presetProfile{
	"cpu_only":     {GPULayers: 0, ContextSize: 2048, BatchSize: 256},
	"consumer_6gb": {GPULayers: -1, ContextSize: 4096, BatchSize: 512},
	"consumer_8gb": {GPULayers: -1, ContextSize: 8192, BatchSize: 1024},
	"high_end":     {GPULayers: -1, ContextSize: 16384, BatchSize: 1024},
}

// GeneratePresets produces a llama.cpp INI-format configuration string suitable
// for the --models-preset flag. It generates one [global] section followed by
// one named section per ModelDefinition.
//
// Embedding models omit ctx-size, n-batch, and chat-template. Chat models
// include ctx-size and n-batch. chat-template = jinja is added only when
// RequiresJinja is true. flash-attn = on is added only when a GPU is detected.
//
// Returns an error when the profile's PresetProfile name is not recognised.
func GeneratePresets(defs []ModelDefinition, profile *hardware.HardwareProfile) (string, error) {
	pp, ok := presetProfiles[profile.PresetProfile]
	if !ok {
		return "", fmt.Errorf("models: unknown preset profile %q", profile.PresetProfile)
	}

	var sb strings.Builder

	// -----------------------------------------------------------------
	// [global] section
	// -----------------------------------------------------------------
	sb.WriteString("[global]\n")
	if profile.GPU.Available {
		sb.WriteString("flash-attn = on\n")
	}
	fmt.Fprintf(&sb, "n-threads = %d\n", profile.InferenceThreads())
	fmt.Fprintf(&sb, "n-threads-batch = %d\n", profile.BatchThreads())
	sb.WriteString("\n")

	// -----------------------------------------------------------------
	// Per-model sections
	// -----------------------------------------------------------------
	for _, def := range defs {
		fmt.Fprintf(&sb, "[%s]\n", def.ID)
		fmt.Fprintf(&sb, "model = /models/%s\n", def.Filename)
		fmt.Fprintf(&sb, "n-gpu-layers = %d\n", pp.GPULayers)

		if !def.IsEmbedding {
			fmt.Fprintf(&sb, "ctx-size = %d\n", pp.ContextSize)
			fmt.Fprintf(&sb, "n-batch = %d\n", pp.BatchSize)
		}

		if def.RequiresJinja {
			sb.WriteString("chat-template = jinja\n")
		}

		if def.IsEmbedding {
			sb.WriteString("embedding = on\n")
		}

		sb.WriteString("\n")
	}

	return sb.String(), nil
}
