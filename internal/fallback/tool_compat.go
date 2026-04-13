package fallback

import "strings"

// toolCapableModelPrefixes contains model name substrings that are known to
// support OpenAI-compatible function calling / tool use.  The list is
// intentionally broad and uses Contains matching so that versioned variants
// (e.g. "qwen2.5-coder-3b", "llama-3.1-70b-instruct") are all captured.
var toolCapableModelPrefixes = []string{
	"qwen",        // Qwen models have solid tool support
	"deepseek",    // DeepSeek models support tools
	"llama",       // Llama 3.1+ supports tools
	"mistral",     // Mistral supports tools
	"gpt",         // GPT models (OpenAI)
	"claude",      // Claude models (Anthropic)
	"command",     // Cohere Command-R
	"functionary", // Purpose-built for function calling
	"hermes",      // Hermes models support tools
	"nous",        // Nous Research models (e.g. NousHermes)
	"openhermes",  // OpenHermes
	"mixtral",     // Mixtral (Mistral MoE)
	"phi-3",       // Microsoft Phi-3 supports tools
	"phi3",        // alternate spelling
	"nexusraven",  // NexusRaven — tool-calling specialist
}

// ModelSupportsTools returns true if the model ID suggests it supports
// OpenAI-style tool / function calling.  This is a best-effort heuristic
// based on known model families; unknown model IDs are assumed capable so
// the chain always tries an unrecognised model rather than silently skipping.
//
// The function is exported so callers outside this package (e.g. gateway
// routing logic or tests) can apply the same check without duplicating the
// list.
func ModelSupportsTools(modelID string) bool {
	if modelID == "" {
		return true // unknown model — try it rather than skip it
	}
	lower := strings.ToLower(modelID)
	for _, prefix := range toolCapableModelPrefixes {
		if strings.Contains(lower, prefix) {
			return true
		}
	}
	return false
}

// modelSupportsTools is the internal alias used by chain.go so call sites
// inside the package remain unchanged.
func modelSupportsTools(modelID string) bool {
	return ModelSupportsTools(modelID)
}
