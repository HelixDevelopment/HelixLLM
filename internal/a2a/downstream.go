package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Downstream calls the live HelixLLM coder's OpenAI-compatible
// /v1/chat/completions route. BaseURL and ModelID are config-injected
// (CONST-045/046) — never hardcoded. This is a REAL HTTP client: it makes
// genuine network calls to the running coder, never a simulated response
// (BLUFF-001, CLAUDE.md §3.3).
type Downstream struct {
	BaseURL string // e.g. "http://localhost:18434" (NO trailing /v1 — see
	// docs/research/07.2026/00_master/ACP_A2A_PROVIDER.md §2.2 endpoint
	// gotcha: BaseURL never carries /v1 itself, this client appends it).
	ModelID    string
	HTTPClient *http.Client
}

// NewDownstream constructs a client with a bounded timeout — code generation
// on the CPU/GPU coder is not instantaneous, but must not hang the A2A
// request forever.
func NewDownstream(baseURL, modelID string, timeout time.Duration) *Downstream {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Downstream{
		BaseURL:    baseURL,
		ModelID:    modelID,
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
}

// modelsResponse mirrors the subset of /v1/models this client reads (an
// OpenAI-compatible `data[].id` list) — used once at startup to discover the
// REAL served model id, never a hardcoded guess (CONST-036/040 spirit).
type modelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// DiscoverModelID queries the downstream coder's own /v1/models and returns
// the first reported model id. Returns an error (never a fabricated id) if
// the coder is unreachable or reports no models.
func (d *Downstream) DiscoverModelID(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.BaseURL+"/v1/models", nil)
	if err != nil {
		return "", err
	}
	resp, err := d.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET %s/v1/models: %w", d.BaseURL, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s/v1/models: status=%d body=%s", d.BaseURL, resp.StatusCode, string(body))
	}
	var mr modelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return "", fmt.Errorf("parse /v1/models response: %w", err)
	}
	if len(mr.Data) == 0 || mr.Data[0].ID == "" {
		return "", fmt.Errorf("downstream coder reported zero models")
	}
	return mr.Data[0].ID, nil
}

// Generate performs a real chat-completion call and returns the model's
// text content. Errors are returned verbatim (never swallowed into a fake
// success) so the caller can fail the Task honestly.
func (d *Downstream) Generate(ctx context.Context, prompt string, maxTokens int) (string, error) {
	reqBody, err := json.Marshal(chatCompletionRequest{
		Model:     d.ModelID,
		Messages:  []chatMessage{{Role: "user", Content: prompt}},
		MaxTokens: maxTokens,
	})
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		d.BaseURL+"/v1/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := d.HTTPClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("POST %s/v1/chat/completions: %w", d.BaseURL, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("POST %s/v1/chat/completions: status=%d body=%s", d.BaseURL, resp.StatusCode, string(body))
	}
	var cr chatCompletionResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return "", fmt.Errorf("parse chat completion response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("downstream returned zero choices")
	}
	return cr.Choices[0].Message.Content, nil
}
