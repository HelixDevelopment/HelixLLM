package main

// Measured model selection for the VIDEO-GENERATION lane (T078 / FR-056).
//
// The rule this file exists to enforce: WHICH model runs is decided from the
// MEASURED host, never from a static preconfigured value. Configuration may
// still say WHERE the catalogue lives (HELIXLLM_CATALOGUE_DIR), what the user
// has DECLARED about their usage (HELIXLLM_DECLARED_USAGE) and which options
// they FORBID (VIDEOGEN_FORBID_MODELS). None of those names the model.
//
// Video has NO processor-viable option — every recorded entry sets
// requires_accelerator, because diffusion video generation without an
// accelerator is not slow-but-usable, it is unusable. A host with no
// accelerator is therefore told what it LACKS and offered nothing. That
// refusal is the correct answer, not a gap: an offer that would start and then
// be unusable is the failure being avoided.
//
// DUPLICATION NOTICE (§11.4.74, reported rather than compounded silently): this
// file is the THIRD near-copy of the same decision, after
// cmd/visiongen-boot/modelchoice.go and cmd/imagegen-boot/modelchoice.go.
// Everything above repositoryFor is byte-identical across all three but for the
// lane's forbid-key and exit-code names. It belongs in one shared internal
// package that each lane parameterises; that package could not be created here
// because internal/ is outside this change's scope.
//
// Two properties follow, and both are load-bearing:
//
//   - There is NO fixed default. When the host cannot be measured the binary
//     says it cannot choose, says why, and exits non-zero. It does not start an
//     arbitrary model that may not fit (FR-056).
//   - A deliberate pin (--pin) is a CONSTRAINT on the choice, not a bypass. It
//     narrows the candidate set and the pinned entry then goes through exactly
//     the same measurement, fit and terms checks as any other — and is refused,
//     with the insufficient resource NAMED, when the host cannot run it.
//
// The three withheld reasons — insufficient resources, unsupported
// configuration, excluded by usage terms — stay distinct all the way to the
// operator's terminal, because each implies a different remedy and collapsing
// them destroys the only actionable part of the answer (FR-055).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/selection"
)

// Exit codes for the selection stage. They are distinct from the admission
// codes (10-14) because their remedies are: a host that cannot be measured is
// investigated, a stale reading is retaken, a refusal is answered by changing
// the host, the model or the declared usage.
const (
	exitHostNotMeasured  = 20
	exitMeasurementStale = 21
	exitNoOptionOffered  = 22
	exitCatalogueMissing = 23
	exitNotServable      = 24
)

// defaultCatalogueDir is where the recorded catalogue lives in a checkout. It
// is a LOCATION, not a model name: it says where the candidates are described,
// never which of them runs.
const defaultCatalogueDir = "internal/catalogue/data"

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
	// Backend is the diffusion pipeline this runtime loads for the chosen
	// model (WanPipeline / LTXPipeline).
	Backend string
	// Precision is the serving precision the chosen build implies.
	Precision string
	// Shape is the clip the chosen option was selected AT — the frame size,
	// count and rate its recorded memory figure belongs to.
	Shape videoShape
}

// catalogueDir reports where the candidate catalogue is read from.
func catalogueDir() string {
	if dir := strings.TrimSpace(os.Getenv("HELIXLLM_CATALOGUE_DIR")); dir != "" {
		return dir
	}
	return defaultCatalogueDir
}

// declaredUsage is how the operator has said the output will be used. Selection
// requires it: terms cannot be applied against an undeclared usage, and
// assuming a permissive one would offer models the operator may not be
// permitted to use.
//
// The default is the NARROWEST purpose, commercial. Defaulting narrow can only
// ever withhold an option the operator was in fact entitled to — never offer
// one they are not — and the default is always reported, so the operator can
// see the assumption that was made and widen it deliberately.
func declaredUsage() (purpose catalogue.UsagePurpose, defaulted bool, err error) {
	raw := strings.TrimSpace(os.Getenv("HELIXLLM_DECLARED_USAGE"))
	if raw == "" {
		return catalogue.UsageCommercial, true, nil
	}
	switch p := catalogue.UsagePurpose(strings.ToLower(raw)); p {
	case catalogue.UsageCommercial, catalogue.UsagePersonal,
		catalogue.UsageResearch, catalogue.UsageEvaluation:
		return p, false, nil
	default:
		return "", false, fmt.Errorf("HELIXLLM_DECLARED_USAGE=%q is not a recorded usage "+
			"(commercial, personal, research, evaluation)", raw)
	}
}

// forbidden reads the operator's forbid-list. Forbidding options is a
// legitimate configuration act — it can only ever REMOVE a candidate the
// measurement offered, never introduce one it did not.
func forbidden(key string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, item := range strings.Split(os.Getenv(key), ",") {
		if item = strings.ToLower(strings.TrimSpace(item)); item != "" {
			out[item] = struct{}{}
		}
	}
	return out
}

// parsePin reads an optional deliberate pin from the argument list. A pin is
// the one legitimate way a caller names a model, and it is stated at invocation
// rather than carried in ambient configuration so it is unmistakably a
// deliberate act.
func parsePin(args []string) (*selection.Pin, error) {
	for i := 0; i < len(args); i++ {
		var value string
		switch {
		case args[i] == "--pin":
			if i+1 >= len(args) {
				return nil, errors.New("--pin needs a model id, optionally id:variant")
			}
			value = args[i+1]
		case strings.HasPrefix(args[i], "--pin="):
			value = strings.TrimPrefix(args[i], "--pin=")
		default:
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, errors.New("--pin needs a model id, optionally id:variant")
		}
		id, variant, _ := strings.Cut(value, ":")
		return &selection.Pin{ModelID: id, Variant: variant}, nil
	}
	return nil, nil
}

// measure reads this host, then reports what it found. The reading is returned
// even when it is incomplete: selection refuses on it, and the refusal names
// what was missing.
func measure(ctx context.Context, weightsDir string) capability.HostCapabilityProfile {
	profile, err := capability.Measure(ctx, capability.Options{WeightsDir: weightsDir})
	if err != nil {
		fmt.Printf("MEASURE-INCOMPLETE: %v\n", err)
		return profile
	}
	fmt.Printf("MEASURED host=%s cpu=%d memory_available=%dMiB storage_available=%dMiB accelerators=%d (%s)\n",
		profile.HostIdentity, profile.CPU.LogicalCores,
		uint64(profile.MemoryAvailable)/(1024*1024),
		uint64(profile.StorageAvailable)/(1024*1024),
		len(profile.Accelerators), profile.AcceleratorState)
	return profile
}

// decide measures the host and returns the options it can actually serve for
// one family, in the order the catalogue records them.
//
// Every failure path here refuses. None of them substitutes a default model.
func decide(
	ctx context.Context,
	family catalogue.CapabilityFamily,
	weightsDir string,
	pin *selection.Pin,
	forbidKey string,
) ([]selection.Option, catalogue.Catalogue, capability.HostCapabilityProfile, catalogue.UsagePurpose, error) {
	loaded, err := catalogue.Load(catalogueDir())
	if err != nil {
		return nil, loaded, capability.HostCapabilityProfile{}, "",
			exitErr(exitCatalogueMissing, "CANNOT-CHOOSE: the catalogue of candidates could not be read "+
				"(%v). Nothing is started without candidates to choose from.", err)
	}

	purpose, defaulted, err := declaredUsage()
	if err != nil {
		return nil, loaded, capability.HostCapabilityProfile{}, "",
			exitErr(exitNoOptionOffered, "CANNOT-CHOOSE: %v", err)
	}
	if defaulted {
		fmt.Printf("DECLARED-USAGE: %s (default — the narrowest purpose; set HELIXLLM_DECLARED_USAGE to declare another)\n", purpose)
	} else {
		fmt.Printf("DECLARED-USAGE: %s (declared)\n", purpose)
	}
	if pin != nil {
		fmt.Printf("PIN: %s — a constraint on the choice, not a bypass: the host is measured first "+
			"and the pin is refused if this host cannot run it.\n", pin.Identity())
	}

	profile := measure(ctx, weightsDir)

	// The freshness policy is stated explicitly. Its ZERO VALUE demands a
	// reading taken at this instant, so leaving it unset would re-measure on
	// every decision; DefaultMaxMeasurementAge is the recency to ask for when
	// there is no stricter requirement.
	policy := capability.FreshnessPolicy{MaxAge: capability.DefaultMaxMeasurementAge}

	result, err := selection.Select(selection.Request{
		Profile:       profile,
		Entries:       loaded.Entries(),
		Families:      []catalogue.CapabilityFamily{family},
		DeclaredUsage: purpose,
		Pin:           pin,
		Now:           time.Now().UTC(),
		MaxProfileAge: policy.MaxAge,
	})
	if err != nil {
		return nil, loaded, profile, purpose, refusalError(result, err)
	}

	fr, ok := result.Family(family)
	if !ok {
		return nil, loaded, profile, purpose, exitErr(exitNoOptionOffered,
			"CANNOT-CHOOSE: nothing in the catalogue serves the %s family on this host.", family)
	}
	reportWithheld(fr)

	offered := applyForbidList(fr.Offered, forbidKey)
	if len(offered) == 0 {
		return nil, loaded, profile, purpose, exitErr(exitNoOptionOffered, "%s", familyRefusalReport(fr, family, forbidKey))
	}
	return offered, loaded, profile, purpose, nil
}

// applyForbidList removes options the operator has forbidden. It reports what
// it removed, so a silent absence never looks like a measurement result.
func applyForbidList(offered []selection.Option, forbidKey string) []selection.Option {
	forbid := forbidden(forbidKey)
	if len(forbid) == 0 {
		return offered
	}
	kept := make([]selection.Option, 0, len(offered))
	for _, o := range offered {
		id := strings.ToLower(o.ModelID)
		identity := strings.ToLower(o.ModelID + ":" + o.Variant)
		if _, no := forbid[id]; no {
			fmt.Printf("FORBIDDEN-BY-CONFIG: %s removed by %s (operator choice, not a measurement)\n", o.Identity, forbidKey)
			continue
		}
		if _, no := forbid[identity]; no {
			fmt.Printf("FORBIDDEN-BY-CONFIG: %s removed by %s (operator choice, not a measurement)\n", o.Identity, forbidKey)
			continue
		}
		kept = append(kept, o)
	}
	return kept
}

// entryFor recovers the catalogue entry an option was derived from. The option
// carries everything host-dependent; the entry carries the source the weights
// come from.
func entryFor(loaded catalogue.Catalogue, o selection.Option) (catalogue.Entry, bool) {
	for _, e := range loaded.Entries() {
		if e.ModelID == o.ModelID && e.Variant == o.Variant {
			return e, true
		}
	}
	return catalogue.Entry{}, false
}

// reportWithheld prints every candidate that was not offered, keeping the three
// reasons distinct and naming the specific each one turns on (FR-055).
func reportWithheld(fr selection.FamilyResult) {
	for _, w := range fr.Withheld {
		fmt.Printf("WITHHELD %s: %s\n", identityOf(w.ModelID, w.Variant), describeWithheld(w))
	}
}

// describeWithheld renders one withholding. The three reasons never collapse
// into a generic unavailability: each names its own specific and its own
// remedy, because each asks something different of the operator.
func describeWithheld(w selection.Withheld) string {
	switch w.Reason {
	case selection.ReasonInsufficientResources:
		if s := w.Shortfall; s != nil {
			short := uint64(0)
			if s.RequiredBytes > s.AvailableBytes {
				short = s.RequiredBytes - s.AvailableBytes
			}
			return fmt.Sprintf("insufficient_resources — %s short by %dMiB "+
				"(needs %dMiB, %dMiB available after %dMiB held back to keep the host responsive); remedy=%s",
				s.Resource, short/(1024*1024), s.RequiredBytes/(1024*1024),
				s.AvailableBytes/(1024*1024), s.ReservedBytes/(1024*1024), w.Reason.Remedy())
		}
		return fmt.Sprintf("insufficient_resources; remedy=%s", w.Reason.Remedy())
	case selection.ReasonUnsupportedConfiguration:
		if u := w.Unsupported; u != nil {
			return fmt.Sprintf("unsupported_configuration — this host provides no %s (%s); "+
				"more memory does not help; remedy=%s", u.Requirement, u.Detail, w.Reason.Remedy())
		}
		return fmt.Sprintf("unsupported_configuration; remedy=%s", w.Reason.Remedy())
	case selection.ReasonExcludedByUsageTerms:
		if x := w.Exclusion; x != nil {
			return fmt.Sprintf("excluded_by_usage_terms — licence %s does not permit %q (term=%s, granted=%t, ref=%s); "+
				"the host could serve it; remedy=%s",
				x.LicenseID, x.Purpose, x.Term, x.Granted, x.Reference, w.Reason.Remedy())
		}
		return fmt.Sprintf("excluded_by_usage_terms; remedy=%s", w.Reason.Remedy())
	default:
		return string(w.Reason)
	}
}

// familyRefusalReport says why nothing could be offered, naming the reason
// closest to being satisfiable so the operator is not sent to buy hardware when
// the obstacle is a licence.
func familyRefusalReport(fr selection.FamilyResult, family catalogue.CapabilityFamily, forbidKey string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CANNOT-CHOOSE: no %s model can run on this measured host.", family)
	if fr.Refusal != nil {
		fmt.Fprintf(&b, "\n  closest reason: %s (remedy=%s)", fr.Refusal.Reason, fr.Refusal.Reason.Remedy())
		if missing := fr.Refusal.Missing(); len(missing) > 0 {
			fmt.Fprintf(&b, "\n  what the candidates lacked: %s", strings.Join(missing, ", "))
		}
	}
	if len(fr.Offered) > 0 {
		fmt.Fprintf(&b, "\n  note: %d option(s) the host CAN serve were removed by %s (operator forbid-list).",
			len(fr.Offered), forbidKey)
	}
	b.WriteString("\n  No model is started: a model that was not chosen from a measurement may not fit this host.")
	return b.String()
}

// refusalError renders a host-level refusal — the reading itself was not a
// usable basis for any choice — and carries the exit code its remedy implies.
func refusalError(result selection.Result, err error) error {
	r := result.Refusal
	switch {
	case errors.Is(err, selection.ErrHostNotMeasured):
		cause := "measurement-incomplete"
		host := ""
		if r != nil {
			cause, host = r.Cause, r.HostIdentity
		}
		return exitErr(exitHostNotMeasured,
			"CANNOT-CHOOSE: this host was not measured (cause=%s, host=%q), so there is no basis for a choice.\n"+
				"  No model is started: there is no fixed default, because a model that was not chosen from a "+
				"measurement may not fit this host (FR-056).\n"+
				"  Remedy: investigate the failed measurement above and retry.", cause, host)
	case errors.Is(err, selection.ErrMeasurementStale):
		age, limit := 0.0, 0.0
		if r != nil {
			age, limit = r.AgeSeconds, r.MaxAgeSeconds
		}
		return exitErr(exitMeasurementStale,
			"CANNOT-CHOOSE: the host reading is %.3fs old, older than the %.3fs this decision allows.\n"+
				"  No model is started on a reading that no longer describes this host. Remedy: re-measure.", age, limit)
	case errors.Is(err, selection.ErrNoDeclaredUsage):
		return exitErr(exitNoOptionOffered,
			"CANNOT-CHOOSE: no declared usage, so licence terms cannot be applied. "+
				"Set HELIXLLM_DECLARED_USAGE.")
	default:
		return exitErr(exitNoOptionOffered, "CANNOT-CHOOSE: %v", err)
	}
}

// repositoryFor is where the chosen model's weights are obtained from.
//
// It reads Entry.Source and nothing else. Source is the field the FR-012 /
// SC-011 acquisition allowlist is checked against, so deriving the repository
// from anywhere else would hand the runtime a weight location that no gate ever
// saw. In particular `annotations.weights_repo` is NOT consulted: annotations
// are carried and shown, never read to make a decision (catalogue.Entry's own
// contract), and they are not validated.
//
// KNOWN DATA GAP, stated rather than worked around (§11.4.6): no entry in
// internal/catalogue/data/video.yaml records `source:` today — that file says so
// deliberately, because `source` is what the acquisition gate checks and
// choosing its value is a data decision. Until an entry records one, this
// function refuses it and the lane boots nothing. That is the honest outcome:
// the alternative is pulling a repository nobody validated.
func repositoryFor(e catalogue.Entry) (string, error) {
	src := strings.TrimSpace(e.Source)
	if src == "" {
		return "", fmt.Errorf("catalogue entry %s records no weight source, so there is no validated "+
			"repository to serve (annotations are not read for this: they are unvalidated). "+
			"Remedy: record `source:` on the entry", e.Identity())
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

// servingBackends maps a recorded model to the diffusion pipeline this runtime
// implements for it (services/videogen/videogen_server.py: WanPipeline or
// LTXPipeline). It is the runtime declaring what it can serve, in the same
// shape as servingPrecisions below.
//
// It names models, but it can only ever REMOVE a candidate the measurement
// offered — never introduce one, and never supply weights. A model absent from
// this map is reported as not-servable-here rather than sent to a pipeline that
// cannot load it. Which model runs is still decided by the measurement; this
// only answers "and can this runtime serve that one at all".
//
// Keyed by model id because that is what distinguishes the two pipelines:
// `architecture` cannot, both WAN and LTX are recorded as diffusion.
var servingBackends = map[string]string{
	"wan2.2-ti2v-5b":  "wan",
	"wan2.2-t2v-a14b": "wan",
	"ltx-video-13b":   "ltx",
}

// backendFor reports which pipeline this runtime would load for the chosen
// model.
func backendFor(o selection.Option) (string, error) {
	b, ok := servingBackends[strings.ToLower(strings.TrimSpace(o.ModelID))]
	if !ok {
		return "", fmt.Errorf("this runtime implements no diffusion pipeline for %s "+
			"(it serves: %s)", o.ModelID, strings.Join(sortedKeys(servingBackends), ", "))
	}
	return b, nil
}

// servingPrecisions are the weight formats the video runtime actually
// implements (services/videogen/videogen_server.py). A build quantised outside
// this set is not servable HERE — reported honestly rather than silently
// coerced into a precision the entry's memory figure was never measured at.
var servingPrecisions = map[string]string{
	"fp8":  "fp8",
	"bf16": "bf16",

	// "gguf-q4" is DELIBERATELY ABSENT, and removing it was a regression fix.
	//
	// services/videogen/videogen_server.py refuses gguf-q4 in
	// _UNIMPLEMENTED_PRECISIONS: the service has no GGUF load path at all — it
	// builds pipelines only through from_pretrained, which resolves a
	// diffusers-format repository and cannot read single-file .gguf weights.
	//
	// This list and that one are two independent statements about the same
	// question, and they disagreed. Nothing noticed while most-capable-first
	// ordering happened to rank a servable fp8 build first. When ordering
	// changed to cheapest-first, the cheaper gguf-q4 entry moved to the front
	// and the lane began choosing a build the runtime refuses: admit the VRAM,
	// compose up, 503 for the full health timeout, exit 4, holding the lease
	// throughout.
	//
	// The service is authoritative here — it is the thing that actually loads
	// weights. If a GGUF load path is added there, add the precision back HERE
	// in the same change, and see the agreement test that now pins the two
	// lists together.
}

// precisionFor reports the serving precision the chosen build implies.
//
// It reads the recorded Descriptor.Quantisation rather than splitting the
// variant string: the variant is an identity ("fp8-480p"), the quantisation is
// the validated field that says what the weights actually are.
func precisionFor(o selection.Option) (string, error) {
	q := strings.ToLower(strings.TrimSpace(o.Descriptor.Quantisation))
	if q == "" {
		return "", fmt.Errorf("build %q of %s records no quantisation, so the serving precision is unknown",
			o.Variant, o.ModelID)
	}
	p, ok := servingPrecisions[q]
	if !ok {
		return "", fmt.Errorf("quantisation %q of %s is not one this runtime serves (%s)",
			q, o.ModelID, strings.Join(sortedKeys(servingPrecisions), ", "))
	}
	return p, nil
}

// videoShape is the clip the chosen entry is offered to produce: the frame
// size, count and rate the option was selected AT. Serving some other shape
// would be serving a configuration whose memory figure nobody measured.
type videoShape struct {
	Size      string // WxH, as the runtime expects it
	NumFrames int
	FPS       int
}

// videoShapeFor reads the shape from the chosen option's recorded video output.
//
// A missing or fractional rate is refused rather than rounded: the runtime
// parses the rate as an integer, so a fractional one would abort the container
// at start-up, and rounding it would serve a clip nobody recorded.
func videoShapeFor(o selection.Option) (videoShape, error) {
	v := o.Expected.Video
	if !v.Recorded() {
		return videoShape{}, fmt.Errorf("%s records no video output, so there is no clip shape to serve",
			o.Identity)
	}
	fps := int(v.FramesPerSecond)
	if float64(fps) != v.FramesPerSecond {
		return videoShape{}, fmt.Errorf("%s records a fractional rate (%g fps) this runtime cannot serve",
			o.Identity, v.FramesPerSecond)
	}
	return videoShape{
		Size:      fmt.Sprintf("%dx%d", v.FrameWidth, v.FrameHeight),
		NumFrames: v.FrameCount,
		FPS:       fps,
	}, nil
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func identityOf(modelID, variant string) string {
	if variant == "" {
		return modelID
	}
	return modelID + ":" + variant
}

// codedError carries the exit code a refusal implies alongside its explanation.
type codedError struct {
	code int
	msg  string
}

func (e *codedError) Error() string { return e.msg }

func exitErr(code int, format string, a ...any) error {
	return &codedError{code: code, msg: fmt.Sprintf(format, a...)}
}

// exitCodeFor reports the exit code an error carries, defaulting to the generic
// refusal code so no failure path can accidentally exit zero.
func exitCodeFor(err error) int {
	var ce *codedError
	if errors.As(err, &ce) {
		return ce.code
	}
	return exitNoOptionOffered
}
