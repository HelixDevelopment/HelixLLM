package knowledge

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/i18n"
)

// knowledgeTranslator is the package-level i18n Translator shared by the
// knowledge internal-API handlers. It is pre-loaded with the English
// defaults registered in the shared i18n package; consumers MAY call
// LoadMessages on it to register additional locales without touching
// handler code.
//
// CONST-046: user-facing HTTP API error messages MUST NOT be hardcoded
// English literals. Each handler resolves its strings through this
// translator using the request's Accept-Language header.
var knowledgeTranslator = i18n.New("en")

// knowledgeLang extracts the preferred language tag from the gin
// request's Accept-Language header. It returns the primary subtag of the
// first listed language (e.g. "sr" from "sr-RS,sr;q=0.9,en;q=0.8") and
// falls back to "en" when the header is absent or empty.
func knowledgeLang(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return "en"
	}
	header := c.GetHeader("Accept-Language")
	if header == "" {
		return "en"
	}
	first := strings.TrimSpace(strings.Split(header, ",")[0])
	if first == "" || first == "*" {
		return "en"
	}
	first = strings.TrimSpace(strings.Split(first, ";")[0])
	if idx := strings.IndexAny(first, "-_"); idx > 0 {
		first = first[:idx]
	}
	if first == "" {
		return "en"
	}
	return strings.ToLower(first)
}

// trKnowledge resolves the localised message for key using the language
// negotiated from the request context. vars supplies {{placeholder}}
// substitutions.
func trKnowledge(c *gin.Context, key string, vars ...map[string]string) string {
	return knowledgeTranslator.T(knowledgeLang(c), key, vars...)
}
