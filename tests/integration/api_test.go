package integration

import (
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

func TestIntegration_HealthEndpoint(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	resp, err := ts.Get("/internal/health")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var result map[string]interface{}
	if err := ReadJSON(resp, &result); err != nil {
		t.Fatalf("ReadJSON error: %v", err)
	}
	if result["status"] != "healthy" {
		t.Errorf("health status = %v, want healthy", result["status"])
	}
}

// ---------------------------------------------------------------------------
// OpenAI endpoints
// ---------------------------------------------------------------------------

func TestIntegration_OpenAI_ChatCompletions(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	resp, err := ts.PostJSON("/v1/chat/completions", api.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []api.ChatMessage{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	if resp.StatusCode != 200 {
		body, _ := ReadBody(resp)
		t.Fatalf("status = %d, want 200; body = %s", resp.StatusCode, body)
	}

	var result api.ChatCompletionResponse
	if err := ReadJSON(resp, &result); err != nil {
		t.Fatalf("ReadJSON error: %v", err)
	}
	if result.Object != "chat.completion" {
		t.Errorf("object = %q, want chat.completion", result.Object)
	}
	if len(result.Choices) == 0 {
		t.Fatal("no choices returned")
	}
	if result.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason = %q, want stop", result.Choices[0].FinishReason)
	}
}

func TestIntegration_OpenAI_ChatCompletionsStreaming(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	resp, err := ts.PostJSON("/v1/chat/completions", api.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []api.ChatMessage{{Role: "user", Content: "Hello"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("request error: %v", err)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	body, err := ReadBody(resp)
	if err != nil {
		t.Fatalf("ReadBody error: %v", err)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Error("stream missing [DONE] sentinel")
	}
	if !strings.Contains(body, "chat.completion.chunk") {
		t.Error("stream missing chat.completion.chunk object")
	}
}

func TestIntegration_OpenAI_ListModels(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	resp, err := ts.Get("/v1/models")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result api.ModelList
	if err := ReadJSON(resp, &result); err != nil {
		t.Fatalf("ReadJSON error: %v", err)
	}
	if result.Object != "list" {
		t.Errorf("object = %q, want list", result.Object)
	}
	if len(result.Data) == 0 {
		t.Error("no models returned")
	}
}

func TestIntegration_OpenAI_Embeddings(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	resp, err := ts.PostJSON("/v1/embeddings", api.EmbeddingRequest{
		Model: "text-embedding-ada-002",
		Input: "Hello world",
	})
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result api.EmbeddingResponse
	if err := ReadJSON(resp, &result); err != nil {
		t.Fatalf("ReadJSON error: %v", err)
	}
	if result.Object != "list" {
		t.Errorf("object = %q, want list", result.Object)
	}
	if len(result.Data) == 0 {
		t.Error("no embeddings returned")
	}
}

// ---------------------------------------------------------------------------
// Anthropic endpoints
// ---------------------------------------------------------------------------

func TestIntegration_Anthropic_Messages(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	resp, err := ts.PostJSON("/v1/messages", api.MessageRequest{
		Model:     "claude-sonnet-4-20250514",
		Messages:  []api.AnthropicMessage{{Role: "user", Content: "Hello"}},
		MaxTokens: 1024,
	})
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	if resp.StatusCode != 200 {
		body, _ := ReadBody(resp)
		t.Fatalf("status = %d, want 200; body = %s", resp.StatusCode, body)
	}

	var result api.MessageResponse
	if err := ReadJSON(resp, &result); err != nil {
		t.Fatalf("ReadJSON error: %v", err)
	}
	if result.Type != "message" {
		t.Errorf("type = %q, want message", result.Type)
	}
	if result.Role != "assistant" {
		t.Errorf("role = %q, want assistant", result.Role)
	}
	if len(result.Content) == 0 {
		t.Error("no content blocks returned")
	}
}

func TestIntegration_Anthropic_MessagesStreaming(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	resp, err := ts.PostJSON("/v1/messages", api.MessageRequest{
		Model:     "claude-sonnet-4-20250514",
		Messages:  []api.AnthropicMessage{{Role: "user", Content: "Hello"}},
		MaxTokens: 1024,
		Stream:    true,
	})
	if err != nil {
		t.Fatalf("request error: %v", err)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	body, err := ReadBody(resp)
	if err != nil {
		t.Fatalf("ReadBody error: %v", err)
	}
	if !strings.Contains(body, "event: message_start") {
		t.Error("stream missing message_start event")
	}
	if !strings.Contains(body, "event: message_stop") {
		t.Error("stream missing message_stop event")
	}
	if !strings.Contains(body, "event: content_block_delta") {
		t.Error("stream missing content_block_delta event")
	}
}

// ---------------------------------------------------------------------------
// Knowledge endpoints (ingest + query round-trip)
// ---------------------------------------------------------------------------

func TestIntegration_Knowledge_IngestAndQuery(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	// Step 1: Ingest a document.
	ingestResp, err := ts.PostJSON("/internal/knowledge/ingest", map[string]interface{}{
		"title":      "Test Document",
		"content":    "HelixLLM is a distributed LLM gateway that routes requests to multiple providers including OpenAI, Anthropic, and local llama.cpp instances.",
		"collection": "integration-test",
		"source":     "integration-test",
	})
	if err != nil {
		t.Fatalf("ingest request error: %v", err)
	}
	if ingestResp.StatusCode != 200 {
		body, _ := ReadBody(ingestResp)
		t.Fatalf("ingest status = %d, want 200; body = %s", ingestResp.StatusCode, body)
	}

	var ingestResult map[string]interface{}
	if err := ReadJSON(ingestResp, &ingestResult); err != nil {
		t.Fatalf("ReadJSON error: %v", err)
	}
	if ingestResult["document_id"] == nil || ingestResult["document_id"] == "" {
		t.Error("ingest did not return a document_id")
	}

	// Step 2: Query the collection.
	queryResp, err := ts.PostJSON("/internal/knowledge/query", map[string]interface{}{
		"query":      "distributed LLM gateway",
		"collection": "integration-test",
		"top_k":      3,
	})
	if err != nil {
		t.Fatalf("query request error: %v", err)
	}
	if queryResp.StatusCode != 200 {
		body, _ := ReadBody(queryResp)
		t.Fatalf("query status = %d, want 200; body = %s", queryResp.StatusCode, body)
	}

	var queryResult map[string]interface{}
	if err := ReadJSON(queryResp, &queryResult); err != nil {
		t.Fatalf("ReadJSON error: %v", err)
	}
	if queryResult["context"] == nil || queryResult["context"] == "" {
		t.Error("query did not return context")
	}
}

// ---------------------------------------------------------------------------
// Agents
// ---------------------------------------------------------------------------

func TestIntegration_Agents_Chat(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	resp, err := ts.PostJSON("/v1/agents/chat", agents.AgentChatRequest{
		Messages: []types.InternalMessage{
			{Role: types.RoleUser, Content: "Hello, what time is it?"},
		},
	})
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	if resp.StatusCode != 200 {
		body, _ := ReadBody(resp)
		t.Fatalf("status = %d, want 200; body = %s", resp.StatusCode, body)
	}

	var result agents.AgentChatResponse
	if err := ReadJSON(resp, &result); err != nil {
		t.Fatalf("ReadJSON error: %v", err)
	}
	if result.Response.Message.Content == "" {
		t.Error("agent returned empty content")
	}
}

func TestIntegration_Agents_Tools(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	resp, err := ts.Get("/v1/agents/tools")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := ReadJSON(resp, &result); err != nil {
		t.Fatalf("ReadJSON error: %v", err)
	}
	toolsList, ok := result["tools"].([]interface{})
	if !ok {
		t.Fatal("tools field is not an array")
	}
	if len(toolsList) < 2 {
		t.Errorf("expected at least 2 tools, got %d", len(toolsList))
	}
}

// ---------------------------------------------------------------------------
// Control
// ---------------------------------------------------------------------------

func TestIntegration_Control_Status(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	resp, err := ts.Get("/internal/cluster/status")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := ReadJSON(resp, &result); err != nil {
		t.Fatalf("ReadJSON error: %v", err)
	}
	// Cluster with no real hosts should report healthy by default.
	if result["healthy"] != true {
		t.Errorf("healthy = %v, want true", result["healthy"])
	}
}
