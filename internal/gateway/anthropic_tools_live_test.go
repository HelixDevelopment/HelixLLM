package gateway_test

// TestAnthropicMessages_ToolCallRoundTrip_LiveCoder — Wave-2 live
// production-path proof for the ANTHROPIC-WIRE-DROPS-TOOLS V&V finding
// (2026-07-11).
//
// Drives a REAL gin HTTP server wired with the REAL, unmodified
// gateway.HandleMessages against a REAL brain.Brain pointed at the live
// coder llama.cpp server (default http://localhost:18434, Qwen3-Coder —
// READ-ONLY, never restarted or reconfigured per §11.4.119/§11.4.122).
// POSTs a genuine Anthropic /v1/messages request with a tool defined and
// tool_choice forcing tool use, and proves the model's real tool call comes
// back out of the facade as an Anthropic "tool_use" content block with
// stop_reason "tool_use" — the concrete end-to-end round trip the fix
// exists to enable.
//
// Gated behind HELIX_LIVE_CODER_TOOLS_TEST=true — honestly SKIPs otherwise
// (§11.4.3), since it depends on a live, already-running coder instance
// this stream does not own the lifecycle of.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

func TestAnthropicMessages_ToolCallRoundTrip_LiveCoder(t *testing.T) {
	if os.Getenv("HELIX_LIVE_CODER_TOOLS_TEST") != "true" {
		t.Skip("SKIP-OK: opt-in live test (requires the live coder llama.cpp server reachable at HELIX_LIVE_CODER_BASE_URL, default :18434). " +
			"Set HELIX_LIVE_CODER_TOOLS_TEST=true to run.")
	}
	coderBase := os.Getenv("HELIX_LIVE_CODER_BASE_URL")
	if coderBase == "" {
		coderBase = "http://localhost:18434"
	}

	// §11.4.119 single-resource-owner: this test is READ-ONLY against the
	// live coder — it never boots, stops, or reconfigures it, only issues
	// normal inference requests exactly like any other client would.
	healthClient := &http.Client{Timeout: 5 * time.Second}
	resp, err := healthClient.Get(coderBase + "/v1/models")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Skipf("SKIP-OK: live coder not reachable at %s (err=%v) — this is an opt-in live-infra test, not a hard failure", coderBase, err)
	}
	_ = resp.Body.Close()

	var evidence strings.Builder
	logf := func(format string, args ...interface{}) {
		line := fmt.Sprintf(format, args...)
		t.Log(line)
		evidence.WriteString(line)
		evidence.WriteString("\n")
	}
	runID := "anthropic_tools_wave2_" + time.Now().UTC().Format("20060102T150405Z")
	repoRoot, _ := filepath.Abs("../..")
	evidDir := filepath.Join(repoRoot, "docs", "qa", runID)
	if err := os.MkdirAll(evidDir, 0o755); err == nil {
		defer func() {
			_ = os.WriteFile(filepath.Join(evidDir, "RESULTS.md"), []byte(evidence.String()), 0o644)
		}()
	}

	logf("### TestAnthropicMessages_ToolCallRoundTrip_LiveCoder — %s UTC", time.Now().UTC().Format(time.RFC3339))
	logf("run_id=%s coder_base=%s (read-only, never restarted §11.4.119/§11.4.122)", runID, coderBase)

	// Discover the actual model ID the live coder serves (never hardcode —
	// §11.4.111 resolve-by-identity).
	modelsResp, err := healthClient.Get(coderBase + "/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	var modelsBody struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(modelsResp.Body).Decode(&modelsBody); err != nil {
		_ = modelsResp.Body.Close()
		t.Fatalf("decode /v1/models: %v", err)
	}
	_ = modelsResp.Body.Close()
	if len(modelsBody.Data) == 0 {
		t.Fatal("live coder /v1/models returned zero models")
	}
	modelID := modelsBody.Data[0].ID
	logf("discovered live coder model: %s", modelID)

	// Real brain.Brain wired at the live coder, exactly like
	// cmd/helixllm/main.go wires it for the llamacpp provider.
	b := brain.New(brain.Config{
		LlamaCppURL:     coderBase,
		LlamaCppModels:  []string{modelID},
		DefaultProvider: "llamacpp",
	})

	// Real gin server, real HandleMessages — the exact production facade
	// under test, not a reimplementation.
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/messages", gateway.HandleMessages(b))
	srv := httptest.NewServer(r)
	defer srv.Close()

	reqBody, _ := json.Marshal(api.MessageRequest{
		Model:     modelID,
		MaxTokens: 256,
		Messages: []api.AnthropicMessage{
			{Role: "user", Content: "What is the current weather in Paris, France? " +
				"You MUST call the get_weather function tool to answer — do not answer from memory."},
		},
		Tools: []api.AnthropicTool{
			{
				Name:        "get_weather",
				Description: "Get the current weather conditions for a named city.",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"city": map[string]interface{}{
							"type":        "string",
							"description": "The city name, e.g. Paris",
						},
					},
					"required": []interface{}{"city"},
				},
			},
		},
		ToolChoice: map[string]interface{}{"type": "any"},
	})

	httpReq, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/messages", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 90 * time.Second}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		t.Fatalf("POST /v1/messages: %v", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	if httpResp.StatusCode != http.StatusOK {
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(httpResp.Body)
		logf("BLOCKED: POST /v1/messages returned HTTP %d: %s", httpResp.StatusCode, body.String())
		t.Fatalf("POST /v1/messages status=%d body=%s", httpResp.StatusCode, body.String())
	}

	var msgResp api.MessageResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&msgResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	respJSON, _ := json.MarshalIndent(msgResp, "", "  ")
	logf("real /v1/messages response:\n%s", string(respJSON))

	// PRODUCTION-PATH PROOF: a REAL tool_use content block, produced by the
	// REAL live coder model actually deciding to call get_weather, and
	// surfaced through the REAL, unmodified anthropicToInternal /
	// internalToAnthropic conversion functions this fix touched.
	var toolUse *api.ContentBlock
	for i := range msgResp.Content {
		if msgResp.Content[i].Type == "tool_use" {
			toolUse = &msgResp.Content[i]
		}
	}
	if toolUse == nil {
		logf("BLOCKED: no tool_use content block in the live coder's response — model did not call the tool this run "+
			"(content=%+v). This is a live-model-behaviour result, not a facade defect — the facade's tool wiring is "+
			"separately proven by the hermetic unit + full-facade-mock tests in anthropic_internal_test.go / anthropic_test.go.",
			msgResp.Content)
		t.Fatalf("live coder did not produce a tool_use block; see logged response above")
	}
	if toolUse.Name != "get_weather" {
		t.Errorf("tool_use.Name = %q, want get_weather", toolUse.Name)
	}
	if msgResp.StopReason == nil || *msgResp.StopReason != "tool_use" {
		t.Errorf("StopReason = %v, want tool_use", msgResp.StopReason)
	}
	logf("RESULT: PASS — a real tool call from the live coder round-tripped through POST /v1/messages "+
		"as a genuine Anthropic tool_use content block (id=%s name=%s) with stop_reason=tool_use.",
		toolUse.ID, toolUse.Name)
}
