package gateway_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/gin-gonic/gin"
)

func TestGatewayRouter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	gateway.RegisterRoutes(r, gateway.RouterOptions{})

	// Test models endpoint
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models", nil)
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("GET /v1/models status = %d, want 200", w.Code)
	}

	// Test chat completions
	body, _ := json.Marshal(api.ChatCompletionRequest{
		Model: "test", Messages: []api.ChatMessage{{Role: "user", Content: "hi"}},
	})
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("POST /v1/chat/completions status = %d, want 200", w.Code)
	}

	// Test Anthropic messages
	abody, _ := json.Marshal(api.MessageRequest{
		Model: "test", Messages: []api.AnthropicMessage{{Role: "user", Content: "hi"}}, MaxTokens: 100,
	})
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(abody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("POST /v1/messages status = %d, want 200", w.Code)
	}
}

func TestGatewayRouterWithAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	gateway.RegisterRoutes(r, gateway.RouterOptions{APIKeys: "sk-test"})

	// Without key
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models", nil)
	r.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Errorf("no key: status = %d, want 401", w.Code)
	}

	// With key
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer sk-test")
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("with key: status = %d, want 200", w.Code)
	}
}
