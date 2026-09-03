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
	// Brain is the primary completion backend used by /v1/chat/completions,
	// /v1/completions, /v1/messages, and the WebSocket handler.
	// It accepts either a *brain.Brain or a *fallback.Chain (both satisfy
	// the Completer interface). When nil, handlers return development
	// fallback responses.
	Brain Completer
	// ModelBrain is used only by /v1/models and /v1/models/:id to list
	// available models. It must be the concrete *brain.Brain because those
	// handlers need Brain.Models(), while Brain above is an interface that may
	// be a fallback.Chain -- which is exactly why these are two options and not
	// one.
	//
	// When nil the endpoints list NOTHING: an empty list carrying a reason that
	// says no backend is configured. They used to return a built-in hardcoded
	// list, and that is gone -- it was a fabricated three-entry advertisement
	// for models this server could never serve.
	//
	// Setting Brain and leaving this nil is therefore a silent no-op for model
	// listing, and it is an easy mistake because the two names are adjacent and
	// one of them is obviously required. The integration test framework made
	// exactly that mistake, and its two model-listing tests failed for so long
	// that three separate agents independently concluded they needed live
	// services. They needed this field.
	ModelBrain *brain.Brain
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
	// SEC-02 hardening: reject malformed-JSON request bodies with 400
	// BEFORE any route handler (and therefore before any brain/provider
	// proxy call) sees them. See middleware.RequireValidJSON for the full
	// rationale and the live reproduction that motivated making this an
	// explicit, centrally-tested gate.
	v1.Use(gwmw.RequireValidJSON())
	if opts.TOONEnabled {
		v1.Use(gwmw.ContentNegotiation())
	}

	// OpenAI-compatible endpoints
	v1.POST("/chat/completions", HandleChatCompletions(opts.Brain, opts.ToolManager, opts.RAGHook))
	v1.POST("/completions", HandleCompletions(opts.Brain))
	v1.GET("/models", HandleListModels(opts.ModelBrain))
	v1.GET("/models/:id", HandleGetModel(opts.ModelBrain))
	v1.POST("/embeddings", HandleEmbeddings(opts.ModelBrain, opts.Embedder))

	// Consumer-configuration endpoints. The Claude Toolkit reads its models
	// off /v1/models above; HelixCode and OpenCode need a configuration
	// FRAGMENT, so they get one here rather than having no path at all.
	v1.GET("/config/:consumer", HandleConsumerConfig(opts.ModelBrain))
	v1.POST("/config/:consumer/merge", HandleConsumerConfigMerge(opts.ModelBrain))

	// Hardware profile endpoint
	v1.GET("/hardware", func(c *gin.Context) {
		c.JSON(200, opts.HardwareProfile)
	})

	// Anthropic-compatible endpoints
	v1.POST("/messages", HandleMessages(opts.Brain))

	// WebSocket endpoint for bidirectional chat
	r.GET("/ws", HandleWebSocket(opts.Brain))
}
