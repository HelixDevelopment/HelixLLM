package runtime_test

import (
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/capability/testdata/fixtures"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	catfixtures "github.com/HelixDevelopment/HelixLLM/internal/catalogue/testdata"
	"github.com/HelixDevelopment/HelixLLM/internal/runtime"
	"github.com/stretchr/testify/require"
)

// withMemoryAvailable returns p with a different amount of free memory and
// nothing else changed.
//
// The fixture hosts are all generously provisioned, so the memory-short cases
// are produced by lowering ONE figure on a real fixture rather than by
// hand-building a second profile. A difference between two tests is then
// visible in the call itself and cannot drift into an unrelated field.
func withMemoryAvailable(p capability.HostCapabilityProfile, free capability.Bytes) capability.HostCapabilityProfile {
	p.MemoryAvailable = free
	return p
}

// chooser is the subject under test, constructed the way a caller must
// construct it. NewChooser is used rather than a zero Chooser deliberately: the
// zero capability.FreshnessPolicy demands a reading taken at this instant, so a
// zero-value chooser would refuse every fixture on freshness and every test
// below would pass or fail for the wrong reason.
func chooser() runtime.Chooser { return runtime.NewChooser() }

func now() time.Time { return time.Now().UTC() }

// refusalFrom asserts that choosing refused, and returns the refusal.
func refusalFrom(t *testing.T, c runtime.Choice, err error) *runtime.Refusal {
	t.Helper()
	require.Error(t, err, "expected a refusal, got choice %+v", c)
	var r *runtime.Refusal
	require.ErrorAs(t, err, &r, "expected a *runtime.Refusal, got %T: %v", err, err)
	return r
}

// --- 1. in-memory wins whenever it can, roster membership notwithstanding ----

// inMemoryRosteredEntry is a roster-eligible entry whose artifact IS served in
// memory — the shape D6 is about.
//
// It is derived here rather than added to the fixture set because the fixtures
// carry no such row, and the two tests below cannot make their point without
// one: their subject is a model that has BOTH shapes, and the fixture roster
// member declares only the streaming one.
//
// The shape is real, not invented for the test. The shipped catalogue's
// qwen3.6-35b-a3b row is exactly this: `runtime: in-memory`, `streaming_family:
// qwen3.6` resolving to eligible, its own GGUF source and integrity value —
// with the Colibri container of the same model kept as a SEPARATE row
// (text.yaml, research §3 "two distinct artifact shapes per model … separate
// catalogue rows with separate integrity values (FR-012)").
func inMemoryRosteredEntry() catalogue.Entry {
	e := catfixtures.StreamingRosterMemberEntry()
	e.Runtime = catalogue.RuntimeInMemory
	return e
}

// TestFitsInMemoryChoosesInMemoryEvenWhenOnTheStreamingRoster.
//
// Streaming trades throughput for feasibility by orders of magnitude, so it is
// a fallback and never a preference (D6). This entry is on the roster AND has
// an in-memory shape that fits this host's memory many times over; choosing
// streaming for it would be a real, user-visible slowdown chosen for no reason.
// Roster membership is what makes the fallback POSSIBLE, never what makes it
// desirable.
//
// Reconciled per §11.4.120: this previously used the fixture roster member,
// whose only artifact shape is the streaming one. Reading its recorded memory
// figure — the streaming runtime's own minimum RAM — as an in-memory footprint
// is the CRITICAL-2 defect, so a streaming-only row can no longer answer
// in-memory. D6 itself is unchanged and is asserted here on the row that
// actually has the choice D6 is about.
func TestFitsInMemoryChoosesInMemoryEvenWhenOnTheStreamingRoster(t *testing.T) {
	host := fixtures.DualAccelerator() // 96 GiB free memory, 2 TiB free disk
	entry := inMemoryRosteredEntry()

	require.True(t, entry.StreamingEligible(),
		"fixture precondition: this entry must be on the streaming roster, or the test proves nothing")
	require.Equal(t, catalogue.RuntimeInMemory, entry.Runtime,
		"fixture precondition: the entry must HAVE an in-memory shape for D6 to be a choice at all")
	require.LessOrEqual(t, entry.MemoryRequiredBytes, uint64(host.MemoryAvailable),
		"fixture precondition: it must fit, or in-memory is not on the table")

	choice, err := chooser().Choose(host, entry, now())

	require.NoError(t, err)
	require.Equal(t, catalogue.RuntimeInMemory, choice.Runtime)
	require.False(t, choice.Fallback, "the in-memory path is the preference, not a fallback")
	require.Nil(t, choice.Tradeoff, "nothing is traded when the preferred path is taken")
}

// TestDeclaredEntryRuntimeNeverMakesStreamingAPreference.
//
// Reconciled per §11.4.120 from TestDeclaredEntryRuntimeDoesNotOverrideTheMeasuredChoice,
// which asserted that a `runtime: streaming` row on a 96 GiB host answers
// in-memory. That is the CRITICAL-2 defect: the row's recorded figure is the
// streaming runtime's stated minimum RAM, not a footprint, and its artifact is
// a Colibri-specific container the in-memory engine cannot open (research §1.8,
// §3). Its 372 GiB weight set was never going to be resident in 20 GiB.
//
// The concern the old test protected is real and is asserted here instead, in
// the form that survives: the declared runtime is read ONLY to know whether an
// in-memory shape exists, and NEVER as a preference for streaming. Both halves
// are pinned in one test, because the two together are the invariant — either
// alone is satisfiable by a wrong implementation.
func TestDeclaredEntryRuntimeNeverMakesStreamingAPreference(t *testing.T) {
	host := fixtures.DualAccelerator() // 96 GiB free memory, 2 TiB free disk

	// (i) A row that DECLARES streaming and has no in-memory shape is served by
	//     streaming even on a host with memory to spare — there is no faster
	//     path for that artifact to be forgone.
	streamingOnly := catfixtures.StreamingRosterMemberEntry()
	require.Equal(t, catalogue.RuntimeStreaming, streamingOnly.Runtime,
		"fixture precondition: the entry must DECLARE streaming for this half to mean anything")

	choice, err := chooser().Choose(host, streamingOnly, now())
	require.NoError(t, err)
	require.Equal(t, catalogue.RuntimeStreaming, choice.Runtime,
		"a Colibri-container row has one shape; answering in-memory offers an artifact that engine cannot open")

	// (ii) The SAME model's in-memory shape, on the SAME host, is served from
	//      memory. So reading the declared runtime did not turn streaming into a
	//      preference — it selected between two artifacts, which is what the
	//      field records.
	inMemory := inMemoryRosteredEntry()
	choice, err = chooser().Choose(host, inMemory, now())
	require.NoError(t, err)
	require.Equal(t, catalogue.RuntimeInMemory, choice.Runtime,
		"an in-memory shape that fits is served from memory; streaming is never preferred over it (D6)")
	require.Nil(t, choice.Tradeoff, "nothing is traded when the preferred path is taken")
}

// --- 2. the D1 trap: architecture is not eligibility -------------------------

// TestUnrosteredMoEThatDoesNotFitIsUnsupportedConfigurationNotStreaming.
//
// This is the test the whole roster mechanism exists for. The entry is
// mixture-of-experts, exactly like the rostered one, with the same memory
// requirement — and it is NOT on the streaming runtime's supported list. An
// implementation that reads Architecture instead of the roster offers it the
// streaming path, and that offer fails at load time instead of here (D1).
//
// The reason is unsupported_configuration, not insufficient_resources: once the
// in-memory path is out, the question is whether any other path exists, and the
// answer is that no runtime lists this model's family. Reporting a resource
// shortfall would send the user to buy memory for a model that has no fallback
// path to buy it for.
func TestUnrosteredMoEThatDoesNotFitIsUnsupportedConfigurationNotStreaming(t *testing.T) {
	host := withMemoryAvailable(fixtures.NoAccelerator(), 8*capability.GiB)
	entry := catfixtures.StreamingIneligibleMoEEntry()

	require.Equal(t, catalogue.ArchitectureMixtureOfExperts, entry.Architecture,
		"fixture precondition: architecturally MoE, so an architecture predicate would admit it")
	require.False(t, entry.StreamingEligible(),
		"fixture precondition: not on the roster, so a roster lookup must reject it")

	choice, err := chooser().Choose(host, entry, now())

	r := refusalFrom(t, choice, err)
	require.Equal(t, runtime.ReasonUnsupportedConfiguration, r.Reason)
	require.NotEqual(t, runtime.ReasonInsufficientResources, r.Reason,
		"a model with no support path anywhere is not a memory problem")
	require.NotNil(t, r.Unsupported, "the refusal must name the requirement that was not met")
	require.Equal(t, runtime.RequirementStreamingRoster, r.Unsupported.Requirement)
	require.Equal(t, catfixtures.StreamingFamilyQwen3MoE, r.Unsupported.Detail,
		"the refusal must say which family name was looked up, so a reader sees what was checked")
	require.Nil(t, r.Shortfall, "exactly one detail is set, and it is the one matching the reason")
	require.Equal(t, catalogue.RuntimeKind(""), choice.Runtime, "no path was chosen")
}

// TestRosterAloneSeparatesTwoIdenticalArchitectures.
//
// The paired form of the test above: the same host, two entries that agree on
// architecture and on memory figure, differing only in roster membership. If
// those two produce the same outcome, eligibility is not being read from the
// roster — whatever the code says it reads.
//
// Reconciled per §11.4.120. The host is now BELOW both figures, and the
// separation is read from the two refusal REASONS rather than from
// streaming-vs-refusal. The previous 12 GiB host produced streaming for the
// rostered row only because the floor was being computed as a quarter of the
// recorded figure — a bound beneath the streaming runtime's own stated minimum,
// which is the CRITICAL-2 defect. With the floor corrected to the recorded
// figure, no single host can both clear the rostered row's floor and fail the
// unrostered row's footprint while the two numbers are equal: the figures now
// MEAN different things (a runtime floor vs an in-memory footprint) even where
// they coincide numerically.
//
// Reading the reasons is the sharper assertion anyway. It pins the property
// FR-055 / D6 actually care about — the two rows ASK THE USER FOR DIFFERENT
// THINGS. The rostered row is short of a resource on a path that exists
// (remedy: a bigger host); the unrostered row has no path at all (remedy: a
// different model). An implementation that ignored the roster would give both
// the same answer.
func TestRosterAloneSeparatesTwoIdenticalArchitectures(t *testing.T) {
	host := withMemoryAvailable(fixtures.SingleAccelerator(), 19*capability.GiB)
	rostered := catfixtures.StreamingRosterMemberEntry()
	unrostered := catfixtures.StreamingIneligibleMoEEntry()

	require.Equal(t, rostered.Architecture, unrostered.Architecture,
		"fixture precondition: identical architecture")
	require.Equal(t, rostered.MemoryRequiredBytes, unrostered.MemoryRequiredBytes,
		"fixture precondition: identical memory figure")
	require.Greater(t, rostered.MemoryRequiredBytes, uint64(host.MemoryAvailable),
		"fixture precondition: the host must be below both figures, so neither row is admitted")

	rosteredChoice, rosteredErr := chooser().Choose(host, rostered, now())
	rr := refusalFrom(t, rosteredChoice, rosteredErr)
	require.Equal(t, runtime.ReasonInsufficientResources, rr.Reason,
		"the rostered row has a path; this host is too small for it")
	require.Equal(t, runtime.RemedyChangeHostOrPickSmaller, rr.Reason.Remedy())

	unrosteredChoice, unrosteredErr := chooser().Choose(host, unrostered, now())
	ur := refusalFrom(t, unrosteredChoice, unrosteredErr)
	require.Equal(t, runtime.ReasonUnsupportedConfiguration, ur.Reason,
		"the unrostered row has no path anywhere; more memory would not create one")
	require.Equal(t, runtime.RemedyDifferentApproach, ur.Reason.Remedy())

	require.NotEqual(t, rr.Reason, ur.Reason,
		"roster membership alone must separate two rows identical in every other respect")
	require.NotEqual(t, rr.Reason.Remedy(), ur.Reason.Remedy(),
		"and it must separate them in what they ASK OF THE USER, not merely in a label")
}

// --- 3. the fallback is taken, and says what it costs ------------------------

// TestRosteredEntryThatDoesNotFitChoosesStreamingAndRecordsTheTradeoff.
//
// All conditions for the streaming path hold: the in-memory path is out, the
// entry is on the roster, and the host meets the streaming runtime's own floors
// on both axes. The choice must also carry what was traded — a path chosen for
// feasibility at a large throughput cost is not the same offer as the fast one,
// and a caller that cannot see the difference cannot tell the user.
//
// Reconciled per §11.4.120: the host was 12 GiB, which cleared the old bound
// only because it was a quarter of the recorded figure. The corrected floor IS
// the recorded figure, so the host now has to meet it. The precondition changes
// with it — for this row the in-memory path is out because the row has no
// in-memory shape, not because a footprint did not fit.
func TestRosteredEntryThatDoesNotFitChoosesStreamingAndRecordsTheTradeoff(t *testing.T) {
	host := withMemoryAvailable(fixtures.SingleAccelerator(), 24*capability.GiB)
	entry := catfixtures.StreamingRosterMemberEntry()

	require.Equal(t, catalogue.RuntimeStreaming, entry.Runtime,
		"fixture precondition: this row's only artifact shape is the streaming one, so in-memory is not on the table")
	require.LessOrEqual(t, entry.MemoryRequiredBytes, uint64(host.MemoryAvailable),
		"fixture precondition: the host must meet the streaming runtime's stated floor, or the path is refused")
	require.LessOrEqual(t, entry.StorageRequiredBytes, uint64(host.StorageAvailable),
		"fixture precondition: the streaming footprint must fit the free disk")

	choice, err := chooser().Choose(host, entry, now())

	require.NoError(t, err)
	require.Equal(t, catalogue.RuntimeStreaming, choice.Runtime)
	require.True(t, choice.Fallback, "streaming is reached only as a fallback")

	require.NotNil(t, choice.Tradeoff, "a fallback that does not state its cost hides the cost")
	require.Equal(t, runtime.TradeoffThroughput, choice.Tradeoff.Cost)
	require.Equal(t, runtime.CauseWeightsStreamedFromDisk, choice.Tradeoff.Cause)
	require.Equal(t, entry.ExpectedCapability.ThroughputTokensPerSecond,
		choice.Tradeoff.ExpectedThroughputTokensPerSecond,
		"the recorded throughput is the one the entry states for this path")
}

// TestStreamingIsRefusedWhenItsOwnMemoryFloorIsNotMet.
//
// Condition (c). Streaming reduces the memory a model needs; it does not remove
// it. A host that cannot hold even the resident working set gets a resource
// refusal naming memory — the remedy really is a bigger host here, which is
// what makes this different from the roster miss above.
func TestStreamingIsRefusedWhenItsOwnMemoryFloorIsNotMet(t *testing.T) {
	host := withMemoryAvailable(fixtures.SingleAccelerator(), 1*capability.GiB)
	entry := catfixtures.StreamingRosterMemberEntry()

	choice, err := chooser().Choose(host, entry, now())

	r := refusalFrom(t, choice, err)
	require.Equal(t, runtime.ReasonInsufficientResources, r.Reason)
	require.NotNil(t, r.Shortfall)
	require.Equal(t, runtime.ResourceMemory, r.Shortfall.Resource)
	require.Nil(t, r.Unsupported)
}

// --- 4. storage is its own axis ---------------------------------------------

// TestStorageShortRefusalNamesStorageAndNeverMemory.
//
// The low-storage host has 160 GiB free memory against a 20 GiB requirement —
// memory fits eight times over — and 2 GiB free disk against a 372 GiB
// footprint. A refusal that names memory here is not merely vague, it is
// provably false, and it sends the user to fix the one resource that is fine
// (D2).
func TestStorageShortRefusalNamesStorageAndNeverMemory(t *testing.T) {
	host := fixtures.LowStorage()
	entry := catfixtures.StreamingRosterMemberEntry()

	require.Greater(t, uint64(host.MemoryAvailable), entry.MemoryRequiredBytes,
		"fixture precondition: memory must fit, so a memory-named refusal would be provably wrong")
	require.Less(t, uint64(host.StorageAvailable), entry.StorageRequiredBytes,
		"fixture precondition: storage must not fit")

	choice, err := chooser().Choose(host, entry, now())

	r := refusalFrom(t, choice, err)
	require.Equal(t, runtime.ReasonInsufficientResources, r.Reason)
	require.NotNil(t, r.Shortfall)
	require.Equal(t, runtime.ResourceStorage, r.Shortfall.Resource,
		"memory fits here; naming memory would be a false statement about the user's machine")
	require.Equal(t, entry.StorageRequiredBytes, r.Shortfall.RequiredBytes)
	require.Equal(t, uint64(host.StorageAvailable), r.Shortfall.AvailableBytes)
	require.Equal(t, catalogue.RuntimeKind(""), choice.Runtime)
}

// --- 5. an unmeasured host is never guessed at -------------------------------

// TestUnmeasurableHostRefusesBecauseItCouldNotBeMeasured.
//
// The unmeasurable fixture carries figures obtained before its measurement
// failed. The point is that they are not enough: accelerator state is unknown,
// which is not the same value as "no accelerator". The answer must say the host
// could not be measured — not pick a default path, and not report a shortfall
// derived from half a reading (FR-056).
func TestUnmeasurableHostRefusesBecauseItCouldNotBeMeasured(t *testing.T) {
	host := fixtures.Unmeasurable()
	entry := catfixtures.CommercialSafeEntry()

	require.False(t, host.AcceleratorStateKnown(),
		"fixture precondition: accelerator state must be unknown, not measured-as-none")

	choice, err := chooser().Choose(host, entry, now())

	r := refusalFrom(t, choice, err)
	require.Equal(t, runtime.ReasonHostNotMeasured, r.Reason)
	require.NotNil(t, r.Measurement, "the refusal must carry what was wrong with the reading")
	require.ErrorIs(t, err, capability.ErrNotMeasured)
	require.Nil(t, r.Shortfall, "no shortfall can be computed from a reading that did not complete")
	require.Nil(t, r.Unsupported)
	require.Equal(t, catalogue.RuntimeKind(""), choice.Runtime, "no default path is taken")
}

// TestStaleMeasurementRefusesRatherThanDecidingOnAnOldReading.
//
// A reading from an hour ago describes a machine that has since changed. It is
// the same class of answer as no reading at all — we do not currently know this
// host — so it carries the same reason and the same remedy (FR-033).
func TestStaleMeasurementRefusesRatherThanDecidingOnAnOldReading(t *testing.T) {
	host := fixtures.Staled(fixtures.DualAccelerator(), time.Hour)
	entry := catfixtures.CommercialSafeEntry()

	choice, err := chooser().Choose(host, entry, now())

	r := refusalFrom(t, choice, err)
	require.Equal(t, runtime.ReasonHostNotMeasured, r.Reason)
	require.ErrorIs(t, err, capability.ErrStaleMeasurement)
	require.Equal(t, catalogue.RuntimeKind(""), choice.Runtime)
}

// TestMandatoryAcceleratorOnAHostMeasuredToHaveNoneIsUnsupported.
//
// The host was successfully measured and has no acceleration device. That is a
// positive finding, not a gap, and no amount of memory changes it — so it is a
// configuration answer, not a resource one.
func TestMandatoryAcceleratorOnAHostMeasuredToHaveNoneIsUnsupported(t *testing.T) {
	host := fixtures.NoAccelerator()
	entry := catfixtures.RevenueCappedEntry() // requires an accelerator

	require.True(t, entry.RequiresAccelerator, "fixture precondition")
	require.True(t, host.HasNoAccelerator(), "fixture precondition: measured, and has none")

	choice, err := chooser().Choose(host, entry, now())

	r := refusalFrom(t, choice, err)
	require.Equal(t, runtime.ReasonUnsupportedConfiguration, r.Reason)
	require.NotNil(t, r.Unsupported)
	require.Equal(t, runtime.RequirementAccelerator, r.Unsupported.Requirement)
	require.Equal(t, catalogue.RuntimeKind(""), choice.Runtime)
}

// --- 6. the two refusal reasons must stay different --------------------------

// TestTheTwoRefusalReasonsAreDistinctAndAskForDifferentThings.
//
// A user told "cannot run" learns nothing. insufficient_resources sends them to
// a bigger host or a smaller model; unsupported_configuration sends them to a
// different model entirely. Both refusals below are produced by real calls, so
// this asserts the distinction survives the code path and is not merely two
// different constants sitting in a file.
func TestTheTwoRefusalReasonsAreDistinctAndAskForDifferentThings(t *testing.T) {
	resourceRefusal := refusalFrom(chooserRefusal(t,
		fixtures.LowStorage(), catfixtures.StreamingRosterMemberEntry()))
	configRefusal := refusalFrom(chooserRefusal(t,
		withMemoryAvailable(fixtures.NoAccelerator(), 8*capability.GiB),
		catfixtures.StreamingIneligibleMoEEntry()))

	require.Equal(t, runtime.ReasonInsufficientResources, resourceRefusal.Reason)
	require.Equal(t, runtime.ReasonUnsupportedConfiguration, configRefusal.Reason)

	require.NotEqual(t, resourceRefusal.Reason, configRefusal.Reason,
		"one generic unavailability destroys the only actionable part of the answer")

	require.NotEmpty(t, resourceRefusal.Reason.Remedy())
	require.NotEmpty(t, configRefusal.Reason.Remedy())
	require.NotEqual(t, resourceRefusal.Reason.Remedy(), configRefusal.Reason.Remedy(),
		"two reasons that ask for the same thing are one reason wearing two names")

	// The third reason is distinct from both: it asks for neither hardware nor a
	// different model, but for a current reading of the machine.
	require.NotEqual(t, runtime.ReasonHostNotMeasured.Remedy(), resourceRefusal.Reason.Remedy())
	require.NotEqual(t, runtime.ReasonHostNotMeasured.Remedy(), configRefusal.Reason.Remedy())

	// Their details do not overlap either: each refusal carries only the detail
	// belonging to its own reason.
	require.NotNil(t, resourceRefusal.Shortfall)
	require.Nil(t, resourceRefusal.Unsupported)
	require.NotNil(t, configRefusal.Unsupported)
	require.Nil(t, configRefusal.Shortfall)
}

// chooserRefusal runs a choice expected to refuse and hands the pair to
// refusalFrom, so the test above reads as two refusals rather than four lines
// of plumbing.
func chooserRefusal(t *testing.T, host capability.HostCapabilityProfile, entry catalogue.Entry) (*testing.T, runtime.Choice, error) {
	t.Helper()
	choice, err := chooser().Choose(host, entry, now())
	return t, choice, err
}

// TestEveryRecordedReasonIsKnownAndHasARemedy guards the closed set: a reason
// added later without a remedy would leave a user with a name and no next step.
func TestEveryRecordedReasonIsKnownAndHasARemedy(t *testing.T) {
	for _, reason := range []runtime.RefusalReason{
		runtime.ReasonInsufficientResources,
		runtime.ReasonUnsupportedConfiguration,
		runtime.ReasonHostNotMeasured,
	} {
		require.True(t, reason.Known(), "reason %q must be in the recorded set", reason)
		require.NotEmpty(t, reason.Remedy(), "reason %q must state a remedy", reason)
	}
	require.False(t, runtime.RefusalReason("unavailable").Known(),
		"a generic unavailability invented downstream is not a reason")
}
