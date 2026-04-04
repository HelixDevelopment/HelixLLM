package middleware

import "github.com/gin-gonic/gin"

// SecurityHeaders returns a Gin middleware that appends standard HTTP security
// headers to every response in the /v1 group.
//
// Headers set:
//   - Strict-Transport-Security  – enforces HTTPS for one year, including subdomains
//   - X-Content-Type-Options     – prevents MIME-type sniffing
//   - X-Frame-Options            – blocks the page from being embedded in a frame
//   - X-XSS-Protection           – enables the legacy XSS filter in older browsers
//   - Content-Security-Policy    – restrictive default-src; no inline scripts/styles
//   - Referrer-Policy            – limits referrer information sent cross-origin
//   - Permissions-Policy         – disables camera, microphone, and geolocation APIs
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Next()
	}
}
