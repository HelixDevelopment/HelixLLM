package middleware

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"digital.vasic.ratelimiter/pkg/limiter"
	"digital.vasic.ratelimiter/pkg/memory"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

// RateLimit returns a Gin middleware that enforces per-IP rate limiting.
//
// maxPerMinute is the maximum number of requests allowed per IP address per
// minute. When maxPerMinute is 0 the middleware is disabled and all requests
// pass through unconditionally.
//
// Exceeded requests receive a 429 response in the OpenAI-compatible error
// JSON format.
func RateLimit(maxPerMinute int) gin.HandlerFunc {
	if maxPerMinute <= 0 {
		return func(c *gin.Context) { c.Next() }
	}

	rl := memory.New(&limiter.Config{
		Rate:   maxPerMinute,
		Window: 60_000_000_000, // one minute in nanoseconds (time.Minute)
	})

	return func(c *gin.Context) {
		ip := clientIP(c)

		result, err := rl.Allow(context.Background(), ip)
		if err != nil || result.Allowed {
			// Fail-open on limiter errors (consistent with the submodule design).
			c.Next()
			return
		}

		c.AbortWithStatusJSON(http.StatusTooManyRequests, api.ErrorResponse{
			Error: api.ErrorDetail{
				Message: "rate limit exceeded",
				Type:    "rate_limit_error",
			},
		})
	}
}

// clientIP extracts the bare IP address from the Gin context, stripping any
// port suffix that may be present in RemoteAddr.
func clientIP(c *gin.Context) string {
	ip := c.ClientIP()
	if ip != "" {
		return ip
	}
	// Fallback: strip port from RemoteAddr.
	addr := c.Request.RemoteAddr
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}
