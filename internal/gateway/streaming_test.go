package gateway_test

import (
	"bufio"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/gin-gonic/gin"
)

func TestSSEWriter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/stream", func(c *gin.Context) {
		w := gateway.NewSSEWriter(c)
		w.WriteHeader()

		chunk := api.ChatCompletionChunk{
			ID:     "chatcmpl-123",
			Object: "chat.completion.chunk",
			Model:  "test-model",
			Choices: []api.ChatCompletionChunkChoice{
				{Index: 0, Delta: api.ChatMessageDelta{Content: "Hello"}},
			},
		}
		w.WriteEvent(chunk) //nolint:errcheck
		w.WriteDone()
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/stream", nil)
	r.ServeHTTP(rec, req)

	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", rec.Header().Get("Content-Type"))
	}

	body := rec.Body.String()
	_ = bufio.NewScanner(strings.NewReader(body)) // ensure bufio import is used
	lines := strings.Split(body, "\n")

	// Find data lines
	var dataLines []string
	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		}
	}

	if len(dataLines) < 2 {
		t.Fatalf("expected at least 2 data lines, got %d", len(dataLines))
	}

	// First should be JSON chunk
	var chunk api.ChatCompletionChunk
	if err := json.Unmarshal([]byte(dataLines[0]), &chunk); err != nil {
		t.Fatalf("first data line is not valid JSON: %v", err)
	}
	if chunk.ID != "chatcmpl-123" {
		t.Errorf("chunk ID = %q, want chatcmpl-123", chunk.ID)
	}

	// Last should be [DONE]
	if dataLines[len(dataLines)-1] != "[DONE]" {
		t.Errorf("last data line = %q, want [DONE]", dataLines[len(dataLines)-1])
	}
}
