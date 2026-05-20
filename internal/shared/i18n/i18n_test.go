package i18n_test

import (
	"strings"
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

// CONST-046 round-321: regression test for the cluster-monitor TUI keys
// migrated from hardcoded literals in internal/control/tui.go. Each
// English template MUST be loaded by i18n.New("en") and resolve to a
// non-key string.
func TestTranslator_MonitorKeys_English(t *testing.T) {
	tr := i18n.New("en")

	cases := []struct {
		key  string
		want string
	}{
		{i18n.KeyMonitorTitle, "HelixLLM Cluster Monitor"},
		{i18n.KeyMonitorTitleRule, "========================"},
		{i18n.KeyMonitorNoHosts, "No hosts configured."},
		{i18n.KeyMonitorColHost, "HOST"},
		{i18n.KeyMonitorColStatus, "STATUS"},
		{i18n.KeyMonitorColCPUCores, "CPU CORES"},
		{i18n.KeyMonitorColMemoryMB, "MEMORY (MB)"},
		{i18n.KeyMonitorColDeploys, "DEPLOYMENTS"},
		{i18n.KeyMonitorOverallOK, "healthy"},
		{i18n.KeyMonitorOverallBad, "DEGRADED"},
	}

	for _, tc := range cases {
		got := tr.T("en", tc.key)
		if got != tc.want {
			t.Errorf("T(en, %q) = %q, want %q", tc.key, got, tc.want)
		}
		// Paired mutation: a bare-key return means the template was
		// never loaded — the migration silently broke the TUI.
		if got == tc.key {
			t.Errorf("T(en, %q) returned bare key — template not loaded", tc.key)
		}
	}
}

// CONST-046 round-321: template-substitution test for the monitor's
// composed status line. {{overall}}, {{hosts}}, {{time}} MUST all be
// substituted.
func TestTranslator_MonitorClusterState_Substitution(t *testing.T) {
	tr := i18n.New("en")

	got := tr.T("en", i18n.KeyMonitorClusterState, map[string]string{
		"overall": "healthy",
		"hosts":   "3",
		"time":    "2026-05-20T10:00:00Z",
	})
	want := "Cluster: healthy  |  Hosts: 3  |  Last check: 2026-05-20T10:00:00Z"
	if got != want {
		t.Errorf("T() = %q, want %q", got, want)
	}
}

func TestTranslator_MonitorLastCheck_Substitution(t *testing.T) {
	tr := i18n.New("en")

	got := tr.T("en", i18n.KeyMonitorLastCheck, map[string]string{
		"time": "2026-05-20T10:00:00Z",
	})
	want := "Last check: 2026-05-20T10:00:00Z"
	if got != want {
		t.Errorf("T() = %q, want %q", got, want)
	}
}

// CONST-046 round-321: cross-language fallback for the monitor keys —
// a non-English operator MUST receive the English template, never the
// raw key.
func TestTranslator_MonitorKeys_FallbackToEnglish(t *testing.T) {
	tr := i18n.New("en")

	got := tr.T("ja", i18n.KeyMonitorTitle)
	if got == i18n.KeyMonitorTitle {
		t.Fatalf("Japanese lookup of %q returned bare key — English fallback did not fire",
			i18n.KeyMonitorTitle)
	}
	if got != "HelixLLM Cluster Monitor" {
		t.Errorf("T(ja, %q) = %q, want English fallback", i18n.KeyMonitorTitle, got)
	}
}

// CONST-046 round-391: regression test for the HTTP gateway + control-
// plane API keys migrated from hardcoded literals in
// internal/gateway/{openai,anthropic,websocket}.go and
// internal/control/api.go. Each English template MUST be loaded by
// i18n.New("en") and resolve to a non-key string.
func TestTranslator_GatewayKeys_English(t *testing.T) {
	tr := i18n.New("en")

	cases := []struct {
		key  string
		want string
	}{
		{i18n.KeyGatewayGreeting, "Hello! I'm HelixLLM."},
		{i18n.KeyGatewayHelpAcknowledgement, "Yes, I can help with that. What would you like me to do?"},
		{i18n.KeyControlNoServicesSpecified, "no services specified"},
	}

	for _, tc := range cases {
		got := tr.T("en", tc.key)
		if got != tc.want {
			t.Errorf("T(en, %q) = %q, want %q", tc.key, got, tc.want)
		}
		// Paired mutation: a bare-key return means the template was
		// never loaded — the migration silently broke the API.
		if got == tc.key {
			t.Errorf("T(en, %q) returned bare key — template not loaded", tc.key)
		}
	}
}

// CONST-046 round-391: template-substitution test for the gateway +
// control-plane error keys. {{detail}} / {{model}} MUST be substituted.
func TestTranslator_GatewayKeys_Substitution(t *testing.T) {
	tr := i18n.New("en")

	cases := []struct {
		key  string
		vars map[string]string
		want string
	}{
		{i18n.KeyGatewayInvalidRequestBody, map[string]string{"detail": "EOF"}, "invalid request body: EOF"},
		{i18n.KeyGatewayInvalidRequest, map[string]string{"detail": "bad json"}, "invalid request: bad json"},
		{i18n.KeyGatewayBrainError, map[string]string{"detail": "timeout"}, "brain error: timeout"},
		{i18n.KeyGatewayBrainStreamError, map[string]string{"detail": "closed"}, "brain stream error: closed"},
		{i18n.KeyGatewayModelNotFound, map[string]string{"model": `"gpt-x"`}, `model "gpt-x" not found`},
		{i18n.KeyControlSchedulingFailed, map[string]string{"detail": "no hosts"}, "scheduling failed: no hosts"},
		{i18n.KeyControlRebalanceFailed, map[string]string{"detail": "no hosts"}, "rebalance scheduling failed: no hosts"},
	}

	for _, tc := range cases {
		got := tr.T("en", tc.key, tc.vars)
		if got != tc.want {
			t.Errorf("T(en, %q) = %q, want %q", tc.key, got, tc.want)
		}
		// Paired mutation: an un-substituted {{placeholder}} means the
		// migration broke variable interpolation.
		if strings.Contains(got, "{{") {
			t.Errorf("T(en, %q) = %q — placeholder not substituted", tc.key, got)
		}
	}
}

// CONST-046 round-391: cross-language fallback — a non-English API
// client MUST receive the English template, never the raw key.
func TestTranslator_GatewayKeys_FallbackToEnglish(t *testing.T) {
	tr := i18n.New("en")

	got := tr.T("sr", i18n.KeyGatewayGreeting)
	if got == i18n.KeyGatewayGreeting {
		t.Fatalf("Serbian lookup of %q returned bare key — English fallback did not fire",
			i18n.KeyGatewayGreeting)
	}
	if got != "Hello! I'm HelixLLM." {
		t.Errorf("T(sr, %q) = %q, want English fallback", i18n.KeyGatewayGreeting, got)
	}
}
