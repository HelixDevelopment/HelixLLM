package metrics

import (
	"time"

	"github.com/gin-gonic/gin"
)

// GinMiddleware returns a Gin middleware that records per-request Prometheus
// metrics: active connection gauge, request counter, and latency histogram.
//
// Mount it BEFORE authentication middleware so that the /metrics scrape
// endpoint itself is not counted (it is registered outside the /v1 group).
func GinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ActiveConnections.Inc()
		start := time.Now()

		c.Next()

		duration := time.Since(start)
		ActiveConnections.Dec()

		endpoint := c.FullPath()
		if endpoint == "" {
			endpoint = "unknown"
		}
		TrackAPIRequest(endpoint, c.Request.Method, c.Writer.Status(), duration)
	}
}
