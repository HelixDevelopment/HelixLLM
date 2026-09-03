package main

// Measured model selection for the AGENT lane, Lane B (FR-056).
//
// The rule this file exists to enforce: WHICH model runs is decided from the
// MEASURED host, never from a static preconfigured value. Configuration may
// still say WHERE model files live (AGENTGEN_MODEL_DIR,
// HELIXLLM_CATALOGUE_DIR), what the user has DECLARED about their usage
// (HELIXLLM_DECLARED_USAGE) and which options they FORBID
// (AGENTGEN_FORBID_MODELS). None of those names the model.
//
// The DECISION ITSELF now lives in internal/laneboot, shared by all four boot
// lanes. This file used to carry the duplication notice that counted the copies
// — it was the fourth — and deferred the extraction because doing it there
// would have rewritten three correct lanes inside a change about a fourth that
// was not. That was the right call then and the extraction is the follow-up:
// the count is now one.
//
// What remains below is the half that is genuinely the agent lane's: locating
// the GGUF weights a chosen option needs on this host, and its own
// weights-not-present exit code.
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
// chosen option, but the weights for it are not on this host. The codes the
// shared decision reaches (20-23) live in laneboot; this one stays here because
// only this lane knows what artefact it needs present to serve a choice.
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

	// WeightsFile is the artefact located on THIS host for the chosen model. It
	// is discovered by looking for the chosen model in the configured directory
	// — the directory is configuration, the model is not.
	WeightsFile string
}

// locateWeights finds the GGUF for ONE chosen model inside the configured
// directory.
//
// The direction matters: the model was chosen first, from the measurement, and
// this only asks where that model's file is. It never scans the directory and
// runs whatever it happens to find — that would be the directory naming the
// model.
//
// The match is on the chosen BUILD — model id AND variant together — because a
// variant is a quantisation and two quantisations of one model are two
// different footprints. Matching the model id alone would find a q8 file for a
// q4 decision and admit the q4's figure for it.
//
// A multimodal projector is skipped rather than considered: an mmproj file is a
// vision lane's companion artefact, never a text model's weights, and this lane
// runs llama-server with no --mmproj at all.
func locateWeights(dir string, o selection.Option) (weights string, err error) {
	names, err := ggufNames(dir)
	if err != nil {
		return "", err
	}
	buildKey := normalise(o.ModelID + o.Variant)

	for _, name := range names {
		n := normalise(name)
		if strings.Contains(n, "mmproj") {
			continue
		}
		if strings.Contains(n, buildKey) {
			return name, nil
		}
	}
	return "", fmt.Errorf("no weights file for %s found in %s", o.Identity, dir)
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
