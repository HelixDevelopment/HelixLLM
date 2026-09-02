package selection

import (
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
)

// WithheldReason is why one candidate was not offered.
//
// There are exactly three, and they are not interchangeable: each implies a
// different remedy, so collapsing them into a single generic unavailability
// destroys the only part of the answer a user can act on (FR-055). Values are
// machine keys — the wording shown to a user is composed from them, never
// stored here (CONST-046).
type WithheldReason string

const (
	// ReasonInsufficientResources: the host lacks the memory or storage the
	// option needs. A different host, or a smaller option, resolves it.
	ReasonInsufficientResources WithheldReason = "insufficient_resources"
	// ReasonUnsupportedConfiguration: nothing about this host's configuration
	// can run the option — a mandatory accelerator it does not have, or a
	// runtime path that does not exist. More memory does not help; the remedy
	// is a different approach.
	ReasonUnsupportedConfiguration WithheldReason = "unsupported_configuration"
	// ReasonExcludedByUsageTerms: the option is otherwise suitable and the
	// host could serve it, but its terms forbid the declared usage. The remedy
	// is a different model or a different declared usage — never hardware.
	ReasonExcludedByUsageTerms WithheldReason = "excluded_by_usage_terms"
)

// Known reports whether r is one of the three recorded reasons. The set is
// closed: a generic "unavailable" invented downstream is not a reason.
func (r WithheldReason) Known() bool {
	switch r {
	case ReasonInsufficientResources, ReasonUnsupportedConfiguration, ReasonExcludedByUsageTerms:
		return true
	default:
		return false
	}
}

// Remedy is the machine key of what would resolve a withholding. It exists so
// the three reasons stay distinguishable in what they ask of the user, not only
// in their names.
type Remedy string

const (
	RemedyChangeHostOrPickSmaller Remedy = "change-host-or-pick-smaller"
	RemedyDifferentApproach       Remedy = "different-approach"
	RemedyDifferentModelOrUsage   Remedy = "different-model-or-declared-usage"
)

// Remedy returns the remedy r implies, or the empty Remedy for an unrecorded
// reason.
func (r WithheldReason) Remedy() Remedy {
	switch r {
	case ReasonInsufficientResources:
		return RemedyChangeHostOrPickSmaller
	case ReasonUnsupportedConfiguration:
		return RemedyDifferentApproach
	case ReasonExcludedByUsageTerms:
		return RemedyDifferentModelOrUsage
	default:
		return ""
	}
}

// Resource is a measured host axis an option consumes. Memory and storage are
// separate values because they are separate axes: a model's disk footprint is
// not implied by its memory figure (D2).
type Resource string

const (
	ResourceMemory      Resource = "memory"
	ResourceStorage     Resource = "storage"
	ResourceAccelerator Resource = "accelerator"
)

// Requirement is something a host configuration must provide before an option
// can run there at all, as distinct from a quantity it must have enough of.
type Requirement string

const (
	// RequirementAccelerator: the option mandates an acceleration device and
	// the host was measured to have none.
	RequirementAccelerator Requirement = "accelerator"
	// RequirementStreamingRoster: the option is served only by the streaming
	// runtime, and that runtime does not list its family. Eligibility is roster
	// membership and nothing else — never architecture (D1).
	RequirementStreamingRoster Requirement = "streaming-roster"
	// RequirementCatalogueEntry: the catalogue records nothing that could serve
	// the request at all.
	RequirementCatalogueEntry Requirement = "catalogue-entry"
)

// ResourceCost is what an option would consume, in the terms the host is
// measured in, so two options can be compared without knowing model internals
// (FR-005).
type ResourceCost struct {
	MemoryRequiredBytes  uint64
	StorageRequiredBytes uint64
	RequiresAccelerator  bool
}

// Headroom is what the measured host would have left while this option serves.
// It is stated on every offer because "it fits" and "it fits and leaves the
// machine usable" are different claims (FR-008, SC-003).
type Headroom struct {
	MemoryRemainingBytes  uint64
	StorageRemainingBytes uint64
	// MemoryRemainingFraction is MemoryRemainingBytes over the host's nameplate
	// total — the figure SC-003's threshold is expressed against.
	MemoryRemainingFraction float64
}

// Option is a runnable choice on one measured host.
//
// Everything host-dependent lives here rather than in the catalogue entry it
// was derived from: the join reads the entry and the measurement, and writes
// back into neither.
type Option struct {
	ModelID string
	Variant string
	// Identity is the host-qualified name helixllm/<host>/<model>[:<variant>].
	// It is formed here because this is the only place that knows the host (D7).
	Identity     string
	HostIdentity string

	Family  catalogue.CapabilityFamily
	Runtime catalogue.RuntimeKind

	Cost       ResourceCost
	Headroom   Headroom
	Expected   catalogue.ExpectedCapability
	Descriptor catalogue.Descriptor
	// Terms travel with the offer so the user sees what they may do with the
	// output at the moment of choosing, not after (FR-054).
	Terms catalogue.UsageTerms
}

// Shortfall records a resource the host does not have enough of. It names the
// axis, so "does not fit" is never reported without saying which axis was
// short.
type Shortfall struct {
	Resource Resource
	// RequiredBytes is what the option needs.
	RequiredBytes uint64
	// AvailableBytes is what the host has for it AFTER the reserve is held
	// back — the figure the requirement was actually compared against.
	AvailableBytes uint64
	// ReservedBytes is what was withheld to keep the host responsive, so a
	// refusal can distinguish "too big for this machine" from "too big once the
	// machine keeps working" (FR-007, FR-008).
	ReservedBytes uint64
}

// Unsupported records a configuration requirement the host does not meet.
type Unsupported struct {
	Requirement Requirement
	// Detail carries the machine-readable specific — the roster family name
	// that was looked up, for instance — so a reader can see what was checked
	// rather than only that a check failed.
	Detail string
}

// Exclusion records the usage terms that withheld an option.
type Exclusion struct {
	// Purpose is the usage the user declared.
	Purpose   catalogue.UsagePurpose
	LicenseID string
	// Granted reports whether the licence grants Purpose at all. False with an
	// empty Term means the licence simply never permitted it, which is an
	// absence of permission rather than a restriction.
	Granted bool
	// Term is the restriction that excluded Purpose — the restricting one, not
	// merely the first one the licence lists.
	Term      catalogue.RestrictionTerm
	Threshold catalogue.Amount
	Reference string
}

// Withheld is one candidate that was not offered, with exactly one reason and
// only the detail belonging to that reason.
type Withheld struct {
	ModelID string
	Variant string
	Family  catalogue.CapabilityFamily

	Reason WithheldReason

	// Exactly one of these is set, matching Reason.
	Shortfall   *Shortfall
	Unsupported *Unsupported
	Exclusion   *Exclusion
}

// Names returns the machine key of what this withholding says is missing: the
// resource, the requirement, or the restricting term.
func (w Withheld) Names() string {
	switch {
	case w.Shortfall != nil:
		return string(w.Shortfall.Resource)
	case w.Unsupported != nil:
		return string(w.Unsupported.Requirement)
	case w.Exclusion != nil:
		if w.Exclusion.Term != "" {
			return string(w.Exclusion.Term)
		}
		return w.Exclusion.LicenseID
	default:
		return ""
	}
}

// FamilyRefusal is a capability family that cannot be served on this host. A
// family is never silently empty: it offers something, or it says why not and
// names what is missing (D5).
type FamilyRefusal struct {
	Family catalogue.CapabilityFamily
	// Reason is the one closest to being satisfiable among the candidates —
	// reporting a hardware obstacle when the real one is a licence sends the
	// user to spend money that will not help.
	Reason WithheldReason
	// Candidates are every entry that was considered, each with its own reason.
	// When the catalogue recorded nothing for the family there was nothing to
	// consider, and a single entry with no model id carries the missing
	// requirement instead — so the refusal still names what is lacking rather
	// than being an empty list with a reason attached.
	Candidates []Withheld
}

// Missing returns the machine keys of what the considered candidates lacked,
// deduplicated and in the order first encountered.
func (r FamilyRefusal) Missing() []string {
	seen := make(map[string]struct{}, len(r.Candidates))
	names := make([]string, 0, len(r.Candidates))
	for _, c := range r.Candidates {
		name := c.Names()
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

// FamilyResult is the answer for one capability family: offers, or a refusal
// that states why. Never both, and never neither.
type FamilyResult struct {
	Family   catalogue.CapabilityFamily
	Offered  []Option
	Withheld []Withheld
	// Refusal is non-nil exactly when Offered is empty.
	Refusal *FamilyRefusal
}

// HostRefusalKind is why a host could not be the basis of any selection at all.
//
// These are deliberately not WithheldReason values: "this host was not
// measured" is not a statement about an option, because no option was reached.
// Keeping the kinds apart is what stops a failed measurement from surfacing as
// a resource shortfall.
type HostRefusalKind string

const (
	// RefusalHostNotMeasured: measurement did not complete. There is no basis
	// to choose from, and no fixed default may stand in for one (FR-056).
	RefusalHostNotMeasured HostRefusalKind = "host_not_measured"
	// RefusalMeasurementStale: the reading is older than the request allows, so
	// offers derived from it would not reflect current conditions (FR-033).
	RefusalMeasurementStale HostRefusalKind = "measurement_stale"
)

// Known reports whether k is a recorded host refusal kind.
func (k HostRefusalKind) Known() bool {
	return k == RefusalHostNotMeasured || k == RefusalMeasurementStale
}

// HostRefusal states that no selection could be made and why, carrying the
// measurement facts the statement rests on.
type HostRefusal struct {
	Kind         HostRefusalKind
	HostIdentity string
	MeasuredAt   time.Time
	EvaluatedAt  time.Time
	// AgeSeconds and MaxAgeSeconds are populated for RefusalMeasurementStale so
	// the refusal shows the comparison it made.
	AgeSeconds    float64
	MaxAgeSeconds float64
	// Cause is the machine-readable reason measurement was not a usable basis.
	Cause string
}

// Result is the answer to one selection request.
//
// Refusal non-nil means no family was evaluated at all: the host itself was not
// a usable basis. Otherwise every requested family appears in Families, each
// either offering or refusing.
type Result struct {
	HostIdentity  string
	EvaluatedAt   time.Time
	DeclaredUsage catalogue.UsagePurpose
	Pin           *Pin
	Families      []FamilyResult
	Refusal       *HostRefusal
}

// Family returns the result for f, and whether f was evaluated.
func (r Result) Family(f catalogue.CapabilityFamily) (FamilyResult, bool) {
	for _, fr := range r.Families {
		if fr.Family == f {
			return fr, true
		}
	}
	return FamilyResult{}, false
}
