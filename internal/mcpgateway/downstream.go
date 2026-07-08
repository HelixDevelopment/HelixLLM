// Package mcpgateway exposes HelixLLM's local capabilities as MCP
// tools/resources over Streamable-HTTP, per the GO'd design memo at
// docs/research/07.2026/05_mcp_acp_protocols/MCP_OKF_GATEWAY_MEMO.md.
//
// The gateway NEVER starts, stops, or restarts the live coder it fronts
// (§11.4.119 / §11.4.122 — the coder is a read-only downstream dependency,
// exactly like the sibling internal/a2a package). All host/port/model/
// credential values are config-injected via environment variables
// (CONST-045/046) — nothing is hardcoded in this file.
package mcpgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CoderClient is a REAL HTTP client against the live HelixLLM coder's
// OpenAI-compatible endpoints (BLUFF-001, CLAUDE.md §3.3 — no simulation).
type CoderClient struct {
	BaseURL    string // e.g. "http://localhost:18434" (NO trailing /v1)
	HTTPClient *http.Client
}

// NewCoderClient constructs a client with a bounded timeout.
func NewCoderClient(baseURL string, timeout time.Duration) *CoderClient {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &CoderClient{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: timeout},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

type chatChoice struct {
	Message chatMessage `json:"message"`
}

type chatCompletionResponse struct {
	Choices []chatChoice `json:"choices"`
	Model   string       `json:"model"`
}

// ModelsResponse mirrors the OpenAI-compatible /v1/models `data[].id` list.
type ModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// ListModels performs a REAL GET against the coder's /v1/models route.
func (c *CoderClient) ListModels(ctx context.Context) ([]string, error) {
	url := c.BaseURL + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building /v1/models request: %w", err)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s failed: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading /v1/models response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("coder /v1/models returned status %d: %s", resp.StatusCode, string(body))
	}
	var parsed ModelsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parsing /v1/models response: %w", err)
	}
	ids := make([]string, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// GenerateResult is the real completion returned from the coder.
type GenerateResult struct {
	ModelID string
	Content string
}

// Generate performs a REAL chat-completion round-trip against the coder's
// /v1/chat/completions route — never a simulated/canned response
// (BLUFF-001).
func (c *CoderClient) Generate(ctx context.Context, prompt string, maxTokens int) (*GenerateResult, error) {
	if prompt == "" {
		return nil, fmt.Errorf("prompt must not be empty")
	}
	if maxTokens <= 0 {
		maxTokens = 256
	}

	// Discover the live model id rather than hardcoding one (CONST-036 —
	// LLMsVerifier/provider registry is the source of truth; here the
	// coder's own /v1/models is that live source).
	models, err := c.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("discovering coder model id: %w", err)
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("coder /v1/models returned no models")
	}
	modelID := models[0]

	reqBody := chatCompletionRequest{
		Model: modelID,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens: maxTokens,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling chat-completion request: %w", err)
	}

	url := c.BaseURL + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("building chat-completion request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("POST %s failed: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading chat-completion response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("coder /v1/chat/completions returned status %d: %s", resp.StatusCode, string(body))
	}

	var parsed chatCompletionResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parsing chat-completion response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("coder returned zero choices")
	}

	respModel := parsed.Model
	if respModel == "" {
		respModel = modelID
	}
	return &GenerateResult{
		ModelID: respModel,
		Content: parsed.Choices[0].Message.Content,
	}, nil
}
