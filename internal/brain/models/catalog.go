package models

// ModelTier classifies models by speed/size trade-off.
type ModelTier string

const (
	// TierFast targets 1-2B parameter models running at 180-300 TPS.
	TierFast ModelTier = "fast"
	// TierBalanced targets ~3B parameter models running at 120-160 TPS.
	TierBalanced ModelTier = "balanced"
	// TierPowerful targets 7-8B parameter models running at 45-75 TPS.
	TierPowerful ModelTier = "powerful"
)

// ModelDefinition describes a single GGUF model that HelixLLM can serve.
type ModelDefinition struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	HuggingFaceRepo string    `json:"huggingface_repo"`
	Filename        string    `json:"filename"`
	Parameters      int64     `json:"parameters"`
	Quantization    string    `json:"quantization"`
	VRAMRequired    int64     `json:"vram_required"` // bytes
	RAMRequired     int64     `json:"ram_required"`  // bytes — system RAM for CPU inference
	ContextLength   int       `json:"context_length"`
	Tier            ModelTier `json:"tier"`
	BFCLScore       float64   `json:"bfcl_score"`
	TPSEstimate     [2]int    `json:"tps_estimate"` // [low, high]
	License         string    `json:"license"`
	SupportsTools   bool      `json:"supports_tools"`
	RequiresJinja   bool      `json:"requires_jinja"`
	Architecture    string    `json:"architecture"`
	IsEmbedding     bool      `json:"is_embedding"`
	EmbeddingDims   int       `json:"embedding_dims"`
	Family          string    `json:"family,omitempty"`
	MoE             bool      `json:"moe,omitempty"`
	ActiveParams    int64     `json:"active_params,omitempty"`
	TotalParams     int64     `json:"total_params,omitempty"`
}

// Catalog holds the full set of known models.
type Catalog struct {
	Models []ModelDefinition `json:"models"`
}

// DefaultCatalog returns the built-in lightweight fleet catalog.
func DefaultCatalog() *Catalog {
	return &Catalog{
		Models: []ModelDefinition{
			{
				ID:              "qwen2.5-coder-1.5b-instruct-q4_k_m",
				Name:            "Qwen2.5-Coder 1.5B Instruct Q4_K_M",
				HuggingFaceRepo: "Qwen/Qwen2.5-Coder-1.5B-Instruct-GGUF",
				Filename:        "qwen2.5-coder-1.5b-instruct-q4_k_m.gguf",
				Parameters:      1_500_000_000,
				Quantization:    "Q4_K_M",
				VRAMRequired:    1 * 1024 * 1024 * 1024, // ~1 GB
				ContextLength:   32768,
				Tier:            TierFast,
				BFCLScore:       52.0,
				TPSEstimate:     [2]int{180, 250},
				License:         "Apache-2.0",
				SupportsTools:   true,
				RequiresJinja:   true,
				Architecture:    "qwen2",
				IsEmbedding:     false,
				EmbeddingDims:   0,
			},
			{
				ID:              "qwen2.5-coder-3b-instruct-q4_k_m",
				Name:            "Qwen2.5-Coder 3B Instruct Q4_K_M",
				HuggingFaceRepo: "Qwen/Qwen2.5-Coder-3B-Instruct-GGUF",
				Filename:        "qwen2.5-coder-3b-instruct-q4_k_m.gguf",
				Parameters:      3_000_000_000,
				Quantization:    "Q4_K_M",
				VRAMRequired:    2 * 1024 * 1024 * 1024, // ~2 GB
				ContextLength:   32768,
				Tier:            TierBalanced,
				BFCLScore:       57.0,
				TPSEstimate:     [2]int{120, 160},
				License:         "Apache-2.0",
				SupportsTools:   true,
				RequiresJinja:   true,
				Architecture:    "qwen2",
				IsEmbedding:     false,
				EmbeddingDims:   0,
			},
			{
				ID:              "functionary-small-v3.2-q4_k_m",
				Name:            "Functionary Small v3.2 Q4_K_M",
				HuggingFaceRepo: "meetkai/functionary-small-v3.2-GGUF",
				Filename:        "functionary-small-v3.2.Q4_0.gguf",
				Parameters:      8_000_000_000,
				Quantization:    "Q4_0",
				VRAMRequired:    5 * 1024 * 1024 * 1024, // ~5 GB
				ContextLength:   131072,
				Tier:            TierPowerful,
				BFCLScore:       68.4,
				TPSEstimate:     [2]int{45, 65},
				License:         "MIT",
				SupportsTools:   true,
				RequiresJinja:   false,
				Architecture:    "llama",
				IsEmbedding:     false,
				EmbeddingDims:   0,
			},
			{
				ID:              "nomic-embed-text-v1.5-q4_k_m",
				Name:            "Nomic Embed Text v1.5 Q4_K_M",
				HuggingFaceRepo: "nomic-ai/nomic-embed-text-v1.5-GGUF",
				Filename:        "nomic-embed-text-v1.5.Q4_K_M.gguf",
				Parameters:      137_000_000,
				Quantization:    "Q4_K_M",
				VRAMRequired:    90 * 1024 * 1024, // ~90 MB
				ContextLength:   2048,
				Tier:            TierFast, // smallest available tier; not used for routing
				BFCLScore:       0.0,
				TPSEstimate:     [2]int{0, 0},
				License:         "Apache-2.0",
				SupportsTools:   false,
				RequiresJinja:   false,
				Architecture:    "nomic-bert",
				IsEmbedding:     true,
				EmbeddingDims:   768,
			},
			{
				ID:              "deepseek-v4-flash",
				Name:            "DeepSeek V4 Flash (284B MoE, IQ4_XS)",
				HuggingFaceRepo: "unsloth/DeepSeek-V4-Flash-GGUF",
				Filename:        "DeepSeek-V4-Flash-IQ4_XS.gguf",
				Quantization:    "IQ4_XS",
				RAMRequired:     120 * 1024 * 1024 * 1024, // ~120 GB
				ContextLength:   131072,
				Family:          "deepseek",
				MoE:             true,
				ActiveParams:    13_000_000_000,
				TotalParams:     284_000_000_000,
			},
			{
				ID:              "deepseek-v4-pro",
				Name:            "DeepSeek V4 Pro (1.6T MoE, IQ4_XS)",
				HuggingFaceRepo: "unsloth/DeepSeek-V4-Pro-GGUF",
				Filename:        "DeepSeek-V4-Pro-IQ4_XS.gguf",
				Quantization:    "IQ4_XS",
				RAMRequired:     680 * 1024 * 1024 * 1024, // ~680 GB
				ContextLength:   131072,
				Family:          "deepseek",
				MoE:             true,
				ActiveParams:    49_000_000_000,
				TotalParams:     1_600_000_000_000,
			},
		},
	}
}

// ByTier returns non-embedding models whose Tier matches the given tier.
func (c *Catalog) ByTier(tier ModelTier) []ModelDefinition {
	var result []ModelDefinition
	for _, m := range c.Models {
		if !m.IsEmbedding && m.Tier == tier {
			result = append(result, m)
		}
	}
	return result
}

// EmbeddingModels returns only models marked as embedding models.
func (c *Catalog) EmbeddingModels() []ModelDefinition {
	var result []ModelDefinition
	for _, m := range c.Models {
		if m.IsEmbedding {
			result = append(result, m)
		}
	}
	return result
}

// ChatModels returns only non-embedding models.
func (c *Catalog) ChatModels() []ModelDefinition {
	var result []ModelDefinition
	for _, m := range c.Models {
		if !m.IsEmbedding {
			result = append(result, m)
		}
	}
	return result
}

// FilterByVRAM returns all models (including embeddings) whose VRAMRequired
// is less than or equal to maxVRAM. Models with VRAMRequired==0 are treated
// as CPU-only (not GPU-loadable).
func (c *Catalog) FilterByVRAM(maxVRAM int64) []ModelDefinition {
	var result []ModelDefinition
	for _, m := range c.Models {
		if m.VRAMRequired > 0 && m.VRAMRequired <= maxVRAM {
			result = append(result, m)
		}
	}
	return result
}

// FilterByRAM returns all models (including embeddings) whose RAMRequired
// is less than or equal to maxRAM. When a model's RAMRequired is zero, it
// is assumed to fit in RAM (legacy entry).
func (c *Catalog) FilterByRAM(maxRAM int64) []ModelDefinition {
	var result []ModelDefinition
	for _, m := range c.Models {
		if m.RAMRequired == 0 || m.RAMRequired <= maxRAM {
			result = append(result, m)
		}
	}
	return result
}

// FilterByCPUCompatible returns non-embedding chat models that fit in the
// given system RAM. It ranks MoE models (lowest active:total ratio first)
// ahead of dense models, then within each group by MoE active:total ratio.
func (c *Catalog) FilterByCPUCompatible(maxRAM int64) []ModelDefinition {
	var moeModels, denseModels []ModelDefinition
	for _, m := range c.Models {
		if m.IsEmbedding {
			continue
		}
		if m.RAMRequired == 0 || m.RAMRequired <= maxRAM {
			if m.MoE {
				moeModels = append(moeModels, m)
			} else {
				denseModels = append(denseModels, m)
			}
		}
	}
	// Sort MoE by active:total ratio (ascending — lower ratio = more efficient)
	sortMoEByRatio(moeModels)
	result := append(moeModels, denseModels...) //nolint:gocritic
	return result
}

func sortMoEByRatio(models []ModelDefinition) {
	for i := 0; i < len(models); i++ {
		for j := i + 1; j < len(models); j++ {
			ratioI := float64(models[i].ActiveParams) / float64(models[i].TotalParams)
			ratioJ := float64(models[j].ActiveParams) / float64(models[j].TotalParams)
			if ratioJ < ratioI {
				models[i], models[j] = models[j], models[i]
			}
		}
	}
}

// Get looks up a model by its ID. Returns (model, true) on success or
// (zero value, false) when the ID is not found.
func (c *Catalog) Get(id string) (ModelDefinition, bool) {
	for _, m := range c.Models {
		if m.ID == id {
			return m, true
		}
	}
	return ModelDefinition{}, false
}
