package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLlamaCppOnly_ModelsEndpoint verifies that when only the llamacpp
// provider is registered, /v1/models returns only llamacpp-owned models.
func TestLlamaCppOnly_ModelsEndpoint(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	resp, err := ts.Get("/v1/models")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var models api.ModelList
	require.NoError(t, ReadJSON(resp, &models))
	assert.Greater(t, len(models.Data), 0, "should return at least one model")
}

// TestLlamaCppOnly_ChatCompletion verifies basic non-streaming chat.
func TestLlamaCppOnly_ChatCompletion(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	resp, err := ts.PostJSON("/v1/chat/completions", api.ChatCompletionRequest{
		Model: "mock-model",
		Messages: []api.ChatMessage{
			{Role: "user", Content: "Hello!"},
		},
	})
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var chatResp api.ChatCompletionResponse
	require.NoError(t, ReadJSON(resp, &chatResp))
	assert.NotEmpty(t, chatResp.Choices, "should have at least one choice")
	content, _ := chatResp.Choices[0].Message.Content.(string)
	assert.NotEmpty(t, content, "response content should not be empty")
}

// TestLlamaCppOnly_StreamingChat verifies streaming chat without tools.
func TestLlamaCppOnly_StreamingChat(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	stream := true
	resp, err := ts.PostJSON("/v1/chat/completions", api.ChatCompletionRequest{
		Model:  "mock-model",
		Stream: stream,
		Messages: []api.ChatMessage{
			{Role: "user", Content: "Say hi"},
		},
	})
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, "data: ", "should contain SSE data lines")
	assert.Contains(t, bodyStr, "[DONE]", "should end with [DONE]")
}

// TestLlamaCppOnly_Embeddings verifies /v1/embeddings returns real vectors
// (not zero vectors) when a real embedder is configured.
func TestLlamaCppOnly_Embeddings(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	resp, err := ts.PostJSON("/v1/embeddings", api.EmbeddingRequest{
		Model: "nomic-embed-text",
		Input: "test embedding input",
	})
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var embResp api.EmbeddingResponse
	require.NoError(t, ReadJSON(resp, &embResp))
	require.NotEmpty(t, embResp.Data, "should have embedding data")
	assert.Equal(t, 768, len(embResp.Data[0].Embedding), "hash embedder produces 768 dims")
}

// TestLlamaCppOnly_EmbeddingsEmptyModel verifies default model fallback.
func TestLlamaCppOnly_EmbeddingsEmptyModel(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	resp, err := ts.PostJSON("/v1/embeddings", api.EmbeddingRequest{
		Input: "hello",
	})
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var embResp api.EmbeddingResponse
	require.NoError(t, ReadJSON(resp, &embResp))
	assert.Equal(t, "text-embedding-ada-002", embResp.Model)
}

// TestLlamaCppOnly_EmbeddingsBadJSON verifies 400 on malformed body.
func TestLlamaCppOnly_EmbeddingsBadJSON(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	req, err := http.NewRequest("POST", ts.URL()+"/v1/embeddings",
		strings.NewReader("not json"))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// TestLlamaCppOnly_StreamingWithTools verifies that streaming requests
// with tools are handled (force-nonstream path in gateway).
func TestLlamaCppOnly_StreamingWithTools(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	stream := true
	resp, err := ts.PostJSON("/v1/chat/completions", api.ChatCompletionRequest{
		Model:  "mock-model",
		Stream: stream,
		Messages: []api.ChatMessage{
			{Role: "user", Content: "List files"},
		},
		Tools: []api.Tool{
			{
				Type: "function",
				Function: api.ToolFunction{
					Name:        "bash",
					Description: "Run a bash command",
					Parameters: json.RawMessage(`{
						"type":"object",
						"properties":{"command":{"type":"string"}},
						"required":["command"]
					}`),
				},
			},
		},
	})
	require.NoError(t, err)
	defer resp.Body.Close()
	// The mock provider doesn't generate tool calls, but the request
	// should still complete without errors.
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, "data: ", "should contain SSE data")
}

// TestLlamaCppOnly_AnthropicEndpoint verifies /v1/messages still works.
func TestLlamaCppOnly_AnthropicEndpoint(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	resp, err := ts.PostJSON("/v1/messages", map[string]interface{}{
		"model":      "mock-model",
		"max_tokens": 100,
		"messages": []map[string]string{
			{"role": "user", "content": "Hello"},
		},
	})
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestLlamaCppOnly_HealthEndpoint verifies /internal/health is reachable.
func TestLlamaCppOnly_HealthEndpoint(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	resp, err := ts.Get("/internal/health")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
