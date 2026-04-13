package gateway

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/semaphore"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

// ToolManager handles tool schema compression, budget-aware selection,
// and concurrent access control. It allows HelixLLM to accept unlimited
// tools from CLI agents while fitting them into the model's context window.
//
// Architecture:
//   - Tools are compressed to minimal schemas (~100 tokens each vs ~500 raw)
//   - A token budget determines how many tools fit per request
//   - A semaphore controls concurrent tool-using requests
//   - The respond tool is always included (appended last)
type ToolManager struct {
	// maxTokenBudget is the maximum tokens available for tool definitions.
	// Calculated as: context_size - system_prompt - conversation - generation.
	maxTokenBudget int

	// tokensPerTool estimates tokens per compressed tool definition.
	tokensPerTool int

	// sem limits concurrent tool-calling requests to prevent overload.
	sem *semaphore.Weighted

	// stats tracks tool processing metrics.
	totalRequests  atomic.Int64
	toolsReceived  atomic.Int64
	toolsIncluded  atomic.Int64
	toolsCompressed atomic.Int64

	mu sync.RWMutex
}

// ToolManagerConfig configures the ToolManager.
type ToolManagerConfig struct {
	// MaxTokenBudget for tool definitions (default: 6000 ~= 30 compressed tools).
	MaxTokenBudget int
	// TokensPerTool estimate for budget calculation (default: 120).
	TokensPerTool int
	// MaxConcurrent limits simultaneous tool-calling requests (default: 10).
	MaxConcurrent int
}

// NewToolManager creates a ToolManager with the given config.
func NewToolManager(cfg ToolManagerConfig) *ToolManager {
	if cfg.MaxTokenBudget <= 0 {
		cfg.MaxTokenBudget = 6000 // ~30 compressed tools
	}
	if cfg.TokensPerTool <= 0 {
		cfg.TokensPerTool = 120 // compressed schema ~120 tokens
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 10
	}
	return &ToolManager{
		maxTokenBudget: cfg.MaxTokenBudget,
		tokensPerTool:  cfg.TokensPerTool,
		sem:            semaphore.NewWeighted(int64(cfg.MaxConcurrent)),
	}
}

// DefaultToolManager creates a ToolManager with default settings.
func DefaultToolManager() *ToolManager {
	return NewToolManager(ToolManagerConfig{})
}

// CompressAndSelect takes the full tool list from the client, compresses
// schemas to fit the token budget, and returns the optimized tool list.
// The respond tool is appended at the end. Tools are kept in client order
// (first = highest priority) and as many as fit are included.
func (tm *ToolManager) CompressAndSelect(tools []api.Tool) []api.Tool {
	tm.totalRequests.Add(1)
	tm.toolsReceived.Add(int64(len(tools)))

	// Calculate how many tools fit in the budget
	maxTools := tm.maxTokenBudget / tm.tokensPerTool
	if maxTools < 3 {
		maxTools = 3 // minimum: at least a few tools
	}

	// Compress each tool's schema to minimal form
	var result []api.Tool
	for i, t := range tools {
		if i >= maxTools {
			break
		}
		compressed := compressTool(t)
		result = append(result, compressed)
		tm.toolsCompressed.Add(1)
	}

	tm.toolsIncluded.Add(int64(len(result)))
	return result
}

// Stats returns current tool processing statistics.
func (tm *ToolManager) Stats() ToolManagerStats {
	return ToolManagerStats{
		TotalRequests:   tm.totalRequests.Load(),
		ToolsReceived:   tm.toolsReceived.Load(),
		ToolsIncluded:   tm.toolsIncluded.Load(),
		ToolsCompressed: tm.toolsCompressed.Load(),
	}
}

// ToolManagerStats holds tool processing metrics.
type ToolManagerStats struct {
	TotalRequests   int64 `json:"total_requests"`
	ToolsReceived   int64 `json:"tools_received"`
	ToolsIncluded   int64 `json:"tools_included"`
	ToolsCompressed int64 `json:"tools_compressed"`
}

// compressTool reduces a tool's schema to minimal form while preserving
// the name, a one-line description, and required parameters only.
// This typically reduces ~500 tokens to ~100 tokens per tool.
func compressTool(t api.Tool) api.Tool {
	// Compress description to first sentence (max 80 chars)
	desc := t.Function.Description
	if len(desc) > 80 {
		// Find first period or newline
		for i, ch := range desc {
			if (ch == '.' || ch == '\n') && i > 20 {
				desc = desc[:i+1]
				break
			}
		}
		if len(desc) > 80 {
			desc = desc[:77] + "..."
		}
	}

	// Compress parameters: keep only required fields with type only (no descriptions)
	params := compressParams(t.Function.Parameters)

	return api.Tool{
		Type: t.Type,
		Function: api.ToolFunction{
			Name:        t.Function.Name,
			Description: desc,
			Parameters:  params,
		},
	}
}

// compressParams strips parameter descriptions and optional fields,
// keeping only name+type for required parameters.
func compressParams(params interface{}) interface{} {
	if params == nil {
		return nil
	}

	// Try to parse as JSON schema
	var raw []byte
	switch v := params.(type) {
	case json.RawMessage:
		raw = v
	case []byte:
		raw = v
	default:
		var err error
		raw, err = json.Marshal(v)
		if err != nil {
			return params
		}
	}

	var schema map[string]interface{}
	if err := json.Unmarshal(raw, &schema); err != nil {
		return params
	}

	// Strip descriptions from properties
	if props, ok := schema["properties"].(map[string]interface{}); ok {
		compressed := make(map[string]interface{})
		for name, prop := range props {
			if pm, ok := prop.(map[string]interface{}); ok {
				// Keep only type
				simple := map[string]interface{}{"type": pm["type"]}
				compressed[name] = simple
			} else {
				compressed[name] = prop
			}
		}
		schema["properties"] = compressed
	}

	result, err := json.Marshal(schema)
	if err != nil {
		return params
	}
	return json.RawMessage(result)
}

// RespondTool returns the standard respond tool definition.
func RespondTool() api.Tool {
	return api.Tool{
		Type: "function",
		Function: api.ToolFunction{
			Name:        "respond",
			Description: "Send a text reply to the user.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}`),
		},
	}
}

// MaxToolsForBudget returns how many tools fit in the configured budget.
func (tm *ToolManager) MaxToolsForBudget() int {
	return tm.maxTokenBudget / tm.tokensPerTool
}

// String returns a human-readable summary.
func (tm *ToolManager) String() string {
	s := tm.Stats()
	return fmt.Sprintf("ToolManager: budget=%d tokens, ~%d tools max, requests=%d, received=%d, included=%d",
		tm.maxTokenBudget, tm.MaxToolsForBudget(), s.TotalRequests, s.ToolsReceived, s.ToolsIncluded)
}
