package runtime

import (
	"fmt"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
)

// Runtime choice.
//
// The rule is deliberately not symmetric. The general in-memory runtime is
// tried FIRST, always. The disk-streaming runtime is reached only when all
// three of these hold:
//
//	(a) the model does not fit in the host's available memory, AND
//	(b) the model's family is on the streaming runtime's declared roster, AND
//	(c) the host meets that runtime's own per-model memory and disk minimums.
//
// Otherwise nothing can serve the model, and the refusal says which of the two
// distinct things went wrong.
//
// Streaming buys feasibility with throughput, by orders of magnitude. It is a
// fallback and never a preference: a model that fits in memory is served from
// memory even when it is also on the roster, because choosing streaming for it
// would be a large, user-visible slowdown accepted for nothing.

// RefusalReason is why no path serves a model on a host.
//
// The set is closed and its members are NOT interchangeable — each implies a
// different remedy, and collapsing them into one generic unavailability
// destroys the only part of the answer a user can act on (FR-055):
//
//	insufficient_resources    → this host lacks memory or storage.
//	                            Remedy: a bigger host, or a smaller model.
//	unsupported_configuration → no runtime here can serve this model at all.
//	                            Remedy: a different model entirely.
//	host_not_measured         → we do not currently know this machine, so no
//	                            answer about it would be honest.
//	                            Remedy: measure it.
//
// Values are machine keys. The wording a user reads is composed from them at
// the presentation boundary, in the user's language (CONST-046).
type RefusalReason string

const (
	// ReasonInsufficientResources: the host does not have enough of a measured
	// resource. The refusal always names WHICH resource — memory and storage
	// are separate axes and sending a user to fix the wrong one is worse than
	// saying nothing (D2).
	ReasonInsufficientResources RefusalReason = "insufficient_resources"
	// ReasonUnsupportedConfiguration: there is no path, not a shortage of one.
	// The host is missing a mandatory accelerator, or the model has no
	// streaming support path to fall back to. More memory does not create a
	// runtime that does not exist.
	ReasonUnsupportedConfiguration RefusalReason = "unsupported_configuration"
	// ReasonHostNotMeasured: the reading is incomplete or stale, so no
	// statement about this host's resources would be true. This is refused
	// rather than defaulted: a guess here tells the user something false about
	// their own machine (FR-033, FR-056).
	ReasonHostNotMeasured RefusalReason = "host_not_measured"
)

// Known reports whether r is one of the recorded reasons. The set is closed: a
// generic "unavailable" invented downstream is not a reason.
func (r RefusalReason) Known() bool {
	switch r {
	case ReasonInsufficientResources, ReasonUnsupportedConfiguration, ReasonHostNotMeasured:
		return true
	default:
		return false
	}
}

// Remedy is the machine key of what would resolve a refusal. It exists so the
// reasons stay distinguishable in what they ASK OF THE USER, not merely in
// their names — two reasons with the same remedy are one reason wearing two.
type Remedy string

const (
	RemedyChangeHostOrPickSmaller Remedy = "change-host-or-pick-smaller"
	RemedyDifferentApproach       Remedy = "different-approach"
	RemedyMeasureHost             Remedy = "measure-the-host"
)

// Remedy returns what r asks of the user, or the empty Remedy for an
// unrecorded reason.
func (r RefusalReason) Remedy() Remedy {
	switch r {
	case ReasonInsufficientResources:
		return RemedyChangeHostOrPickSmaller
	case ReasonUnsupportedConfiguration:
		return RemedyDifferentApproach
	case ReasonHostNotMeasured:
		return RemedyMeasureHost
	default:
		return ""
	}
}

// Resource is a measured host axis a model consumes. Memory and storage are
// separate values because they are separate axes: a model's disk footprint is
// not implied by its memory figure, and the streaming path exists precisely for
// weights whose on-disk size dwarfs memory (D2).
type Resource string

const (
	ResourceMemory  Resource = "memory"
	ResourceStorage Resource = "storage"
)

// Requirement is something a host configuration must provide before a model can
// run there at all, as distinct from a quantity it must have enough of.
type Requirement string

const (
	// RequirementAccelerator: the model mandates an acceleration device and the
	// host was MEASURED to have none. An unknown accelerator state is not this
	// — that is an unmeasured host, and it refuses earlier.
	RequirementAccelerator Requirement = "accelerator"
	// RequirementStreamingRoster: the model does not fit memory and the
	// streaming runtime does not list its family, so the fallback has nothing
	// to fall back to. Eligibility is roster membership and nothing else —
	// never architecture (D1).
	RequirementStreamingRoster Requirement = "streaming-roster"
)

// Shortfall records the axis the host does not have enough of. "Does not fit"
// is never reported without saying which axis was short.
type Shortfall struct {
	Resource Resource
	// RequiredBytes is what the model needs on this axis.
	RequiredBytes uint64
	// AvailableBytes is what the measurement says the host has for it — the
	// figure the requirement was actually compared against.
	AvailableBytes uint64
}

// Unsupported records the configuration requirement the host does not meet.
type Unsupported struct {
	Requirement Requirement
	// Detail carries the machine-readable specific — for a roster miss, the
	// family name that was looked up — so a reader can see WHAT was checked
	// rather than only that a check failed.
	Detail string
}

// MeasurementGap carries why the host reading could not justify a decision:
// incomplete measurement, or a reading too old to describe the machine now.
type MeasurementGap struct{ Err error }

// Refusal is the answer when no path serves. It is an error so callers can use
// errors.As, and it carries exactly one detail — the one belonging to its
// reason. A refusal with two details would be two answers.
type Refusal struct {
	Reason       RefusalReason
	ModelID      string
	Variant      string
	HostIdentity string

	// Exactly one of these is set, matching Reason.
	Shortfall   *Shortfall
	Unsupported *Unsupported
	Measurement *MeasurementGap
}

func (r *Refusal) Error() string {
	return fmt.Sprintf("runtime: no path serves %q on host %q: %s (remedy: %s)",
		r.model(), r.HostIdentity, r.Reason, r.Reason.Remedy())
}

// Unwrap exposes the underlying measurement error, so a caller can tell a stale
// reading from an incomplete one with errors.Is without this package having to
// re-enumerate capability's sentinels.
func (r *Refusal) Unwrap() error {
	if r.Measurement == nil {
		return nil
	}
	return r.Measurement.Err
}

func (r *Refusal) model() string {
	if r.Variant == "" {
		return r.ModelID
	}
	return r.ModelID + ":" + r.Variant
}

// TradeoffCost is what a fallback path costs relative to the preferred one.
type TradeoffCost string

// TradeoffThroughput: the path serves, but materially slower.
const TradeoffThroughput TradeoffCost = "throughput"

// TradeoffCause is why the cost is paid.
type TradeoffCause string

// CauseWeightsStreamedFromDisk: weights are read from disk during inference
// rather than held resident, which is what makes the model feasible here and
// what makes it slow.
const CauseWeightsStreamedFromDisk TradeoffCause = "weights-streamed-from-disk"

// Tradeoff is what taking a fallback path costs. It travels ON the choice
// because a path chosen for feasibility at a large throughput cost is not the
// same offer as the fast one, and a caller that cannot see the difference
// cannot tell the user.
type Tradeoff struct {
	Cost  TradeoffCost
	Cause TradeoffCause
	// ExpectedThroughputTokensPerSecond is what the catalogue records for this
	// model on this path. It is carried rather than recomputed: the figure is
	// research output, not something this package can derive.
	ExpectedThroughputTokensPerSecond float64
}

// Basis is the comparison the answer rests on — what was required against what
// was measured. It is stated on every choice so a reader can check the decision
// rather than trust it.
type Basis struct {
	MemoryRequiredBytes   uint64
	MemoryAvailableBytes  uint64
	StorageRequiredBytes  uint64
	StorageAvailableBytes uint64
}

// Choice is the path that serves a model on one measured host.
type Choice struct {
	Runtime      catalogue.RuntimeKind
	ModelID      string
	Variant      string
	HostIdentity string

	// Fallback reports whether the preferred path was unavailable. False on
	// every in-memory choice.
	Fallback bool
	// Tradeoff is what this path costs, set only when Fallback is true.
	Tradeoff *Tradeoff
	Basis    Basis
}

// StreamingMinimums are the disk-streaming runtime's own per-model floors —
// condition (c). They are a STATED POLICY carried here rather than a constant
// buried in the check, so a caller can supply a different one instead of
// discovering this one by its effects.
//
// The zero value fails CLOSED: it credits streaming with no memory reduction at
// all, so a model that does not fit in memory can never be served by streaming
// under it. A forgotten configuration therefore refuses rather than silently
// admitting a path the host may not sustain.
type StreamingMinimums struct {
	// ResidentMemoryFraction scales the entry's recorded memory figure to reach
	// the working set the streaming runtime must hold resident.
	//
	// It exists for the case where a caller has a MEASURED in-memory footprint
	// and a MEASURED resident share of it — two separate figures, only the
	// second of which is a fraction of the first. It is NOT a way to guess a
	// floor from a number that is already one.
	//
	// Values outside (0,1) are treated as "no reduction credited" — the recorded
	// figure is used whole. That covers the zero value and any nonsense value
	// with the same fail-closed behaviour, and it is the project default: see
	// defaultResidentMemoryFraction for why.
	ResidentMemoryFraction float64
}

// defaultResidentMemoryFraction credits streaming with NO memory reduction, so
// ResidentMemoryBytes returns the entry's recorded memory figure whole.
//
// This is not caution for its own sake — it is what the catalogue's figures
// MEAN. For every family on the streaming runtime's roster, research §1.8
// records a per-family "Min RAM (Colibri)" and the catalogue carries THAT as
// memory_required_bytes:
//
//	glm-5.2  17179869184 B  "16 GB min, 24 GB comfortable"
//	qwen3.6  25769803776 B  "24 GB RAM minimum"
//	olmoe     8589934592 B  "8 GB RAM"
//
// Those are the runtime's own floors — the number below which it will not run
// the model. Scaling one DOWN produces an admission bound beneath the runtime's
// stated minimum, which is the defect this constant used to cause: at the
// former 0.25 a 4 GiB host was admitted for glm-5.2 ("16 GB min") and a 6 GiB
// host for qwen3.6 ("24 GB RAM minimum") — a path offered to a user whose
// machine cannot start it (FR-027).
//
// A caller with a genuinely measured resident share may still supply one
// through ResidentMemoryFraction. The project does not invent one.
const defaultResidentMemoryFraction = 1.0

// DefaultStreamingMinimums is the project's stated streaming floor policy.
func DefaultStreamingMinimums() StreamingMinimums {
	return StreamingMinimums{ResidentMemoryFraction: defaultResidentMemoryFraction}
}

// ResidentMemoryBytes is the working set the streaming runtime still needs for
// e. See ResidentMemoryFraction for the fail-closed handling of out-of-range
// fractions.
func (m StreamingMinimums) ResidentMemoryBytes(e catalogue.Entry) uint64 {
	if m.ResidentMemoryFraction <= 0 || m.ResidentMemoryFraction >= 1 {
		return e.MemoryRequiredBytes
	}
	return uint64(float64(e.MemoryRequiredBytes) * m.ResidentMemoryFraction)
}

// StorageBytes is the on-disk footprint streaming needs free. Streaming reads
// the weights from disk continuously, so the FULL footprint must be present —
// this is the axis the streaming path trades INTO, and the reason storage is
// checked in its own right and never derived from the memory figure (D2).
func (m StreamingMinimums) StorageBytes(e catalogue.Entry) uint64 {
	return e.StorageRequiredBytes
}

// Chooser decides which path serves a model, under stated policies.
//
// Construct it with NewChooser. A zero-value Chooser is usable but strict in
// both fields, and deliberately so: capability.FreshnessPolicy's zero value
// demands a reading taken at this instant, and the zero StreamingMinimums
// credits streaming with no memory reduction. A forgotten configuration
// therefore refuses rather than quietly widening either window.
type Chooser struct {
	// Freshness is how recent the host reading must be to justify a decision.
	Freshness capability.FreshnessPolicy
	// Streaming is the streaming runtime's own per-model floors.
	Streaming StreamingMinimums
}

// NewChooser builds a Chooser with the project's stated policies: readings no
// older than capability.DefaultMaxMeasurementAge, and DefaultStreamingMinimums.
func NewChooser() Chooser {
	return Chooser{
		Freshness: capability.FreshnessPolicy{MaxAge: capability.DefaultMaxMeasurementAge},
		Streaming: DefaultStreamingMinimums(),
	}
}

// Choose returns the path that serves e on host, or a *Refusal saying why none
// does.
//
// A malformed catalogue entry is returned as a plain error rather than a
// refusal: that is a data defect with a research remedy, not a statement about
// this host.
//
// The entry's declared Runtime is read for ONE purpose: to know whether the row
// has an in-memory shape at all. It is never read as a PREFERENCE.
//
// A row's figures are denominated in the terms of the runtime that serves its
// artifact, and the two artifact shapes are different files. Research §3: a
// vendor GGUF/safetensors release and a "Colibri-specific pre-converted
// container" are distinct artifacts needing "separate catalogue rows with
// separate integrity values (FR-012) even when they represent 'the same model'
// to the end user". So on a `runtime: streaming` row:
//
//   - MemoryRequiredBytes is the streaming runtime's own stated minimum RAM
//     (research §1.8's per-family "Min RAM (Colibri)" column), NOT an
//     in-memory footprint. Comparing it against free memory in step (a) asks
//     "does this model fit in memory?" of a number that never answered that.
//   - the artifact is Colibri-specific, and the in-memory engine cannot open
//     it. There is no in-memory path to prefer.
//
// Reading the field therefore forgoes NOTHING: a model's fast path is its OWN
// in-memory row (the qwen3.6 pattern — GGUF row `runtime: in-memory`, Colibri
// row `runtime: streaming`), and Choose picks that row whenever it fits. The
// "streaming as a preference, serving from disk on a host with memory to spare"
// error remains forbidden and remains enforced, on the rows where an in-memory
// alternative actually exists: a `runtime: in-memory` row that is ALSO on the
// roster is served from memory whenever it fits, roster membership
// notwithstanding (D6).
//
// Left unread, the field produced the inverse defect: a 744B model whose weight
// set is 372 GiB was reported servable IN MEMORY on any host with 16 GiB free,
// because its recorded 16 GiB streaming floor was read as its footprint. MoE
// models in memory need "the full weight set resident in RAM" (research §3).
//
// Eligibility is still roster membership and only roster membership (D1), and
// the host's two axes are still measured, never declared.
//
// Host-responsiveness headroom is not applied here. This answers which path CAN
// serve; holding memory back so the machine stays usable while it does is the
// selection layer's policy, applied on top.
func (c Chooser) Choose(host capability.HostCapabilityProfile, e catalogue.Entry, now time.Time) (Choice, error) {
	if err := e.Validate(); err != nil {
		return Choice{}, err
	}

	// An unmeasured or stale reading is refused before anything is compared
	// against it. Every figure below would otherwise be a claim about a machine
	// we have not currently read.
	if err := c.Freshness.ValidateBasis(host, now); err != nil {
		return Choice{}, refuse(host, e, ReasonHostNotMeasured, func(r *Refusal) {
			r.Measurement = &MeasurementGap{Err: err}
		})
	}

	// A mandatory accelerator the measured host does not have rules out every
	// path at once, so it is answered before either runtime is considered. The
	// measurement gate above guarantees this "has none" is a positive finding
	// rather than an unknown state.
	if e.RequiresAccelerator && host.HasNoAccelerator() {
		return Choice{}, refuse(host, e, ReasonUnsupportedConfiguration, func(r *Refusal) {
			r.Unsupported = &Unsupported{Requirement: RequirementAccelerator}
		})
	}

	basis := basisOf(host, e)

	// (a) The preferred path, tried first for every row that HAS one.
	//
	// A streaming-only row is skipped past it rather than compared: its memory
	// figure is the streaming runtime's floor, so the comparison below would
	// read a floor as a footprint and answer a question it was never measured
	// for. See the doc comment — this forgoes no faster path, because the
	// artifact this row names is one the in-memory engine cannot open.
	inMemoryShapeExists := e.Runtime != catalogue.RuntimeStreaming
	if inMemoryShapeExists && e.MemoryRequiredBytes <= uint64(host.MemoryAvailable) {
		// Memory fits. Storage is still checked in its own right — the weights
		// have to be on disk whichever runtime reads them, and a host can have
		// abundant memory and no room for the file (D2).
		if s, short := storageShortfall(host, e.StorageRequiredBytes); short {
			return Choice{}, refuse(host, e, ReasonInsufficientResources, func(r *Refusal) {
				r.Shortfall = &s
			})
		}
		return Choice{
			Runtime:      catalogue.RuntimeInMemory,
			ModelID:      e.ModelID,
			Variant:      e.Variant,
			HostIdentity: host.HostIdentity,
			Basis:        basis,
		}, nil
	}

	// The in-memory path is out — either the model does not fit it, or this row
	// has no in-memory shape to fit. From here the only question is whether the
	// streaming path exists and this host meets it — not how much memory is
	// missing.
	//
	// Fallback stays true for both cases: it reports that the preferred path is
	// unavailable, which holds whether that is because the model is too large or
	// because no in-memory artifact of it exists. The throughput Tradeoff is
	// likewise real either way, and FR-027 requires it be labelled.

	// (b) and (c) belong to the streaming path itself and live with it, in
	// streaming.go: eligibility is roster membership and only roster membership
	// (D1), and the runtime's own memory and disk floors are checked separately
	// (D2). They are delegated rather than repeated here so there is ONE
	// implementation of them — a second copy would keep passing its own tests
	// while drifting from the one the launcher plans against.
	//
	// The verdict carries exactly one detail, matching its reason, so attaching
	// both below can never produce a refusal with two answers.
	if v := c.Streaming.Admit(host, e); !v.Admitted {
		return Choice{}, refuse(host, e, v.Reason, func(r *Refusal) {
			r.Unsupported = v.Unsupported
			r.Shortfall = v.Shortfall
		})
	}

	return Choice{
		Runtime:      catalogue.RuntimeStreaming,
		ModelID:      e.ModelID,
		Variant:      e.Variant,
		HostIdentity: host.HostIdentity,
		Fallback:     true,
		Tradeoff: &Tradeoff{
			Cost:                              TradeoffThroughput,
			Cause:                             CauseWeightsStreamedFromDisk,
			ExpectedThroughputTokensPerSecond: e.ExpectedCapability.ThroughputTokensPerSecond,
		},
		Basis: basis,
	}, nil
}

// storageShortfall compares a required footprint against free disk. It is a
// function rather than an inline comparison so both paths report the storage
// axis identically — a divergence between them would be a refusal that names
// the right resource on one path and the wrong one on the other.
func storageShortfall(host capability.HostCapabilityProfile, required uint64) (Shortfall, bool) {
	available := uint64(host.StorageAvailable)
	if required <= available {
		return Shortfall{}, false
	}
	return Shortfall{
		Resource:       ResourceStorage,
		RequiredBytes:  required,
		AvailableBytes: available,
	}, true
}

func basisOf(host capability.HostCapabilityProfile, e catalogue.Entry) Basis {
	return Basis{
		MemoryRequiredBytes:   e.MemoryRequiredBytes,
		MemoryAvailableBytes:  uint64(host.MemoryAvailable),
		StorageRequiredBytes:  e.StorageRequiredBytes,
		StorageAvailableBytes: uint64(host.StorageAvailable),
	}
}

// refuse builds a refusal with the identity of what was asked for, and lets the
// caller attach the one detail belonging to its reason.
func refuse(host capability.HostCapabilityProfile, e catalogue.Entry, reason RefusalReason, detail func(*Refusal)) *Refusal {
	r := &Refusal{
		Reason:       reason,
		ModelID:      e.ModelID,
		Variant:      e.Variant,
		HostIdentity: host.HostIdentity,
	}
	detail(r)
	return r
}
