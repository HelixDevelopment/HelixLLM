package main

// FR-056 regression guard for the VIDEO-GENERATION boot path: which model runs
// is decided from the MEASURED host, never from a static preconfigured value.
//
// POLARITY (§11.4.115). One source, two roles, switched by RED_MODE:
//
//	RED_MODE=1 — reproduce the defect on the pre-fix artifact. This binary's
//	             defect was a STATIC ADMISSION FIGURE: needBytes() returned a
//	             fixed 10 GiB (overridable by VIDEOGEN_NEED_BYTES) and nothing
//	             measured the host or consulted the catalogue, so one number
//	             stood in for three models with different recorded
//	             requirements and the model itself was whatever the ambient
//	             environment held. Run against the pre-fix source the
//	             structural checks PASS.
//	RED_MODE=0 — the standing guard (default): the static figure is ABSENT,
//	             the decision is measured, and it is indifferent to every
//	             model-naming variable.
//
// Honest boundary (§11.4.6): the BEHAVIOURAL checks exist only in guard mode —
// the pre-fix artifact had no decision entry point to call. The pre-fix
// evidence was captured separately against that artifact.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/laneboot"
	"github.com/HelixDevelopment/HelixLLM/internal/selection"
)

// redMode reports whether this run asserts the defect is PRESENT (pre-fix
// baseline) rather than absent (standing guard).
func redMode() bool { return os.Getenv("RED_MODE") == "1" }

// modelNamingEnvironment is every variable that described a model in the
// pre-fix artifact, plus shapes a regression would plausibly take. The clip
// shape is included because a configured size/frame-count/rate serves a
// configuration whose memory cost nobody measured.
func modelNamingEnvironment() map[string]string {
	return map[string]string{
		"VIDEOGEN_MODEL":         "statically-named/Model-That-Does-Not-Fit",
		"VIDEOGEN_BACKEND":       "statically-named-backend",
		"VIDEOGEN_PRECISION":     "statically-named-precision",
		"VIDEOGEN_NEED_BYTES":    "99999999999",
		"VIDEOGEN_SIZE":          "9999x9999",
		"VIDEOGEN_NUM_FRAMES":    "99999",
		"VIDEOGEN_FPS":           "999",
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
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, familyData), videoCatalogue(t), 0o600); err != nil {
		t.Fatalf("write catalogue fixture: %v", err)
	}
	t.Setenv("HELIXLLM_CATALOGUE_DIR", dir)
}

const familyData = "video.yaml"

func videoCatalogue(t *testing.T) []byte {
	t.Helper()
	src := filepath.Join("..", "..", "internal", "catalogue", "data", familyData)
	content, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read catalogue %s: %v", src, err)
	}
	return content
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

// TestAdmissionFigureIsMeasuredNotStatic. The pre-fix defect: the figure handed
// to the broker was a fixed 10 GiB honouring VIDEOGEN_NEED_BYTES, and nothing
// in the binary measured the host or consulted the catalogue.
func TestAdmissionFigureIsMeasuredNotStatic(t *testing.T) {
	staticFigure, honoursNeedBytes, measures, writesModel := false, false, false, false
	for path, src := range productionSources(t) {
		if strings.Contains(src, "return 10 * GiB") {
			staticFigure = true
			t.Logf("static admission figure in %s", path)
		}
		// The legacy key may be NAMED (to report that it is ignored); what is
		// forbidden is reading it as the admission figure.
		if strings.Contains(src, `os.Getenv("VIDEOGEN_NEED_BYTES")`) {
			honoursNeedBytes = true
			t.Logf("VIDEOGEN_NEED_BYTES read as an input in %s", path)
		}
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
		if !staticFigure || !honoursNeedBytes || measures || writesModel {
			t.Fatalf("RED_MODE=1: expected the pre-fix defect (static 10 GiB figure, VIDEOGEN_NEED_BYTES "+
				"honoured, no measurement, no model write); staticFigure=%t honoursNeedBytes=%t "+
				"measures=%t writesModel=%t", staticFigure, honoursNeedBytes, measures, writesModel)
		}
		t.Log("DEFECT PRESENT: one static VRAM figure stands in for every model, and nothing measures " +
			"this host; the model that runs is whatever the environment holds")
		return
	}
	if staticFigure || honoursNeedBytes {
		t.Fatal("the admission figure still comes from a static source, so it cannot belong to the " +
			"model that was chosen (FR-056)")
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
// that runs when nothing measured the host. The allowlist HOST prefix is not a
// repository — it carries no owner or model — so it is deliberately not matched.
func TestNoHardcodedModelRepository(t *testing.T) {
	repoLiteral := regexp.MustCompile(`https://huggingface\.co/[A-Za-z0-9._-]+`)
	var found []string
	for path, src := range productionSources(t) {
		for _, needle := range []string{`"Wan-AI/`, `"Lightricks/`, `"stabilityai/`, `"black-forest-labs/`} {
			if strings.Contains(src, needle) {
				found = append(found, path+": "+needle)
			}
		}
		for _, m := range repoLiteral.FindAllString(src, -1) {
			found = append(found, path+": "+m)
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
// must never become what the runtime is pointed at.
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
	for label, got := range map[string]string{
		"repository": c.Repository, "backend": c.Backend, "precision": c.Precision, "size": c.Shape.Size,
	} {
		if strings.Contains(got, staticallyNamed) {
			t.Fatalf("a %s named only by the environment was served: %q", label, got)
		}
	}

	// The decision is also what compose is handed: applying it must overwrite
	// the configured values rather than leave them in place.
	applyChoice(c)
	if got := os.Getenv(modelKey); got != c.Repository {
		t.Fatalf("%s = %q, want the measured choice %q", modelKey, got, c.Repository)
	}
}

// TestDecisionOverwritesConfiguredNames. The model and clip-shape variables are
// OUTPUTS of the decision. A value already in the environment described a model
// no measurement chose, so applying the decision must overwrite it.
func TestDecisionOverwritesConfiguredNames(t *testing.T) {
	if redMode() {
		t.Skip("SKIP-OK: behavioural check has no pre-fix counterpart — the pre-fix artifact never writes the model variable")
	}
	for k, v := range modelNamingEnvironment() {
		t.Setenv(k, v)
	}

	decided := choice{
		Option:     selection.Option{ModelID: "decided-model", Variant: "fp8", Identity: "helixllm/host/decided-model:fp8"},
		Repository: "decided-owner/Decided-Model",
		Backend:    "wan",
		Precision:  "fp8",
		Shape:      videoShape{Size: "832x480", NumFrames: 49, FPS: 16},
	}
	applyChoice(decided)

	for key, want := range map[string]string{
		modelKey:     decided.Repository,
		backendKey:   decided.Backend,
		precisionKey: decided.Precision,
		sizeKey:      decided.Shape.Size,
		framesKey:    "49",
		fpsKey:       "16",
	} {
		if got := os.Getenv(key); got != want {
			t.Fatalf("%s = %q, want the measured choice %q — a configured value survived the decision",
				key, got, want)
		}
	}
	t.Logf("configured values overwritten by the measured choice: %s=%s %s=%s %s=%s",
		modelKey, decided.Repository, backendKey, decided.Backend, sizeKey, decided.Shape.Size)
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
	os.Args = []string{"videogen-boot", "plan", "--pin", staticallyNamed + "-model"}

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

// TestNoProcessorViableOptionIsOffered. Video generation has NO processor-viable
// option, so a host with no accelerator must be offered NOTHING and told what it
// LACKS — never handed a fallback that would start and then be unusable.
//
// The refusal is driven against a synthetic no-accelerator host with abundant
// memory and storage, so the only thing that can withhold an entry is the
// missing accelerator: the assertion is that the reason names the accelerator,
// not merely that nothing was offered.
func TestNoProcessorViableOptionIsOffered(t *testing.T) {
	if redMode() {
		t.Skip("SKIP-OK: behavioural check has no pre-fix counterpart — the pre-fix artifact never consulted the catalogue")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, familyData), videoCatalogue(t), 0o600); err != nil {
		t.Fatalf("write catalogue fixture: %v", err)
	}
	loaded, err := catalogue.Load(dir)
	if err != nil {
		t.Fatalf("load video catalogue: %v", err)
	}
	if len(loaded.Entries()) == 0 {
		t.Fatal("the video catalogue records no entries; the guard would prove nothing")
	}

	const plenty = 512 << 30 // far above every recorded video requirement
	noAccelerator := capability.HostCapabilityProfile{
		HostIdentity:        "no-accelerator-host",
		CPU:                 capability.CPUProfile{Architecture: "amd64", PhysicalCores: 16, LogicalCores: 32},
		MemoryTotal:         plenty,
		MemoryAvailable:     plenty,
		AcceleratorState:    capability.AcceleratorStateMeasured, // measured, and there are none
		StorageAvailable:    plenty,
		MeasuredAt:          time.Now().UTC(),
		MeasurementComplete: true,
	}

	result, err := selection.Select(selection.Request{
		Profile:       noAccelerator,
		Entries:       loaded.Entries(),
		Families:      []catalogue.CapabilityFamily{family},
		DeclaredUsage: catalogue.UsagePersonal, // the most permissive of the recorded licences
		Now:           time.Now().UTC(),
		MaxProfileAge: capability.DefaultMaxMeasurementAge,
	})
	if err != nil {
		t.Logf("selection refused outright on a no-accelerator host: %v", err)
		return
	}
	fr, ok := result.Family(family)
	if !ok {
		t.Fatalf("no result for the %s family at all; a host must still be told why", family)
	}
	if len(fr.Offered) != 0 {
		var names []string
		for _, o := range fr.Offered {
			names = append(names, o.Identity)
		}
		t.Fatalf("a host with no accelerator was offered video generation it cannot run: %v", names)
	}
	if len(fr.Withheld) == 0 {
		t.Fatal("nothing was offered and nothing was withheld, so the host was told nothing")
	}
	for _, w := range fr.Withheld {
		text := laneboot.DescribeWithheld(w)
		if !strings.Contains(text, string(selection.RequirementAccelerator)) {
			t.Fatalf("%s was withheld without naming the accelerator it lacks: %s",
				laneboot.IdentityOf(w.ModelID, w.Variant), text)
		}
		t.Logf("withheld %s: %s", laneboot.IdentityOf(w.ModelID, w.Variant), text)
	}
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
