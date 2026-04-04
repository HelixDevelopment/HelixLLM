package i18n_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/i18n"
)

func TestTranslator_English_DefaultMessages(t *testing.T) {
	tr := i18n.New("en")

	cases := []struct {
		key  string
		want string
	}{
		{i18n.KeyInvalidAPIKey, "Invalid API key provided"},
		{i18n.KeyRateLimitExceeded, "Rate limit exceeded, please try again later"},
		{i18n.KeyInternalError, "Internal server error"},
	}

	for _, tc := range cases {
		got := tr.T("en", tc.key)
		if got != tc.want {
			t.Errorf("T(en, %q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func TestTranslator_TemplateSubstitution_Model(t *testing.T) {
	tr := i18n.New("en")

	got := tr.T("en", i18n.KeyModelNotFound, map[string]string{
		"model": "gpt-99",
	})
	want := "The model 'gpt-99' does not exist"
	if got != want {
		t.Errorf("T() = %q, want %q", got, want)
	}
}

func TestTranslator_TemplateSubstitution_Detail(t *testing.T) {
	tr := i18n.New("en")

	got := tr.T("en", i18n.KeyInvalidRequest, map[string]string{
		"detail": "missing field 'prompt'",
	})
	want := "Invalid request: missing field 'prompt'"
	if got != want {
		t.Errorf("T() = %q, want %q", got, want)
	}
}

func TestTranslator_FallbackToDefaultLang(t *testing.T) {
	tr := i18n.New("en")
	// Load French, but omit model_not_found so fallback to English fires.
	tr.LoadMessages("fr", map[string]string{
		i18n.KeyInvalidAPIKey: "Clé API invalide",
	})

	// French translation present → use it.
	got := tr.T("fr", i18n.KeyInvalidAPIKey)
	if got != "Clé API invalide" {
		t.Errorf("T(fr, invalid_api_key) = %q, want French message", got)
	}

	// French translation absent → fall back to English default.
	got = tr.T("fr", i18n.KeyInternalError)
	want := "Internal server error"
	if got != want {
		t.Errorf("T(fr, internal_error) = %q, want English fallback %q", got, want)
	}
}

func TestTranslator_UnknownKeyReturnsKey(t *testing.T) {
	tr := i18n.New("en")

	key := "totally_unknown_key"
	got := tr.T("en", key)
	if got != key {
		t.Errorf("T(en, %q) = %q, want key itself", key, got)
	}
}

func TestTranslator_UnknownKeyUnknownLang(t *testing.T) {
	tr := i18n.New("en")

	key := "no_such_key"
	got := tr.T("zz", key)
	if got != key {
		t.Errorf("T(zz, %q) = %q, want key itself", key, got)
	}
}

func TestTranslator_LoadMessages_CustomLang(t *testing.T) {
	tr := i18n.New("en")
	tr.LoadMessages("de", map[string]string{
		i18n.KeyRateLimitExceeded: "Anfragelimit überschritten, bitte später erneut versuchen",
	})

	got := tr.T("de", i18n.KeyRateLimitExceeded)
	want := "Anfragelimit überschritten, bitte später erneut versuchen"
	if got != want {
		t.Errorf("T(de, rate_limit_exceeded) = %q, want %q", got, want)
	}
}

func TestTranslator_NoVars(t *testing.T) {
	tr := i18n.New("en")
	// Template placeholders remain intact when no vars are passed.
	got := tr.T("en", i18n.KeyModelNotFound)
	want := "The model '{{model}}' does not exist"
	if got != want {
		t.Errorf("T() without vars = %q, want %q", got, want)
	}
}
