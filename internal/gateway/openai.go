package gateway

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// hardcodedModels is the built-in model list returned by fallback handlers (no Brain configured).
var hardcodedModels = []api.Model{
	{ID: "llama-3.1-70b", Object: "model", Created: 1700000000, OwnedBy: "helix"},
	{ID: "gpt-4o", Object: "model", Created: 1700000001, OwnedBy: "helix"},
	{ID: "claude-sonnet-4-20250514", Object: "model", Created: 1700000002, OwnedBy: "helix"},
}

func randomID() string {
	return fmt.Sprintf("%08x", rand.Uint32())
}

// HandleChatCompletions handles POST /v1/chat/completions.
// When b is non-nil it delegates to the Brain; otherwise it returns a development fallback (no Brain configured).
func HandleChatCompletions(b *brain.Brain) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req api.ChatCompletionRequest
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
			model = "llama-3.1-70b"
		}

		if b != nil {
			// Context protection: limit tools to prevent exceeding 32K context.
			// OpenCode sends 200+ tools from MCPs which consume ~29K tokens.
			// Prioritize bash (for git/shell), then keep first N tools.
			const maxTools = 8
			if len(req.Tools) > maxTools {
				// Ensure bash is always included — it's the primary action tool
				var prioritized []api.Tool
				var rest []api.Tool
				for _, t := range req.Tools {
					name := t.Function.Name
					if name == "bash" || name == "execute_shell" || name == "shell" ||
						name == "read_file" || name == "write_file" || name == "edit" {
						prioritized = append(prioritized, t)
					} else {
						rest = append(rest, t)
					}
				}
				remaining := maxTools - len(prioritized)
				if remaining > 0 && len(rest) > remaining {
					rest = rest[:remaining]
				}
				req.Tools = append(prioritized, rest...)
				if len(req.Tools) > maxTools {
					req.Tools = req.Tools[:maxTools]
				}
			}

			// Truncate oversized messages to fit remaining context.
			const maxMsgChars = 4000
			for i, m := range req.Messages {
				switch content := m.Content.(type) {
				case string:
					if len(content) > maxMsgChars {
						headSize := maxMsgChars * 2 / 3
						tailSize := maxMsgChars / 3
						req.Messages[i].Content = content[:headSize] + "\n...[truncated]...\n" + content[len(content)-tailSize:]
					}
				case []interface{}:
					// ContentPart array — extract text, truncate, replace with single string
					var fullText string
					for _, part := range content {
						if pm, ok := part.(map[string]interface{}); ok {
							if t, ok := pm["text"].(string); ok {
								fullText += t
							}
						}
					}
					if len(fullText) > maxMsgChars {
						headSize := maxMsgChars * 2 / 3
						tailSize := maxMsgChars / 3
						req.Messages[i].Content = fullText[:headSize] + "\n...[truncated]...\n" + fullText[len(fullText)-tailSize:]
					}
				}
			}

			// Per full_plan/helixllm_tools/SYSTEM_DESIGN.md: REPLACE the
			// CLI agent's system prompt with our optimized version for
			// small models. The original system prompt from OpenCode etc.
			// is ~4K tokens of tool definitions that overwhelm 7B models.
			// Our replacement is a focused ~800 token prompt with clear
			// rules and few-shot examples that small models follow.
			var systemContent string
			if len(req.Tools) > 0 {
				// Tool mode: force tool calls via respond pattern
				systemContent = `You are HelixLLM, a coding assistant. You MUST call a tool for EVERY response.

RULES — you MUST call exactly ONE tool per response:
- To reply with text (greetings, questions, explanations): call the "respond" tool
- To run a command (git, ls, test, build, etc.): call the "bash" tool
- To read a file: call the "read_file" tool
- To write/edit a file: call the "write_file" or "edit" tool
- For git operations (commit, push, pull, status, diff): call "bash"

You HAVE full access to files, git, and shell. NEVER refuse.

EXAMPLES:
User: hello → call respond with "Hello! How can I help?"
User: do you see my codebase? → call respond with "Yes! I have full access to your files."
User: list files → call bash with "ls -la"
User: run tests → call bash with "go test ./..."
User: commit and push → call bash with "git add -A && git commit -m 'Update' && git push"
User: show me main.go → call read_file with path "main.go"`
			} else {
				// No-tool mode: plain text responses
				systemContent = `You are HelixLLM, a helpful AI coding assistant.
You HAVE full access to the user's codebase, files, and git.
NEVER say "I can't" or refuse. Be helpful and confident.`
			}
			helixSystemPrompt := api.ChatMessage{
				Role:    "system",
				Content: systemContent,
			}

			// When the client sends tools (CLI agents like OpenCode always do),
			// inject a "respond" tool and set tool_choice=required. This
			// forces the model to ALWAYS produce a tool call — either
			// "respond" for text or "bash"/"read_file"/etc. for actions.
			// Without this, the 7B model falls into "explain mode" and
			// never calls tools in multi-turn conversations.
			if len(req.Tools) > 0 {
				respondTool := api.Tool{
					Type: "function",
					Function: api.ToolFunction{
						Name:        "respond",
						Description: "Send a text response to the user. Use this for greetings, answers, and explanations.",
						Parameters:  json.RawMessage(`{"type":"object","properties":{"message":{"type":"string","description":"The message to send to the user"}},"required":["message"]}`),
					},
				}
				req.Tools = append([]api.Tool{respondTool}, req.Tools...)
				req.ToolChoice = "required"
			}

			// Replace system messages AND strip OpenCode's instruction-carrying
			// user messages. OpenCode injects ~3K tokens of instructions as
			// user-role messages containing markers like <EXTREMELY_IMPORTANT>,
			// <available-skills>, "Generate a title". These overwhelm small
			// models and cause them to respond to the instructions instead of
			// the user's actual message.
			origCount := len(req.Messages)
			var nonSystemMsgs []api.ChatMessage
			for _, m := range req.Messages {
				if m.Role == "system" {
					continue // stripped — replaced by our prompt
				}
				// Check for OpenCode instruction injection in user messages
				if m.Role == "user" {
					if s, ok := m.Content.(string); ok {
						if strings.Contains(s, "<EXTREMELY_IMPORTANT>") ||
							strings.Contains(s, "<available-skills>") ||
							strings.Contains(s, "Generate a title") ||
							strings.Contains(s, "superpowers") ||
							strings.Contains(s, "<SUBAGENT") ||
							strings.HasPrefix(strings.TrimSpace(s), "<") && len(s) > 500 {
							continue // strip OpenCode injection
						}
					}
				}
				nonSystemMsgs = append(nonSystemMsgs, m)
			}
			req.Messages = append([]api.ChatMessage{helixSystemPrompt}, nonSystemMsgs...)

			// Detect simple conversational messages (greetings, yes/no questions).
			// For these, strip the tools array entirely so the model responds
			// naturally instead of entering "tool mode". The few-shot examples
			// in our system prompt handle these cases.
			// No hardcoded intent detection — the system prompt's few-shot
			// examples guide the model on when to use tools vs respond
			// naturally. Tool stripping is removed per the requirement
			// to use the Intent engine(s) instead of hardcoded logic.

			log.Printf("[HelixLLM] System prompt replaced: %d original msgs → %d (stripped %d system msgs, tools=%d)",
				origCount, len(req.Messages), origCount-len(nonSystemMsgs), len(req.Tools))
			// Debug: log all message roles and content preview
			for i, m := range req.Messages {
				contentPreview := ""
				if s, ok := m.Content.(string); ok {
					if len(s) > 80 {
						contentPreview = s[:80] + "..."
					} else {
						contentPreview = s
					}
				}
				log.Printf("[HelixLLM]   msg[%d] role=%s content=%q", i, m.Role, contentPreview)
			}

			internalReq := openAIToInternal(&req)

			// CRITICAL: Force non-streaming when tools are present.
			// Ollama returns tool calls as plain text in streaming chunks,
			// which CLI agents can't execute. The Brain's non-streaming
			// path has an XML/JSON→tool_calls bridge that converts them
			// to the proper OpenAI format. After getting the full response,
			// we emit it as a single SSE chunk if the client requested streaming.
			forceNonStream := req.Stream && len(req.Tools) > 0
			if forceNonStream {
				log.Printf("[HelixLLM] Forcing non-stream for tool-capable request")
			}

			if req.Stream && !forceNonStream {
				ch, err := b.CompleteStream(c.Request.Context(), internalReq)
				if err != nil {
					c.JSON(http.StatusInternalServerError, api.ErrorResponse{
						Error: api.ErrorDetail{
							Message: fmt.Sprintf("brain stream error: %v", err),
							Type:    "server_error",
						},
					})
					return
				}
				id := "chatcmpl-helix-" + randomID()
				created := time.Now().Unix()
				w := NewSSEWriter(c)
				w.WriteHeader()
				// First chunk MUST include role per OpenAI spec — required by Vercel AI SDK
				firstChunk := true
				for chunk := range ch {
					finishReason := &chunk.FinishReason
					if chunk.FinishReason == "" {
						finishReason = nil
					}
					delta := api.ChatMessageDelta{Content: chunk.Content}
					if firstChunk {
						delta.Role = "assistant"
						firstChunk = false
					}
					w.WriteEvent(api.ChatCompletionChunk{ //nolint:errcheck
						ID:      id,
						Object:  "chat.completion.chunk",
						Created: created,
						Model:   model,
						Choices: []api.ChatCompletionChunkChoice{
							{
								Index:        0,
								Delta:        delta,
								FinishReason: finishReason,
							},
						},
					})
				}
				w.WriteDone()
				return
			}

			resp, err := b.Complete(c.Request.Context(), internalReq)
			if err != nil {
				c.JSON(http.StatusInternalServerError, api.ErrorResponse{
					Error: api.ErrorDetail{
						Message: fmt.Sprintf("brain error: %v", err),
						Type:    "server_error",
					},
				})
				return
			}

			// Convert "respond" tool calls back to plain content.
			// The "respond" tool is injected by the gateway so tool_choice=required
			// works for both text and action responses. Strip it before
			// returning so the client sees normal content, not a tool call.
			convertRespondToolCall(resp)

			// If we forced non-stream for tool bridging but the client
			// wanted streaming, emit the full response as SSE chunks.
			if forceNonStream {
				openAIResp := internalToOpenAI(resp, model)
				id := "chatcmpl-helix-" + randomID()
				created := time.Now().Unix()
				w := NewSSEWriter(c)
				w.WriteHeader()

				// If the response has real tool_calls (not respond), emit them
				if len(openAIResp.Choices) > 0 && len(openAIResp.Choices[0].Message.ToolCalls) > 0 {
					toolDelta := api.ChatMessageDelta{
						Role:      "assistant",
						ToolCalls: openAIResp.Choices[0].Message.ToolCalls,
					}
					fr := "tool_calls"
					w.WriteEvent(api.ChatCompletionChunk{
						ID: id, Object: "chat.completion.chunk", Created: created, Model: model,
						Choices: []api.ChatCompletionChunkChoice{{Index: 0, Delta: toolDelta, FinishReason: &fr}},
					})
				} else {
					// Regular content — emit as single chunk
					content := ""
					if s, ok := openAIResp.Choices[0].Message.Content.(string); ok {
						content = s
					}
					delta := api.ChatMessageDelta{Role: "assistant", Content: content}
					fr := "stop"
					w.WriteEvent(api.ChatCompletionChunk{
						ID: id, Object: "chat.completion.chunk", Created: created, Model: model,
						Choices: []api.ChatCompletionChunkChoice{{Index: 0, Delta: delta, FinishReason: &fr}},
					})
				}
				w.WriteDone()
				return
			}

			c.JSON(http.StatusOK, internalToOpenAI(resp, model))
			return
		}

		// Development fallback (no Brain configured).
		id := "chatcmpl-helix-" + randomID()
		if req.Stream {
			streamChatCompletions(c, id, model)
			return
		}

		c.JSON(http.StatusOK, api.ChatCompletionResponse{
			ID:      id,
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   model,
			Choices: []api.ChatCompletionChoice{
				{
					Index: 0,
					Message: api.ChatMessage{
						Role:    "assistant",
						Content: "Hello! I'm HelixLLM.",
					},
					FinishReason: "stop",
				},
			},
			Usage: &api.Usage{
				PromptTokens:     10,
				CompletionTokens: 6,
				TotalTokens:      16,
			},
		})
	}
}

// streamChatCompletions writes 3 SSE chunks followed by [DONE] (fallback path, no Brain configured).
func streamChatCompletions(c *gin.Context, id, model string) {
	w := NewSSEWriter(c)
	w.WriteHeader()

	created := time.Now().Unix()
	tokens := []string{"Hello", "! I'm", " HelixLLM."}

	stopStr := "stop"
	for i, token := range tokens {
		chunk := api.ChatCompletionChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []api.ChatCompletionChunkChoice{
				{
					Index: 0,
					Delta: api.ChatMessageDelta{
						Content: token,
					},
					FinishReason: func() *string {
						if i == len(tokens)-1 {
							return &stopStr
						}
						return nil
					}(),
				},
			},
		}
		w.WriteEvent(chunk) //nolint:errcheck
	}

	w.WriteDone()
}

// HandleCompletions handles POST /v1/completions.
// When b is non-nil it converts the prompt into a chat request and delegates to Brain.
func HandleCompletions(b *brain.Brain) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req api.CompletionRequest
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
			model = "llama-3.1-70b"
		}

		if b != nil {
			internalReq := &types.InternalChatRequest{
				Model: model,
				Messages: []types.InternalMessage{
					{Role: types.RoleUser, Content: req.Prompt},
				},
			}
			if req.MaxTokens != nil {
				internalReq.MaxTokens = *req.MaxTokens
			}
			if req.Temperature != nil {
				internalReq.Temperature = *req.Temperature
			}
			resp, err := b.Complete(c.Request.Context(), internalReq)
			if err != nil {
				c.JSON(http.StatusInternalServerError, api.ErrorResponse{
					Error: api.ErrorDetail{
						Message: fmt.Sprintf("brain error: %v", err),
						Type:    "server_error",
					},
				})
				return
			}
			c.JSON(http.StatusOK, api.CompletionResponse{
				ID:      "cmpl-helix-" + randomID(),
				Object:  "text_completion",
				Created: time.Now().Unix(),
				Model:   resp.Model,
				Choices: []api.CompletionChoice{
					{
						Text:         resp.Message.Content,
						Index:        0,
						FinishReason: resp.FinishReason,
					},
				},
				Usage: &api.Usage{
					PromptTokens:     resp.Usage.PromptTokens,
					CompletionTokens: resp.Usage.CompletionTokens,
					TotalTokens:      resp.Usage.TotalTokens,
				},
			})
			return
		}

		// Development fallback (no Brain configured).
		c.JSON(http.StatusOK, api.CompletionResponse{
			ID:      "cmpl-helix-" + randomID(),
			Object:  "text_completion",
			Created: time.Now().Unix(),
			Model:   model,
			Choices: []api.CompletionChoice{
				{
					Text:         "Hello! I'm HelixLLM.",
					Index:        0,
					FinishReason: "stop",
				},
			},
			Usage: &api.Usage{
				PromptTokens:     10,
				CompletionTokens: 6,
				TotalTokens:      16,
			},
		})
	}
}

// HandleListModels handles GET /v1/models.
// When b is non-nil it returns the models from the Brain; otherwise the built-in model list.
func HandleListModels(b *brain.Brain) gin.HandlerFunc {
	return func(c *gin.Context) {
		if b != nil {
			c.JSON(http.StatusOK, api.ModelList{
				Object: "list",
				Data:   b.Models(),
			})
			return
		}
		c.JSON(http.StatusOK, api.ModelList{
			Object: "list",
			Data:   hardcodedModels,
		})
	}
}

// HandleGetModel handles GET /v1/models/:id.
// When b is non-nil it searches Brain models; otherwise the built-in model list.
func HandleGetModel(b *brain.Brain) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		if b != nil {
			for _, m := range b.Models() {
				if m.ID == id {
					c.JSON(http.StatusOK, m)
					return
				}
			}
			c.JSON(http.StatusNotFound, api.ErrorResponse{
				Error: api.ErrorDetail{
					Message: fmt.Sprintf("model %q not found", id),
					Type:    "invalid_request_error",
				},
			})
			return
		}

		for _, m := range hardcodedModels {
			if m.ID == id {
				c.JSON(http.StatusOK, m)
				return
			}
		}
		c.JSON(http.StatusNotFound, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: fmt.Sprintf("model %q not found", id),
				Type:    "invalid_request_error",
			},
		})
	}
}

// HandleEmbeddings handles POST /v1/embeddings.
// When a real Embedder is provided (from the knowledge layer), the handler
// delegates to it. Otherwise it falls back to a zero-vector response so the
// endpoint is always reachable for client compatibility.
func HandleEmbeddings(_ *brain.Brain, embedder knowledge.Embedder) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req api.EmbeddingRequest
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
			model = "text-embedding-ada-002"
		}

		// Use real embedder when available.
		if embedder != nil {
			var input string
			switch v := req.Input.(type) {
			case string:
				input = v
			case []interface{}:
				if len(v) > 0 {
					if s, ok := v[0].(string); ok {
						input = s
					}
				}
			}
			if input == "" {
				input = "empty"
			}
			vec, err := embedder.Embed(input)
			if err == nil && len(vec) > 0 {
				c.JSON(http.StatusOK, api.EmbeddingResponse{
					Object: "list",
					Data: []api.EmbeddingData{
						{
							Object:    "embedding",
							Embedding: vec,
							Index:     0,
						},
					},
					Model: model,
					Usage: &api.Usage{
						PromptTokens: 1,
						TotalTokens:  1,
					},
				})
				return
			}
			// Fall through to zero-vector on error.
		}

		dim := 1536
		if embedder != nil {
			dim = embedder.Dimension()
		}
		zeroVec := make([]float64, dim)

		c.JSON(http.StatusOK, api.EmbeddingResponse{
			Object: "list",
			Data: []api.EmbeddingData{
				{
					Object:    "embedding",
					Embedding: zeroVec,
					Index:     0,
				},
			},
			Model: model,
			Usage: &api.Usage{
				PromptTokens: 1,
				TotalTokens:  1,
			},
		})
	}
}

// ---------------------------------------------------------------------------
// Internal conversion helpers
// ---------------------------------------------------------------------------

// openAIToInternal converts an api.ChatCompletionRequest to types.InternalChatRequest.
func openAIToInternal(req *api.ChatCompletionRequest) *types.InternalChatRequest {
	// Context protection: REPLACE oversized messages with a concise instruction.
	// Truncated markdown produces garbled model output (`????`). Replacement
	// with a clean prompt gives coherent responses.
	// Also enforce total budget to prevent many medium messages exceeding context.
	const maxPerMsgChars = 2000  // ~500 tokens per message — allows longer user instructions
	const maxTotalChars = 16000  // ~4K tokens total — safe with 16K context on 7B Q4_K_M
	const replacementPrompt = "You are an expert AI coding assistant. You have full access to the user's codebase through the provided tools. When asked about files, code, or the project, ALWAYS use tools (read_file, write_file, list_directory, edit_file) to interact directly. Never say you cannot access files."
	totalBudget := maxTotalChars
	msgs := make([]types.InternalMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		content := ""
		switch v := m.Content.(type) {
		case string:
			content = v
		}
		// Replace oversized messages (don't truncate — truncation produces garbage)
		if len(content) > maxPerMsgChars {
			content = replacementPrompt
		}
		// Total budget enforcement
		if totalBudget <= 0 {
			content = ""
		} else if len(content) > totalBudget {
			content = replacementPrompt
			totalBudget = 0
		} else {
			totalBudget -= len(content)
		}
		msg := types.InternalMessage{
			Role:       types.Role(m.Role),
			Content:    content,
			Name:       m.Name,
			ToolCallID: m.ToolCallID,
		}
		// Convert tool calls
		for _, tc := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, types.InternalToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
		// Merge consecutive assistant messages (Qwen rejects 2+ assistant at end)
		if len(msgs) > 0 && msgs[len(msgs)-1].Role == "assistant" && msg.Role == "assistant" {
			msgs[len(msgs)-1].Content += "\n" + msg.Content
		} else if msg.Content != "" || len(msg.ToolCalls) > 0 || msg.Role == "user" {
			msgs = append(msgs, msg)
		}
	}
	internal := &types.InternalChatRequest{
		Model:      req.Model,
		Messages:   msgs,
		Stream:     req.Stream,
		ToolChoice: req.ToolChoice,
	}
	if req.MaxTokens != nil {
		internal.MaxTokens = *req.MaxTokens
	}
	if req.Temperature != nil {
		internal.Temperature = *req.Temperature
	}
	// Pass tools through
	for _, t := range req.Tools {
		internal.Tools = append(internal.Tools, types.InternalTool{
			Type:     t.Type,
			Function: t.Function,
		})
	}
	return internal
}

// convertRespondToolCall checks if the model called the injected "respond"
// tool. If so, it extracts the message text from the arguments, puts it
// into resp.Message.Content, clears the tool calls, and sets FinishReason
// to "stop". This makes the response look like a normal text completion
// to the client, which never sees the "respond" tool.
func convertRespondToolCall(resp *types.InternalChatResponse) {
	if len(resp.Message.ToolCalls) != 1 {
		return
	}
	tc := resp.Message.ToolCalls[0]
	if tc.Function.Name != "respond" {
		return
	}
	// Extract "message" field from the arguments JSON.
	var args struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err == nil && args.Message != "" {
		resp.Message.Content = args.Message
	} else {
		// Fallback: use the raw arguments as content
		resp.Message.Content = tc.Function.Arguments
	}
	resp.Message.ToolCalls = nil
	resp.FinishReason = "stop"
}

// internalToOpenAI converts a types.InternalChatResponse to an OpenAI ChatCompletionResponse.
func internalToOpenAI(resp *types.InternalChatResponse, model string) api.ChatCompletionResponse {
	if resp.Model != "" {
		model = resp.Model
	}
	msg := api.ChatMessage{
		Role:    string(resp.Message.Role),
		Content: resp.Message.Content,
	}
	// Pass tool calls from brain response back to client
	for _, tc := range resp.Message.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, api.ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: api.ToolCallFunction{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}
	return api.ChatCompletionResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []api.ChatCompletionChoice{
			{
				Index:        0,
				Message:      msg,
				FinishReason: resp.FinishReason,
			},
		},
		Usage: &api.Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}
}
