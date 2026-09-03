package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

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

		// Semantic validation — see the note in HandleChatCompletions.
		// Anthropic documents max_tokens:0 as prompt-cache pre-warming, so
		// this endpoint's floor is 0; requestvalidate.go records why.
		if d := validateMessageRequest(&req); d != nil {
			d.write(c)
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
					writeUpstreamError(c, "stream", err)
					return
				}
				streamBrainMessages(c, id, model, ch)
				return
			}

			resp, err := b.Complete(c.Request.Context(), internalReq)
			if err != nil {
				writeUpstreamError(c, "complete", err)
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
//
// Tools/ToolChoice were previously silently dropped here (the OpenAI wire
// handled tools end-to-end via openAIToInternal/internalToOpenAI; the
// Anthropic wire did not) — this is a real capability gap in the schema
// itself declares Tools/ToolChoice fields (pkg/api/anthropic.go), so tools
// are in-scope for this facade and are now wired through.
func anthropicToInternal(req *api.MessageRequest) *types.InternalChatRequest {
	msgs := make([]types.InternalMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, types.InternalMessage{
			Role:    types.RoleSystem,
			Content: req.System,
		})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, anthropicMessageToInternal(m)...)
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
	if len(req.Tools) > 0 {
		internal.Tools = anthropicToolsToInternal(req.Tools)
	}
	if req.ToolChoice != nil {
		internal.ToolChoice = anthropicToolChoiceToInternal(req.ToolChoice)
	}
	return internal
}

// anthropicMessageToInternal converts one Anthropic message to zero or more
// InternalMessages. Content is either a bare string (the common case) or,
// for tool-use/tool-result turns, a JSON array of content blocks decoded by
// encoding/json into []interface{} of map[string]interface{}. A single
// Anthropic message can therefore expand into multiple internal messages:
// text + tool_use blocks collapse into ONE assistant message (matching the
// OpenAI-wire convention of one ToolCalls-bearing message per turn), while
// each tool_result block becomes its OWN RoleTool message keyed by
// ToolCallID (also matching the OpenAI/llama.cpp tool-result convention
// openAIToInternal already relies on).
func anthropicMessageToInternal(m api.AnthropicMessage) []types.InternalMessage {
	switch v := m.Content.(type) {
	case string:
		return []types.InternalMessage{{Role: types.Role(m.Role), Content: v}}
	case []interface{}:
		return anthropicContentBlocksToInternal(m.Role, v)
	default:
		return []types.InternalMessage{{Role: types.Role(m.Role), Content: ""}}
	}
}

// anthropicContentBlocksToInternal converts a decoded Anthropic content-block
// array (text / tool_use / tool_result blocks) into InternalMessages.
func anthropicContentBlocksToInternal(role string, blocks []interface{}) []types.InternalMessage {
	var textParts []string
	var toolCalls []types.InternalToolCall
	var toolResultMsgs []types.InternalMessage

	for _, raw := range blocks {
		block, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)
		switch blockType {
		case "text":
			if text, ok := block["text"].(string); ok {
				textParts = append(textParts, text)
			}
		case "tool_use":
			id, _ := block["id"].(string)
			name, _ := block["name"].(string)
			argsJSON := "{}"
			if input, ok := block["input"]; ok {
				if b, err := json.Marshal(input); err == nil {
					argsJSON = string(b)
				}
			}
			tc := types.InternalToolCall{ID: id, Type: "function"}
			tc.Function.Name = name
			tc.Function.Arguments = argsJSON
			toolCalls = append(toolCalls, tc)
		case "tool_result":
			toolUseID, _ := block["tool_use_id"].(string)
			toolResultMsgs = append(toolResultMsgs, types.InternalMessage{
				Role:       types.RoleTool,
				Content:    anthropicToolResultContentToString(block["content"]),
				ToolCallID: toolUseID,
			})
		}
	}

	var msgs []types.InternalMessage
	if len(textParts) > 0 || len(toolCalls) > 0 {
		msgs = append(msgs, types.InternalMessage{
			Role:      types.Role(role),
			Content:   strings.Join(textParts, "\n"),
			ToolCalls: toolCalls,
		})
	}
	msgs = append(msgs, toolResultMsgs...)
	return msgs
}

// anthropicToolResultContentToString flattens a tool_result content block's
// "content" field, which per the Anthropic API may be a bare string or an
// array of nested content blocks (typically {"type":"text","text":"..."}).
func anthropicToolResultContentToString(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if text, ok := m["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	case nil:
		return ""
	default:
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", v)
	}
}

// anthropicToolsToInternal converts Anthropic tool definitions to the
// internal, OpenAI-function-shaped Tool representation every downstream
// OpenAI-compatible provider (llama.cpp, openai_compat) already consumes.
func anthropicToolsToInternal(tools []api.AnthropicTool) []types.InternalTool {
	result := make([]types.InternalTool, 0, len(tools))
	for _, t := range tools {
		result = append(result, types.InternalTool{
			Type: "function",
			Function: map[string]interface{}{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.InputSchema,
			},
		})
	}
	return result
}

// anthropicToolChoiceToInternal converts Anthropic's tool_choice shape
// ({"type":"auto"|"any"|"none"|"tool","name":"..."}) to the OpenAI-shaped
// tool_choice value ("auto"|"required"|"none"|{"type":"function",...})
// that InternalChatRequest.ToolChoice flows straight through to every
// downstream OpenAI-compatible provider (see internal/gateway/openai.go's
// identical ToolChoice pass-through, internal/brain/llamacpp.go,
// internal/brain/openai_compat_provider.go).
func anthropicToolChoiceToInternal(tc interface{}) interface{} {
	switch v := tc.(type) {
	case string:
		// Already OpenAI-shaped (defensive tolerance for non-standard
		// callers); pass through unchanged.
		return v
	case map[string]interface{}:
		switch t, _ := v["type"].(string); t {
		case "auto":
			return "auto"
		case "any":
			return "required"
		case "none":
			return "none"
		case "tool":
			name, _ := v["name"].(string)
			return map[string]interface{}{
				"type":     "function",
				"function": map[string]interface{}{"name": name},
			}
		default:
			return "auto"
		}
	default:
		return nil
	}
}

// internalToAnthropic converts a types.InternalChatResponse to an
// api.MessageResponse.
//
// Previously this dropped resp.Message.ToolCalls entirely — a tool-calling
// model response came back to an Anthropic client as an empty/partial text
// message with no way to know a tool was invoked. Tool calls are now
// emitted as "tool_use" content blocks (the Anthropic wire format) and the
// stop_reason is set to "tool_use" per the Anthropic API contract.
func internalToAnthropic(resp *types.InternalChatResponse, id, model string) api.MessageResponse {
	if resp.ID != "" {
		id = resp.ID
	}
	if resp.Model != "" {
		model = resp.Model
	}

	var blocks []api.ContentBlock
	if resp.Message.Content != "" {
		blocks = append(blocks, api.ContentBlock{Type: "text", Text: resp.Message.Content})
	}
	for _, tc := range resp.Message.ToolCalls {
		blocks = append(blocks, api.ContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: toolCallArgumentsToInput(tc.Function.Arguments),
		})
	}
	if len(blocks) == 0 {
		// Preserve the pre-existing shape for a genuinely empty response
		// (always at least one content block, even if blank text).
		blocks = []api.ContentBlock{{Type: "text", Text: ""}}
	}

	stopReason := anthropicStopReason(resp.FinishReason, len(resp.Message.ToolCalls) > 0)
	sr := stopReason
	return api.MessageResponse{
		ID:         id,
		Type:       "message",
		Role:       "assistant",
		Content:    blocks,
		Model:      model,
		StopReason: &sr,
		Usage: api.AnthropicUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}
}

// toolCallArgumentsToInput decodes a tool call's JSON-encoded Arguments
// string into the interface{} value the "input" field of an Anthropic
// tool_use content block expects. An empty or unparseable arguments string
// decodes to an empty object rather than erroring — a malformed tool-call
// argument payload from the model must not break the whole response.
func toolCallArgumentsToInput(argsJSON string) interface{} {
	if strings.TrimSpace(argsJSON) == "" {
		return map[string]interface{}{}
	}
	var input interface{}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return map[string]interface{}{}
	}
	return input
}

// anthropicStopReason maps an internal finish reason (the OpenAI-shaped
// values every provider in internal/brain emits — "stop", "length",
// "tool_calls", or provider-native values already carrying Anthropic's own
// vocabulary) to the closed set of Anthropic stop_reason values: end_turn,
// max_tokens, stop_sequence, tool_use.
func anthropicStopReason(finishReason string, hasToolCalls bool) string {
	if hasToolCalls || finishReason == "tool_calls" {
		return "tool_use"
	}
	switch finishReason {
	case "", "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "end_turn", "max_tokens", "stop_sequence", "tool_use":
		// Already Anthropic-native (e.g. passthrough from the real
		// Anthropic provider) — pass through unchanged.
		return finishReason
	default:
		return "end_turn"
	}
}
