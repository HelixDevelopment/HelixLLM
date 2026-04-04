package middleware

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/gin-gonic/gin"
)

// RequestID is a Gin middleware that ensures every request carries an
// X-Request-ID header. If the incoming request already has one, it is
// preserved; otherwise a cryptographically-random 32-hex-char ID is
// generated. The ID is also stored in the Gin context under the key
// "request_id" for downstream handlers.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = generateID()
			c.Request.Header.Set("X-Request-ID", id)
		}
		c.Header("X-Request-ID", id)
		c.Set("request_id", id)
		c.Next()
	}
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b) //nolint:errcheck // crypto/rand.Read never returns an error on supported platforms
	return hex.EncodeToString(b)
}
