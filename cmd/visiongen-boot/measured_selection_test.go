package main

// FR-056 regression guard for the VISION boot path: which model runs is decided
// from the MEASURED host, never from a static preconfigured value.
//
// POLARITY (§11.4.115). One source, two roles, switched by RED_MODE:
//
//	RED_MODE=1 — reproduce the defect on the pre-fix artifact. The structural
//	             checks assert the defect signature IS present (a hardcoded
//	             model filename, and env-seeding that lets an environment value
//	             win). Run against the pre-fix source these PASS.
//	RED_MODE=0 — the standing guard (default). The same checks assert the
//	             signature is ABSENT, plus behavioural checks that the decision
//	             is indifferent to every model-naming variable.
//
// Honest boundary (§11.4.6): the BEHAVIOURAL checks exist only in guard mode.
// They call the measured-selection entry point, which does not exist in the
// pre-fix artifact — a test cannot call a function that was never written. The
// pre-fix behavioural evidence was captured separately by executing the pre-fix
// artifact's own `applyDefaultVisionEnv`, which honoured the environment
// verbatim; the structural half above is what carries that defect signature
// forward in a form both artifacts can be measured against.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/laneboot"
	"github.com/HelixDevelopment/HelixLLM/internal/selection"
)

// redMode reports whether this run asserts the defect is PRESENT (pre-fix
// baseline) rather than absent (standing guard).
func redMode() bool { return os.Getenv("RED_MODE") == "1" }

// modelNamingEnvironment is every variable that named a model in the pre-fix
// artifact, plus shapes a regression would plausibly take. The decision must be
// indifferent to all of them.
func modelNamingEnvironment() map[string]string {
	return map[string]string{
		"VISIONGEN_MODEL_GGUF":   "statically-named-model.gguf",
		"VISIONGEN_MMPROJ":       "statically-named-mmproj.gguf",
		"VISIONGEN_NEED_BYTES":   "99999999999",
		"HELIXLLM_MODEL":         "statically-named-model",
		"HELIXLLM_DEFAULT_MODEL": "statically-named-model",
	}
}

const staticallyNamed = "statically-named"

// catalogueFixture points the decision at the recorded catalogue entries for
// THIS lane's family. Where the candidates are described is configuration;
// which one runs is not.
//
// It copies the family's own data file rather than reading the whole catalogue
// directory, so this guard measures the boot path's decision and is not made
// red or green by an unrelated data file elsewhere in that directory.
func catalogueFixture(t *testing.T) {
	t.Helper()
	const familyData = "vision_image.yaml"
	src := filepath.Join("..", "..", "internal", "catalogue", "data", familyData)
	content, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read catalogue %s: %v", src, err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, familyData), content, 0o600); err != nil {
		t.Fatalf("write catalogue fixture: %v", err)
	}
	t.Setenv("HELIXLLM_CATALOGUE_DIR", dir)
}

// weightsFixture creates a model directory holding the given artefact names.
func weightsFixture(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("fixture"), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", n, err)
		}
	}
	return dir
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// productionSources returns every non-test Go source of this command.
func productionSources(t *testing.T) map[string]string {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob sources: %v", err)
	}
	out := map[string]string{}
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		b, readErr := os.ReadFile(p)
		if readErr != nil {
			t.Fatalf("read %s: %v", p, readErr)
		}
		out[p] = string(b)
	}
	if len(out) == 0 {
		t.Fatal("no production sources scanned; the guard would prove nothing")
	}
	return out
}

// hardcodedModelFile matches a quoted string that names a model weight file —
// a filename with a stem, not the bare ".gguf" extension used when listing a
// directory.
var hardcodedModelFile = regexp.MustCompile(`"[^"]+\.gguf"`)

// TestModelNameIsNotHardcoded. A fixed model name in the binary IS the static
// selection FR-056 forbids: it is what runs when nothing measures the host.
func TestModelNameIsNotHardcoded(t *testing.T) {
	var found []string
	for path, src := range productionSources(t) {
		for _, m := range hardcodedModelFile.FindAllString(src, -1) {
			if strings.EqualFold(m, `".gguf"`) {
				continue // an extension, not a model name
			}
			found = append(found, path+": "+m)
		}
		if strings.Contains(src, `"mmproj-`) {
			found = append(found, path+`: "mmproj-…" projector filename`)
		}
	}

	if redMode() {
		if len(found) == 0 {
			t.Fatal("RED_MODE=1: expected a hardcoded model filename in the pre-fix artifact, found none")
		}
		t.Logf("DEFECT PRESENT: model filenames hardcoded in the binary: %v", found)
		return
	}
	if len(found) != 0 {
		t.Fatalf("a model filename is hardcoded in production sources, so a model can run that no "+
			"measurement chose (FR-056): %v", found)
	}
}

// TestEnvironmentIsNotHonouredAsModelInput. The pre-fix artifact seeded the
// model variables only-if-unset, so an operator-supplied value WON. The fixed
// binary treats those variables as outputs it overwrites.
func TestEnvironmentIsNotHonouredAsModelInput(t *testing.T) {
	seedsOnlyIfUnset := false
	for _, src := range productionSources(t) {
		if strings.Contains(src, "func setDefaultEnv") || strings.Contains(src, "applyDefaultVisionEnv") {
			seedsOnlyIfUnset = true
		}
	}

	if redMode() {
		if !seedsOnlyIfUnset {
			t.Fatal("RED_MODE=1: expected the pre-fix only-if-unset model seeding, found none")
		}
		t.Log("DEFECT PRESENT: model variables are seeded only if unset, so an environment value names the model")
		return
	}
	if seedsOnlyIfUnset {
		t.Fatal("model variables are seeded only-if-unset, so an environment value would name the model (FR-056)")
	}
}

// TestEnvironmentDoesNotChangeTheDecision. The same host, decided twice — once
// with every model-naming variable set, once with none — must produce the same
// answer. A difference means the environment reached the decision.
func TestEnvironmentDoesNotChangeTheDecision(t *testing.T) {
	if redMode() {
		t.Skip("SKIP-OK: behavioural check has no pre-fix counterpart — the pre-fix artifact has no measured-selection entry point to call")
	}
	catalogueFixture(t)
	dir := weightsFixture(t,
		"Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf",
		"mmproj-Qwen2.5-VL-3B-Instruct-Q8_0.gguf",
	)

	clean, cleanErr := chooseModel(testContext(t), dir)

	for k, v := range modelNamingEnvironment() {
		t.Setenv(k, v)
	}
	configured, configuredErr := chooseModel(testContext(t), dir)

	if (cleanErr == nil) != (configuredErr == nil) {
		t.Fatalf("the environment changed whether a model could be chosen: clean=%v configured=%v",
			cleanErr, configuredErr)
	}
	if cleanErr != nil {
		t.Fatalf("no model could be chosen on this host, so the guard proved nothing: %v", cleanErr)
	}
	if clean.Option.Identity != configured.Option.Identity {
		t.Fatalf("the environment changed the chosen model: %q vs %q",
			clean.Option.Identity, configured.Option.Identity)
	}
	if clean.WeightsFile != configured.WeightsFile || clean.ProjectorFile != configured.ProjectorFile {
		t.Fatalf("the environment changed the chosen artefacts: %q/%q vs %q/%q",
			clean.WeightsFile, clean.ProjectorFile, configured.WeightsFile, configured.ProjectorFile)
	}
	if strings.Contains(configured.WeightsFile, staticallyNamed) ||
		strings.Contains(configured.ProjectorFile, staticallyNamed) {
		t.Fatalf("a model named only by the environment was chosen: %q/%q",
			configured.WeightsFile, configured.ProjectorFile)
	}
	t.Logf("decision unchanged by the environment: %s (%s)", configured.Option.Identity, configured.WeightsFile)
}

// TestDecisionOverwritesConfiguredNames. The model variables are OUTPUTS of the
// decision. A value already in the environment named a model no measurement
// chose, so applying the decision must overwrite it rather than defer to it —
// this is the exact behaviour the pre-fix artifact inverted.
func TestDecisionOverwritesConfiguredNames(t *testing.T) {
	if redMode() {
		t.Skip("SKIP-OK: behavioural check has no pre-fix counterpart — the pre-fix artifact honours the configured name instead")
	}
	for k, v := range modelNamingEnvironment() {
		t.Setenv(k, v)
	}

	decided := choice{
		Option:        selection.Option{ModelID: "decided-model", Variant: "q4", Identity: "helixllm/host/decided-model:q4"},
		WeightsFile:   "Decided-Model-Q4.gguf",
		ProjectorFile: "mmproj-Decided-Model.gguf",
	}
	applyChoice(decided)

	if got := os.Getenv(weightsKey); got != decided.WeightsFile {
		t.Fatalf("%s = %q, want the measured choice %q — a configured name survived the decision",
			weightsKey, got, decided.WeightsFile)
	}
	if got := os.Getenv(projectorKey); got != decided.ProjectorFile {
		t.Fatalf("%s = %q, want the measured choice %q", projectorKey, got, decided.ProjectorFile)
	}
	t.Logf("configured names overwritten by the measured choice: %s=%s %s=%s",
		weightsKey, decided.WeightsFile, projectorKey, decided.ProjectorFile)
}

// TestStaticallyNamedFileIsNeverBooted. The model directory holds ONLY files
// the environment names, matching nothing the catalogue records. The binary
// must REFUSE — booting a file that happens to be present is a model nobody
// chose.
func TestStaticallyNamedFileIsNeverBooted(t *testing.T) {
	if redMode() {
		t.Skip("SKIP-OK: behavioural check has no pre-fix counterpart — the pre-fix artifact has no measured-selection entry point to call")
	}
	catalogueFixture(t)
	for k, v := range modelNamingEnvironment() {
		t.Setenv(k, v)
	}
	dir := weightsFixture(t, "statically-named-model.gguf", "statically-named-mmproj.gguf")

	c, err := chooseModel(testContext(t), dir)
	if err == nil {
		t.Fatalf("a model named only by the environment was booted: %q (%s)", c.Option.Identity, c.WeightsFile)
	}
	if code := laneboot.ExitCodeFor(err); code != exitWeightsNotPresent {
		t.Fatalf("expected a weights-not-present refusal (%d), got %d: %v", exitWeightsNotPresent, code, err)
	}
	t.Logf("refused as required: %v", err)
}

// TestNoFixedDefaultWhenTheHostCannotBeChosenFor. With no candidates readable
// there is no basis for a choice, and no fixed default may stand in for one.
func TestNoFixedDefaultWhenTheHostCannotBeChosenFor(t *testing.T) {
	if redMode() {
		t.Skip("SKIP-OK: behavioural check has no pre-fix counterpart — the pre-fix artifact has no measured-selection entry point to call")
	}
	t.Setenv("HELIXLLM_CATALOGUE_DIR", t.TempDir()) // readable, but records nothing
	for k, v := range modelNamingEnvironment() {
		t.Setenv(k, v)
	}
	dir := weightsFixture(t, "statically-named-model.gguf", "statically-named-mmproj.gguf")

	c, err := chooseModel(testContext(t), dir)
	if err == nil {
		t.Fatalf("a model was started with no candidates to choose from: %q", c.Option.Identity)
	}
	if code := laneboot.ExitCodeFor(err); code != laneboot.ExitCatalogueMissing {
		t.Fatalf("expected a catalogue refusal (%d), got %d: %v", laneboot.ExitCatalogueMissing, code, err)
	}
	if !strings.Contains(err.Error(), "CANNOT-CHOOSE") {
		t.Fatalf("the refusal does not say it cannot choose: %v", err)
	}
	t.Logf("refused as required: %v", err)
}

// TestPinIsAConstraintNotABypass. A pin is the one legitimate way to name a
// model. It still measures the host first, and a name the catalogue does not
// record is refused rather than started.
func TestPinIsAConstraintNotABypass(t *testing.T) {
	if redMode() {
		t.Skip("SKIP-OK: behavioural check has no pre-fix counterpart — the pre-fix artifact has no pin path to call")
	}
	catalogueFixture(t)
	dir := weightsFixture(t,
		"Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf",
		"mmproj-Qwen2.5-VL-3B-Instruct-Q8_0.gguf",
	)

	saved := os.Args
	t.Cleanup(func() { os.Args = saved })
	os.Args = []string{"visiongen-boot", "plan", "--pin", staticallyNamed + "-model"}

	c, err := chooseModel(testContext(t), dir)
	if err == nil {
		t.Fatalf("a pin naming a model the catalogue does not record was started: %q", c.Option.Identity)
	}
	if code := laneboot.ExitCodeFor(err); code != laneboot.ExitNoOptionOffered {
		t.Fatalf("expected an offer refusal (%d), got %d: %v", laneboot.ExitNoOptionOffered, code, err)
	}
	if !strings.Contains(err.Error(), string(selection.RequirementCatalogueEntry)) {
		t.Fatalf("the refusal does not name the missing catalogue entry: %v", err)
	}
	t.Logf("pin refused as required: %v", err)
}

// TestWithheldReasonsStayDistinct. The three reasons imply different remedies,
// so each must render its own specific rather than a generic unavailability.
func TestWithheldReasonsStayDistinct(t *testing.T) {
	if redMode() {
		t.Skip("SKIP-OK: behavioural check has no pre-fix counterpart — the pre-fix artifact records no withheld reasons")
	}
	seen := map[string]string{}
	for _, w := range []selection.Withheld{
		{
			ModelID: "too-big", Reason: selection.ReasonInsufficientResources,
			Shortfall: &selection.Shortfall{
				Resource: selection.ResourceMemory, RequiredBytes: 8 << 30,
				AvailableBytes: 4 << 30, ReservedBytes: 1 << 30,
			},
		},
		{
			ModelID: "needs-accelerator", Reason: selection.ReasonUnsupportedConfiguration,
			Unsupported: &selection.Unsupported{
				Requirement: selection.RequirementAccelerator, Detail: "none measured",
			},
		},
		{
			ModelID: "wrong-licence", Reason: selection.ReasonExcludedByUsageTerms,
			Exclusion: &selection.Exclusion{
				Purpose: catalogue.UsageCommercial, LicenseID: "example-non-commercial",
				Granted: false, Term: catalogue.TermNonCommercial, Reference: "example licence",
			},
		},
	} {
		seen[string(w.Reason)] = laneboot.DescribeWithheld(w)
	}
	if len(seen) != 3 {
		t.Fatalf("expected three distinct reasons, described %d: %v", len(seen), seen)
	}
	for reason, text := range seen {
		if !strings.Contains(text, reason) {
			t.Fatalf("reason %q is not named in its own description: %q", reason, text)
		}
		if !strings.Contains(text, "remedy=") {
			t.Fatalf("reason %q states no remedy: %q", reason, text)
		}
	}
	remedies := map[string]struct{}{}
	for _, text := range seen {
		remedies[text[strings.Index(text, "remedy="):]] = struct{}{}
	}
	if len(remedies) != 3 {
		t.Fatalf("the three reasons collapsed onto %d remedies: %v", len(remedies), remedies)
	}
}

// TestMeasurementFailureIsReportedAndRefused. When the host cannot be measured
// there is no basis for a choice. The binary must say so, say WHY, and exit
// non-zero — never fall back to a fixed default.
//
// This host measures successfully, so the refusal is driven directly rather
// than by breaking the machine: what is asserted is the reporting contract the
// measurement failure path depends on.
func TestMeasurementFailureIsReportedAndRefused(t *testing.T) {
	if redMode() {
		t.Skip("SKIP-OK: behavioural check has no pre-fix counterpart — the pre-fix artifact has no refusal path")
	}
	notMeasured := selection.Result{
		Refusal: &selection.HostRefusal{
			Kind:         selection.RefusalHostNotMeasured,
			HostIdentity: "unmeasurable-host",
			Cause:        "measurement-incomplete",
		},
	}
	err := laneboot.RefusalError(notMeasured, fmt.Errorf("%w: storage", selection.ErrHostNotMeasured))
	if code := laneboot.ExitCodeFor(err); code != laneboot.ExitHostNotMeasured {
		t.Fatalf("expected exit %d for an unmeasured host, got %d", laneboot.ExitHostNotMeasured, code)
	}
	for _, want := range []string{"CANNOT-CHOOSE", "was not measured", "measurement-incomplete", "No model is started"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not state %q: %v", want, err)
		}
	}
	t.Logf("unmeasured host refused with exit %d: %v", laneboot.ExitCodeFor(err), err)

	stale := selection.Result{
		Refusal: &selection.HostRefusal{
			Kind:          selection.RefusalMeasurementStale,
			AgeSeconds:    30,
			MaxAgeSeconds: 5,
		},
	}
	serr := laneboot.RefusalError(stale, fmt.Errorf("%w: too old", selection.ErrMeasurementStale))
	if code := laneboot.ExitCodeFor(serr); code != laneboot.ExitMeasurementStale {
		t.Fatalf("expected exit %d for a stale reading, got %d", laneboot.ExitMeasurementStale, code)
	}
	if !strings.Contains(serr.Error(), "re-measure") {
		t.Fatalf("the stale refusal does not state its remedy: %v", serr)
	}
	t.Logf("stale reading refused with exit %d: %v", laneboot.ExitCodeFor(serr), serr)
}
