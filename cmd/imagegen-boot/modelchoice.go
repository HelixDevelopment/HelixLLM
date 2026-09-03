package main

// Measured model selection for the IMAGE-GENERATION lane (T036 / FR-056).
//
// The rule this file exists to enforce: WHICH model runs is decided from the
// MEASURED host, never from a static preconfigured value. Configuration may
// still say WHERE the catalogue lives (HELIXLLM_CATALOGUE_DIR), what the user
// has DECLARED about their usage (HELIXLLM_DECLARED_USAGE) and which options
// they FORBID (IMAGEGEN_FORBID_MODELS). None of those names the model.
//
// The DECISION ITSELF now lives in internal/laneboot, shared by all four boot
// lanes; it was previously one of four byte-identical copies. What remains
// below is the half that is genuinely the image lane's: where its weights come
// from, which serving precision the chosen build implies, its own not-servable
// exit code, and the two small helpers those use.
//
// One of those helpers, normalise, has had no caller here since it was
// introduced (1b66afb) — it is left in place rather than deleted, because
// removing apparently-dead code needs its own investigation of how it came to
// be unwired, and its own commit.
//
// The properties laneboot enforces — no fixed default, and a pin that
// constrains rather than bypasses — are documented there and are not restated
// here, so there is one place to read them and one place to change them.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/selection"
)

// exitNotServable is this lane's own refusal code: the host CAN serve a chosen
// option, but this runtime cannot serve that BUILD of it. The codes the shared
// decision reaches (20-23) live in laneboot; this one stays here because only
// this lane knows which precisions it serves.
const exitNotServable = 24

// choice is one decided model, with the measurement it was decided from and the
// artefacts on this host that serve it.
type choice struct {
	Option  selection.Option
	Entry   catalogue.Entry
	Profile capability.HostCapabilityProfile
	// Usage is the purpose the terms were applied against, so a report can
	// state what the licence permits rather than merely naming it.
	Usage catalogue.UsagePurpose

	// Repository is where the chosen model's weights are obtained from, taken
	// from the chosen entry's recorded source. It is DERIVED from the decision,
	// never read from configuration.
	Repository string
	// Precision is the serving precision the chosen build implies.
	Precision string
}

// repositoryFor is where the chosen model's weights are obtained from.
//
// It is derived from the catalogue entry the decision selected — never read
// from configuration. An entry whose source is not a repository reference is
// refused rather than guessed at (§11.4.6): starting the runtime with a blank
// or malformed model reference would fall back to whatever default the runtime
// itself carries, which is exactly the static selection FR-056 forbids.
func repositoryFor(e catalogue.Entry) (string, error) {
	src := strings.TrimSpace(e.Source)
	if src == "" {
		return "", fmt.Errorf("catalogue entry %s records no weight source", e.Identity())
	}
	const host = "https://huggingface.co/"
	if !strings.HasPrefix(src, host) {
		return "", fmt.Errorf("weight source %q for %s is not a model-repository reference this runtime can pull",
			src, e.Identity())
	}
	repo := strings.Trim(strings.TrimPrefix(src, host), "/")
	if strings.Count(repo, "/") != 1 || repo == "" {
		return "", fmt.Errorf("weight source %q for %s is not an <owner>/<model> repository", src, e.Identity())
	}
	return repo, nil
}

// servingPrecisions are the precisions the image-generation runtime actually
// implements (services/imagegen). A build whose variant is outside this set is
// not servable HERE — reported honestly rather than silently coerced into a
// precision the entry's memory figure was never measured at.
var servingPrecisions = map[string]string{
	"nvfp4": "nvfp4",
	"nf4":   "nf4",
	"bf16":  "bf16",
}

// precisionFor reports the serving precision the chosen build implies.
func precisionFor(o selection.Option) (string, error) {
	p, ok := servingPrecisions[strings.ToLower(strings.TrimSpace(o.Variant))]
	if !ok {
		return "", fmt.Errorf("build %q of %s is not one this runtime serves (%s)",
			o.Variant, o.ModelID, strings.Join(sortedKeys(servingPrecisions), ", "))
	}
	return p, nil
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// normalise reduces a name to its alphanumeric core so a catalogue identity and
// a filename can be compared without depending on either one's punctuation.
func normalise(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
