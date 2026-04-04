// Package i18n wraps digital.vasic.i18n to provide multi-language API
// error messages for HelixLLM.  English messages for the most common
// API errors are pre-loaded at construction time.
package i18n

import (
	upstream "digital.vasic.i18n/pkg/i18n"
)

// English error message keys.
const (
	KeyInvalidAPIKey       = "invalid_api_key"
	KeyRateLimitExceeded   = "rate_limit_exceeded"
	KeyModelNotFound       = "model_not_found"
	KeyInvalidRequest      = "invalid_request"
	KeyInternalError       = "internal_error"
)

// defaultEnglishMessages is pre-loaded into every new Translator.
var defaultEnglishMessages = map[string]string{
	KeyInvalidAPIKey:     "Invalid API key provided",
	KeyRateLimitExceeded: "Rate limit exceeded, please try again later",
	KeyModelNotFound:     "The model '{{model}}' does not exist",
	KeyInvalidRequest:    "Invalid request: {{detail}}",
	KeyInternalError:     "Internal server error",
}

// Translator wraps an upstream Bundle and exposes a simplified API
// that accepts string variable maps instead of interface{} maps.
type Translator struct {
	bundle *upstream.Bundle
}

// New creates a Translator whose default (fallback) language is
// defaultLang.  English messages for the common API error keys are
// pre-loaded automatically.
func New(defaultLang string) *Translator {
	b := upstream.NewBundle(defaultLang)
	b.AddMessages("en", defaultEnglishMessages)
	return &Translator{bundle: b}
}

// LoadMessages adds (or merges) messages for the given language tag.
// Call this before the first call to T for that language.
func (t *Translator) LoadMessages(lang string, messages map[string]string) {
	t.bundle.AddMessages(lang, messages)
}

// T returns the localised message for key in language lang.
// vars is an optional replacement map: keys match {{placeholder}} tokens
// in the message template.  Variable substitution is delegated to the
// upstream Bundle.
// Falls back to the default language when the key is absent in lang.
// Returns key itself when the key is absent in all loaded languages.
func (t *Translator) T(lang, key string, vars ...map[string]string) string {
	var params map[string]interface{}
	if len(vars) > 0 && vars[0] != nil {
		params = make(map[string]interface{}, len(vars[0]))
		for k, v := range vars[0] {
			params[k] = v
		}
	}
	if params != nil {
		return t.bundle.GetMessage(lang, key, params)
	}
	return t.bundle.GetMessage(lang, key)
}
