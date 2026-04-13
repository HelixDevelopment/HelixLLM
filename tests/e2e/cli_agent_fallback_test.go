//go:build !short

// Package e2e_test contains end-to-end tests for HelixLLM's fallback chain.
// These tests use httptest servers to simulate the request patterns that the
// 48 HelixAgent CLI agents (OpenCode, Crush, HelixCode, KiloCode, + 44 generic)
// would produce when routing through HelixAgent → HelixLLM.
package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

func init() {
	// Honour the project-wide resource limit: cap parallelism at 2 logical CPUs.
	if runtime.GOMAXPROCS(0) > 2 {
		runtime.GOMAXPROCS(2)
	}
}

// makeOKServer returns an httptest.Server that always responds with a valid
// chat-completion JSON body.  The response content is set to content so tests
// can verify which server answered.
func makeOKServer(t *testing.T, content string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"id":    "test-ok",
			"model": "test-model",
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": content,
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Logf("makeOKServer encode: %v", err)
		}
	}))
}

// make429Server returns an httptest.Server that always responds with HTTP 429.
func make429Server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-ratelimit-remaining-requests", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limit exceeded","type":"rate_limit_error"}}`))
	}))
}

// buildChain creates a two-entry fallback Chain: primary uses primarySrv and
// secondary uses secondarySrv.  Both entries get a fresh CircuitBreaker.
func buildChain(t *testing.T, primarySrv, secondarySrv *httptest.Server) *fallback.Chain {
	t.Helper()
	primary := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name:       "primary",
		BaseURL:    primarySrv.URL,
		APIKey:     "test-key",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	})
	secondary := brain.NewOpenAICompat(brain.OpenAICompatConfig{
		Name:       "secondary",
		BaseURL:    secondarySrv.URL,
		APIKey:     "test-key",
		AuthHeader: "Authorization",
		AuthPrefix: "Bearer ",
	})

	providers := map[string]brain.Provider{
		"primary":   primary,
		"secondary": secondary,
	}
	rl := fallback.NewRateLimitTracker(1, 10)
	chain := fallback.NewChain(providers, rl)
	chain.SetEntries([]fallback.ChainEntry{
		{
			ProviderName:   "primary",
			ModelID:        "test-model",
			Score:          9.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 30*time.Second),
		},
		{
			ProviderName:   "secondary",
			ModelID:        "test-model",
			Score:          7.0,
			Status:         fallback.EntryActive,
			CircuitBreaker: fallback.NewCircuitBreaker(3, 30*time.Second),
		},
	})
	return chain
}

// toolDef builds a minimal InternalTool with the given name.
func toolDef(name string) types.InternalTool {
	return types.InternalTool{
		Type: "function",
		Function: map[string]interface{}{
			"name":        name,
			"description": fmt.Sprintf("tool %s for testing", name),
			"parameters": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

// TestFallbackChain_AgentRequestPatterns verifies that the fallback Chain
// correctly handles the diverse request patterns that HelixAgent's 48 CLI
// agents would produce: simple chat, tool calling, streaming, varying token
// limits, and empty model IDs.
//
// Each sub-test creates its own httptest servers and Chain so there is no
// shared mutable state between parallel sub-tests.
func TestFallbackChain_AgentRequestPatterns(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		model     string
		hasTools  bool
		maxTokens int
		withSys   bool
	}{
		{"simple_chat", "auto", false, 1000, false},
		{"tool_calling", "auto", true, 2000, false},
		// streaming patterns covered in cli_agent_streaming_fallback_test.go
		{"high_token_limit", "auto", false, 8000, false},
		{"low_token_limit", "auto", false, 100, false},
		{"empty_model_uses_chain_default", "", false, 1000, false},
		{"multi_tool_request", "auto", true, 4000, false},
		{"system_message_included", "auto", false, 1000, true},
	}

	for _, tc := range testCases {
		tc := tc // capture
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Each sub-test owns its own servers and chain — no shared state.
			primarySrv := makeOKServer(t, "agent-pattern-ok")
			defer primarySrv.Close()
			secondarySrv := make429Server()
			defer secondarySrv.Close()
			chain := buildChain(t, primarySrv, secondarySrv)

			messages := []types.InternalMessage{
				{Role: types.RoleUser, Content: "test message for " + tc.name},
			}
			if tc.withSys {
				messages = append([]types.InternalMessage{
					{Role: types.RoleSystem, Content: "You are a helpful CLI agent."},
				}, messages...)
			}

			req := &types.InternalChatRequest{
				Model:     tc.model,
				Messages:  messages,
				MaxTokens: tc.maxTokens,
			}
			if tc.hasTools {
				req.Tools = []types.InternalTool{
					toolDef("read_file"),
					toolDef("write_file"),
				}
				req.ToolChoice = "auto"
			}

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			resp, err := chain.Complete(ctx, req)
			if err != nil {
				t.Fatalf("chain.Complete(%s): unexpected error: %v", tc.name, err)
			}
			if resp == nil {
				t.Fatalf("chain.Complete(%s): nil response", tc.name)
			}
			if resp.Message.Content == "" {
				t.Errorf("chain.Complete(%s): empty response content", tc.name)
			}
		})
	}
}

// TestFallbackChain_AgentRequestPatterns_FallsBackOnPrimary429 checks that
// when the primary is rate-limited (429) the chain falls back to the secondary
// and returns a valid response — same fallback path all 48 CLI agents would
// experience during provider exhaustion.
func TestFallbackChain_AgentRequestPatterns_FallsBackOnPrimary429(t *testing.T) {
	t.Parallel()

	primarySrv := make429Server()
	defer primarySrv.Close()
	secondarySrv := makeOKServer(t, "fallback-agent-response")
	defer secondarySrv.Close()

	chain := buildChain(t, primarySrv, secondarySrv)

	req := &types.InternalChatRequest{
		Model:    "auto",
		Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "hello from agent"}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := chain.Complete(ctx, req)
	if err != nil {
		t.Fatalf("chain.Complete: %v", err)
	}
	if resp.Message.Content != "fallback-agent-response" {
		t.Errorf("expected secondary content %q, got %q",
			"fallback-agent-response", resp.Message.Content)
	}

	// Primary must now be marked exhausted.
	entries := chain.Entries()
	if len(entries) < 1 {
		t.Fatal("expected at least one entry")
	}
	if entries[0].Status != fallback.EntryExhausted {
		t.Errorf("primary entry status = %v, want exhausted", entries[0].Status)
	}
}
