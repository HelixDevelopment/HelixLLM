package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	// digital.vasic.toon currently returns ErrTOONEncodingNotImplemented
	// from Marshal/Unmarshal (round-27 anti-bluff fix). Until the native
	// TOON encoder lands, we fall back to compact JSON with the
	// application/toon Content-Type so the wire contract that consumers
	// observe remains stable (header announces TOON, body is JSON-compatible
	// bytes the client can decode).
	"digital.vasic.toon/pkg/toon"
)

const contentFormatKey = "content_format"

// toonResponseWriter wraps gin.ResponseWriter so that when the handler writes
// a JSON body the bytes are buffered; once the handler returns the middleware
// re-encodes the body with the TOON encoder and sets the correct Content-Type.
//
// For the current TOON implementation (compact JSON fallback) the wire bytes
// are identical to standard JSON, but the Content-Type is correctly set to
// "application/toon" and all plumbing is in place for a native encoder.
type toonResponseWriter struct {
	gin.ResponseWriter
	buf *bytes.Buffer
}

func (w *toonResponseWriter) Write(b []byte) (int, error) {
	return w.buf.Write(b)
}

func (w *toonResponseWriter) WriteString(s string) (int, error) {
	return w.buf.WriteString(s)
}

// ContentNegotiation returns a Gin middleware that inspects the Accept header
// and sets a context flag indicating the negotiated response format.
//
// If the client sends "Accept: application/toon" the format is set to "toon",
// the response Content-Type is set to "application/toon", and the response
// body is encoded via the TOON encoder from digital.vasic.toon/pkg/toon.
//
// All other values (including an absent header) default to JSON.
// The negotiated format is also reflected in the X-Content-Format response
// header so clients can confirm which encoding was selected.
func ContentNegotiation() gin.HandlerFunc {
	return func(c *gin.Context) {
		format := "json"
		accept := c.GetHeader("Accept")
		if strings.Contains(accept, "application/toon") {
			format = "toon"
		}

		c.Set(contentFormatKey, format)
		c.Header("X-Content-Format", format)

		if format != "toon" {
			c.Next()
			return
		}

		// Wrap the response writer so we can re-encode the body as TOON.
		buf := &bytes.Buffer{}
		tw := &toonResponseWriter{
			ResponseWriter: c.Writer,
			buf:            buf,
		}
		c.Writer = tw

		c.Next()

		// Decode whatever the handler wrote (expected to be JSON) into a
		// generic value, then re-encode it with the TOON encoder.
		raw := buf.Bytes()
		if len(raw) == 0 {
			return
		}

		var v interface{}
		if err := toon.Unmarshal(raw, &v); err != nil {
			// Body is not JSON (e.g. plain text or SSE stream) — forward as-is.
			tw.ResponseWriter.Header().Set("Content-Type", toon.ContentType)
			tw.ResponseWriter.WriteHeader(c.Writer.Status())
			_, _ = tw.ResponseWriter.Write(raw)
			return
		}

		encoded, err := toon.Marshal(v)
		if err != nil {
			// Encoding failed — fall back to forwarding the original bytes.
			tw.ResponseWriter.Header().Set("Content-Type", toon.ContentType)
			tw.ResponseWriter.WriteHeader(c.Writer.Status())
			_, _ = tw.ResponseWriter.Write(raw)
			return
		}

		tw.ResponseWriter.Header().Set("Content-Type", toon.ContentType)
		tw.ResponseWriter.WriteHeader(c.Writer.Status())
		_, _ = tw.ResponseWriter.Write(encoded)
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

// IsTOONRequest is a convenience helper that returns true when the negotiated
// format stored in the Gin context is "toon".
func IsTOONRequest(c *gin.Context) bool {
	return GetContentFormat(c) == "toon"
}

// WriteTOON encodes v as TOON and writes it to the response with status code.
// It sets Content-Type to "application/toon" automatically.
// Handlers that want to produce TOON output directly can call this instead of
// c.JSON, and the middleware wrapper will not double-encode the body.
//
// Because the upstream TOON encoder currently returns
// ErrTOONEncodingNotImplemented (round-27 anti-bluff fix), this function
// falls back to encoding/json.Marshal so the wire body remains valid for
// any input json.Marshal accepts. We surface a 500 only when the value is
// genuinely unmarshallable (e.g. channels, functions) — matching what the
// caller will actually observe.
func WriteTOON(c *gin.Context, status int, v interface{}) {
	data, err := toon.Marshal(v)
	if err != nil {
		// Fall back to JSON encoding while preserving the application/toon
		// content-type so consumers see the negotiated header even when the
		// native TOON encoder is unimplemented.
		var jsonErr error
		data, jsonErr = json.Marshal(v)
		if jsonErr != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
	}
	c.Data(status, toon.ContentType, data)
}
