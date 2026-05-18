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

// CONST-046 round-95: regression test for the two CLI keys migrated
// from cmd/helixllm/{challenges,main}.go. The English templates MUST
// be loaded by i18n.New("en") and substitute {{detail}} correctly.
func TestTranslator_HelixllmCLIFailedToLoadBanks_English(t *testing.T) {
	tr := i18n.New("en")
	got := tr.T("en", i18n.KeyHelixllmCLIFailedToLoadBanks, map[string]string{
		"detail": "open banks/: no such file or directory",
	})
	want := "failed to load banks: open banks/: no such file or directory"
	if got != want {
		t.Errorf("T() = %q, want %q", got, want)
	}
}

func TestTranslator_HelixllmCLIErrorLoadingConfig_English(t *testing.T) {
	tr := i18n.New("en")
	got := tr.T("en", i18n.KeyHelixllmCLIErrorLoadingConfig, map[string]string{
		"detail": "permission denied",
	})
	want := "error loading config: permission denied"
	if got != want {
		t.Errorf("T() = %q, want %q", got, want)
	}
}

// CONST-046 round-95: cross-language fallback test for the CLI keys.
// French speakers asking the CLI for a non-translated key MUST get the
// English template (not a Go fmt.Errorf wrapper, not the raw key).
func TestTranslator_HelixllmCLIKeys_FallbackToEnglish(t *testing.T) {
	tr := i18n.New("en")
	got := tr.T("fr", i18n.KeyHelixllmCLIFailedToLoadBanks, map[string]string{
		"detail": "no banks dir",
	})
	if got == i18n.KeyHelixllmCLIFailedToLoadBanks {
		t.Fatalf("French lookup returned bare key %q — fallback to English template did not fire", got)
	}
	wantPrefix := "failed to load banks:"
	if len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Errorf("T(fr, %q) = %q, want English fallback starting with %q",
			i18n.KeyHelixllmCLIFailedToLoadBanks, got, wantPrefix)
	}
}

// TranslatorAPI compile-time assertion — *Translator MUST satisfy the
// minimal contract used by call sites (cmd/helixllm/challenges.go,
// cmd/helixllm/main.go). Decoupling proof per CONST-051(B).
func TestTranslatorAPI_ContractSatisfied(t *testing.T) {
	var _ i18n.TranslatorAPI = (*i18n.Translator)(nil)
	var _ i18n.TranslatorAPI = i18n.New("en")
}
