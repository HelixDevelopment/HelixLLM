package gateway

import (
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

// randomID generates a short random hex suffix for synthetic IDs.
// isActionRequest returns true if the message asks the model to DO something
// that requires tools (read/write files, run commands, git ops). Simple
// greetings, questions, and yes/no queries return false.
func isActionRequest(msg string) bool {
	lower := strings.ToLower(strings.TrimSpace(msg))
	// Strip surrounding quotes (OpenCode wraps user messages in quotes)
	lower = strings.Trim(lower, "\"'`")
	lower = strings.TrimSpace(lower)
	// Questions about capabilities are NOT action requests
	questionPrefixes := []string{
		"can you", "do you", "are you", "could you", "would you",
		"what can", "what do", "how do", "is it", "does it",
		"hello", "hi ", "hey", "thanks", "thank you",
	}
	for _, q := range questionPrefixes {
		if strings.HasPrefix(lower, q) {
			return false
		}
	}
	actionWords := []string{
		"list", "read", "write", "create", "delete", "edit", "modify",
		"run", "execute", "commit", "push", "pull", "git ", "test",
		"build", "install", "search", "find", "grep", "cat ", "ls ",
		"mkdir", "rm ", "mv ", "cp ", "diff", "show me", "open ",
		"update ", "change ", "fix ", "refactor", "implement", "/init",
	}
	for _, w := range actionWords {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
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
			// Limit to 15 most important tools (~2K tokens).
			const maxTools = 5 // 5 tools max — each tool is ~500 tokens with full schema
			if len(req.Tools) > maxTools {
				req.Tools = req.Tools[:maxTools]
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
			helixSystemPrompt := api.ChatMessage{
				Role: "system",
				Content: `You are HelixLLM, a helpful AI coding assistant with FULL ACCESS to the user's codebase through your tools.

=== CRITICAL RULES ===
1. You HAVE access to all files, directories, and git through your tools. NEVER say "I can't access" or "I don't have access".
2. For greetings (hello, hi, hey): Respond with a friendly greeting like "Hello! How can I help you with your project today?"
3. For questions about the codebase: Answer YES and describe what you can see/do.
4. For action requests: Use the appropriate tool immediately.
5. ALWAYS be helpful and confident about your capabilities.

=== EXAMPLES ===
User: hello!
Assistant: Hello! How can I help you with your project today?

User: Do you see my codebase?
Assistant: Yes! I have full access to your codebase and can read, modify, and manage all your source files. What would you like me to help with?

User: Can you read and modify my source code files?
Assistant: Yes, I can read any file in your project and make modifications using my editing tools. Just let me know what you'd like me to change.

User: List the files in the current directory.
Assistant: [calls list_directory tool]

User: What does main.go contain?
Assistant: [calls read_file tool with path "main.go"]

User: Run the tests
Assistant: [calls bash tool with command "go test ./..."]

User: Commit and push all changes
Assistant: [calls bash tool with command "git add -A && git commit -m 'Update' && git push"]`,
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

			// If we forced non-stream for tool bridging but the client
			// wanted streaming, emit the full response as SSE chunks.
			if forceNonStream {
				openAIResp := internalToOpenAI(resp, model)
				id := "chatcmpl-helix-" + randomID()
				created := time.Now().Unix()
				w := NewSSEWriter(c)
				w.WriteHeader()

				// If the response has tool_calls, emit them as a chunk
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
	const maxPerMsgChars = 800   // ~200 tokens per message — stays under Q4_K_M degradation threshold
	const maxTotalChars = 12000  // ~3K tokens total — safe for 7B Q4_K_M (degrades at ~8K tokens)
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
