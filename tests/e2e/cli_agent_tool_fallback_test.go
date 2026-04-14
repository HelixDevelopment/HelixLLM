//go:build !short

package e2e_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// toolCallResponse writes a chat-completion JSON response that contains a
// tool_call in the assistant message, matching what a real provider would
// return when the model decides to invoke a tool.
func toolCallResponse(w http.ResponseWriter, toolName, toolArgs string) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{
		"id":    "test-tool-call",
		"model": "test-model",
		"choices": []map[string]interface{}{
			{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": nil,
					"tool_calls": []map[string]interface{}{
						{
							"id":   "call_abc123",
							"type": "function",
							"function": map[string]interface{}{
								"name":      toolName,
								"arguments": toolArgs,
							},
						},
					},
				},
				"finish_reason": "tool_calls",
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens": 20, "completion_tokens": 10, "total_tokens": 30,
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// TestFallbackChain_ToolCallingFallback verifies that when the primary provider
// returns 429 on a tool-calling request, the chain falls back to the secondary
// which also returns a valid tool_calls response, and that the tool definition
// and tool_choice parameter are forwarded correctly.
func TestFallbackChain_ToolCallingFallback(t *testing.T) {
	t.Parallel()

	// Primary: 429 on every request.
	primarySrv := make429Server()
	defer primarySrv.Close()

	// Secondary: returns a tool_call response.
	secondarySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		// Verify tools were forwarded.
		tools, _ := req["tools"].([]interface{})
		if len(tools) == 0 {
			http.Error(w, "tools not forwarded", http.StatusBadRequest)
			return
		}
		toolCallResponse(w, "read_file", `{"path":"/tmp/test.txt"}`)
	}))
	defer secondarySrv.Close()

	primary := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "tool-primary-429", BaseURL: primarySrv.URL,
		APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})
	secondary := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "tool-secondary-ok", BaseURL: secondarySrv.URL,
		APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})

	providers := map[string]brain.Provider{
		"tool-primary-429":  primary,
		"tool-secondary-ok": secondary,
	}
	rl := fallback.NewRateLimitTracker(1, 10)
	chain := fallback.NewChain(providers, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{
			ProviderName:   "tool-primary-429",
			ModelID:        "test-model",
			Score:          9.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 30*time.Second),
		},
		{
			ProviderName:   "tool-secondary-ok",
			ModelID:        "test-model",
			Score:          7.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 30*time.Second),
		},
	})

	req := &types.InternalChatRequest{
		Model: "test-model",
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "Read the file at /tmp/test.txt"},
		},
		Tools:      []types.InternalTool{toolDef("read_file"), toolDef("write_file")},
		ToolChoice: "auto",
		MaxTokens:  500,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := chain.Complete(ctx, req)
	if err != nil {
		t.Fatalf("chain.Complete: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}

	// The secondary returned a tool_calls finish reason.
	if resp.FinishReason != "tool_calls" {
		t.Errorf("finish_reason = %q, want %q", resp.FinishReason, "tool_calls")
	}

	// Primary must be exhausted.
	entries := chain.Entries()
	if entries[0].Status != fallback.EntryExhausted {
		t.Errorf("primary status = %v, want exhausted", entries[0].Status)
	}
}

// TestFallbackChain_ToolChoiceForwarded verifies that tool_choice="required"
// is forwarded through the chain and the provider receives it.
func TestFallbackChain_ToolChoiceForwarded(t *testing.T) {
	t.Parallel()

	var receivedToolChoice interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		_ = json.Unmarshal(body, &req)
		receivedToolChoice = req["tool_choice"]
		toolCallResponse(w, "list_dir", `{"path":"/tmp"}`)
	}))
	defer srv.Close()

	provider := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "tool-choice-srv", BaseURL: srv.URL,
		APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})
	rl := fallback.NewRateLimitTracker(1, 10)
	chain := fallback.NewChain(map[string]brain.Provider{"tool-choice-srv": provider}, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{
			ProviderName:   "tool-choice-srv",
			ModelID:        "test-model",
			Score:          9.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 30*time.Second),
		},
	})

	req := &types.InternalChatRequest{
		Model:      "test-model",
		Messages:   []types.InternalMessage{{Role: types.RoleUser, Content: "list /tmp"}},
		Tools:      []types.InternalTool{toolDef("list_dir")},
		ToolChoice: "required",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := chain.Complete(ctx, req); err != nil {
		t.Fatalf("chain.Complete: %v", err)
	}

	if receivedToolChoice == nil {
		t.Fatal("tool_choice was not forwarded to provider")
	}
	if tc, ok := receivedToolChoice.(string); !ok || tc != "required" {
		t.Errorf("tool_choice forwarded as %v, want %q", receivedToolChoice, "required")
	}
}

// TestFallbackChain_BothProvidersReturnToolCalls confirms that when both
// primary and secondary return tool_calls responses, the chain uses the first
// successful one (primary, since it is not rate-limited here).
func TestFallbackChain_BothProvidersReturnToolCalls(t *testing.T) {
	t.Parallel()

	makeTCSrv := func(t *testing.T, tool string) *httptest.Server {
		t.Helper()
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			toolCallResponse(w, tool, `{}`)
		}))
	}

	primarySrv := makeTCSrv(t, "primary_tool")
	defer primarySrv.Close()
	secondarySrv := makeTCSrv(t, "secondary_tool")
	defer secondarySrv.Close()

	primary := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "tc-primary", BaseURL: primarySrv.URL,
		APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})
	secondary := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name: "tc-secondary", BaseURL: secondarySrv.URL,
		APIKey: "k", AuthHeader: "Authorization", AuthPrefix: "Bearer ",
	})

	providers := map[string]brain.Provider{"tc-primary": primary, "tc-secondary": secondary}
	rl := fallback.NewRateLimitTracker(1, 10)
	chain := fallback.NewChain(providers, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{ProviderName: "tc-primary", ModelID: "gpt-4", Score: 9.0, Status: fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 30*time.Second)},
		{ProviderName: "tc-secondary", ModelID: "gpt-4", Score: 7.0, Status: fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 30*time.Second)},
	})

	req := &types.InternalChatRequest{
		Model:    "m",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "use a tool"}},
		Tools:    []types.InternalTool{toolDef("primary_tool"), toolDef("secondary_tool")},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := chain.Complete(ctx, req)
	if err != nil {
		t.Fatalf("chain.Complete: %v", err)
	}

	// Primary is healthy so it answers first.
	if resp.FinishReason != "tool_calls" {
		t.Errorf("finish_reason = %q, want %q", resp.FinishReason, "tool_calls")
	}

	// Primary entry must remain active (no errors).
	entries := chain.Entries()
	if entries[0].Status != fallback.EntryActive {
		t.Errorf("primary status = %v, want active", entries[0].Status)
	}
}
