package main

// FR-056 regression guard for the IMAGE-GENERATION boot path: which model runs
// is decided from the MEASURED host, never from a static preconfigured value.
//
// POLARITY (§11.4.115). One source, two roles, switched by RED_MODE:
//
//	RED_MODE=1 — reproduce the defect on the pre-fix artifact. This binary's
//	             defect was not a hardcoded name but a pure PASSTHROUGH: it
//	             never wrote IMAGEGEN_MODEL and never measured anything, so
//	             whatever the ambient environment held was interpolated into
//	             compose and handed to the container. Run against the pre-fix
//	             source the structural checks PASS.
//	RED_MODE=0 — the standing guard (default): the passthrough signature is
//	             ABSENT, and the decision is indifferent to every model-naming
//	             variable.
//
// Honest boundary (§11.4.6): the BEHAVIOURAL checks exist only in guard mode —
// the pre-fix artifact has no decision entry point to call. The pre-fix
// evidence was captured separately against that artifact.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
// artifact, plus shapes a regression would plausibly take.
func modelNamingEnvironment() map[string]string {
	return map[string]string{
		"IMAGEGEN_MODEL":         "statically-named/Model-That-Does-Not-Fit",
		"IMAGEGEN_PRECISION":     "statically-named-precision",
		"IMAGEGEN_NEED_BYTES":    "99999999999",
		"HELIXLLM_MODEL":         "statically-named-model",
		"HELIXLLM_DEFAULT_MODEL": "statically-named-model",
	}
}

const staticallyNamed = "statically-named"

// catalogueFixture points the decision at the recorded catalogue entries for
// THIS lane's family, so the guard measures the boot path's decision and is not
// made red or green by an unrelated data file elsewhere in that directory.
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

// TestBinaryMeasuresRatherThanPassesThrough. The pre-fix defect: nothing in the
// binary measured the host or consulted the catalogue, and it never wrote
// IMAGEGEN_MODEL — so the ambient value reached the container untouched.
func TestBinaryMeasuresRatherThanPassesThrough(t *testing.T) {
	measures, writesModel := false, false
	for _, src := range productionSources(t) {
		for _, pkg := range []string{"internal/capability", "internal/selection", "internal/catalogue"} {
			if strings.Contains(src, pkg) {
				measures = true
			}
		}
		if strings.Contains(src, "modelKey") && strings.Contains(src, "os.Setenv") {
			writesModel = true
		}
	}

	if redMode() {
		if measures || writesModel {
			t.Fatalf("RED_MODE=1: expected the pre-fix passthrough (no measurement, no model write); "+
				"measures=%t writesModel=%t", measures, writesModel)
		}
		t.Log("DEFECT PRESENT: the binary neither measures the host nor writes the model; " +
			"the ambient environment value is what runs")
		return
	}
	if !measures {
		t.Fatal("the binary consults neither the measurement nor the catalogue, so the model cannot " +
			"have been decided from the host (FR-056)")
	}
	if !writesModel {
		t.Fatal("the binary never writes the model variable, so an ambient environment value would " +
			"still name the model (FR-056)")
	}
}

// TestNoHardcodedModelRepository. A fixed repository in the binary is a model
// that runs when nothing measured the host.
func TestNoHardcodedModelRepository(t *testing.T) {
	var found []string
	for path, src := range productionSources(t) {
		for _, needle := range []string{`"black-forest-labs/`, `"stabilityai/`, `"Qwen/`, `"runwayml/`} {
			if strings.Contains(src, needle) {
				found = append(found, path+": "+needle)
			}
		}
	}
	if len(found) != 0 {
		t.Fatalf("a model repository is hardcoded in production sources (FR-056): %v", found)
	}
}

// TestEnvironmentDoesNotChangeTheDecision. The same host, decided twice — once
// with every model-naming variable set, once with none — must produce the same
// answer. A difference means the environment reached the decision.
//
// The assertion holds whether the host can serve a model or refuses: a refusal
// that changes under the environment would be just as much a leak as an offer
// that does.
func TestEnvironmentDoesNotChangeTheDecision(t *testing.T) {
	if redMode() {
		t.Skip("SKIP-OK: behavioural check has no pre-fix counterpart — the pre-fix artifact has no decision entry point to call")
	}
	catalogueFixture(t)

	clean, cleanErr := chooseModel(testContext(t))

	for k, v := range modelNamingEnvironment() {
		t.Setenv(k, v)
	}
	configured, configuredErr := chooseModel(testContext(t))

	switch {
	case (cleanErr == nil) != (configuredErr == nil):
		t.Fatalf("the environment changed whether a model could be chosen: clean=%v configured=%v",
			cleanErr, configuredErr)
	case cleanErr != nil:
		if cleanErr.Error() != configuredErr.Error() {
			t.Fatalf("the environment changed the refusal:\n clean=%v\n configured=%v", cleanErr, configuredErr)
		}
		if strings.Contains(configuredErr.Error(), staticallyNamed) {
			t.Fatalf("a model named only by the environment entered the decision: %v", configuredErr)
		}
		t.Logf("decision unchanged by the environment (both refused identically): %v", cleanErr)
	default:
		if clean.Option.Identity != configured.Option.Identity || clean.Repository != configured.Repository {
			t.Fatalf("the environment changed the chosen model: %q/%q vs %q/%q",
				clean.Option.Identity, clean.Repository, configured.Option.Identity, configured.Repository)
		}
		if strings.Contains(configured.Repository, staticallyNamed) {
			t.Fatalf("a model named only by the environment was chosen: %q", configured.Repository)
		}
		t.Logf("decision unchanged by the environment: %s (%s)", configured.Option.Identity, configured.Repository)
	}
}

// TestStaticallyNamedModelIsNeverServed. Whatever the environment names, it
// must never become the repository the runtime is pointed at.
func TestStaticallyNamedModelIsNeverServed(t *testing.T) {
	if redMode() {
		t.Skip("SKIP-OK: behavioural check has no pre-fix counterpart — the pre-fix artifact has no decision entry point to call")
	}
	catalogueFixture(t)
	for k, v := range modelNamingEnvironment() {
		t.Setenv(k, v)
	}

	c, err := chooseModel(testContext(t))
	if err != nil {
		if strings.Contains(err.Error(), staticallyNamed) {
			t.Fatalf("a model named only by the environment entered the decision: %v", err)
		}
		t.Logf("refused rather than serving the configured name: %v", err)
		return
	}
	if strings.Contains(c.Repository, staticallyNamed) || strings.Contains(c.Precision, staticallyNamed) {
		t.Fatalf("a model named only by the environment was served: repo=%q precision=%q",
			c.Repository, c.Precision)
	}

	// The decision is also what compose is handed: applying it must overwrite
	// the configured names rather than leave them in place.
	applyChoice(c)
	if got := os.Getenv(modelKey); got != c.Repository {
		t.Fatalf("%s = %q, want the measured choice %q", modelKey, got, c.Repository)
	}
	if got := os.Getenv(precisionKey); got != c.Precision {
		t.Fatalf("%s = %q, want the measured choice %q", precisionKey, got, c.Precision)
	}
}

// TestDecisionOverwritesConfiguredNames. The model variables are OUTPUTS of the
// decision. A value already in the environment named a model no measurement
// chose, so applying the decision must overwrite it rather than defer to it.
func TestDecisionOverwritesConfiguredNames(t *testing.T) {
	if redMode() {
		t.Skip("SKIP-OK: behavioural check has no pre-fix counterpart — the pre-fix artifact never writes the model variable")
	}
	for k, v := range modelNamingEnvironment() {
		t.Setenv(k, v)
	}

	decided := choice{
		Option:     selection.Option{ModelID: "decided-model", Variant: "nvfp4", Identity: "helixllm/host/decided-model:nvfp4"},
		Repository: "decided-owner/Decided-Model",
		Precision:  "nvfp4",
	}
	applyChoice(decided)

	if got := os.Getenv(modelKey); got != decided.Repository {
		t.Fatalf("%s = %q, want the measured choice %q — a configured name survived the decision",
			modelKey, got, decided.Repository)
	}
	if got := os.Getenv(precisionKey); got != decided.Precision {
		t.Fatalf("%s = %q, want the measured choice %q", precisionKey, got, decided.Precision)
	}
	t.Logf("configured names overwritten by the measured choice: %s=%s %s=%s",
		modelKey, decided.Repository, precisionKey, decided.Precision)
}

// TestNoFixedDefaultWhenThereAreNoCandidates. With nothing to choose from there
// is no basis for a choice, and no fixed default may stand in for one.
func TestNoFixedDefaultWhenThereAreNoCandidates(t *testing.T) {
	if redMode() {
		t.Skip("SKIP-OK: behavioural check has no pre-fix counterpart — the pre-fix artifact has no decision entry point to call")
	}
	t.Setenv("HELIXLLM_CATALOGUE_DIR", t.TempDir()) // readable, but records nothing
	for k, v := range modelNamingEnvironment() {
		t.Setenv(k, v)
	}

	c, err := chooseModel(testContext(t))
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

// TestPinIsAConstraintNotABypass. A pin still measures the host first, and a
// name the catalogue does not record is refused rather than started.
func TestPinIsAConstraintNotABypass(t *testing.T) {
	if redMode() {
		t.Skip("SKIP-OK: behavioural check has no pre-fix counterpart — the pre-fix artifact has no pin path to call")
	}
	catalogueFixture(t)

	saved := os.Args
	t.Cleanup(func() { os.Args = saved })
	os.Args = []string{"imagegen-boot", "plan", "--pin", staticallyNamed + "-model"}

	c, err := chooseModel(testContext(t))
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
	remedies := map[string]struct{}{}
	for reason, text := range seen {
		if !strings.Contains(text, reason) {
			t.Fatalf("reason %q is not named in its own description: %q", reason, text)
		}
		if !strings.Contains(text, "remedy=") {
			t.Fatalf("reason %q states no remedy: %q", reason, text)
		}
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
