package gateway_test

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeTool creates a test tool with the given name and a long description + full schema.
func makeTool(name string) api.Tool {
	return api.Tool{
		Type: "function",
		Function: api.ToolFunction{
			Name:        name,
			Description: fmt.Sprintf("This is the %s tool. It performs important operations on the codebase including reading, writing, and managing files in the project directory structure.", name),
			Parameters: json.RawMessage(fmt.Sprintf(`{
				"type": "object",
				"properties": {
					"arg1": {"type": "string", "description": "The first argument for %s which should be a valid path or identifier"},
					"arg2": {"type": "string", "description": "The second optional argument providing additional context"},
					"timeout": {"type": "number", "description": "Timeout in milliseconds for the operation"}
				},
				"required": ["arg1"]
			}`, name)),
		},
	}
}

// makeTools creates N test tools.
func makeTools(n int) []api.Tool {
	tools := make([]api.Tool, n)
	for i := 0; i < n; i++ {
		tools[i] = makeTool(fmt.Sprintf("tool_%d", i))
	}
	return tools
}

// --- Unit Tests ---

func TestToolManager_DefaultConfig(t *testing.T) {
	tm := gateway.DefaultToolManager()
	require.NotNil(t, tm)
	assert.Greater(t, tm.MaxToolsForBudget(), 0)
}

func TestToolManager_ZeroTools(t *testing.T) {
	tm := gateway.DefaultToolManager()
	result := tm.CompressAndSelect(nil)
	assert.Empty(t, result)

	result = tm.CompressAndSelect([]api.Tool{})
	assert.Empty(t, result)
}

func TestToolManager_SingleTool(t *testing.T) {
	tm := gateway.DefaultToolManager()
	tools := makeTools(1)
	result := tm.CompressAndSelect(tools)
	assert.Len(t, result, 1)
	assert.Equal(t, "tool_0", result[0].Function.Name)
}

func TestToolManager_FiveTools(t *testing.T) {
	tm := gateway.DefaultToolManager()
	tools := makeTools(5)
	result := tm.CompressAndSelect(tools)
	assert.Len(t, result, 5)
	for i, r := range result {
		assert.Equal(t, fmt.Sprintf("tool_%d", i), r.Function.Name)
	}
}

func TestToolManager_TenTools(t *testing.T) {
	tm := gateway.NewToolManager(gateway.ToolManagerConfig{MaxTools: 10})
	tools := makeTools(10)
	result := tm.CompressAndSelect(tools)
	assert.Len(t, result, 10)
}

func TestToolManager_TwentyTools(t *testing.T) {
	tm := gateway.NewToolManager(gateway.ToolManagerConfig{MaxTools: 20})
	tools := makeTools(20)
	result := tm.CompressAndSelect(tools)
	assert.Len(t, result, 20)
}

func TestToolManager_FiftyTools(t *testing.T) {
	tm := gateway.NewToolManager(gateway.ToolManagerConfig{MaxTools: 50})
	tools := makeTools(50)
	result := tm.CompressAndSelect(tools)
	assert.Equal(t, 50, len(result))
}

func TestToolManager_HundredTools(t *testing.T) {
	tm := gateway.DefaultToolManager() // MaxTools=5
	tools := makeTools(100)
	result := tm.CompressAndSelect(tools)
	assert.Equal(t, 5, len(result)) // hard capped at 5
}

func TestToolManager_HundredTools_LargeBudget(t *testing.T) {
	tm := gateway.NewToolManager(gateway.ToolManagerConfig{
		MaxTools:       100,
		MaxTokenBudget: 12000,
		TokensPerTool:  120,
	})
	tools := makeTools(100)
	result := tm.CompressAndSelect(tools)
	assert.Equal(t, 100, len(result))
}

func TestToolManager_DefaultCapsAtFive(t *testing.T) {
	tm := gateway.DefaultToolManager()
	tools := makeTools(51) // OpenCode sends ~51 tools
	result := tm.CompressAndSelect(tools)
	assert.Equal(t, 5, len(result), "default should cap at 5 for 7B model")
}

func TestToolManager_PreservesOrder(t *testing.T) {
	tm := gateway.DefaultToolManager()
	tools := makeTools(30)
	result := tm.CompressAndSelect(tools)
	for i := range result {
		assert.Equal(t, fmt.Sprintf("tool_%d", i), result[i].Function.Name)
	}
}

func TestToolManager_CompressesDescription(t *testing.T) {
	tm := gateway.DefaultToolManager()
	tool := makeTool("test")
	origDescLen := len(tool.Function.Description)

	result := tm.CompressAndSelect([]api.Tool{tool})
	require.Len(t, result, 1)
	compressedDescLen := len(result[0].Function.Description)

	assert.Less(t, compressedDescLen, origDescLen,
		"compressed description should be shorter than original")
	assert.LessOrEqual(t, compressedDescLen, 80,
		"compressed description should be ≤80 chars")
}

func TestToolManager_CompressesParameters(t *testing.T) {
	tm := gateway.DefaultToolManager()
	tool := makeTool("test")

	result := tm.CompressAndSelect([]api.Tool{tool})
	require.Len(t, result, 1)

	// Parse compressed params
	var params map[string]interface{}
	raw, err := json.Marshal(result[0].Function.Parameters)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &params))

	// Check properties have no descriptions
	props, ok := params["properties"].(map[string]interface{})
	require.True(t, ok)
	for _, prop := range props {
		pm, ok := prop.(map[string]interface{})
		require.True(t, ok)
		_, hasDesc := pm["description"]
		assert.False(t, hasDesc, "compressed params should not have descriptions")
	}
}

func TestToolManager_PreservesName(t *testing.T) {
	tm := gateway.DefaultToolManager()
	tools := []api.Tool{
		makeTool("Bash"),
		makeTool("Read"),
		makeTool("Write"),
	}
	result := tm.CompressAndSelect(tools)
	assert.Equal(t, "Bash", result[0].Function.Name)
	assert.Equal(t, "Read", result[1].Function.Name)
	assert.Equal(t, "Write", result[2].Function.Name)
}

func TestToolManager_Stats(t *testing.T) {
	tm := gateway.DefaultToolManager() // MaxTools=5

	tm.CompressAndSelect(makeTools(10)) // 10 received, 5 included
	tm.CompressAndSelect(makeTools(20)) // 20 received, 5 included

	stats := tm.Stats()
	assert.Equal(t, int64(2), stats.TotalRequests)
	assert.Equal(t, int64(30), stats.ToolsReceived)
	assert.Equal(t, int64(10), stats.ToolsIncluded)  // 5+5
	assert.Equal(t, int64(10), stats.ToolsCompressed) // 5+5
}

func TestToolManager_StatsOverBudget(t *testing.T) {
	tm := gateway.NewToolManager(gateway.ToolManagerConfig{
		MaxTokenBudget: 600, // room for ~5 tools
		TokensPerTool:  120,
	})
	tm.CompressAndSelect(makeTools(20))

	stats := tm.Stats()
	assert.Equal(t, int64(20), stats.ToolsReceived)
	assert.Equal(t, int64(5), stats.ToolsIncluded)
}

func TestToolManager_RespondTool(t *testing.T) {
	tool := gateway.RespondTool()
	assert.Equal(t, "respond", tool.Function.Name)
	assert.Equal(t, "function", tool.Type)
	assert.NotEmpty(t, tool.Function.Description)
}

func TestToolManager_MinimumThreeTools(t *testing.T) {
	// Even with tiny budget, at least 3 tools should be included
	tm := gateway.NewToolManager(gateway.ToolManagerConfig{
		MaxTokenBudget: 1, // impossibly small
		TokensPerTool:  120,
	})
	tools := makeTools(10)
	result := tm.CompressAndSelect(tools)
	assert.GreaterOrEqual(t, len(result), 3,
		"should include at least 3 tools regardless of budget")
}

func TestToolManager_String(t *testing.T) {
	tm := gateway.DefaultToolManager()
	s := tm.String()
	assert.Contains(t, s, "ToolManager")
	assert.Contains(t, s, "budget=")
}

func TestToolManager_NilParams(t *testing.T) {
	tm := gateway.DefaultToolManager()
	tool := api.Tool{
		Type: "function",
		Function: api.ToolFunction{
			Name:        "simple",
			Description: "A tool with no parameters",
		},
	}
	result := tm.CompressAndSelect([]api.Tool{tool})
	require.Len(t, result, 1)
	assert.Equal(t, "simple", result[0].Function.Name)
}

// --- Benchmark Tests ---

func BenchmarkToolManager_CompressAndSelect_10(b *testing.B) {
	tm := gateway.DefaultToolManager()
	tools := makeTools(10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tm.CompressAndSelect(tools)
	}
}

func BenchmarkToolManager_CompressAndSelect_50(b *testing.B) {
	tm := gateway.DefaultToolManager()
	tools := makeTools(50)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tm.CompressAndSelect(tools)
	}
}

func BenchmarkToolManager_CompressAndSelect_100(b *testing.B) {
	tm := gateway.DefaultToolManager()
	tools := makeTools(100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tm.CompressAndSelect(tools)
	}
}

// --- Stress Tests ---

func TestToolManager_ConcurrentAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")  // SKIP-OK: #short-mode
	}
	tm := gateway.DefaultToolManager()
	var wg sync.WaitGroup
	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				tools := makeTools(id%30 + 1)
				result := tm.CompressAndSelect(tools)
				if len(result) == 0 && len(tools) > 0 {
					t.Errorf("goroutine %d: got 0 tools for %d input", id, len(tools))
				}
			}
		}(g)
	}
	wg.Wait()
	stats := tm.Stats()
	assert.Equal(t, int64(1000), stats.TotalRequests) // 50*20
}
