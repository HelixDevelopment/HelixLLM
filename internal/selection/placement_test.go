package selection_test

import (
	"sync"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/capability/testdata/fixtures"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/discovery"
	"github.com/HelixDevelopment/HelixLLM/internal/selection"
	"github.com/stretchr/testify/require"
)

// --- fixtures for placement -------------------------------------------------
//
// Placement needs hosts that differ from one another along ONE axis at a time,
// which the five capability fixtures deliberately do not: they differ in
// accelerators and storage because that is what selection has to tell apart.
// So these builders take a real fixture host and restate only its identity and
// its two capacity figures. Everything else — the measured accelerator state,
// the CPU, the freshness — comes from the fixture, so a placement host is a
// measured host and not a hand-built stand-in for one.

// measuredHost is a reachable, authenticated, healthy host with the given
// identity and memory figures.
func measuredHost(identity string, total, available capability.Bytes) selection.Host {
	p := fixtures.NoAccelerator()
	p.HostIdentity = identity
	p.MemoryTotal = total
	p.MemoryAvailable = available
	return selection.Host{
		Profile:  p,
		Instance: healthyInstance(identity),
	}
}

func healthyInstance(identity string) discovery.Instance {
	return discovery.Instance{
		Endpoint:     "http://" + identity + ":11434",
		Reachability: discovery.LocalNetwork,
		Trusted:      true,
		Health: discovery.Health{
			Reachable: true,
			LastSeen:  time.Now().UTC(),
		},
	}
}

// placementEntry is one model to place, sized in the terms the host is
// measured in.
func placementEntry(id string, memory, storage capability.Bytes) catalogue.Entry {
	return catalogue.Entry{
		ModelID:              id,
		Family:               catalogue.FamilyText,
		Architecture:         catalogue.ArchitectureDense,
		MemoryRequiredBytes:  uint64(memory),
		StorageRequiredBytes: uint64(storage),
		Runtime:              catalogue.RuntimeInMemory,
		UsageTerms: catalogue.UsageTerms{
			LicenseID: "Apache-2.0",
			Permitted: []catalogue.UsagePurpose{
				catalogue.UsageCommercial,
				catalogue.UsagePersonal,
				catalogue.UsageResearch,
				catalogue.UsageEvaluation,
			},
		},
	}
}

func newFleet(t *testing.T, hosts ...selection.Host) *selection.Fleet {
	t.Helper()
	f, err := selection.NewFleet(selection.FleetOptions{
		Hosts:         hosts,
		Reserve:       selection.DefaultReserve(),
		HealthTTL:     discovery.DefaultHealthTTL,
		MaxProfileAge: time.Minute,
	})
	require.NoError(t, err)
	return f
}

func placeRequest(e catalogue.Entry) selection.PlacementRequest {
	return selection.PlacementRequest{
		Entry:         e,
		DeclaredUsage: catalogue.UsagePersonal,
		Now:           time.Now().UTC(),
	}
}

func considerationFor(t *testing.T, res selection.PlacementResult, identity string) selection.HostConsideration {
	t.Helper()
	for _, c := range res.Considered {
		if c.HostIdentity == identity {
			return c
		}
	}
	t.Fatalf("host %q absent from the considered set", identity)
	return selection.HostConsideration{}
}

// --- T061: placement, a single decision at a point in time ------------------

// TestPlacementChoosesTheOnlyHostThatFits is the base case: of several
// reachable hosts, exactly one can run the model, and that is where it goes.
//
// The two rejected hosts are rejected for DIFFERENT axes — one is short of
// memory, the other of storage — because a placement that reported the wrong
// axis would still choose correctly here, and the choice alone would not catch
// it (D2).
func TestPlacementChoosesTheOnlyHostThatFits(t *testing.T) {
	roomy := measuredHost("roomy", 128*capability.GiB, 96*capability.GiB)

	small := measuredHost("small", 64*capability.GiB, 44*capability.GiB)

	lowDisk := measuredHost("low-disk", 128*capability.GiB, 96*capability.GiB)
	lowDisk.Profile.StorageAvailable = 2 * capability.GiB

	fleet := newFleet(t, roomy, small, lowDisk)

	// 60 GiB of memory and 40 GiB of disk. `roomy` has 96 GiB free against a
	// 128 GiB total, so 19.2 GiB is reserved and 76.8 GiB is usable — it fits.
	// `small` has 44 GiB free against 64 GiB, so 34.4 GiB is usable. `low-disk`
	// has the memory and 2 GiB of disk.
	res, err := fleet.Place(placeRequest(placementEntry("m", 60*capability.GiB, 40*capability.GiB)))

	require.NoError(t, err)
	require.NotNil(t, res.Chosen, "one host fits; placement must choose it")
	require.Equal(t, "roomy", res.Chosen.HostIdentity)

	short := considerationFor(t, res, "small")
	require.False(t, short.Eligible)
	require.Equal(t, selection.ExcludedInsufficientResources, short.Excluded.Reason)
	require.Equal(t, selection.ResourceMemory, short.Excluded.Shortfall.Resource)

	disk := considerationFor(t, res, "low-disk")
	require.False(t, disk.Eligible)
	require.Equal(t, selection.ExcludedInsufficientResources, disk.Excluded.Reason)
	require.Equal(t, selection.ResourceStorage, disk.Excluded.Shortfall.Resource,
		"a host with ample memory and no disk must be reported short of DISK")
}

// TestPlacementChoosesTheHostLeftMostAbleToServe states what "best able" means
// and proves it is that rule and not a plausible neighbour.
//
// Best able = the host that retains the LARGEST SHARE OF ITS OWN MEMORY once
// the model is loaded. Two rival rules are eliminated by construction:
//
//   - "most bytes left" would choose `big-but-busy`, which ends with the most
//     absolute headroom (124 GiB) but only 48% of itself — a machine already
//     half-committed, chosen because it is physically large.
//   - "closest fit", the bin-packing answer, would choose `tight`, which ends
//     with 21% of itself free. That rule optimises fleet packing, an objective
//     the spec never states, by deliberately picking the host that will be
//     least comfortable — the opposite of "best able to run it".
//
// The share is measured against each host's own nameplate total, so a small
// idle machine can beat a large busy one. It is also the exact figure SC-003's
// responsiveness threshold is expressed against, which is why the answer is
// stated in terms a user can act on rather than in a rank nobody can check.
func TestPlacementChoosesTheHostLeftMostAbleToServe(t *testing.T) {
	roomy := measuredHost("roomy", 64*capability.GiB, 60*capability.GiB)
	bigButBusy := measuredHost("big-but-busy", 256*capability.GiB, 130*capability.GiB)
	tight := measuredHost("tight", 64*capability.GiB, 20*capability.GiB)

	fleet := newFleet(t, roomy, bigButBusy, tight)

	res, err := fleet.Place(placeRequest(placementEntry("m", 6*capability.GiB, 5*capability.GiB)))
	require.NoError(t, err)
	require.NotNil(t, res.Chosen)

	// All three genuinely fit: the choice is between eligible hosts, not
	// between a fit and a refusal.
	for _, id := range []string{"roomy", "big-but-busy", "tight"} {
		require.Truef(t, considerationFor(t, res, id).Eligible, "%s should have been eligible", id)
	}

	require.Equal(t, "roomy", res.Chosen.HostIdentity,
		"best able is the host left with the largest share of itself free")

	// And the stated reason is the figure the rule ranked on, so the answer can
	// be checked rather than taken on trust (FR-041).
	require.Equal(t, selection.RankMemoryRemainingFraction, res.Chosen.Basis)
	require.InDelta(t, 54.0/64.0, res.Chosen.Option.Headroom.MemoryRemainingFraction, 1e-9)

	// The rivals this rule beats, named so a future change to the rule cannot
	// pass this test by accident.
	busy := considerationFor(t, res, "big-but-busy")
	require.Greater(t, busy.Headroom.MemoryRemainingBytes, res.Chosen.Option.Headroom.MemoryRemainingBytes,
		"the rejected host has MORE bytes left: a bytes rule would have chosen it")
	fit := considerationFor(t, res, "tight")
	require.Less(t, fit.Headroom.MemoryRemainingBytes, res.Chosen.Option.Headroom.MemoryRemainingBytes,
		"the rejected host wastes LESS: a closest-fit rule would have chosen it")
}

// --- T059: fleet capacity accounting, state over time -----------------------

// TestPlacingOnOneHostIsReflectedInWhatTheOthersAreOffered is the defect this
// whole ledger exists for, and it is invisible to any single-decision test.
//
// Each placement below is individually correct: at the moment it is made the
// chosen host has 38.4 GiB usable and the model needs 20 GiB. Without
// accounting, all three are made against the same untouched reading and all
// three land on `a` — the fleet has then promised 60 GiB of a machine with 48,
// and no single decision was wrong.
func TestPlacingOnOneHostIsReflectedInWhatTheOthersAreOffered(t *testing.T) {
	a := measuredHost("a", 64*capability.GiB, 48*capability.GiB)
	b := measuredHost("b", 64*capability.GiB, 48*capability.GiB)
	baselineB := b.Profile

	fleet := newFleet(t, a, b)
	model := placementEntry("m", 20*capability.GiB, 30*capability.GiB)

	// First placement. Both hosts are identical, so it lands on the first by
	// identity order; which one is not the point.
	first, err := fleet.Place(placeRequest(model))
	require.NoError(t, err)
	require.Equal(t, "a", first.Chosen.HostIdentity)

	// What `a` is offered next must reflect the placement.
	afterA, ok := fleet.Available("a")
	require.True(t, ok)
	require.Equal(t, 28*capability.GiB, afterA.MemoryAvailable,
		"a's remaining memory must reflect the 20 GiB placed on it")
	require.Equal(t, 512*capability.GiB-30*capability.GiB, afterA.StorageAvailable,
		"storage is a separate axis and must be accounted separately (D2)")

	// What `b` is offered next must be untouched: capacity is per host, and a
	// ledger that debited the fleet as a pool would fail here.
	afterB, ok := fleet.Available("b")
	require.True(t, ok)
	require.Equal(t, baselineB, afterB, "placing on a must not consume b's capacity")

	// The nameplate total never moves — only what is free does. SC-003's
	// threshold is expressed against the total, so a host under commitment must
	// not quietly get a smaller reserve.
	require.Equal(t, 64*capability.GiB, afterA.MemoryTotal)

	// Second placement: `a` now has 28 GiB free against a 9.6 GiB reserve, so
	// 18.4 GiB is usable and 20 GiB no longer fits there. It must move to `b`.
	second, err := fleet.Place(placeRequest(model))
	require.NoError(t, err)
	require.Equal(t, "b", second.Chosen.HostIdentity,
		"a is now committed; the second placement must go elsewhere")
	require.Equal(t, selection.ExcludedInsufficientResources,
		considerationFor(t, second, "a").Excluded.Reason)

	// Third placement: neither host has room left. The fleet must refuse rather
	// than promise the same bytes a third time.
	third, err := fleet.Place(placeRequest(model))
	require.ErrorIs(t, err, selection.ErrNoPlacement)
	require.Nil(t, third.Chosen)
	require.NotNil(t, third.Refusal)
	require.Equal(t, selection.ExcludedInsufficientResources, third.Refusal.Reason)
	require.Len(t, third.Considered, 2, "a refusal must still account for every host")

	// The accounted commitment is exactly what was placed — no more, and no
	// less than the two placements that succeeded.
	require.Equal(t, 20*capability.GiB, fleet.Commitment("a").MemoryBytes)
	require.Equal(t, 20*capability.GiB, fleet.Commitment("b").MemoryBytes)
}

// TestReleasingAPlacementReturnsItsCapacityToTheFleet: capacity that was held
// and then given back must be offered again. A ledger that only ever debits
// leaks the fleet away one placement at a time.
func TestReleasingAPlacementReturnsItsCapacityToTheFleet(t *testing.T) {
	a := measuredHost("a", 64*capability.GiB, 48*capability.GiB)
	baseline := a.Profile

	fleet := newFleet(t, a)
	model := placementEntry("m", 20*capability.GiB, 30*capability.GiB)

	first, err := fleet.Place(placeRequest(model))
	require.NoError(t, err)

	// With the placement standing, a second copy does not fit.
	_, err = fleet.Place(placeRequest(model))
	require.ErrorIs(t, err, selection.ErrNoPlacement)

	require.True(t, fleet.Release(first.Chosen.ID))

	restored, ok := fleet.Available("a")
	require.True(t, ok)
	require.Equal(t, baseline, restored, "release must restore the measured reading exactly")
	require.Zero(t, fleet.Commitment("a").MemoryBytes)
	require.Zero(t, fleet.Commitment("a").StorageBytes)

	// And the capacity is genuinely offered again, not merely reported as free.
	again, err := fleet.Place(placeRequest(model))
	require.NoError(t, err)
	require.Equal(t, "a", again.Chosen.HostIdentity)

	// Releasing an unknown placement is reported, not silently accepted: a
	// release that credits capacity nothing ever debited over-states the fleet
	// exactly as an unaccounted placement under-states it.
	require.False(t, fleet.Release(first.Chosen.ID), "a placement may not be released twice")
	require.False(t, fleet.Release(selection.PlacementID("never-issued")))
	require.Equal(t, 20*capability.GiB, fleet.Commitment("a").MemoryBytes)
}

// --- who may be a placement target at all -----------------------------------

// TestUnreachableOrUntrustedHostIsNeverAPlacementTarget. Discovery already
// decides both questions (T060, FR-024); placement must honour that decision
// rather than re-deciding it, and must never place on a host that would not be
// exported as available.
//
// Each disqualified host is the one that WOULD have won on capacity, so a
// placement that ignored the gate would visibly choose it.
func TestUnreachableOrUntrustedHostIsNeverAPlacementTarget(t *testing.T) {
	fallback := measuredHost("fallback", 64*capability.GiB, 40*capability.GiB)

	cases := []struct {
		name     string
		mutate   func(*discovery.Instance)
		expected selection.PlacementExclusion
	}{
		{
			name: "unreachable",
			mutate: func(i *discovery.Instance) {
				i.Health = discovery.Health{
					Reachable:           false,
					LastFailure:         time.Now().UTC(),
					ConsecutiveFailures: 3,
					Reason:              discovery.ReasonUnreachable,
				}
			},
			expected: selection.ExcludedUnreachable,
		},
		{
			name: "observation gone stale",
			mutate: func(i *discovery.Instance) {
				// Reachable at the last observation, but that observation is
				// older than the freshness window: nothing says it is down, and
				// nothing confirms it is up. The two must not be conflated.
				i.Health.LastSeen = time.Now().UTC().Add(-2 * discovery.DefaultHealthTTL)
			},
			expected: selection.ExcludedUnreachable,
		},
		{
			name: "never observed",
			mutate: func(i *discovery.Instance) {
				i.Health = discovery.Health{}
			},
			expected: selection.ExcludedUnreachable,
		},
		{
			name:     "untrusted",
			mutate:   func(i *discovery.Instance) { i.Trusted = false },
			expected: selection.ExcludedUntrusted,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The disqualified host is the roomiest by a wide margin: on
			// capacity alone it wins every time.
			best := measuredHost("best-on-capacity", 256*capability.GiB, 240*capability.GiB)
			tc.mutate(&best.Instance)

			fleet := newFleet(t, best, fallback)
			res, err := fleet.Place(placeRequest(placementEntry("m", 6*capability.GiB, 5*capability.GiB)))

			require.NoError(t, err)
			require.Equal(t, "fallback", res.Chosen.HostIdentity,
				"a host that would not be exported as available is not a placement target")

			excluded := considerationFor(t, res, "best-on-capacity")
			require.False(t, excluded.Eligible)
			require.Equal(t, tc.expected, excluded.Excluded.Reason)
			require.Nil(t, excluded.Excluded.Shortfall,
				"an unavailable host was never measured against; it is not short of anything")

			// Nothing was committed against it either — a disqualified host is
			// not a host with a reservation on it.
			require.Zero(t, fleet.Commitment("best-on-capacity").MemoryBytes)
		})
	}
}

// TestUnmeasurableHostIsNeverAPlacementTarget. The figures on an incomplete
// profile are not wrong, they are unfinished — and placing against them is
// guessing (FR-056). The fixture reports 20 GiB free, which would comfortably
// hold the model, so a placement that read the figures rather than the
// completeness flag would choose it.
func TestUnmeasurableHostIsNeverAPlacementTarget(t *testing.T) {
	unmeasured := selection.Host{
		Profile:  fixtures.Unmeasurable(),
		Instance: healthyInstance("fixture-unmeasurable"),
	}
	require.NoError(t, unmeasured.Profile.Validate(),
		"the fixture is well-formed; what it is not is complete")

	t.Run("alone", func(t *testing.T) {
		fleet := newFleet(t, unmeasured)
		res, err := fleet.Place(placeRequest(placementEntry("m", 6*capability.GiB, 5*capability.GiB)))

		require.ErrorIs(t, err, selection.ErrNoPlacement)
		require.Nil(t, res.Chosen, "there is no basis for a choice, so no choice is made")
		require.NotNil(t, res.Refusal)
		require.Equal(t, selection.ExcludedHostNotMeasured, res.Refusal.Reason,
			"the refusal must say the host was not measured, never that it was too small")

		c := considerationFor(t, res, "fixture-unmeasurable")
		require.Equal(t, selection.ExcludedHostNotMeasured, c.Excluded.Reason)
		require.Nil(t, c.Excluded.Shortfall)
		require.NotEmpty(t, c.Excluded.Detail, "what was missing from the measurement must be named")
	})

	t.Run("alongside a measured host", func(t *testing.T) {
		lesser := measuredHost("measured", 32*capability.GiB, 24*capability.GiB)
		fleet := newFleet(t, unmeasured, lesser)

		res, err := fleet.Place(placeRequest(placementEntry("m", 6*capability.GiB, 5*capability.GiB)))
		require.NoError(t, err)
		require.Equal(t, "measured", res.Chosen.HostIdentity)
		require.Equal(t, selection.ExcludedHostNotMeasured,
			considerationFor(t, res, "fixture-unmeasurable").Excluded.Reason)
	})
}

// TestStaleMeasurementIsNeverAPlacementTarget: a reading older than the fleet
// allows describes conditions that have moved on, and capacity accounted from
// it would be accounting for a machine as it used to be (FR-033).
func TestStaleMeasurementIsNeverAPlacementTarget(t *testing.T) {
	stale := measuredHost("stale", 256*capability.GiB, 240*capability.GiB)
	stale.Profile = fixtures.Staled(stale.Profile, time.Hour)
	fresh := measuredHost("fresh", 64*capability.GiB, 40*capability.GiB)

	fleet := newFleet(t, stale, fresh)
	res, err := fleet.Place(placeRequest(placementEntry("m", 6*capability.GiB, 5*capability.GiB)))

	require.NoError(t, err)
	require.Equal(t, "fresh", res.Chosen.HostIdentity)

	c := considerationFor(t, res, "stale")
	require.Equal(t, selection.ExcludedMeasurementStale, c.Excluded.Reason)
	require.Nil(t, c.Excluded.Shortfall)
}

// TestPlacementDegradesToTheLocalHost (FR-043). With every networked host
// unreachable, placement offers what the local machine genuinely supports
// rather than failing — and it is a genuine offer, checked against the local
// measurement like any other.
func TestPlacementDegradesToTheLocalHost(t *testing.T) {
	local := measuredHost("local", 64*capability.GiB, 48*capability.GiB)
	local.Instance.Reachability = discovery.LocalHost
	local.Instance.Endpoint = "http://127.0.0.1:11434"

	remote := measuredHost("remote", 256*capability.GiB, 240*capability.GiB)
	remote.Instance.Reachability = discovery.Remote
	remote.Instance.Health = discovery.Health{Reachable: false, Reason: discovery.ReasonUnreachable}

	fleet := newFleet(t, local, remote)

	fits, err := fleet.Place(placeRequest(placementEntry("small", 6*capability.GiB, 5*capability.GiB)))
	require.NoError(t, err)
	require.Equal(t, "local", fits.Chosen.HostIdentity)
	require.Equal(t, discovery.LocalHost, fits.Chosen.Reachability)

	// Degrading is not the same as accepting anything: a model the local
	// machine cannot hold is still refused, with the resource named.
	tooBig, err := fleet.Place(placeRequest(placementEntry("huge", 200*capability.GiB, 5*capability.GiB)))
	require.ErrorIs(t, err, selection.ErrNoPlacement)
	require.Equal(t, selection.ExcludedInsufficientResources, tooBig.Refusal.Reason)
	require.Equal(t, selection.ResourceMemory,
		considerationFor(t, tooBig, "local").Excluded.Shortfall.Resource)
}

// TestPlacementAppliesUsageTerms: terms travel with the model, not with the
// host, so a model whose licence forbids the declared usage is refused for that
// reason on every host — never reported as a capacity problem the user could
// spend money to fix.
func TestPlacementAppliesUsageTerms(t *testing.T) {
	entry := placementEntry("research-only", 6*capability.GiB, 5*capability.GiB)
	entry.UsageTerms = catalogue.UsageTerms{
		LicenseID: "fixture-research-only",
		Permitted: []catalogue.UsagePurpose{catalogue.UsageResearch},
	}

	fleet := newFleet(t,
		measuredHost("a", 64*capability.GiB, 48*capability.GiB),
		measuredHost("b", 256*capability.GiB, 240*capability.GiB),
	)

	res, err := fleet.Place(selection.PlacementRequest{
		Entry:         entry,
		DeclaredUsage: catalogue.UsageCommercial,
		Now:           time.Now().UTC(),
	})

	require.ErrorIs(t, err, selection.ErrNoPlacement)
	require.Equal(t, selection.ExcludedByUsageTerms, res.Refusal.Reason)
	for _, id := range []string{"a", "b"} {
		c := considerationFor(t, res, id)
		require.Equal(t, selection.ExcludedByUsageTerms, c.Excluded.Reason)
		require.NotNil(t, c.Excluded.Exclusion)
		require.Nil(t, c.Excluded.Shortfall, "a licence is not a shortage of memory")
	}
	require.Zero(t, fleet.Commitment("a").MemoryBytes, "a refused placement commits nothing")
}

// --- properties that must hold across the whole surface ---------------------

// TestPlacementDoesNotWriteBackIntoMeasurement. The ledger is mutable — that is
// the point of it — but what it mutates is its own record of commitments, never
// the readings it was handed. Selection stays a pure function of (host,
// catalogue, usage); the fleet composes the host argument, it does not rewrite
// the measurement.
func TestPlacementDoesNotWriteBackIntoMeasurement(t *testing.T) {
	a := measuredHost("a", 64*capability.GiB, 48*capability.GiB)
	b := measuredHost("b", 128*capability.GiB, 96*capability.GiB)
	beforeA, beforeB := a.Profile, b.Profile

	fleet := newFleet(t, a, b)
	res, err := fleet.Place(placeRequest(placementEntry("m", 20*capability.GiB, 30*capability.GiB)))
	require.NoError(t, err)
	// b is the larger, proportionally-idler host, so it wins on the stated
	// rule; a is left uncommitted, which is what the aliasing check below
	// needs — a host whose derived reading must equal its baseline exactly.
	require.Equal(t, "b", res.Chosen.HostIdentity)

	require.Equal(t, beforeA, a.Profile, "placement mutated the caller's measured profile")
	require.Equal(t, beforeB, b.Profile, "placement mutated the caller's measured profile")

	// And the derived reading the fleet hands back is a copy, not a window onto
	// the ledger's baseline: mutating what Available returns — the scalar and
	// the slices behind it — must not reach back.
	derived, ok := fleet.Available("a")
	require.True(t, ok)
	require.Equal(t, beforeA, derived, "an uncommitted host reads back exactly as measured")
	derived.MemoryAvailable = 1
	derived.CPU.Features[0] = "mutated-by-caller"
	if len(derived.Accelerators) > 0 {
		derived.Accelerators[0].MemoryAvailable = 1
	}

	again, ok := fleet.Available("a")
	require.True(t, ok)
	require.Equal(t, beforeA, again, "a caller's mutation of a derived profile reached the ledger")
}

// TestConcurrentPlacementsNeverOvercommitAHost. The ledger is shared state, so
// two placements decided at once must not both be granted the same bytes. This
// is the over-commitment defect again, reached by a different route: under
// concurrency a read-then-write ledger fails it while the sequential test above
// still passes.
//
// The expected grant count is DERIVED by running the same attempts one at a
// time on an identical fleet, rather than hand-computed here. Concurrency must
// not change what the fleet is willing to hold — that metamorphic relation is
// the actual property, and unlike a literal it cannot be wrong about the
// reserve arithmetic.
func TestConcurrentPlacementsNeverOvercommitAHost(t *testing.T) {
	hosts := func() []selection.Host {
		return []selection.Host{
			measuredHost("a", 64*capability.GiB, 48*capability.GiB),
			measuredHost("b", 64*capability.GiB, 48*capability.GiB),
		}
	}
	model := placementEntry("m", 10*capability.GiB, 20*capability.GiB)
	const attempts = 16

	sequential := newFleet(t, hosts()...)
	expected := 0
	for range attempts {
		if _, err := sequential.Place(placeRequest(model)); err == nil {
			expected++
		}
	}
	require.Positive(t, expected, "the fleet must hold at least one copy for this test to mean anything")
	require.Less(t, expected, attempts, "the fleet must run out, or nothing is being contended for")

	concurrent := newFleet(t, hosts()...)
	var wg sync.WaitGroup
	granted := make([]bool, attempts)
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := concurrent.Place(placeRequest(model))
			granted[i] = err == nil
		}()
	}
	wg.Wait()

	count := 0
	for _, ok := range granted {
		if ok {
			count++
		}
	}
	require.Equal(t, expected, count,
		"deciding placements concurrently changed how many the fleet granted")

	for _, id := range []string{"a", "b"} {
		remaining, ok := concurrent.Available(id)
		require.True(t, ok)
		require.LessOrEqual(t, concurrent.Commitment(id).MemoryBytes, 48*capability.GiB,
			"host %s was committed beyond its measured free memory", id)
		require.Equal(t, sequential.Commitment(id), concurrent.Commitment(id),
			"host %s ended in a different state under concurrency", id)
		require.LessOrEqual(t, remaining.MemoryAvailable, remaining.MemoryTotal,
			"an over-committed host would underflow its own reading")
	}
}

// TestPlacementIsDeterministic: the same fleet state and the same model must
// yield the same host. A placement that varied between identical calls could
// not be explained to the user it is required to explain itself to (FR-041).
func TestPlacementIsDeterministic(t *testing.T) {
	build := func() *selection.Fleet {
		return newFleet(t,
			measuredHost("c", 64*capability.GiB, 48*capability.GiB),
			measuredHost("a", 64*capability.GiB, 48*capability.GiB),
			measuredHost("b", 64*capability.GiB, 48*capability.GiB),
		)
	}
	model := placementEntry("m", 6*capability.GiB, 5*capability.GiB)

	first, err := build().Place(placeRequest(model))
	require.NoError(t, err)
	second, err := build().Place(placeRequest(model))
	require.NoError(t, err)

	require.Equal(t, first.Chosen.HostIdentity, second.Chosen.HostIdentity)
	require.Equal(t, len(first.Considered), len(second.Considered))
	for i := range first.Considered {
		require.Equal(t, first.Considered[i].HostIdentity, second.Considered[i].HostIdentity,
			"the considered set must be reported in a stable order")
	}
}

// TestFleetRefusesTwoHostsSharingOneIdentity. Capacity is accounted per host
// identity. Two entries under one identity would share a ledger line, so a
// placement on either would silently debit the other — the over-commitment
// defect introduced at construction time rather than at placement time.
func TestFleetRefusesTwoHostsSharingOneIdentity(t *testing.T) {
	_, err := selection.NewFleet(selection.FleetOptions{
		Hosts: []selection.Host{
			measuredHost("same", 64*capability.GiB, 48*capability.GiB),
			measuredHost("same", 128*capability.GiB, 96*capability.GiB),
		},
	})
	require.ErrorIs(t, err, selection.ErrDuplicateHost)
}
