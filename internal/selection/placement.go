package selection

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/discovery"
)

// Placement answers two questions that look like one and are not.
//
// WHERE should this model run (FR-040, FR-041) is a single decision at a point
// in time: profile every reachable host, and choose the one best able to run
// it. WHAT IS LEFT afterwards (FR-042) is state over time: a host that is
// hosting something has less to offer the next model than the reading taken
// before it did.
//
// Only the second needs a ledger, and it is the one that cannot be tested by
// looking at any single decision. Three placements of a 20 GiB model onto a
// host with 48 GiB free are each individually correct — the host fits it, every
// time, if every decision re-reads the same untouched measurement. The fleet is
// then over-committed by 12 GiB with no decision having been wrong. That defect
// is why [Fleet] exists.
//
// # Where the accounting state lives, and why
//
// It lives HERE, in an explicitly constructed [Fleet] value the caller holds and
// passes, and deliberately not in either of the two places it could otherwise
// have gone.
//
// Not in a package-level variable. A process-global ledger is invisible in the
// signatures of the calls that consult and mutate it, cannot be reset between
// two tests, and cannot express two independent fleets at once. Every one of
// those is a property this feature actually needs: the fleet is per user and per
// configuration, and the whole surface has to be exercisable from fixtures.
//
// Not in the caller. The arithmetic of "what is left" is the arithmetic [fits]
// already performs — two separate axes, each with its own reserve held back
// (D2). A ledger kept outside this package would have to restate that, and the
// first time the two restatements drifted, placement would grant capacity that
// selection would then refuse.
//
// What the ledger mutates is its own record of commitments, never a measured
// profile. [Fleet.Place] reads (measurement, commitments) and writes only
// commitments; the profile it evaluates against is DERIVED, and derived afresh
// on every call. So the one-directional join survives intact: [Select] remains a
// pure function of (host, catalogue, declared usage), and the fleet is simply
// the thing that composes its host argument.
type Placement struct {
	// ID is the handle capacity is released by. A placement that could not be
	// named could not be given back, and a ledger that only debits leaks the
	// fleet away one placement at a time.
	ID PlacementID

	HostIdentity string
	Endpoint     string
	Reachability discovery.Reachability

	// Option is the offer as it stands on the chosen host, headroom included.
	// Its Identity is host-qualified, so which host was chosen is recorded in
	// the selection itself and not only in this wrapper (T061, D7).
	Option Option

	// Basis names the figure the choice was ranked on, so the answer to "why
	// this host" is a value the user can check rather than a rank they must
	// take on trust (FR-041).
	Basis RankBasis

	PlacedAt time.Time
}

// PlacementID identifies one standing commitment.
type PlacementID string

// RankBasis is the figure eligible hosts were ordered by.
type RankBasis string

// RankMemoryRemainingFraction is the ordering this package applies, and the
// meaning of "best able to run it": the host left with the LARGEST SHARE OF ITS
// OWN MEMORY once the model is loaded.
//
// A share rather than a byte count, because a byte count answers "which machine
// is physically biggest", not "which machine will serve this well" — an
// already-half-committed 256 GiB host outranks an idle 64 GiB one on absolute
// bytes while being the worse place to add work. Normalising to each host's own
// nameplate total lets a small idle machine beat a large busy one.
//
// And a share LEFT rather than a tight fit, because closest-fit is the answer to
// a question the spec does not ask. It packs the fleet densely by deliberately
// choosing the host that will be least comfortable afterwards, which is the
// opposite of "best able to run it" (FR-041). The share left is also the exact
// quantity SC-003's responsiveness threshold is expressed against, which is what
// makes it explicable: the ranking figure and the guarantee are the same number.
const RankMemoryRemainingFraction RankBasis = "memory-remaining-fraction"

// PlacementExclusion is why a host was not a placement target.
//
// The last three values are the [WithheldReason] keys unchanged, because they
// are the same findings reached the same way — a host excluded for its licence
// terms during placement was excluded by the same [evaluateEntry] call that
// would have excluded it during selection. The first four have no selection
// equivalent: they are reasons a host never became a candidate at all, and
// collapsing them into "insufficient resources" would send a user to buy memory
// for a machine that is simply switched off.
type PlacementExclusion string

const (
	// ExcludedUnreachable: discovery does not currently observe this instance
	// as live, so it must not be exported as available (T060). It covers a
	// failed probe, an observation gone stale, and a host never observed at
	// all — in every case nothing confirms it is up, which is the only thing
	// that matters here.
	ExcludedUnreachable PlacementExclusion = "unreachable"
	// ExcludedUntrusted: the instance did not prove it holds the pre-shared
	// secret. It is never a model source however healthy it is (FR-024).
	ExcludedUntrusted PlacementExclusion = "untrusted"
	// ExcludedHostNotMeasured: measurement did not complete, so there is no
	// basis for placing anything here. The figures such a profile carries are
	// not wrong, they are unfinished — using them is guessing (FR-056).
	ExcludedHostNotMeasured PlacementExclusion = "host_not_measured"
	// ExcludedMeasurementStale: the reading is older than the fleet allows, so
	// capacity accounted from it would describe the machine as it used to be
	// (FR-033).
	ExcludedMeasurementStale PlacementExclusion = "measurement_stale"

	// ExcludedInsufficientResources: measured, reachable, and short of memory
	// or storage AFTER what is already committed to it.
	ExcludedInsufficientResources = PlacementExclusion(ReasonInsufficientResources)
	// ExcludedUnsupportedConfiguration: nothing about this host can run the
	// model. More memory does not help.
	ExcludedUnsupportedConfiguration = PlacementExclusion(ReasonUnsupportedConfiguration)
	// ExcludedByUsageTerms: the host could serve it; the licence forbids the
	// declared usage. The remedy is never hardware.
	ExcludedByUsageTerms = PlacementExclusion(ReasonExcludedByUsageTerms)
)

// Known reports whether e is one of the recorded exclusions. The set is closed:
// a generic "could not place" invented downstream is not a reason.
func (e PlacementExclusion) Known() bool {
	switch e {
	case ExcludedUnreachable, ExcludedUntrusted, ExcludedHostNotMeasured,
		ExcludedMeasurementStale, ExcludedInsufficientResources,
		ExcludedUnsupportedConfiguration, ExcludedByUsageTerms:
		return true
	default:
		return false
	}
}

// exclusionOrder ranks exclusions by how close the host came to being chosen.
// A fleet's stated reason is drawn from it for the same purpose closestReason
// serves within one host: reporting a hardware obstacle when every host was
// really blocked by a licence sends the user to spend money that will not help.
var exclusionOrder = []PlacementExclusion{
	ExcludedByUsageTerms,
	ExcludedInsufficientResources,
	ExcludedUnsupportedConfiguration,
	ExcludedMeasurementStale,
	ExcludedHostNotMeasured,
	ExcludedUntrusted,
	ExcludedUnreachable,
}

// exclusionFor maps a selection withholding onto its placement equivalent. The
// mapping is explicit rather than a string conversion so an unrecorded reason
// cannot pass through as a well-formed one.
func exclusionFor(r WithheldReason) PlacementExclusion {
	switch r {
	case ReasonInsufficientResources:
		return ExcludedInsufficientResources
	case ReasonUnsupportedConfiguration:
		return ExcludedUnsupportedConfiguration
	case ReasonExcludedByUsageTerms:
		return ExcludedByUsageTerms
	default:
		return ExcludedUnsupportedConfiguration
	}
}

// HostExclusion states why one host was not a placement target, carrying only
// the detail belonging to that reason.
type HostExclusion struct {
	Reason PlacementExclusion

	// Exactly one of these is set, and only for the three exclusions that come
	// from evaluating the model against the host. The other four never reach an
	// evaluation, so an unreachable host is not short of anything — reporting a
	// shortfall for it would invent a measurement that was never made.
	Shortfall   *Shortfall
	Unsupported *Unsupported
	Exclusion   *Exclusion

	// Detail is the machine-readable specific — the health reason discovery
	// recorded, the part of the measurement that was missing — so a reader can
	// see what was checked rather than only that a check failed.
	Detail string
}

// HostConsideration is one host's part in a placement decision: what it would
// have left if the model went there, or why it could not.
//
// Every host appears, including the ones that were not targets, because
// FR-041's "why" is not answerable from the winner alone. A user told only that
// a model landed on the small machine cannot tell whether the large one was
// full, unreachable, or never looked at.
type HostConsideration struct {
	HostIdentity string
	Endpoint     string
	Reachability discovery.Reachability

	Eligible bool
	// Headroom is what this host would have left, set when Eligible. It is the
	// figure the ranking used.
	Headroom Headroom
	// Excluded is why not, set when not Eligible.
	Excluded *HostExclusion
}

// PlacementRefusal states that no host could take the model, and why.
type PlacementRefusal struct {
	// Reason is the exclusion closest to being satisfiable across the fleet.
	Reason PlacementExclusion
	// HostsConsidered is the size of the fleet this refusal speaks for.
	HostsConsidered int
	// HostsWeighed is how many of them the model was actually measured against.
	// Zero says nothing in the fleet was even a candidate — an unreachable or
	// unmeasured fleet — which is a different situation, with a different
	// remedy, from a fleet that weighed the model everywhere and had no room.
	HostsWeighed int
}

// PlacementRequest is everything one placement is decided from.
type PlacementRequest struct {
	// Entry is the model to place.
	Entry catalogue.Entry
	// DeclaredUsage is how the user has said the output will be used. Required,
	// for the same reason [Select] requires it: terms cannot be applied against
	// an undeclared usage.
	DeclaredUsage catalogue.UsagePurpose
	// Now is the instant the decision is made, against which health freshness
	// and measurement freshness are both judged. Zero means time.Now().UTC().
	Now time.Time
}

// PlacementResult is the answer to one placement request.
type PlacementResult struct {
	ModelID string
	Variant string

	// Chosen is where the model was placed, nil when nothing could take it.
	Chosen *Placement
	// Considered is every host, in a stable order, eligible or not.
	Considered []HostConsideration
	// Refusal is non-nil exactly when Chosen is nil.
	Refusal *PlacementRefusal
}

// Host is one candidate serving host: what discovery knows about reaching it,
// joined to what measurement knows about running things on it.
//
// The two halves are separate packages and stay separate values. Discovery does
// not measure and measurement does not reach out; a host is the pairing, and
// placement needs both — one to decide whether this machine may be used at all,
// the other to decide whether the model fits on it.
type Host struct {
	// Profile is the measured host. It is read and never written back into.
	Profile capability.HostCapabilityProfile
	// Instance is what discovery found: the endpoint, its reachability class,
	// whether it authenticated, and its liveness.
	Instance discovery.Instance
}

// Commitment is what a host currently owes to placements standing on it.
type Commitment struct {
	MemoryBytes  capability.Bytes
	StorageBytes capability.Bytes
	// Placements is how many standing placements make up the totals above.
	Placements int
	// Devices is what each accelerator owes, ordered by device identity.
	//
	// Device memory is accounted SEPARATELY from host memory and cannot be
	// folded into it: a model that mandates a card consumes that card's memory,
	// and a host with 200 GiB of RAM free has none of it to give a full 24 GiB
	// GPU. Without this dimension a second placement is weighed against a card
	// the first one already filled — the same over-commitment [Fleet] exists to
	// prevent, reached one axis over.
	//
	// Empty for a host nothing device-bound stands on.
	Devices []DeviceCommitment
}

// DeviceCommitment is what one accelerator owes to placements standing on it.
//
// The device is named by its STABLE identity, never by its position in the
// host's enumeration: the ledger outlives reboots, driver reloads and added
// cards, all of which reorder that enumeration, and a debit that followed a
// position would after any of them be charged to a different physical device
// (§11.4.111).
type DeviceCommitment struct {
	Device      capability.DeviceIdentity
	MemoryBytes capability.Bytes
	Placements  int
}

// FleetOptions configures a Fleet.
type FleetOptions struct {
	// Hosts is the fleet. Each must carry a distinct host identity: capacity is
	// accounted per identity, so two entries sharing one would share a ledger
	// line and each would silently debit the other.
	Hosts []Host
	// Reserve is what to hold back on every host so it stays responsive while
	// serving. The zero Reserve means DefaultReserve.
	Reserve Reserve
	// HealthTTL is how old a liveness observation may be and still count as
	// live. Zero means discovery.DefaultHealthTTL — the same window discovery
	// itself applies, read from there rather than restated, so the two cannot
	// disagree about which instances are available.
	HealthTTL time.Duration
	// MaxProfileAge is how old a measurement may be and still be a basis for
	// placement (FR-033). Zero leaves freshness unbounded, appropriate only
	// when the caller has just measured every host.
	MaxProfileAge time.Duration
}

// Errors reported by the fleet.
var (
	// ErrNoPlacement means no host could take the model. The result carries a
	// refusal saying why, and an accounting of every host that was considered.
	ErrNoPlacement = errors.New("selection: no host in the fleet can take this model")
	// ErrDuplicateHost means two hosts were supplied under one identity.
	ErrDuplicateHost = errors.New("selection: two hosts share one host identity")
	// ErrNoHostIdentity means a host was supplied without an identity, which
	// cannot be accounted against.
	ErrNoHostIdentity = errors.New("selection: host has no identity to account against")
	// ErrNoDeclaredPlacementUsage mirrors ErrNoDeclaredUsage for placement.
	ErrNoDeclaredPlacementUsage = errors.New("selection: no declared usage, terms cannot be applied to a placement")
)

// Fleet is the capacity ledger across hosts, over time.
//
// It is safe for concurrent use. That is not incidental: two placements decided
// at once against a read-then-write ledger are exactly the over-commitment this
// type exists to prevent, reached by a second route.
type Fleet struct {
	mu sync.Mutex

	// hosts and order are fixed at construction. order is sorted by identity so
	// every result reports its considered set the same way twice.
	hosts map[string]Host
	order []string

	committed map[string]Commitment
	// devices is the per-accelerator ledger, keyed host → device identity. It
	// is held apart from committed because a Commitment is handed OUT by value
	// and a map inside it would be shared with the caller; this way the public
	// value carries a freshly built, immutable slice.
	devices    map[string]map[capability.DeviceIdentity]deviceCommit
	placements map[PlacementID]placementRecord
	nextID     uint64

	reserve       Reserve
	healthTTL     time.Duration
	maxProfileAge time.Duration
}

// deviceCommit is one accelerator's line in the ledger.
type deviceCommit struct {
	memory     capability.Bytes
	placements int
}

// placementRecord is what has to be known to give capacity back.
//
// The device is recorded alongside the amount because releasing needs to credit
// the SAME card that was debited. Re-deriving it at release time from the
// current measurement would credit whichever card has most memory free THEN,
// which after any other placement is a different card.
type placementRecord struct {
	hostIdentity string
	memory       capability.Bytes
	storage      capability.Bytes
	device       capability.DeviceIdentity
	deviceMemory capability.Bytes
}

// NewFleet builds a ledger over hosts.
func NewFleet(opts FleetOptions) (*Fleet, error) {
	reserve := opts.Reserve
	if reserve.Zero() {
		reserve = DefaultReserve()
	}
	ttl := opts.HealthTTL
	if ttl <= 0 {
		ttl = discovery.DefaultHealthTTL
	}

	f := &Fleet{
		hosts:         make(map[string]Host, len(opts.Hosts)),
		order:         make([]string, 0, len(opts.Hosts)),
		committed:     make(map[string]Commitment, len(opts.Hosts)),
		devices:       make(map[string]map[capability.DeviceIdentity]deviceCommit, len(opts.Hosts)),
		placements:    make(map[PlacementID]placementRecord),
		reserve:       reserve,
		healthTTL:     ttl,
		maxProfileAge: opts.MaxProfileAge,
	}

	for _, h := range opts.Hosts {
		id := h.Profile.HostIdentity
		if id == "" {
			return nil, fmt.Errorf("%w: endpoint %q", ErrNoHostIdentity, h.Instance.Endpoint)
		}
		if _, dup := f.hosts[id]; dup {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateHost, id)
		}
		h.Profile = cloneProfile(h.Profile)
		f.hosts[id] = h
		f.order = append(f.order, id)
	}
	sort.Strings(f.order)
	return f, nil
}

// Hosts returns every host identity in the fleet, in reporting order.
func (f *Fleet) Hosts() []string {
	out := make([]string, len(f.order))
	copy(out, f.order)
	return out
}

// Commitment returns what is currently committed to a host. An unknown host
// carries the zero Commitment, which is the honest answer: nothing is committed
// to a machine the fleet does not have.
func (f *Fleet) Commitment(hostIdentity string) Commitment {
	f.mu.Lock()
	defer f.mu.Unlock()

	c := f.committed[hostIdentity]
	c.Devices = f.deviceCommitments(hostIdentity)
	return c
}

// deviceCommitments renders a host's per-device ledger as a stable, freshly
// allocated slice — ordered by identity so two readings of one ledger compare
// equal, and copied so a caller holding it cannot reach back into the fleet.
// Callers hold f.mu.
func (f *Fleet) deviceCommitments(hostIdentity string) []DeviceCommitment {
	owed := f.devices[hostIdentity]
	if len(owed) == 0 {
		return nil
	}
	out := make([]DeviceCommitment, 0, len(owed))
	for id, d := range owed {
		out = append(out, DeviceCommitment{
			Device:      id,
			MemoryBytes: d.memory,
			Placements:  d.placements,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Device < out[j].Device })
	return out
}

// Available is what a host is offered NEXT: its measurement with everything
// standing on it already taken off.
//
// This is FR-042 in one method. The profile it returns is derived on each call
// from (measurement, commitments) and is a copy, so a caller may hold it, pass
// it to [Select], and mutate it without any of that reaching the reading the
// fleet was built from.
//
// Only the two free-capacity figures move. The nameplate totals do not, because
// SC-003's reserve is expressed against the total: a host under commitment must
// not quietly become entitled to a smaller reserve just because it is busy.
func (f *Fleet) Available(hostIdentity string) (capability.HostCapabilityProfile, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	h, known := f.hosts[hostIdentity]
	if !known {
		return capability.HostCapabilityProfile{}, false
	}
	return f.effective(h), true
}

// effective derives the reading a host presents right now. Callers hold f.mu.
func (f *Fleet) effective(h Host) capability.HostCapabilityProfile {
	p := cloneProfile(h.Profile)
	c := f.committed[h.Profile.HostIdentity]
	p.MemoryAvailable = subtract(p.MemoryAvailable, c.MemoryBytes)
	p.StorageAvailable = subtract(p.StorageAvailable, c.StorageBytes)

	// Each accelerator is drawn down by what stands on IT, matched by identity.
	// cloneProfile has already copied the slice, so the measured reading the
	// fleet was built from is untouched. A committed identity that is no longer
	// among the measured devices simply matches nothing — the card is gone, and
	// so is the capacity it was lent.
	if owed := f.devices[h.Profile.HostIdentity]; len(owed) > 0 {
		for i := range p.Accelerators {
			if d, standing := owed[p.Accelerators[i].Identity]; standing {
				p.Accelerators[i].MemoryAvailable = subtract(p.Accelerators[i].MemoryAvailable, d.memory)
			}
		}
	}
	return p
}

// subtract floors at zero. A commitment can never exceed what was available —
// Place refuses before it would — but flooring here means an arithmetic mistake
// elsewhere surfaces as a host with nothing left rather than as a host that
// wrapped around to appearing almost empty.
func subtract(from, amount capability.Bytes) capability.Bytes {
	if amount >= from {
		return 0
	}
	return from - amount
}

// cloneProfile copies a profile including its slices, preserving nil-ness so a
// clone compares equal to its original.
func cloneProfile(p capability.HostCapabilityProfile) capability.HostCapabilityProfile {
	if p.Accelerators != nil {
		accelerators := make([]capability.Accelerator, len(p.Accelerators))
		copy(accelerators, p.Accelerators)
		p.Accelerators = accelerators
	}
	if p.CPU.Features != nil {
		features := make([]capability.CPUFeature, len(p.CPU.Features))
		copy(features, p.CPU.Features)
		p.CPU.Features = features
	}
	return p
}

// Place chooses the host best able to run the model and commits its capacity.
//
// The two halves happen under one lock. Deciding and committing separately is
// the concurrent form of the over-commitment defect: two callers both read a
// host with room, both conclude it fits, and both commit.
func (f *Fleet) Place(req PlacementRequest) (PlacementResult, error) {
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	result := PlacementResult{
		ModelID: req.Entry.ModelID,
		Variant: req.Entry.Variant,
	}
	if req.DeclaredUsage == "" {
		return result, ErrNoDeclaredPlacementUsage
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	considered := make([]HostConsideration, 0, len(f.order))
	// candidate pairs an eligible host with the offer it would serve, so the
	// ranking never has to re-derive a fit it has already computed.
	type candidate struct {
		hostIdentity string
		option       Option
	}
	var eligible []candidate
	// weighed counts the hosts that got past the availability gate and were
	// actually measured against the model, so a refusal can say whether the
	// fleet had no room or had no candidates.
	weighed := 0

	for _, id := range f.order {
		h := f.hosts[id]
		view := HostConsideration{
			HostIdentity: id,
			Endpoint:     h.Instance.Endpoint,
			Reachability: h.Instance.Reachability,
		}

		if excluded := f.disqualify(h, now); excluded != nil {
			view.Excluded = excluded
			considered = append(considered, view)
			continue
		}

		// The model is weighed against what this host has left, not against
		// what it had when it was measured (FR-042).
		weighed++
		option, withheld := evaluateEntry(req.Entry, f.effective(h), req.DeclaredUsage, f.reserve)
		if withheld != nil {
			view.Excluded = &HostExclusion{
				Reason:      exclusionFor(withheld.Reason),
				Shortfall:   withheld.Shortfall,
				Unsupported: withheld.Unsupported,
				Exclusion:   withheld.Exclusion,
				Detail:      withheld.Names(),
			}
			considered = append(considered, view)
			continue
		}

		view.Eligible = true
		view.Headroom = option.Headroom
		considered = append(considered, view)
		eligible = append(eligible, candidate{hostIdentity: id, option: *option})
	}

	result.Considered = considered

	if len(eligible) == 0 {
		result.Refusal = &PlacementRefusal{
			Reason:          statedReason(considered),
			HostsConsidered: len(considered),
			HostsWeighed:    weighed,
		}
		return result, fmt.Errorf("%w: %s", ErrNoPlacement, result.Refusal.Reason)
	}

	best := eligible[0]
	for _, c := range eligible[1:] {
		if betterTarget(c.option.Headroom, best.option.Headroom, c.hostIdentity, best.hostIdentity) {
			best = c
		}
	}

	chosen := f.commit(best.hostIdentity, req.Entry, best.option, now)
	result.Chosen = &chosen
	return result, nil
}

// disqualify reports why a host is not a candidate at all, or nil when it is.
//
// The order is from cheapest and most categorical to most specific, and each
// gate answers a different question: may this machine be used, was it measured,
// and is that measurement current. A host that fails an earlier gate is never
// weighed against the model, which is why its exclusion carries no shortfall:
// there is no measurement to be short against.
func (f *Fleet) disqualify(h Host, now time.Time) *HostExclusion {
	if !h.Instance.Trusted {
		// FR-024. An instance that did not prove it holds the secret is not a
		// model source, and running our model on it would be exactly that.
		return &HostExclusion{Reason: ExcludedUntrusted, Detail: string(h.Instance.Reachability)}
	}

	if !h.Instance.Health.Live(now, f.healthTTL) {
		// T060. Live() is discovery's own rule, called rather than restated, so
		// "available" cannot come to mean two things in two packages.
		return &HostExclusion{Reason: ExcludedUnreachable, Detail: healthDetail(h.Instance.Health)}
	}

	if err := h.Profile.ValidateForSelection(); err != nil {
		// FR-056. The figures an incomplete profile carries are not a smaller
		// host; they are an unfinished reading, and placing against them is a
		// guess dressed as a measurement.
		return &HostExclusion{Reason: ExcludedHostNotMeasured, Detail: causeKey(err)}
	}

	if f.maxProfileAge > 0 {
		if age := h.Profile.Age(now); age > f.maxProfileAge {
			return &HostExclusion{
				Reason: ExcludedMeasurementStale,
				Detail: strconv.FormatInt(int64(age.Seconds()), 10) + "s",
			}
		}
	}

	return nil
}

// healthDetail names why liveness failed, distinguishing the three ways it can:
// a recorded failure, an observation that aged out, and a host nothing has ever
// said anything about.
func healthDetail(h discovery.Health) string {
	switch {
	case h.Reason != "":
		return h.Reason
	case h.LastSeen.IsZero():
		return "never-observed"
	default:
		return "observation-stale"
	}
}

// betterTarget reports whether candidate a is a better placement target than b.
//
// The rule is RankMemoryRemainingFraction: the largest share of its own memory
// left. The remaining comparisons exist only so two hosts that are genuinely
// equal on the rule are ordered the same way twice — a placement that varied
// between identical calls could not be explained to the user FR-041 requires it
// to be explained to.
func betterTarget(a, b Headroom, aHost, bHost string) bool {
	if a.MemoryRemainingFraction != b.MemoryRemainingFraction {
		return a.MemoryRemainingFraction > b.MemoryRemainingFraction
	}
	if a.MemoryRemainingBytes != b.MemoryRemainingBytes {
		return a.MemoryRemainingBytes > b.MemoryRemainingBytes
	}
	if a.StorageRemainingBytes != b.StorageRemainingBytes {
		return a.StorageRemainingBytes > b.StorageRemainingBytes
	}
	return aHost < bHost
}

// commit records the placement and debits the host. Callers hold f.mu.
//
// This is the whole of FR-042: without the two lines that add to the ledger,
// every placement is decided against the reading the fleet started with, and the
// same free bytes are promised to every model in turn.
func (f *Fleet) commit(hostIdentity string, e catalogue.Entry, option Option, now time.Time) Placement {
	f.nextID++
	id := PlacementID(hostIdentity + "#" + strconv.FormatUint(f.nextID, 10))

	memory := capability.Bytes(e.MemoryRequiredBytes)
	storage := capability.Bytes(e.StorageRequiredBytes)

	c := f.committed[hostIdentity]
	c.MemoryBytes += memory
	c.StorageBytes += storage
	c.Placements++
	f.committed[hostIdentity] = c

	// The device this placement takes, and how much of it. The identity is the
	// one the OFFER was weighed against — read from the headroom rather than
	// re-derived — so the card that was judged able to hold the model is the
	// card that is charged for it. For an accelerator-required entry the
	// catalogue's memory figure IS the device-memory requirement, which is why
	// the same number is debited on both axes: the model occupies that much of
	// the card AND that much of the host while it serves.
	device := option.Headroom.AcceleratorDevice
	var deviceMemory capability.Bytes
	if device != "" {
		deviceMemory = memory
		owed := f.devices[hostIdentity]
		if owed == nil {
			owed = make(map[capability.DeviceIdentity]deviceCommit, 1)
			f.devices[hostIdentity] = owed
		}
		d := owed[device]
		d.memory += deviceMemory
		d.placements++
		owed[device] = d
	}

	f.placements[id] = placementRecord{
		hostIdentity: hostIdentity,
		memory:       memory,
		storage:      storage,
		device:       device,
		deviceMemory: deviceMemory,
	}

	h := f.hosts[hostIdentity]
	return Placement{
		ID:           id,
		HostIdentity: hostIdentity,
		Endpoint:     h.Instance.Endpoint,
		Reachability: h.Instance.Reachability,
		Option:       option,
		Basis:        RankMemoryRemainingFraction,
		PlacedAt:     now,
	}
}

// Release gives a placement's capacity back to its host, reporting whether
// there was such a placement to release.
//
// The report is not decoration. Crediting capacity that nothing ever debited
// over-states the fleet exactly as failing to debit under-states it, so a
// release of an unknown or already-released id changes nothing and says so.
func (f *Fleet) Release(id PlacementID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	record, known := f.placements[id]
	if !known {
		return false
	}
	delete(f.placements, id)

	c := f.committed[record.hostIdentity]
	c.MemoryBytes = subtract(c.MemoryBytes, record.memory)
	c.StorageBytes = subtract(c.StorageBytes, record.storage)
	if c.Placements > 0 {
		c.Placements--
	}
	f.committed[record.hostIdentity] = c

	// Credit the SAME card that was debited, and drop its line once nothing
	// stands on it — a device left in the ledger owing zero would report as a
	// commitment that is not one.
	if record.device != "" {
		if owed := f.devices[record.hostIdentity]; owed != nil {
			d := owed[record.device]
			d.memory = subtract(d.memory, record.deviceMemory)
			if d.placements > 0 {
				d.placements--
			}
			if d.placements == 0 && d.memory == 0 {
				delete(owed, record.device)
				if len(owed) == 0 {
					delete(f.devices, record.hostIdentity)
				}
			} else {
				owed[record.device] = d
			}
		}
	}
	return true
}

// Placements lists the standing placements, ordered by host then by id, so two
// listings of the same ledger read the same.
func (f *Fleet) Placements() []Placement {
	f.mu.Lock()
	defer f.mu.Unlock()

	ids := make([]PlacementID, 0, len(f.placements))
	for id := range f.placements {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := f.placements[ids[i]], f.placements[ids[j]]
		if a.hostIdentity != b.hostIdentity {
			return a.hostIdentity < b.hostIdentity
		}
		return ids[i] < ids[j]
	})

	out := make([]Placement, 0, len(ids))
	for _, id := range ids {
		record := f.placements[id]
		h := f.hosts[record.hostIdentity]
		out = append(out, Placement{
			ID:           id,
			HostIdentity: record.hostIdentity,
			Endpoint:     h.Instance.Endpoint,
			Reachability: h.Instance.Reachability,
			Basis:        RankMemoryRemainingFraction,
		})
	}
	return out
}

// statedReason picks the fleet's reason from the hosts' own, by how close each
// came to taking the model.
func statedReason(considered []HostConsideration) PlacementExclusion {
	for _, reason := range exclusionOrder {
		for _, c := range considered {
			if c.Excluded != nil && c.Excluded.Reason == reason {
				return reason
			}
		}
	}
	// No host was even supplied. That is not a host being unreachable; it is a
	// fleet with nothing in it, and saying so is the actionable answer.
	return ExcludedUnreachable
}
