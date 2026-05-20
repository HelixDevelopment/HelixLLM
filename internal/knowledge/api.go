package knowledge

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/i18n"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

// RegisterKnowledgeRoutes attaches the knowledge internal API handlers to r.
//
//	POST /internal/knowledge/ingest      — IngestRequest → IngestResult
//	POST /internal/knowledge/query       — QueryRequest  → QueryResult
//	GET  /internal/knowledge/collections — []Collection
//	GET  /internal/knowledge/stats       — Stats
func RegisterKnowledgeRoutes(r *gin.Engine, pipeline *Pipeline) {
	g := r.Group("/internal/knowledge")
	g.POST("/ingest", handleIngest(pipeline))
	g.POST("/query", handleQuery(pipeline))
	g.GET("/collections", handleCollections(pipeline))
	g.GET("/stats", handleStats(pipeline))
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleIngest(p *Pipeline) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req IngestRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, errorResponse(
				trKnowledge(c, i18n.KeyKnowledgeInvalidRequestBody,
					map[string]string{"detail": err.Error()})))
			return
		}

		result, err := p.Ingest(c.Request.Context(), req)
		if err != nil {
			status := http.StatusInternalServerError
			if isValidationError(err) {
				status = http.StatusBadRequest
			}
			c.JSON(status, errorResponse(err.Error()))
			return
		}

		c.JSON(http.StatusOK, result)
	}
}

func handleQuery(p *Pipeline) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req QueryRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, errorResponse(
				trKnowledge(c, i18n.KeyKnowledgeInvalidRequestBody,
					map[string]string{"detail": err.Error()})))
			return
		}

		result, err := p.Query(c.Request.Context(), req)
		if err != nil {
			status := http.StatusInternalServerError
			if isValidationError(err) {
				status = http.StatusBadRequest
			}
			c.JSON(status, errorResponse(err.Error()))
			return
		}

		c.JSON(http.StatusOK, result)
	}
}

func handleCollections(p *Pipeline) gin.HandlerFunc {
	return func(c *gin.Context) {
		cols, err := p.Collections(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		c.JSON(http.StatusOK, cols)
	}
}

func handleStats(p *Pipeline) gin.HandlerFunc {
	return func(c *gin.Context) {
		stats, err := p.Stats(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		c.JSON(http.StatusOK, stats)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func errorResponse(msg string) api.ErrorResponse {
	return api.ErrorResponse{
		Error: api.ErrorDetail{
			Message: msg,
			Type:    "knowledge_error",
		},
	}
}

// isValidationError returns true when the error originated from pipeline
// input validation (empty content, empty collection, empty query).
func isValidationError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "must not be empty")
}
