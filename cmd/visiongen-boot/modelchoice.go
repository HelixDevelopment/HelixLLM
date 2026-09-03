package main

// Measured model selection for the VISION lane (T035 / FR-056).
//
// The rule this file exists to enforce: WHICH model runs is decided from the
// MEASURED host, never from a static preconfigured value. Configuration may
// still say WHERE model files live (VISIONGEN_MODEL_DIR,
// HELIXLLM_CATALOGUE_DIR), what the user has DECLARED about their usage
// (HELIXLLM_DECLARED_USAGE) and which options they FORBID
// (VISIONGEN_FORBID_MODELS). None of those names the model.
//
// The DECISION ITSELF now lives in internal/laneboot, shared by all four boot
// lanes; it was previously one of four byte-identical copies. What remains
// below is the half that is genuinely the vision lane's: locating the GGUF
// weights and the matching projector a chosen option needs on this host, and
// its own weights-not-present exit code.
//
// The properties laneboot enforces — no fixed default, and a pin that
// constrains rather than bypasses — are documented there and are not restated
// here, so there is one place to read them and one place to change them.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/selection"
)

// exitWeightsNotPresent is this lane's own refusal code: the host CAN serve a
// chosen option, but the weights (and the matching projector) for it are not on
// this host. The codes the shared decision reaches (20-23) live in laneboot;
// this one stays here because only this lane knows what artefacts it needs
// present to serve a choice.
const exitWeightsNotPresent = 24

// choice is one decided model, with the measurement it was decided from and the
// artefacts on this host that serve it.
type choice struct {
	Option  selection.Option
	Entry   catalogue.Entry
	Profile capability.HostCapabilityProfile
	// Usage is the purpose the terms were applied against, so a report can
	// state what the licence permits rather than merely naming it.
	Usage catalogue.UsagePurpose

	// WeightsFile and ProjectorFile are the artefacts located on THIS host for
	// the chosen model. They are discovered by looking for the chosen model in
	// the configured directory — the directory is configuration, the model is
	// not.
	WeightsFile   string
	ProjectorFile string
}

// locateWeights finds the artefacts for ONE chosen model inside the configured
// directory.
//
// The direction matters: the model was chosen first, from the measurement, and
// this only asks where that model's files are. It never scans the directory and
// runs whatever it happens to find — that would be the directory naming the
// model.
func locateWeights(dir string, o selection.Option) (weights, projector string, err error) {
	names, err := ggufNames(dir)
	if err != nil {
		return "", "", err
	}
	modelKey := normalise(o.ModelID)
	buildKey := normalise(o.ModelID + o.Variant)

	for _, name := range names {
		n := normalise(name)
		switch {
		case strings.Contains(n, "mmproj"):
			if projector == "" && strings.Contains(n, modelKey) {
				projector = name
			}
		case weights == "" && strings.Contains(n, buildKey):
			weights = name
		}
	}
	if weights == "" {
		return "", "", fmt.Errorf("no weights file for %s found in %s", o.Identity, dir)
	}
	if projector == "" {
		return "", "", fmt.Errorf("no multimodal projector (mmproj) for %s found in %s", o.Identity, dir)
	}
	return weights, projector, nil
}

// ggufNames lists the GGUF artefacts in dir, in a deterministic order.
func ggufNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read model directory %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(e.Name()), ".gguf") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
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
