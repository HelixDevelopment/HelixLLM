package gateway

import (
	"github.com/gin-gonic/gin"

	gwmw "github.com/HelixDevelopment/HelixLLM/internal/gateway/middleware"
)

// RouterOptions configures the gateway middleware applied to all /v1 routes.
type RouterOptions struct {
	// APIKeys is a comma-separated list of valid Bearer tokens.
	// Empty string means open-access (no authentication required).
	APIKeys string
	// RateLimit is the maximum number of requests per minute per IP.
	// 0 disables rate limiting.
	RateLimit int
}

// RegisterRoutes attaches all gateway endpoint handlers and middleware to r
// under the /v1 prefix.
func RegisterRoutes(r *gin.Engine, opts RouterOptions) {
	v1 := r.Group("/v1")
	v1.Use(gwmw.APIKeyAuth(opts.APIKeys))
	v1.Use(gwmw.RateLimit(opts.RateLimit))

	// OpenAI-compatible endpoints
	v1.POST("/chat/completions", HandleChatCompletions)
	v1.POST("/completions", HandleCompletions)
	v1.GET("/models", HandleListModels)
	v1.GET("/models/:id", HandleGetModel)
	v1.POST("/embeddings", HandleEmbeddings)

	// Anthropic-compatible endpoints
	v1.POST("/messages", HandleMessages)
}
