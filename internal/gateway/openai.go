package gateway

import (
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

// hardcodedModels is the static model list returned by the stub handlers.
var hardcodedModels = []api.Model{
	{ID: "llama-3.1-70b", Object: "model", Created: 1700000000, OwnedBy: "helix"},
	{ID: "gpt-4o", Object: "model", Created: 1700000001, OwnedBy: "helix"},
	{ID: "claude-sonnet-4-20250514", Object: "model", Created: 1700000002, OwnedBy: "helix"},
}

// randomID generates a short random hex suffix for synthetic IDs.
func randomID() string {
	return fmt.Sprintf("%08x", rand.Uint32()) //nolint:gosec // stub, not security-sensitive
}

// HandleChatCompletions handles POST /v1/chat/completions.
//
// Stub behaviour:
//   - stream=false → returns a single canned ChatCompletionResponse.
//   - stream=true  → emits 3 SSE chunks then the [DONE] sentinel.
func HandleChatCompletions(c *gin.Context) {
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

	id := "chatcmpl-helix-" + randomID()
	model := req.Model
	if model == "" {
		model = "llama-3.1-70b"
	}

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

// streamChatCompletions writes 3 SSE chunks followed by [DONE].
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

// HandleCompletions handles POST /v1/completions (stub).
func HandleCompletions(c *gin.Context) {
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

// HandleListModels handles GET /v1/models.
func HandleListModels(c *gin.Context) {
	c.JSON(http.StatusOK, api.ModelList{
		Object: "list",
		Data:   hardcodedModels,
	})
}

// HandleGetModel handles GET /v1/models/:id.
func HandleGetModel(c *gin.Context) {
	id := c.Param("id")
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

// HandleEmbeddings handles POST /v1/embeddings.
// Returns a single zero-vector embedding of dimension 1536 (ada-002 default).
func HandleEmbeddings(c *gin.Context) {
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
