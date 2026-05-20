package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/i18n"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
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
// When b is non-nil it delegates to the Completer; otherwise returns a development fallback (no backend configured).
func HandleMessages(b Completer) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req api.MessageRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, api.ErrorResponse{
				Error: api.ErrorDetail{
					Message: tr(c, i18n.KeyGatewayInvalidRequestBody,
						map[string]string{"detail": err.Error()}),
					Type: "invalid_request_error",
				},
			})
			return
		}

		model := req.Model
		if model == "" {
			model = "claude-sonnet-4-20250514"
		}

		id := "msg-helix-" + randomID()

		if b != nil {
			internalReq := anthropicToInternal(&req)

			if req.Stream {
				ch, err := b.CompleteStream(c.Request.Context(), internalReq)
				if err != nil {
					c.JSON(http.StatusInternalServerError, api.ErrorResponse{
						Error: api.ErrorDetail{
							Message: tr(c, i18n.KeyGatewayBrainStreamError,
								map[string]string{"detail": err.Error()}),
							Type: "server_error",
						},
					})
					return
				}
				streamBrainMessages(c, id, model, ch)
				return
			}

			resp, err := b.Complete(c.Request.Context(), internalReq)
			if err != nil {
				c.JSON(http.StatusInternalServerError, api.ErrorResponse{
					Error: api.ErrorDetail{
						Message: tr(c, i18n.KeyGatewayBrainError,
							map[string]string{"detail": err.Error()}),
						Type: "server_error",
					},
				})
				return
			}
			c.JSON(http.StatusOK, internalToAnthropic(resp, id, model))
			return
		}

		// Development fallback (no Brain configured).
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
				{Type: "text", Text: tr(c, i18n.KeyGatewayGreeting)},
			},
			Model:      model,
			StopReason: &stopReason,
			Usage: api.AnthropicUsage{
				InputTokens:  10,
				OutputTokens: 6,
			},
		})
	}
}

// streamBrainMessages writes the Anthropic SSE event sequence for Brain-sourced chunks.
func streamBrainMessages(c *gin.Context, id, model string, ch <-chan types.StreamChunk) {
	w := newAnthropicSSEWriter(c)
	w.writeHeader()

	// message_start
	w.writeEvent("message_start", api.MessageStreamEvent{ //nolint:errcheck
		Type: "message_start",
		Message: &api.MessageResponse{
			ID:      id,
			Type:    "message",
			Role:    "assistant",
			Content: []api.ContentBlock{},
			Model:   model,
			Usage:   api.AnthropicUsage{InputTokens: 0, OutputTokens: 0},
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

	var outputTokens int
	var stopReason string
	for chunk := range ch {
		if chunk.Content != "" {
			outputTokens++
			deltaText := chunk.Content
			w.writeEvent("content_block_delta", api.MessageStreamEvent{ //nolint:errcheck
				Type:  "content_block_delta",
				Index: &idx,
				Delta: &api.StreamDelta{
					Type: "text_delta",
					Text: deltaText,
				},
			})
		}
		if chunk.FinishReason != "" {
			stopReason = chunk.FinishReason
		}
	}

	// content_block_stop
	w.writeEvent("content_block_stop", api.MessageStreamEvent{ //nolint:errcheck
		Type:  "content_block_stop",
		Index: &idx,
	})

	// message_delta
	if stopReason == "" {
		stopReason = "end_turn"
	}
	w.writeEvent("message_delta", api.MessageStreamEvent{ //nolint:errcheck
		Type: "message_delta",
		Delta: &api.StreamDelta{
			Type:       "message_delta",
			StopReason: &stopReason,
		},
		Usage: &api.AnthropicUsage{OutputTokens: outputTokens},
	})

	// message_stop
	w.writeEvent("message_stop", map[string]string{ //nolint:errcheck
		"type": "message_stop",
	})

	fmt.Fprintf(c.Writer, "event: ping\ndata: {\"type\":\"ping\"}\n\n")
	c.Writer.Flush()
}

// streamMessages writes the Anthropic SSE event sequence for a development fallback (no Brain configured).
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

// ---------------------------------------------------------------------------
// Internal conversion helpers
// ---------------------------------------------------------------------------

// anthropicToInternal converts an api.MessageRequest to types.InternalChatRequest.
func anthropicToInternal(req *api.MessageRequest) *types.InternalChatRequest {
	msgs := make([]types.InternalMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, types.InternalMessage{
			Role:    types.RoleSystem,
			Content: req.System,
		})
	}
	for _, m := range req.Messages {
		content := ""
		switch v := m.Content.(type) {
		case string:
			content = v
		}
		msgs = append(msgs, types.InternalMessage{
			Role:    types.Role(m.Role),
			Content: content,
		})
	}
	internal := &types.InternalChatRequest{
		Model:     req.Model,
		Messages:  msgs,
		MaxTokens: req.MaxTokens,
		Stream:    req.Stream,
		Provider:  types.ProviderAnthropic,
	}
	if req.Temperature != nil {
		internal.Temperature = *req.Temperature
	}
	return internal
}

// internalToAnthropic converts a types.InternalChatResponse to an api.MessageResponse.
func internalToAnthropic(resp *types.InternalChatResponse, id, model string) api.MessageResponse {
	if resp.ID != "" {
		id = resp.ID
	}
	if resp.Model != "" {
		model = resp.Model
	}
	stopReason := resp.FinishReason
	if stopReason == "" {
		stopReason = "end_turn"
	}
	sr := stopReason
	return api.MessageResponse{
		ID:   id,
		Type: "message",
		Role: "assistant",
		Content: []api.ContentBlock{
			{Type: "text", Text: resp.Message.Content},
		},
		Model:      model,
		StopReason: &sr,
		Usage: api.AnthropicUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}
}
