package gateway

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/i18n"
)

// ctxWithAcceptLanguage builds a minimal *gin.Context carrying the given
// Accept-Language header value (empty string means no header).
func ctxWithAcceptLanguage(header string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	if header != "" {
		c.Request.Header.Set("Accept-Language", header)
	}
	return c
}

// CONST-046 round-391: langFromContext MUST extract the primary language
// subtag from a variety of Accept-Language header shapes and fall back to
// "en" when the header is absent, empty, or wildcard-only.
func TestLangFromContext(t *testing.T) {
	cases := []struct {
		header string
		want   string
	}{
		{"", "en"},
		{"*", "en"},
		{"en", "en"},
		{"en-US", "en"},
		{"sr-RS,sr;q=0.9,en;q=0.8", "sr"},
		{"ja;q=1.0", "ja"},
		{"de_DE", "de"},
		{"  fr-FR  ", "fr"},
		{"ZH-CN", "zh"},
	}
	for _, tc := range cases {
		got := langFromContext(ctxWithAcceptLanguage(tc.header))
		if got != tc.want {
			t.Errorf("langFromContext(%q) = %q, want %q", tc.header, got, tc.want)
		}
	}
	// Nil context MUST not panic and MUST fall back to English.
	if got := langFromContext(nil); got != "en" {
		t.Errorf("langFromContext(nil) = %q, want en", got)
	}
}

// CONST-046 round-391: the tr helper MUST resolve a gateway key through
// the package translator using the negotiated language. An English
// request gets the English template; a request in a locale without a
// loaded bundle falls back to English — never the bare key.
func TestTr_ResolvesGatewayKey(t *testing.T) {
	got := tr(ctxWithAcceptLanguage("en"),
		i18n.KeyGatewayInvalidRequestBody,
		map[string]string{"detail": "unexpected EOF"})
	want := "invalid request body: unexpected EOF"
	if got != want {
		t.Fatalf("tr(en, KeyGatewayInvalidRequestBody) = %q, want %q", got, want)
	}

	// Paired mutation: a Serbian request MUST still resolve (English
	// fallback) and MUST NOT return the bare key or an un-substituted
	// placeholder — that would mean the migration broke localisation.
	sr := tr(ctxWithAcceptLanguage("sr-RS"), i18n.KeyGatewayGreeting)
	if sr == i18n.KeyGatewayGreeting {
		t.Errorf("tr(sr, KeyGatewayGreeting) returned bare key — fallback broken")
	}
	if sr != "Hello! I'm HelixLLM." {
		t.Errorf("tr(sr, KeyGatewayGreeting) = %q, want English fallback", sr)
	}
}

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}
