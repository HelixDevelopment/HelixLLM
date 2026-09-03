package laneboot

// Measured model selection, shared by every boot lane.
//
// The rule this package exists to enforce: WHICH model runs is decided from the
// MEASURED host, never from a static preconfigured value. Configuration may
// still say WHERE the catalogue lives (HELIXLLM_CATALOGUE_DIR), what the user
// has DECLARED about their usage (HELIXLLM_DECLARED_USAGE) and which options
// they FORBID (the lane's own forbid-key). None of those names the model.
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
//
// WHY THIS IS ONE PACKAGE (§11.4.74). This decision was previously carried as
// four byte-identical copies, one per boot lane (video, image, vision, agent).
// The copies were not a stylistic problem. This feature has already been bitten
// by the failure mode they invite: two independent statements of which
// precisions are servable drifted apart, nothing noticed because each was
// self-consistent, and a lane planned a build the runtime then refused. Four
// copies of a decision are four chances for that; there is now one.
//
// The lanes parameterise it by family, forbid-key, and weights directory — the
// last is not decoration: it reaches capability.Measure, so for the lanes that
// pass a real directory it can change the measurement and therefore the choice.
//
// WHAT STAYS IN THE LANE. Everything downstream of the decision, because it
// genuinely differs: where a lane's weights come from, which backend or
// precision it serves, the clip shape a video option was selected at, and the
// lane's own not-servable exit code. Only the choosing is shared.
//
// KNOWN FOLLOW-UP, recorded rather than left to be rediscovered. Six of the
// exports below — DescribeWithheld, RefusalError, IdentityOf, and the
// ExitHostNotMeasured / ExitMeasurementStale / ExitCatalogueMissing codes —
// have NO production caller. They are exported only so each lane's
// measured_selection_test.go can still reach them, and those tests now cover
// behaviour that is entirely shared: TestMeasurementFailureIsReportedAndRefused
// is byte-identical in all four lanes, TestWithheldReasonsStayDistinct in two
// pairs. Moving them here would collapse four copies to one — the same
// argument this package is the answer to — and let all six go unexported.
//
// It is not done in this change because it DELETES tests from four packages,
// which deserves to be proposed and reviewed on its own rather than carried in
// as a side effect of moving production code.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/runtime"
	"github.com/HelixDevelopment/HelixLLM/internal/selection"
)

// Exit codes for the selection stage. They are distinct from the admission
// codes (10-14) because their remedies are: a host that cannot be measured is
// investigated, a stale reading is retaken, a refusal is answered by changing
// the host, the model or the declared usage.
//
// These four are the codes the SHARED decision can reach, so they live with it.
// Code 24 means "the lane cannot serve what was chosen" and is the lane's own
// to name: the video and image lanes call it exitNotServable, the vision and
// agent lanes exitWeightsNotPresent. Nothing here reaches it.
const (
	ExitHostNotMeasured  = 20
	ExitMeasurementStale = 21
	ExitNoOptionOffered  = 22
	ExitCatalogueMissing = 23
)

// defaultCatalogueDir is where the recorded catalogue lives in a checkout. It
// is a LOCATION, not a model name: it says where the candidates are described,
// never which of them runs.
const defaultCatalogueDir = "internal/catalogue/data"

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

// ParsePin reads an optional deliberate pin from the argument list. A pin is
// the one legitimate way a caller names a model, and it is stated at invocation
// rather than carried in ambient configuration so it is unmistakably a
// deliberate act.
func ParsePin(args []string) (*selection.Pin, error) {
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

// selectionRequest builds the selection every lane makes.
//
// It is a named function rather than a literal inline because two of its fields
// are not describing the model or the host — they are this binary declaring how
// it will USE what it is given, and both have to stay in agreement with the
// admission gate a few steps later in the same process:
//
//   - Reserve carries the ADMISSION GATE's own device-memory margin
//     (runtime.SelectionReserve), so selection holds back what the gate will
//     hold back.
//   - AcceleratorBound says the memory figure will be SPENT ON THE DEVICE.
//     Every lane computes its admission need as the chosen option's
//     MemoryRequiredBytes and hands it to vrambroker.Acquire; without this,
//     selection checks that figure against host RAM and the lane spends it
//     against the card.
//
// Those two together are what make "this binary cannot offer a model it will
// then refuse to start" true. The margin alone did not: it only ever applied on
// an axis that entries with requires_accelerator: false never reached, and the
// shipped text catalogue is mostly such entries. On a host with plenty of RAM
// and a small card the lane chose a 16-24 GiB model, announced it, and was then
// refused by its own gate.
func selectionRequest(
	profile capability.HostCapabilityProfile,
	loaded catalogue.Catalogue,
	family catalogue.CapabilityFamily,
	purpose catalogue.UsagePurpose,
	pin *selection.Pin,
	maxProfileAge time.Duration,
) selection.Request {
	return selection.Request{
		Profile:          profile,
		Entries:          loaded.Entries(),
		Families:         []catalogue.CapabilityFamily{family},
		DeclaredUsage:    purpose,
		Pin:              pin,
		Now:              time.Now().UTC(),
		MaxProfileAge:    maxProfileAge,
		Reserve:          runtime.SelectionReserve(),
		AcceleratorBound: true,
	}
}

// Decide measures the host and returns the options it can actually serve for
// one family, in the order the catalogue records them.
//
// Every failure path here refuses. None of them substitutes a default model.
func Decide(
	ctx context.Context,
	family catalogue.CapabilityFamily,
	weightsDir string,
	pin *selection.Pin,
	forbidKey string,
) ([]selection.Option, catalogue.Catalogue, capability.HostCapabilityProfile, catalogue.UsagePurpose, error) {
	loaded, err := catalogue.Load(catalogueDir())
	if err != nil {
		return nil, loaded, capability.HostCapabilityProfile{}, "",
			ExitErr(ExitCatalogueMissing, "CANNOT-CHOOSE: the catalogue of candidates could not be read "+
				"(%v). Nothing is started without candidates to choose from.", err)
	}

	purpose, defaulted, err := declaredUsage()
	if err != nil {
		return nil, loaded, capability.HostCapabilityProfile{}, "",
			ExitErr(ExitNoOptionOffered, "CANNOT-CHOOSE: %v", err)
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

	result, err := selection.Select(selectionRequest(profile, loaded, family, purpose, pin, policy.MaxAge))
	if err != nil {
		return nil, loaded, profile, purpose, RefusalError(result, err)
	}

	fr, ok := result.Family(family)
	if !ok {
		return nil, loaded, profile, purpose, ExitErr(ExitNoOptionOffered,
			"CANNOT-CHOOSE: nothing in the catalogue serves the %s family on this host.", family)
	}
	reportWithheld(fr)

	offered := applyForbidList(fr.Offered, forbidKey)
	if len(offered) == 0 {
		return nil, loaded, profile, purpose, ExitErr(ExitNoOptionOffered, "%s", familyRefusalReport(fr, family, forbidKey))
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

// EntryFor recovers the catalogue entry an option was derived from. The option
// carries everything host-dependent; the entry carries the source the weights
// come from.
func EntryFor(loaded catalogue.Catalogue, o selection.Option) (catalogue.Entry, bool) {
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
		fmt.Printf("WITHHELD %s: %s\n", IdentityOf(w.ModelID, w.Variant), DescribeWithheld(w))
	}
}

// DescribeWithheld renders one withholding. The three reasons never collapse
// into a generic unavailability: each names its own specific and its own
// remedy, because each asks something different of the operator.
//
// Not to be confused with [selection.DescribeWithheld], which shares the name
// but not the job: that one returns structured fields for a machine to read,
// this one returns the sentence an operator reads on their terminal. Both are
// wanted — the structured form cannot carry the remedy phrasing, and prose
// cannot be queried — so they stay separate rather than one wrapping the other.
func DescribeWithheld(w selection.Withheld) string {
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

// RefusalError renders a host-level refusal — the reading itself was not a
// usable basis for any choice — and carries the exit code its remedy implies.
func RefusalError(result selection.Result, err error) error {
	r := result.Refusal
	switch {
	case errors.Is(err, selection.ErrHostNotMeasured):
		cause := "measurement-incomplete"
		host := ""
		if r != nil {
			cause, host = r.Cause, r.HostIdentity
		}
		return ExitErr(ExitHostNotMeasured,
			"CANNOT-CHOOSE: this host was not measured (cause=%s, host=%q), so there is no basis for a choice.\n"+
				"  No model is started: there is no fixed default, because a model that was not chosen from a "+
				"measurement may not fit this host (FR-056).\n"+
				"  Remedy: investigate the failed measurement above and retry.", cause, host)
	case errors.Is(err, selection.ErrMeasurementStale):
		age, limit := 0.0, 0.0
		if r != nil {
			age, limit = r.AgeSeconds, r.MaxAgeSeconds
		}
		return ExitErr(ExitMeasurementStale,
			"CANNOT-CHOOSE: the host reading is %.3fs old, older than the %.3fs this decision allows.\n"+
				"  No model is started on a reading that no longer describes this host. Remedy: re-measure.", age, limit)
	case errors.Is(err, selection.ErrNoDeclaredUsage):
		return ExitErr(ExitNoOptionOffered,
			"CANNOT-CHOOSE: no declared usage, so licence terms cannot be applied. "+
				"Set HELIXLLM_DECLARED_USAGE.")
	default:
		return ExitErr(ExitNoOptionOffered, "CANNOT-CHOOSE: %v", err)
	}
}

func IdentityOf(modelID, variant string) string {
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

func ExitErr(code int, format string, a ...any) error {
	return &codedError{code: code, msg: fmt.Sprintf(format, a...)}
}

// ExitCodeFor reports the exit code an error carries, defaulting to the generic
// refusal code so no failure path can accidentally exit zero.
func ExitCodeFor(err error) int {
	var ce *codedError
	if errors.As(err, &ce) {
		return ce.code
	}
	return ExitNoOptionOffered
}
