package metrics

import (
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/i18n"
)

// fakeMetricsTranslator is a non-production test double (unit-test only,
// per CONST-050(A)) that resolves every metrics-package i18n key to a
// localised marker so tests can prove the call site routes through the
// Translator seam rather than emitting a hardcoded English literal.
type fakeMetricsTranslator struct {
	prefix string
}

// T returns prefix+key for every key, ignoring vars. The marked output
// proves the seam was consulted: if a call site emitted a hardcoded
// English literal, the marker would be absent.
func (f fakeMetricsTranslator) T(_, key string, _ ...map[string]string) string {
	return f.prefix + key
}

// restoreMetricsI18n resets the package-level translator + lang to the
// English default so tests do not leak state into one another.
func restoreMetricsI18n(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		SetTranslator(nil)
		SetLang("")
	})
}

// allMetricKeys is every i18n key the metrics package owns. The test
// suite walks it so a newly added metric without a key is caught.
var allMetricKeys = []string{
	keyHelpModelInferenceTotal,
	keyHelpModelInferenceDuration,
	keyHelpModelTokensGenerated,
	keyHelpModelTokensPerSecond,
	keyHelpRAGSearchDuration,
	keyHelpRAGDocumentsIndexed,
	keyHelpToolExecutionTotal,
	keyHelpToolExecutionDuration,
	keyHelpAPIRequestsTotal,
	keyHelpAPIRequestDuration,
	keyHelpActiveConnections,
	keyHelpVRAMUsageBytes,
	keyHelpRAMUsageBytes,
}

// TestMetricsI18n_DefaultFallbackIsEnglish verifies that with no
// translator wired, every metric help key resolves to the bundled
// English fallback — a sensible standalone default per CONST-046 /
// CONST-051(B).
func TestMetricsI18n_DefaultFallbackIsEnglish(t *testing.T) {
	restoreMetricsI18n(t)
	SetTranslator(nil)

	for _, key := range allMetricKeys {
		got := helpText(key)
		want, ok := englishFallbacks[key]
		if !ok {
			t.Fatalf("key %q has no English fallback", key)
		}
		if got != want {
			t.Errorf("helpText(%q) = %q, want %q", key, got, want)
		}
		if got == "" {
			t.Errorf("helpText(%q) returned empty string", key)
		}
	}
}

// TestMetricsI18n_TranslatorSeamConsulted is the anti-bluff core test:
// it wires a fake translator and asserts every metric help key resolves
// through the seam carrying the fake's marker. If helpText regressed to
// a hardcoded literal, the marker would be missing and this test FAILs
// — that is the paired mutation per §1.1.
func TestMetricsI18n_TranslatorSeamConsulted(t *testing.T) {
	restoreMetricsI18n(t)
	const marker = "XX_LOCALE::"
	SetTranslator(fakeMetricsTranslator{prefix: marker})
	SetLang("xx")

	for _, key := range allMetricKeys {
		got := helpText(key)
		if !strings.HasPrefix(got, marker) {
			t.Errorf("helpText(%q) = %q — translator seam NOT consulted "+
				"(missing marker %q)", key, got, marker)
		}
		if got != marker+key {
			t.Errorf("helpText(%q) = %q, want %q", key, got, marker+key)
		}
	}
}

// TestMetricsI18n_MissingKeyFallsBackToEnglish verifies that when the
// wired translator does not know a key (returns the key verbatim),
// helpText still emits the bundled English text — users never see a
// raw key.
func TestMetricsI18n_MissingKeyFallsBackToEnglish(t *testing.T) {
	restoreMetricsI18n(t)
	// emptyTranslator returns the key verbatim for every lookup,
	// simulating a translator with no messages loaded.
	SetTranslator(emptyMetricsTranslator{})

	for _, key := range allMetricKeys {
		got := helpText(key)
		want := englishFallbacks[key]
		if got != want {
			t.Errorf("helpText(%q) on translator miss = %q, want English fallback %q",
				key, got, want)
		}
	}
}

// emptyMetricsTranslator returns the key verbatim for every lookup.
type emptyMetricsTranslator struct{}

func (emptyMetricsTranslator) T(_, key string, _ ...map[string]string) string {
	return key
}

// TestMetricsI18n_RealTranslatorLocalises proves the seam works with a
// real upstream Translator carrying a non-English bundle: a localised
// help text supersedes the English fallback.
func TestMetricsI18n_RealTranslatorLocalises(t *testing.T) {
	restoreMetricsI18n(t)
	tr := i18n.New("en")
	tr.LoadMessages("en", englishFallbacks)
	tr.LoadMessages("sr", map[string]string{
		keyHelpModelInferenceTotal: "Укупан број инференци модела.",
	})
	SetTranslator(tr)
	SetLang("sr")

	got := helpText(keyHelpModelInferenceTotal)
	if got != "Укупан број инференци модела." {
		t.Errorf("helpText localised = %q, want Serbian text", got)
	}
	// A key absent in the sr bundle must fall back to English.
	gotEN := helpText(keyHelpRAMUsageBytes)
	if gotEN != englishFallbacks[keyHelpRAMUsageBytes] {
		t.Errorf("helpText sr fallback = %q, want English %q",
			gotEN, englishFallbacks[keyHelpRAMUsageBytes])
	}
}

// TestMetricsI18n_FallbackCountMatchesKeys is a structural guard: every
// declared key MUST have exactly one English fallback and vice versa,
// so no key silently lacks a human-readable string.
func TestMetricsI18n_FallbackCountMatchesKeys(t *testing.T) {
	if len(englishFallbacks) != len(allMetricKeys) {
		t.Fatalf("englishFallbacks has %d entries, allMetricKeys has %d — "+
			"every key needs exactly one fallback", len(englishFallbacks), len(allMetricKeys))
	}
	for _, key := range allMetricKeys {
		if _, ok := englishFallbacks[key]; !ok {
			t.Errorf("key %q has no English fallback", key)
		}
	}
}
