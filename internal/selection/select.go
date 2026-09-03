package selection

import (
	"errors"
	"fmt"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
)

// Request is everything a selection is decided from.
//
// That the whole input is here — and that nothing else is consulted — is what
// makes Select a pure function, and what lets every path through it, refusals
// included, be driven from fixture hosts with no hardware present. In
// particular no environment variable, configuration file or ambient default
// reaches this decision: which model runs is decided from the measured host
// (FR-056).
type Request struct {
	// Profile is the measured host. It is read and never written back into.
	Profile capability.HostCapabilityProfile

	// Entries is the catalogue to choose from.
	Entries []catalogue.Entry

	// Families narrows the answer. Empty means every family the catalogue has
	// an entry for.
	Families []catalogue.CapabilityFamily

	// DeclaredUsage is how the user has said the output will be used. It is
	// required: terms cannot be applied against an undeclared usage, and
	// assuming one would offer models the user may not be permitted to use.
	DeclaredUsage catalogue.UsagePurpose

	// Pin narrows the answer to one deliberately chosen model. It constrains
	// the choice; it never bypasses the measurement (FR-056).
	Pin *Pin

	// Now is the instant the selection is made, against which the measurement's
	// freshness is judged. Zero means time.Now().UTC().
	Now time.Time

	// MaxProfileAge is how old a reading may be and still be a current
	// measurement (FR-033). Zero means freshness is not bounded by this call —
	// appropriate only when the caller has just measured.
	MaxProfileAge time.Duration

	// Reserve is what to hold back so the host stays responsive while serving.
	// The zero Reserve means DefaultReserve.
	Reserve Reserve

	// AcceleratorBound says the caller will spend the chosen option's memory
	// requirement on the ACCELERATOR, whatever the entry's own
	// RequiresAccelerator flag says.
	//
	// It is a fact about the CALLER, which is why it is stated here and not
	// read off the entry. Every *-boot lane admits its chosen option through
	// vrambroker.Acquire, and the figure it admits is that option's
	// MemoryRequiredBytes — the same number selection has just checked. Without
	// this field selection checks that number against host RAM and the lane
	// then spends it against device memory: two different resources, one
	// number, and nothing comparing them.
	//
	// What that costs, measured on the shipped catalogue: on a host with 44 GiB
	// of free RAM and a 4 GiB card, the text family offered four options
	// needing 16-24 GiB, each of which the lane would have tried to admit
	// against that card. The lane's own doc comment claimed it "cannot offer a
	// model it will then refuse to start" on the strength of passing the
	// admission gate's margin to Reserve — but the margin was only ever applied
	// on an axis that accelerator-optional entries never reached.
	//
	// Setting it does NOT reclassify the model. An entry stays
	// processor-servable, its catalogue figures stay exactly what they were
	// sourced as, and a caller that does not bind the accelerator gets the same
	// answer as before. It states where THIS caller will put the bytes.
	AcceleratorBound bool
}

// Errors Select reports. They are distinct because their remedies are: a
// reading that never completed is investigated, one that is merely old is
// refreshed, and an undeclared usage is supplied by the caller.
var (
	ErrHostNotMeasured  = errors.New("selection: host was not measured, no basis for a choice")
	ErrMeasurementStale = errors.New("selection: measurement is older than this selection allows")
	ErrNoDeclaredUsage  = errors.New("selection: no declared usage, terms cannot be applied")
)

// Select joins a measured host against the catalogue under a declared usage and
// returns the options that host can actually serve.
//
// The join is one-directional: the profile and the entries are read, and
// neither is written back into. Everything host-dependent — fit, headroom, the
// host-qualified identity — is produced here and lives on the result.
//
// When the host is not a usable basis for a choice, the answer is a refusal
// that says so, carried on the Result alongside the error. There is no fixed
// default to fall back to, because a model that was not chosen from a
// measurement may not fit the machine it is started on (FR-056).
func Select(req Request) (Result, error) {
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	result := Result{
		HostIdentity:  req.Profile.HostIdentity,
		EvaluatedAt:   now,
		DeclaredUsage: req.DeclaredUsage,
		Pin:           req.Pin,
	}

	if req.DeclaredUsage == "" {
		return result, ErrNoDeclaredUsage
	}

	// The measurement is the basis of everything below it, so it is checked
	// before any candidate is looked at. An incomplete profile is not a profile
	// that needs completing with defaults — it is a refusal.
	if err := req.Profile.ValidateForSelection(); err != nil {
		result.Refusal = &HostRefusal{
			Kind:         RefusalHostNotMeasured,
			HostIdentity: req.Profile.HostIdentity,
			MeasuredAt:   req.Profile.MeasuredAt,
			EvaluatedAt:  now,
			Cause:        causeKey(err),
		}
		return result, fmt.Errorf("%w: %v", ErrHostNotMeasured, err)
	}

	// Headroom is judged from the reading handed to this call. A reading older
	// than the caller allows is not a current measurement, and offers derived
	// from it would describe conditions that have moved on (FR-006, FR-033).
	if req.MaxProfileAge > 0 {
		age := req.Profile.Age(now)
		if age > req.MaxProfileAge {
			result.Refusal = &HostRefusal{
				Kind:          RefusalMeasurementStale,
				HostIdentity:  req.Profile.HostIdentity,
				MeasuredAt:    req.Profile.MeasuredAt,
				EvaluatedAt:   now,
				AgeSeconds:    age.Seconds(),
				MaxAgeSeconds: req.MaxProfileAge.Seconds(),
				Cause:         string(RefusalMeasurementStale),
			}
			return result, fmt.Errorf("%w: %s old, limit %s", ErrMeasurementStale, age, req.MaxProfileAge)
		}
	}

	reserve := req.Reserve
	if reserve.Zero() {
		reserve = DefaultReserve()
	}

	result.Families = evaluate(req, fitPolicy{
		reserve:          reserve,
		acceleratorBound: req.AcceleratorBound,
	})
	return result, nil
}

// evaluate produces one FamilyResult per requested family.
func evaluate(req Request, policy fitPolicy) []FamilyResult {
	entries := req.Entries

	// A pin narrows the candidate set and nothing else. Everything that follows
	// — configuration, fit, terms — applies to the pinned entry exactly as it
	// applies to any other, which is the whole difference between a constraint
	// and a bypass.
	if req.Pin != nil {
		matched := req.Pin.constrain(entries)
		if len(matched) == 0 {
			family := catalogue.CapabilityFamily("")
			if len(req.Families) == 1 {
				family = req.Families[0]
			}
			return []FamilyResult{unmatchedFamily(*req.Pin, family)}
		}
		entries = matched
	}

	byFamily := make(map[catalogue.CapabilityFamily][]catalogue.Entry, len(entries))
	for _, e := range entries {
		byFamily[e.Family] = append(byFamily[e.Family], e)
	}

	wanted := req.Families
	if len(wanted) == 0 {
		// Answer for every family the candidate set covers. A family nothing is
		// recorded for is not silently absent when it was asked for by name —
		// see the branch below — but it is not invented when it was not.
		for _, f := range familyOrder {
			if _, present := byFamily[f]; present {
				wanted = append(wanted, f)
			}
		}
		for f := range byFamily {
			if !f.Known() {
				wanted = append(wanted, f)
			}
		}
	}

	results := make([]FamilyResult, 0, len(wanted))
	seen := make(map[catalogue.CapabilityFamily]struct{}, len(wanted))
	for _, f := range wanted {
		if _, dup := seen[f]; dup {
			continue
		}
		seen[f] = struct{}{}
		results = append(results, evaluateFamily(f, byFamily[f], req.Profile, req.DeclaredUsage, policy))
	}

	sortFamilies(results)
	return results
}

// causeKey renders a measurement failure as a stable machine key, so a refusal
// states what was missing without carrying a sentence composed elsewhere.
func causeKey(err error) string {
	switch {
	case errors.Is(err, capability.ErrNotMeasured):
		return "measurement-incomplete"
	case errors.Is(err, capability.ErrNoHostIdentity):
		return "no-host-identity"
	case errors.Is(err, capability.ErrNoMeasurementTime):
		return "no-measurement-time"
	case errors.Is(err, capability.ErrNoCPUCores):
		return "no-cpu-cores"
	case errors.Is(err, capability.ErrNoMemoryTotal):
		return "no-memory-total"
	case errors.Is(err, capability.ErrCompleteButStateUnknown),
		errors.Is(err, capability.ErrUnknownStateHasDevices):
		return "accelerator-state-unknown"
	default:
		return "profile-malformed"
	}
}
