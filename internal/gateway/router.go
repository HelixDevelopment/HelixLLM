package gateway

import (
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	gwmw "github.com/HelixDevelopment/HelixLLM/internal/gateway/middleware"
	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/metrics"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
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
	// Embedder is the knowledge layer's embedding provider. When non-nil,
	// /v1/embeddings delegates to it instead of returning zero vectors.
	Embedder knowledge.Embedder
	// ToolManager handles tool schema compression and budget-aware selection.
	// When non-nil, tools are compressed before being sent to the model.
	ToolManager *ToolManager
	// RAGHook augments requests with retrieved codebase context before
	// sending to the model. This is the KEY to making small models work
	// for coding tasks — they don't need to explore via tools because
	// the relevant context is already in the prompt.
	RAGHook func(*types.InternalChatRequest) *types.InternalChatRequest
	// HardwareProfile is the detected hardware profile exposed via the
	// /v1/hardware endpoint. Typed as interface{} to avoid coupling the
	// gateway package to the hardware package.
	HardwareProfile interface{} // *hardware.HardwareProfile
}

// RegisterRoutes attaches all gateway endpoint handlers and middleware to r
// under the /v1 prefix.
func RegisterRoutes(r *gin.Engine, opts RouterOptions) {
	// Prometheus metrics endpoint — outside /v1 auth so scrapers can
	// reach it without an API key.
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Global metrics middleware tracks every request.
	r.Use(metrics.GinMiddleware())

	v1 := r.Group("/v1")
	v1.Use(gwmw.APIKeyAuth(opts.APIKeys))
	v1.Use(gwmw.RateLimit(opts.RateLimit))
	v1.Use(gwmw.SecurityHeaders())
	if opts.TOONEnabled {
		v1.Use(gwmw.ContentNegotiation())
	}

	// OpenAI-compatible endpoints
	v1.POST("/chat/completions", HandleChatCompletions(opts.Brain, opts.ToolManager, opts.RAGHook))
	v1.POST("/completions", HandleCompletions(opts.Brain))
	v1.GET("/models", HandleListModels(opts.Brain))
	v1.GET("/models/:id", HandleGetModel(opts.Brain))
	v1.POST("/embeddings", HandleEmbeddings(opts.Brain, opts.Embedder))

	// Hardware profile endpoint
	v1.GET("/hardware", func(c *gin.Context) {
		c.JSON(200, opts.HardwareProfile)
	})

	// Anthropic-compatible endpoints
	v1.POST("/messages", HandleMessages(opts.Brain))

	// WebSocket endpoint for bidirectional chat
	r.GET("/ws", HandleWebSocket(opts.Brain))
}
