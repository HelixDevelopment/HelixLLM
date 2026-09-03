package runtime_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/capability/testdata/fixtures"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	catfixtures "github.com/HelixDevelopment/HelixLLM/internal/catalogue/testdata"
	"github.com/HelixDevelopment/HelixLLM/internal/runtime"
	"github.com/HelixDevelopment/HelixLLM/internal/vrambroker"
	"github.com/stretchr/testify/require"
)

// This file covers the streaming path as its own unit: who is eligible for it
// (T069/T071), what its own floors demand (T073), that its two refusals stay
// different answers (T075), and what launching it costs and releases (T072).
//
// withMemoryAvailable, chooser and now are shared with choose_test.go — both
// files are package runtime_test in this directory. A second copy of them here
// could drift from the one the choice tests use, and then the two files would
// be testing two different hosts while appearing to test one.

// withStorageAvailable returns p with a different amount of free disk and
// nothing else changed. Storage is varied on its own precisely because it is
// its own axis (D2): a test that had to rebuild a whole profile to move disk
// could silently move memory with it, and then a storage assertion would be
// resting on an unstated memory change.
func withStorageAvailable(p capability.HostCapabilityProfile, free capability.Bytes) capability.HostCapabilityProfile {
	p.StorageAvailable = free
	return p
}

// --- T069 / T071. Eligibility is a roster lookup ----------------------------

// TestEligibilityOfARosteredEntryIsTheRosterAnswerAndNamesTheFamilyLookedUp.
//
// The answer carries the family name so a later reader can see WHICH name was
// resolved against the runtime's declared set. "Not eligible" with no name is
// indistinguishable from "no lookup happened".
func TestEligibilityOfARosteredEntryIsTheRosterAnswerAndNamesTheFamilyLookedUp(t *testing.T) {
	e := catfixtures.StreamingRosterMemberEntry()

	got := runtime.StreamingEligibilityOf(e)

	require.True(t, got.Eligible)
	require.Equal(t, catfixtures.StreamingFamilyDeepSeekR1, got.FamilyName,
		"the answer must name the family it looked up, not merely say yes")
}

// TestRosterAbsentMoEIsNotOfferedStreaming is the negative case this whole
// mechanism exists for, and the one the paired mutation must break.
//
// The entry is mixture-of-experts — architecturally identical to the rostered
// one — and is NOT on the streaming runtime's supported list. Qwen3-30B-A3B is
// among the MoE models that runtime names as unsupported. An implementation
// that reads Architecture instead of the roster calls this eligible, offers it
// the streaming path, and the offer then fails at load time instead of here
// (D1). Eligibility must be false, and the refusal must be a configuration
// answer that names the roster requirement — never a resource shortfall.
func TestRosterAbsentMoEIsNotOfferedStreaming(t *testing.T) {
	e := catfixtures.StreamingIneligibleMoEEntry()

	require.Equal(t, catalogue.ArchitectureMixtureOfExperts, e.Architecture,
		"fixture precondition: architecturally MoE, so an architecture predicate would admit it")

	got := runtime.StreamingEligibilityOf(e)

	require.False(t, got.Eligible,
		"a roster-absent model must not be offered streaming, however suitable its architecture looks")
	require.Equal(t, catfixtures.StreamingFamilyQwen3MoE, got.FamilyName,
		"the family name looked up must be carried, so 'not listed' is distinguishable from 'not checked'")

	// The same answer, reached through the admission the chooser actually uses.
	host := withMemoryAvailable(fixtures.NoAccelerator(), 8*capability.GiB)
	v := runtime.DefaultStreamingMinimums().Admit(host, e)

	require.False(t, v.Admitted)
	require.Equal(t, runtime.ReasonUnsupportedConfiguration, v.Reason,
		"no runtime here serves this model at all; that is not a shortage of memory")
	require.NotNil(t, v.Unsupported, "the refusal must name the requirement that was not met")
	require.Equal(t, runtime.RequirementStreamingRoster, v.Unsupported.Requirement)
	require.Equal(t, catfixtures.StreamingFamilyQwen3MoE, v.Unsupported.Detail)
	require.Nil(t, v.Shortfall,
		"reporting a shortfall would send the user to buy memory for a model with no path to buy it for")
}

// TestEligibilityIgnoresArchitectureInBothDirections.
//
// The test above can be passed by an architecture predicate that happens to
// answer correctly for the two fixtures. This one cannot: it moves Architecture
// while holding roster membership fixed, in both directions at once.
//
//   - A rostered entry relabelled dense stays eligible. An architecture
//     predicate that admits MoE would now refuse it.
//   - An unrostered entry relabelled dense stays ineligible, and an unrostered
//     entry left MoE stays ineligible.
//
// Architecture is descriptive only; no value of it may change this answer.
func TestEligibilityIgnoresArchitectureInBothDirections(t *testing.T) {
	rostered := catfixtures.StreamingRosterMemberEntry()
	unrostered := catfixtures.StreamingIneligibleMoEEntry()

	for _, arch := range []catalogue.Architecture{
		catalogue.ArchitectureDense,
		catalogue.ArchitectureMixtureOfExperts,
		catalogue.ArchitectureDiffusion,
		catalogue.ArchitectureEncoderDecoder,
	} {
		r, u := rostered, unrostered
		r.Architecture, u.Architecture = arch, arch

		require.True(t, runtime.StreamingEligibilityOf(r).Eligible,
			"a rostered entry stays eligible whatever its architecture says (%q)", arch)
		require.False(t, runtime.StreamingEligibilityOf(u).Eligible,
			"an unrostered entry stays ineligible whatever its architecture says (%q)", arch)
	}
}

// TestEntryWithNoRosterStandingAtAllIsIneligibleAndNamesNothing.
//
// An entry the catalogue never resolved against the roster carries an empty
// membership. That is ineligible — the runtime lists no family for it — and the
// answer names no family, because none was looked up.
func TestEntryWithNoRosterStandingAtAllIsIneligibleAndNamesNothing(t *testing.T) {
	e := catfixtures.CommercialSafeEntry()

	got := runtime.StreamingEligibilityOf(e)

	require.False(t, got.Eligible)
	require.Empty(t, got.FamilyName)
}

// TestEligibilityAgreesWithTheCatalogueEntryItReads guards against this package
// growing a second, divergent opinion about who is eligible.
func TestEligibilityAgreesWithTheCatalogueEntryItReads(t *testing.T) {
	for _, e := range catfixtures.Entries() {
		require.Equal(t, e.StreamingEligible(), runtime.StreamingEligibilityOf(e).Eligible,
			"entry %q: the runtime's eligibility answer must be the catalogue's roster answer", e.Identity())
	}
}

// --- T073. The runtime's own floors, on both axes, separately ---------------

// TestStreamingMemoryFloorRefusesNamingMemoryWhileDiskIsAmple.
//
// Streaming reduces the memory a model needs; it does not remove it. This host
// has 2 TiB of free disk — the footprint fits many times over — and 1 GiB of
// free memory against a 5 GiB resident working set. The refusal must name
// memory. Naming storage here would be provably false.
func TestStreamingMemoryFloorRefusesNamingMemoryWhileDiskIsAmple(t *testing.T) {
	e := catfixtures.StreamingRosterMemberEntry()
	m := runtime.DefaultStreamingMinimums()
	host := withMemoryAvailable(fixtures.DualAccelerator(), 1*capability.GiB)

	require.Greater(t, uint64(host.StorageAvailable), m.StorageBytes(e),
		"fixture precondition: disk must be ample, so a storage-named refusal would be wrong")

	v := m.Admit(host, e)

	require.False(t, v.Admitted)
	require.Equal(t, runtime.ReasonInsufficientResources, v.Reason,
		"the path exists and a bigger host would take it — that is a resource answer, not a configuration one")
	require.NotNil(t, v.Shortfall)
	require.Equal(t, runtime.ResourceMemory, v.Shortfall.Resource)
	require.Equal(t, m.ResidentMemoryBytes(e), v.Shortfall.RequiredBytes,
		"the figure compared against is the resident working set, not the full in-memory requirement")
	require.Equal(t, uint64(host.MemoryAvailable), v.Shortfall.AvailableBytes)
	require.Nil(t, v.Unsupported)
}

// TestStreamingDiskFloorRefusesNamingStorageWhileMemoryIsAmple.
//
// The mirror case, and the reason storage is checked in its own right. The
// low-storage host has 160 GiB free memory against a 5 GiB resident set —
// memory fits many times over — and 2 GiB free disk against a 372 GiB
// footprint. A memory-named refusal here sends the user to fix the one resource
// that is fine (D2).
func TestStreamingDiskFloorRefusesNamingStorageWhileMemoryIsAmple(t *testing.T) {
	e := catfixtures.StreamingRosterMemberEntry()
	m := runtime.DefaultStreamingMinimums()
	host := fixtures.LowStorage()

	require.Greater(t, uint64(host.MemoryAvailable), m.ResidentMemoryBytes(e),
		"fixture precondition: memory must be ample, so a memory-named refusal would be wrong")

	v := m.Admit(host, e)

	require.False(t, v.Admitted)
	require.Equal(t, runtime.ReasonInsufficientResources, v.Reason)
	require.NotNil(t, v.Shortfall)
	require.Equal(t, runtime.ResourceStorage, v.Shortfall.Resource)
	require.Equal(t, e.StorageRequiredBytes, v.Shortfall.RequiredBytes)
	require.Equal(t, uint64(host.StorageAvailable), v.Shortfall.AvailableBytes)
	require.Nil(t, v.Unsupported)
}

// TestTheDiskMinimumIsTheFullFootprintAndIsNeverDerivedFromMemory.
//
// Streaming reads the weights from disk throughout inference, so the whole file
// must be present — the disk axis is the one this path trades INTO. A minimum
// derived from the memory figure (say, the resident fraction applied to storage
// too) would admit a host with 93 GiB free for a 372 GiB file, and the failure
// would land at load time.
func TestTheDiskMinimumIsTheFullFootprintAndIsNeverDerivedFromMemory(t *testing.T) {
	e := catfixtures.StreamingRosterMemberEntry()
	m := runtime.DefaultStreamingMinimums()

	require.Equal(t, e.StorageRequiredBytes, m.StorageBytes(e),
		"the whole file has to be on disk, whatever share of it is resident in memory")
	require.Greater(t, m.StorageBytes(e), m.ResidentMemoryBytes(e),
		"fixture precondition: the footprint dwarfs the resident set, which is why this path exists")

	// One byte short of the footprint refuses; exactly the footprint admits.
	// This pins the comparison itself, not merely its sign on a distant value.
	//
	// The host must clear the entry's MEMORY floor so the storage axis is the
	// one under test: Admit reports memory first, so a memory-short host would
	// answer this test's storage probes with a memory shortfall. (Reconciled per
	// §11.4.120 — the floor is now the recorded figure, not a quarter of it.)
	host := withMemoryAvailable(fixtures.DualAccelerator(), 24*capability.GiB)

	short := m.Admit(withStorageAvailable(host, capability.Bytes(e.StorageRequiredBytes-1)), e)
	require.False(t, short.Admitted)
	require.NotNil(t, short.Shortfall)
	require.Equal(t, runtime.ResourceStorage, short.Shortfall.Resource)

	exact := m.Admit(withStorageAvailable(host, capability.Bytes(e.StorageRequiredBytes)), e)
	require.True(t, exact.Admitted, "a footprint that exactly fits the free disk is served, not refused")
}

// TestBothAxesAreEnforcedAndOnlyOneIsEverReported.
//
// A host short on both must still produce exactly one answer: two shortfalls in
// one refusal is two answers, and the user cannot act on both at once. Memory
// is reported first — it is the axis that decides whether the process can run
// at all — and the storage detail must not also be attached.
func TestBothAxesAreEnforcedAndOnlyOneIsEverReported(t *testing.T) {
	e := catfixtures.StreamingRosterMemberEntry()
	m := runtime.DefaultStreamingMinimums()
	host := withStorageAvailable(withMemoryAvailable(fixtures.DualAccelerator(), 1*capability.GiB), 1*capability.GiB)

	v := m.Admit(host, e)

	require.False(t, v.Admitted)
	require.NotNil(t, v.Shortfall)
	require.Equal(t, runtime.ResourceMemory, v.Shortfall.Resource)
	require.Nil(t, v.Unsupported, "exactly one detail is set, and it is the one matching the reason")
}

// TestZeroMinimumsFailClosed.
//
// A forgotten configuration must refuse rather than quietly admit a path the
// host may not sustain. The zero StreamingMinimums credits streaming with no
// memory reduction at all, so the full in-memory requirement must be free —
// which, on the streaming path, is by definition not the case.
func TestZeroMinimumsFailClosed(t *testing.T) {
	e := catfixtures.StreamingRosterMemberEntry()
	var zero runtime.StreamingMinimums
	host := withMemoryAvailable(fixtures.DualAccelerator(), 12*capability.GiB)

	require.Equal(t, e.MemoryRequiredBytes, zero.ResidentMemoryBytes(e),
		"no reduction is credited, so the resident set is the whole requirement")

	v := zero.Admit(host, e)

	require.False(t, v.Admitted, "a forgotten policy refuses; it does not widen the window")
	require.Equal(t, runtime.ReasonInsufficientResources, v.Reason)
	require.Equal(t, runtime.ResourceMemory, v.Shortfall.Resource)
}

// TestAdmittedVerdictCarriesNoRefusalDetail.
func TestAdmittedVerdictCarriesNoRefusalDetail(t *testing.T) {
	e := catfixtures.StreamingRosterMemberEntry()
	// 24 GiB clears the entry's recorded streaming floor, so this is an
	// admission and the test is about an admitted verdict's SHAPE (§11.4.120).
	host := withMemoryAvailable(fixtures.SingleAccelerator(), 24*capability.GiB)

	v := runtime.DefaultStreamingMinimums().Admit(host, e)

	require.True(t, v.Admitted)
	require.Empty(t, string(v.Reason), "an admission is not a refusal wearing an empty reason")
	require.Nil(t, v.Shortfall)
	require.Nil(t, v.Unsupported)
}

// --- T075. The two refusals stay two answers --------------------------------

// TestStreamingRefusalsStayDistinctAndAskForDifferentThings.
//
// Both refusals below come from real Admit calls on the streaming path, so this
// asserts the distinction survives the code rather than checking that two
// constants differ in a file.
//
// A roster miss means no runtime here serves this model at all: the remedy is a
// different model. A floor miss means this host is too small for a path that
// does exist: the remedy is a bigger host or a smaller model. Collapsed into
// one generic unavailability, the user is left with the only part of the answer
// they cannot act on (FR-055, D6).
func TestStreamingRefusalsStayDistinctAndAskForDifferentThings(t *testing.T) {
	m := runtime.DefaultStreamingMinimums()

	rosterMiss := m.Admit(
		withMemoryAvailable(fixtures.NoAccelerator(), 8*capability.GiB),
		catfixtures.StreamingIneligibleMoEEntry())
	floorMiss := m.Admit(
		withMemoryAvailable(fixtures.DualAccelerator(), 1*capability.GiB),
		catfixtures.StreamingRosterMemberEntry())

	require.False(t, rosterMiss.Admitted)
	require.False(t, floorMiss.Admitted)

	require.Equal(t, runtime.ReasonUnsupportedConfiguration, rosterMiss.Reason)
	require.Equal(t, runtime.ReasonInsufficientResources, floorMiss.Reason)
	require.NotEqual(t, rosterMiss.Reason, floorMiss.Reason)

	require.NotEmpty(t, rosterMiss.Reason.Remedy())
	require.NotEmpty(t, floorMiss.Reason.Remedy())
	require.NotEqual(t, rosterMiss.Reason.Remedy(), floorMiss.Reason.Remedy(),
		"two reasons that ask the user for the same thing are one reason wearing two names")

	// Their details do not overlap: each carries only what belongs to its reason.
	require.NotNil(t, rosterMiss.Unsupported)
	require.Nil(t, rosterMiss.Shortfall)
	require.NotNil(t, floorMiss.Shortfall)
	require.Nil(t, floorMiss.Unsupported)
}

// TestTheDistinctionSurvivesTheWholeChoosePath.
//
// Admit is where the two answers are formed; Choose is where a caller meets
// them. A refusal that is distinct inside the unit and generic by the time it
// leaves the package would be no use to anyone.
func TestTheDistinctionSurvivesTheWholeChoosePath(t *testing.T) {
	_, rosterErr := chooser().Choose(
		withMemoryAvailable(fixtures.NoAccelerator(), 8*capability.GiB),
		catfixtures.StreamingIneligibleMoEEntry(), now())
	_, floorErr := chooser().Choose(
		withMemoryAvailable(fixtures.DualAccelerator(), 1*capability.GiB),
		catfixtures.StreamingRosterMemberEntry(), now())

	var roster, floor *runtime.Refusal
	require.ErrorAs(t, rosterErr, &roster)
	require.ErrorAs(t, floorErr, &floor)

	require.Equal(t, runtime.ReasonUnsupportedConfiguration, roster.Reason)
	require.Equal(t, runtime.ReasonInsufficientResources, floor.Reason)
	require.NotEqual(t, roster.Reason.Remedy(), floor.Reason.Remedy())
}

// --- T072. Launching the streaming runtime ----------------------------------

// The lifecycle below is exercised against recorded doubles. They stand in for
// the streaming runtime's process and the host's admission gate, which is what
// makes the ORDERING and the RELEASE PATHS testable without a card and without
// the runtime itself — the part T070 has not yet authorised adopting.

// recorder records the lifecycle events in the order they happened, so the
// tests can assert on order rather than on counts alone. A lease released
// before the process stopped, or a process started before admission, are both
// bugs that a count-only assertion cannot see.
type recorder struct{ events []string }

func (r *recorder) record(e string) { r.events = append(r.events, e) }

type fakeLease struct {
	rec      *recorder
	releases int
}

func (l *fakeLease) Release() {
	l.releases++
	l.rec.record("lease.release")
}

type fakeAdmitter struct {
	rec   *recorder
	err   error
	lease *fakeLease
	// class and need capture what admission was actually asked for.
	class vrambroker.Class
	need  int64
	calls int
}

func (a *fakeAdmitter) Acquire(_ context.Context, class vrambroker.Class, need int64) (runtime.Lease, error) {
	a.calls++
	a.class, a.need = class, need
	a.rec.record("lease.acquire")
	if a.err != nil {
		return nil, a.err
	}
	a.lease = &fakeLease{rec: a.rec}
	return a.lease, nil
}

type fakeProcess struct {
	rec      *recorder
	startErr error
	stopErr  error
	starts   int
	stops    int
}

func (p *fakeProcess) Start(context.Context) error {
	p.starts++
	p.rec.record("process.start")
	return p.startErr
}

func (p *fakeProcess) Stop(context.Context) error {
	p.stops++
	p.rec.record("process.stop")
	return p.stopErr
}

// fakeProbe answers unhealthy until healthyAfter probes have been made. Zero
// means healthy on the first probe; a negative value never becomes healthy.
type fakeProbe struct {
	rec          *recorder
	healthyAfter int
	probes       int
}

func (h *fakeProbe) Healthy(context.Context) bool {
	h.probes++
	h.rec.record("health.probe")
	return h.healthyAfter >= 0 && h.probes > h.healthyAfter
}

// launcher builds a Launcher with a poll interval short enough that the
// budget-expiry test finishes quickly and makes many attempts before it does.
func launcher(a runtime.Admitter, h runtime.HealthProbe, budget time.Duration) runtime.Launcher {
	return runtime.Launcher{
		Admit:          a,
		Health:         h,
		HealthBudget:   budget,
		HealthInterval: time.Millisecond,
	}
}

// streamingChoice returns a real streaming Choice for the rostered fixture, made
// by the chooser rather than hand-built, so the launch tests run on a choice the
// decision code actually produces.
func streamingChoice(t *testing.T) (runtime.Choice, catalogue.Entry) {
	t.Helper()
	e := catfixtures.StreamingRosterMemberEntry()
	// 24 GiB clears the entry's recorded streaming floor. Reconciled per
	// §11.4.120: the former 12 GiB cleared only the discounted bound that
	// CRITICAL-2 removed, so every launch test below was starting from a choice
	// the corrected chooser refuses.
	c, err := chooser().Choose(withMemoryAvailable(fixtures.SingleAccelerator(), 24*capability.GiB), e, now())
	require.NoError(t, err)
	require.Equal(t, catalogue.RuntimeStreaming, c.Runtime, "precondition: this must be the streaming path")
	return c, e
}

// TestLaunchAdmitsBeforeStartingAndProbesAfter.
//
// The order is the discipline. Admission is a promise that the host has room;
// starting the process before it is granted spends the resource the gate
// exists to protect, and a lease taken after the process is up guards nothing.
func TestLaunchAdmitsBeforeStartingAndProbesAfter(t *testing.T) {
	rec := &recorder{}
	admitter := &fakeAdmitter{rec: rec}
	probe := &fakeProbe{rec: rec}
	choice, entry := streamingChoice(t)

	plan, err := runtime.NewChooser().PlanLaunch(choice, entry, vrambroker.ClassCoder)
	require.NoError(t, err)

	session, err := launcher(admitter, probe, time.Second).Launch(context.Background(), plan, &fakeProcess{rec: rec})

	require.NoError(t, err)
	require.NotNil(t, session)
	require.Equal(t, []string{"lease.acquire", "process.start", "health.probe"}, rec.events,
		"admission precedes the process, and health is asked only of something that is running")
}

// TestLaunchAsksAdmissionForTheResidentWorkingSet.
//
// The admission figure is the ONE figure the streaming policy states, and it is
// not the disk footprint — that is not memory at all. Asking the broker for the
// footprint would refuse hosts this path exists to serve; asking it for less
// than the policy states would reserve too little for a process selection has
// already admitted, so the two must be the same number from one source.
//
// Reconciled per §11.4.120. This previously also asserted
// `resident < entry.MemoryRequiredBytes` — that a discount is always applied —
// on the rationale that "asking for the whole requirement refuses hosts it
// could serve". Investigated before changing: that rationale holds only if the
// recorded figure were an in-memory footprint streaming genuinely reduces.
// Research §1.8 records it as the streaming runtime's own stated MINIMUM RAM
// ("16 GB min" for glm-5.2, "24 GB RAM minimum" for qwen3.6), so asking for it
// whole asks for exactly the runtime's minimum and refuses only hosts the
// runtime itself cannot serve. No over-refusal occurs — the boundary is pinned
// from both sides by TestTheFloorAdmitsExactlyAtTheRuntimesStatedMinimum and
// TestOneByteBelowTheFloorIsRefusedAndTheFloorItselfIsNot. The stale assertion
// is replaced below by the invariants that DO hold, plus proof the
// resident-share mechanism is still wired for a caller who has measured one.
func TestLaunchAsksAdmissionForTheResidentWorkingSet(t *testing.T) {
	choice, entry := streamingChoice(t)
	minimums := runtime.DefaultStreamingMinimums()

	plan, err := runtime.NewChooser().PlanLaunch(choice, entry, vrambroker.ClassCoder)
	require.NoError(t, err)

	require.Equal(t, minimums.ResidentMemoryBytes(entry), plan.ResidentMemoryBytes,
		"the plan carries the policy's figure; a second computation here could drift from the admitted one")
	require.NotEqual(t, entry.StorageRequiredBytes, plan.ResidentMemoryBytes,
		"the resident set is never the disk footprint — that axis is not memory (D2)")

	// The resident-share seam is still real, not vestigial: a caller with a
	// MEASURED share gets a smaller figure. The project supplies no such
	// measurement, which is why its default credits no reduction.
	measured := runtime.StreamingMinimums{ResidentMemoryFraction: 0.5}
	require.Less(t, measured.ResidentMemoryBytes(entry), minimums.ResidentMemoryBytes(entry),
		"a caller-supplied measured share must still reduce the resident figure")

	require.Equal(t, entry.StorageRequiredBytes, plan.StorageBytes,
		"the disk figure travels too, and it is the full footprint")

	rec := &recorder{}
	admitter := &fakeAdmitter{rec: rec}
	_, err = launcher(admitter, &fakeProbe{rec: rec}, time.Second).
		Launch(context.Background(), plan, &fakeProcess{rec: rec})
	require.NoError(t, err)

	require.Equal(t, int64(plan.ResidentMemoryBytes), admitter.need,
		"admission is asked for the resident set")
	require.Equal(t, vrambroker.ClassCoder, admitter.class,
		"the residency class travels from the plan, and is never invented here")
}

// TestLaunchRefusedByAdmissionNeverStartsTheProcess.
//
// A refused lease is the gate doing its job. Starting anyway would defeat it,
// and the caller must be able to tell WHY it was refused — so the broker's own
// error is passed through rather than replaced.
func TestLaunchRefusedByAdmissionNeverStartsTheProcess(t *testing.T) {
	rec := &recorder{}
	admitter := &fakeAdmitter{rec: rec, err: vrambroker.ErrBudgetExceeded}
	process := &fakeProcess{rec: rec}
	choice, entry := streamingChoice(t)
	plan, err := runtime.NewChooser().PlanLaunch(choice, entry, vrambroker.ClassCoder)
	require.NoError(t, err)

	session, err := launcher(admitter, &fakeProbe{rec: rec}, time.Second).Launch(context.Background(), plan, process)

	require.Error(t, err)
	require.ErrorIs(t, err, vrambroker.ErrBudgetExceeded,
		"the caller decides whether to queue, degrade or surface this; a replaced error takes that away")
	require.Nil(t, session)
	require.Zero(t, process.starts, "nothing is started on a refused admission")
	require.Equal(t, []string{"lease.acquire"}, rec.events)
}

// TestLaunchReleasesTheLeaseWhenTheProcessFailsToStart.
//
// The lease is a reservation against a shared budget. Holding one for a process
// that never ran removes that capacity from every other workload until the
// program exits.
func TestLaunchReleasesTheLeaseWhenTheProcessFailsToStart(t *testing.T) {
	rec := &recorder{}
	admitter := &fakeAdmitter{rec: rec}
	process := &fakeProcess{rec: rec, startErr: errors.New("streaming runtime failed to start")}
	choice, entry := streamingChoice(t)
	plan, err := runtime.NewChooser().PlanLaunch(choice, entry, vrambroker.ClassCoder)
	require.NoError(t, err)

	session, err := launcher(admitter, &fakeProbe{rec: rec}, time.Second).Launch(context.Background(), plan, process)

	require.Error(t, err)
	require.Nil(t, session)
	require.Equal(t, 1, admitter.lease.releases, "a lease taken for a process that never ran is released")
	require.Equal(t, []string{"lease.acquire", "process.start", "process.stop", "lease.release"}, rec.events,
		"a failed start is still torn down before the reservation is given back")
}

// TestLaunchStopsAndReleasesWhenHealthNeverArrives.
//
// A process that starts and never answers is the most likely real failure, and
// the one most likely to leak: it looks alive. It must be stopped and its lease
// released, and the error must say the health budget was what ran out — not
// that the process failed, which it did not.
func TestLaunchStopsAndReleasesWhenHealthNeverArrives(t *testing.T) {
	rec := &recorder{}
	admitter := &fakeAdmitter{rec: rec}
	process := &fakeProcess{rec: rec}
	probe := &fakeProbe{rec: rec, healthyAfter: -1} // never healthy
	choice, entry := streamingChoice(t)
	plan, err := runtime.NewChooser().PlanLaunch(choice, entry, vrambroker.ClassCoder)
	require.NoError(t, err)

	session, err := launcher(admitter, probe, 40*time.Millisecond).Launch(context.Background(), plan, process)

	require.Error(t, err)
	require.ErrorIs(t, err, runtime.ErrNotHealthy)
	require.Nil(t, session)
	require.Positive(t, probe.probes, "the budget is spent probing, not merely waited out")
	require.Equal(t, 1, process.stops, "a process that never answered is stopped")
	require.Equal(t, 1, admitter.lease.releases, "and its reservation is given back")
	require.Equal(t, "process.stop", rec.events[len(rec.events)-2])
	require.Equal(t, "lease.release", rec.events[len(rec.events)-1],
		"the lease is released last, after the process it was holding capacity for is gone")
}

// TestLaunchSucceedsOnceHealthAnswersAfterSeveralProbes.
//
// Loading streamed weights is slow by construction, so the first probe answering
// unhealthy is the normal case, not a failure. Only the budget expiring is.
func TestLaunchSucceedsOnceHealthAnswersAfterSeveralProbes(t *testing.T) {
	rec := &recorder{}
	admitter := &fakeAdmitter{rec: rec}
	probe := &fakeProbe{rec: rec, healthyAfter: 3}
	choice, entry := streamingChoice(t)
	plan, err := runtime.NewChooser().PlanLaunch(choice, entry, vrambroker.ClassCoder)
	require.NoError(t, err)

	session, err := launcher(admitter, probe, time.Second).Launch(context.Background(), plan, &fakeProcess{rec: rec})

	require.NoError(t, err)
	require.NotNil(t, session)
	require.Equal(t, 4, probe.probes, "it kept probing until the runtime answered")
	require.Zero(t, admitter.lease.releases, "a healthy session still holds its reservation")
}

// TestSessionCloseStopsThenReleasesAndIsIdempotent.
//
// Close is the ordinary teardown and is reached from defer, from a signal
// handler and from an error path at once. Releasing twice hands capacity back
// that was only taken once; stopping twice is a spurious failure. Both must be
// harmless.
func TestSessionCloseStopsThenReleasesAndIsIdempotent(t *testing.T) {
	rec := &recorder{}
	admitter := &fakeAdmitter{rec: rec}
	process := &fakeProcess{rec: rec}
	choice, entry := streamingChoice(t)
	plan, err := runtime.NewChooser().PlanLaunch(choice, entry, vrambroker.ClassCoder)
	require.NoError(t, err)

	session, err := launcher(admitter, &fakeProbe{rec: rec}, time.Second).Launch(context.Background(), plan, process)
	require.NoError(t, err)

	require.NoError(t, session.Close(context.Background()))
	require.NoError(t, session.Close(context.Background()), "a second close is a no-op, not an error")

	require.Equal(t, 1, process.stops)
	require.Equal(t, 1, admitter.lease.releases)
	require.Equal(t, []string{"lease.acquire", "process.start", "health.probe", "process.stop", "lease.release"},
		rec.events)
}

// TestCloseReleasesTheLeaseEvenWhenStoppingTheProcessFails.
//
// A stop that fails is reported — the caller has an orphan process to deal with
// — but the reservation is still given back. Withholding it would punish every
// other workload for one process's bad exit.
func TestCloseReleasesTheLeaseEvenWhenStoppingTheProcessFails(t *testing.T) {
	rec := &recorder{}
	admitter := &fakeAdmitter{rec: rec}
	process := &fakeProcess{rec: rec, stopErr: errors.New("stop failed")}
	choice, entry := streamingChoice(t)
	plan, err := runtime.NewChooser().PlanLaunch(choice, entry, vrambroker.ClassCoder)
	require.NoError(t, err)

	session, err := launcher(admitter, &fakeProbe{rec: rec}, time.Second).Launch(context.Background(), plan, process)
	require.NoError(t, err)

	require.Error(t, session.Close(context.Background()), "a failed stop is reported, never swallowed")
	require.Equal(t, 1, admitter.lease.releases, "and the reservation is still returned")
}

// TestPlanLaunchRefusesAChoiceThatIsNotTheStreamingPath.
//
// This launcher runs the streaming runtime. Handed an in-memory choice it must
// refuse rather than start the streaming runtime for a model the decision said
// to serve from memory — which would be the D6 error committed at launch time
// instead of at choice time.
func TestPlanLaunchRefusesAChoiceThatIsNotTheStreamingPath(t *testing.T) {
	entry := catfixtures.CommercialSafeEntry()
	choice, err := chooser().Choose(fixtures.DualAccelerator(), entry, now())
	require.NoError(t, err)
	require.Equal(t, catalogue.RuntimeInMemory, choice.Runtime, "precondition")

	_, err = runtime.NewChooser().PlanLaunch(choice, entry, vrambroker.ClassCoder)

	require.Error(t, err)
	require.ErrorIs(t, err, runtime.ErrNotStreamingChoice)
}

// TestPlanLaunchRefusesAChoiceForADifferentModel.
//
// The plan's figures come from the entry; the path came from the choice. If
// they are not the same model the plan is admitting one model's memory for
// another model's process, and no later check would catch it.
func TestPlanLaunchRefusesAChoiceForADifferentModel(t *testing.T) {
	choice, _ := streamingChoice(t)
	other := catfixtures.CommercialSafeEntry()

	_, err := runtime.NewChooser().PlanLaunch(choice, other, vrambroker.ClassCoder)

	require.Error(t, err)
	require.ErrorIs(t, err, runtime.ErrChoiceEntryMismatch)
}

// TestLaunchCarriesTheTradeoffIntoTheSession.
//
// The session is what a caller holds while the model serves. A streaming
// session that does not carry what it cost cannot tell anyone downstream that
// this model is slow by construction rather than slow by fault.
func TestLaunchCarriesTheTradeoffIntoTheSession(t *testing.T) {
	rec := &recorder{}
	choice, entry := streamingChoice(t)
	plan, err := runtime.NewChooser().PlanLaunch(choice, entry, vrambroker.ClassCoder)
	require.NoError(t, err)

	session, err := launcher(&fakeAdmitter{rec: rec}, &fakeProbe{rec: rec}, time.Second).
		Launch(context.Background(), plan, &fakeProcess{rec: rec})
	require.NoError(t, err)

	require.NotNil(t, plan.Tradeoff)
	require.Equal(t, runtime.TradeoffThroughput, plan.Tradeoff.Cost)
	require.Equal(t, runtime.CauseWeightsStreamedFromDisk, plan.Tradeoff.Cause)
	require.Equal(t, entry.ExpectedCapability.ThroughputTokensPerSecond,
		plan.Tradeoff.ExpectedThroughputTokensPerSecond)
	require.Equal(t, plan.Identity, session.Identity)
}
