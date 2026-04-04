package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

const contentFormatKey = "content_format"

// ContentNegotiation returns a Gin middleware that inspects the Accept header
// and sets a context flag indicating the negotiated response format.
//
// If the client sends "Accept: application/toon" the format is set to "toon".
// All other values (including an absent header) default to "json".
// The negotiated format is also reflected in the X-Content-Format response header
// so clients can confirm which encoding was selected.
func ContentNegotiation() gin.HandlerFunc {
	return func(c *gin.Context) {
		format := "json"
		accept := c.GetHeader("Accept")
		if strings.Contains(accept, "application/toon") {
			format = "toon"
		}
		c.Set(contentFormatKey, format)
		c.Header("X-Content-Format", format)
		c.Next()
	}
}

// GetContentFormat retrieves the negotiated content format from the Gin context.
// Returns "json" if the value is absent (i.e. the middleware was not applied).
func GetContentFormat(c *gin.Context) string {
	if v, exists := c.Get(contentFormatKey); exists {
		return v.(string)
	}
	return "json"
}
