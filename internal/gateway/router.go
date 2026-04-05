package gateway

import (
	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
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
	// Brain is the LLM coordination service. When non-nil, handlers delegate
	// to it instead of returning development fallback responses.
	Brain *brain.Brain
	// TOONEnabled controls whether the TOON content negotiation middleware
	// is applied. When false, ContentNegotiation() is skipped entirely.
	TOONEnabled bool
}

// RegisterRoutes attaches all gateway endpoint handlers and middleware to r
// under the /v1 prefix.
func RegisterRoutes(r *gin.Engine, opts RouterOptions) {
	v1 := r.Group("/v1")
	v1.Use(gwmw.APIKeyAuth(opts.APIKeys))
	v1.Use(gwmw.RateLimit(opts.RateLimit))
	v1.Use(gwmw.SecurityHeaders())
	if opts.TOONEnabled {
		v1.Use(gwmw.ContentNegotiation())
	}

	// OpenAI-compatible endpoints
	v1.POST("/chat/completions", HandleChatCompletions(opts.Brain))
	v1.POST("/completions", HandleCompletions(opts.Brain))
	v1.GET("/models", HandleListModels(opts.Brain))
	v1.GET("/models/:id", HandleGetModel(opts.Brain))
	v1.POST("/embeddings", HandleEmbeddings(opts.Brain))

	// Anthropic-compatible endpoints
	v1.POST("/messages", HandleMessages(opts.Brain))

	// WebSocket endpoint for bidirectional chat
	r.GET("/ws", HandleWebSocket(opts.Brain))
}
