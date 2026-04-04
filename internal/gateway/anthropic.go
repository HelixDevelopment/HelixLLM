package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

// anthropicSSEWriter writes Anthropic-format SSE events.
// Anthropic SSE uses named events:
//
//	event: <type>
//	data: <json>
//	(blank line)
type anthropicSSEWriter struct {
	c *gin.Context
}

func newAnthropicSSEWriter(c *gin.Context) *anthropicSSEWriter {
	return &anthropicSSEWriter{c: c}
}

// writeHeader sets the required SSE headers and status 200.
func (w *anthropicSSEWriter) writeHeader() {
	w.c.Header("Content-Type", "text/event-stream")
	w.c.Header("Cache-Control", "no-cache")
	w.c.Header("Connection", "keep-alive")
	w.c.Header("X-Accel-Buffering", "no")
	w.c.Status(200)
}

// writeEvent emits a named SSE event.
func (w *anthropicSSEWriter) writeEvent(eventType string, data interface{}) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	fmt.Fprintf(w.c.Writer, "event: %s\ndata: %s\n\n", eventType, jsonBytes)
	w.c.Writer.Flush()
	return nil
}

// HandleMessages handles POST /v1/messages (Anthropic-compatible).
//
// Stub behaviour:
//   - stream=false → returns a single canned MessageResponse.
//   - stream=true  → emits the full Anthropic SSE event sequence then stops.
func HandleMessages(c *gin.Context) {
	var req api.MessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: fmt.Sprintf("invalid request body: %v", err),
				Type:    "invalid_request_error",
			},
		})
		return
	}

	model := req.Model
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	id := "msg-helix-" + randomID()

	if req.Stream {
		streamMessages(c, id, model)
		return
	}

	stopReason := "end_turn"
	c.JSON(http.StatusOK, api.MessageResponse{
		ID:   id,
		Type: "message",
		Role: "assistant",
		Content: []api.ContentBlock{
			{Type: "text", Text: "Hello! I'm HelixLLM."},
		},
		Model:      model,
		StopReason: &stopReason,
		Usage: api.AnthropicUsage{
			InputTokens:  10,
			OutputTokens: 6,
		},
	})
}

// streamMessages writes the Anthropic SSE event sequence for a canned response.
func streamMessages(c *gin.Context, id, model string) {
	w := newAnthropicSSEWriter(c)
	w.writeHeader()

	stopReason := "end_turn"

	// message_start
	w.writeEvent("message_start", api.MessageStreamEvent{ //nolint:errcheck
		Type: "message_start",
		Message: &api.MessageResponse{
			ID:         id,
			Type:       "message",
			Role:       "assistant",
			Content:    []api.ContentBlock{},
			Model:      model,
			StopReason: nil,
			Usage:      api.AnthropicUsage{InputTokens: 10, OutputTokens: 0},
		},
	})

	// content_block_start
	idx := 0
	w.writeEvent("content_block_start", api.MessageStreamEvent{ //nolint:errcheck
		Type:  "content_block_start",
		Index: &idx,
		ContentBlock: &api.ContentBlock{
			Type: "text",
			Text: "",
		},
	})

	// content_block_delta events (3 tokens)
	tokens := []string{"Hello", "! I'm", " HelixLLM."}
	for _, token := range tokens {
		deltaText := token
		w.writeEvent("content_block_delta", api.MessageStreamEvent{ //nolint:errcheck
			Type:  "content_block_delta",
			Index: &idx,
			Delta: &api.StreamDelta{
				Type: "text_delta",
				Text: deltaText,
			},
		})
	}

	// content_block_stop
	w.writeEvent("content_block_stop", api.MessageStreamEvent{ //nolint:errcheck
		Type:  "content_block_stop",
		Index: &idx,
	})

	// message_delta (stop reason)
	w.writeEvent("message_delta", api.MessageStreamEvent{ //nolint:errcheck
		Type: "message_delta",
		Delta: &api.StreamDelta{
			Type:       "message_delta",
			StopReason: &stopReason,
		},
		Usage: &api.AnthropicUsage{OutputTokens: 6},
	})

	// message_stop
	w.writeEvent("message_stop", map[string]string{ //nolint:errcheck
		"type": "message_stop",
	})

	// Anthropic stream ends without a [DONE] sentinel; a final ping is optional.
	fmt.Fprintf(c.Writer, "event: ping\ndata: {\"type\":\"ping\"}\n\n")
	c.Writer.Flush()
}

