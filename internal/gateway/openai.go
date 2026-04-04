package gateway

import (
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// hardcodedModels is the static model list returned by the stub handlers.
var hardcodedModels = []api.Model{
	{ID: "llama-3.1-70b", Object: "model", Created: 1700000000, OwnedBy: "helix"},
	{ID: "gpt-4o", Object: "model", Created: 1700000001, OwnedBy: "helix"},
	{ID: "claude-sonnet-4-20250514", Object: "model", Created: 1700000002, OwnedBy: "helix"},
}

// randomID generates a short random hex suffix for synthetic IDs.
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
			internalReq := openAIToInternal(&req)
			if req.Stream {
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
				for chunk := range ch {
					finishReason := &chunk.FinishReason
					if chunk.FinishReason == "" {
						finishReason = nil
					}
					w.WriteEvent(api.ChatCompletionChunk{ //nolint:errcheck
						ID:      id,
						Object:  "chat.completion.chunk",
						Created: created,
						Model:   model,
						Choices: []api.ChatCompletionChunkChoice{
							{
								Index:        0,
								Delta:        api.ChatMessageDelta{Content: chunk.Content},
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

// streamChatCompletions writes 3 SSE chunks followed by [DONE] (stub path).
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
// When b is non-nil it returns the models from the Brain; otherwise the hardcoded stub list.
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
// When b is non-nil it searches Brain models; otherwise the hardcoded stub list.
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
// Returns a single zero-vector embedding of dimension 1536 (ada-002 default).
// Brain is accepted for interface consistency but not yet used (embeddings are
// not part of the Phase 3 Brain scope).
func HandleEmbeddings(_ *brain.Brain) gin.HandlerFunc {
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

		const dim = 1536
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
	msgs := make([]types.InternalMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		content := ""
		switch v := m.Content.(type) {
		case string:
			content = v
		}
		msgs = append(msgs, types.InternalMessage{
			Role:    types.Role(m.Role),
			Content: content,
			Name:    m.Name,
		})
	}
	internal := &types.InternalChatRequest{
		Model:    req.Model,
		Messages: msgs,
		Stream:   req.Stream,
	}
	if req.MaxTokens != nil {
		internal.MaxTokens = *req.MaxTokens
	}
	if req.Temperature != nil {
		internal.Temperature = *req.Temperature
	}
	return internal
}

// internalToOpenAI converts a types.InternalChatResponse to an OpenAI ChatCompletionResponse.
func internalToOpenAI(resp *types.InternalChatResponse, model string) api.ChatCompletionResponse {
	if resp.Model != "" {
		model = resp.Model
	}
	return api.ChatCompletionResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []api.ChatCompletionChoice{
			{
				Index: 0,
				Message: api.ChatMessage{
					Role:    string(resp.Message.Role),
					Content: resp.Message.Content,
				},
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
