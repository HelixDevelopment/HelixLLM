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

// TestFitsInMemoryChoosesInMemoryEvenWhenOnTheStreamingRoster.
//
// Streaming trades throughput for feasibility by orders of magnitude, so it is
// a fallback and never a preference (D6). The rostered fixture entry fits this
// host's memory many times over; choosing streaming for it would be a real,
// user-visible slowdown chosen for no reason. Roster membership is what makes
// the fallback POSSIBLE, never what makes it desirable.
func TestFitsInMemoryChoosesInMemoryEvenWhenOnTheStreamingRoster(t *testing.T) {
	host := fixtures.DualAccelerator() // 96 GiB free memory, 2 TiB free disk
	entry := catfixtures.StreamingRosterMemberEntry()

	require.True(t, entry.StreamingEligible(),
		"fixture precondition: this entry must be on the streaming roster, or the test proves nothing")

	choice, err := chooser().Choose(host, entry, now())

	require.NoError(t, err)
	require.Equal(t, catalogue.RuntimeInMemory, choice.Runtime)
	require.False(t, choice.Fallback, "the in-memory path is the preference, not a fallback")
	require.Nil(t, choice.Tradeoff, "nothing is traded when the preferred path is taken")
}

// TestDeclaredEntryRuntimeDoesNotOverrideTheMeasuredChoice.
//
// The rostered fixture records Runtime: streaming in the catalogue. If Choose
// read that field it would serve this model from disk on a host with 96 GiB
// free — the exact "streaming as a preference" error D6 forbids. The path comes
// from the measurement and the roster, never from the entry's declared runtime.
func TestDeclaredEntryRuntimeDoesNotOverrideTheMeasuredChoice(t *testing.T) {
	entry := catfixtures.StreamingRosterMemberEntry()
	require.Equal(t, catalogue.RuntimeStreaming, entry.Runtime,
		"fixture precondition: the entry must DECLARE streaming for this test to mean anything")

	choice, err := chooser().Choose(fixtures.DualAccelerator(), entry, now())

	require.NoError(t, err)
	require.Equal(t, catalogue.RuntimeInMemory, choice.Runtime)
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
// architecture and on memory requirement, differing only in roster membership.
// If those two produce the same outcome, eligibility is not being read from the
// roster — whatever the code says it reads.
func TestRosterAloneSeparatesTwoIdenticalArchitectures(t *testing.T) {
	host := withMemoryAvailable(fixtures.SingleAccelerator(), 12*capability.GiB)
	rostered := catfixtures.StreamingRosterMemberEntry()
	unrostered := catfixtures.StreamingIneligibleMoEEntry()

	require.Equal(t, rostered.Architecture, unrostered.Architecture,
		"fixture precondition: identical architecture")
	require.Equal(t, rostered.MemoryRequiredBytes, unrostered.MemoryRequiredBytes,
		"fixture precondition: identical memory requirement")

	rosteredChoice, rosteredErr := chooser().Choose(host, rostered, now())
	require.NoError(t, rosteredErr)
	require.Equal(t, catalogue.RuntimeStreaming, rosteredChoice.Runtime)

	unrosteredChoice, unrosteredErr := chooser().Choose(host, unrostered, now())
	r := refusalFrom(t, unrosteredChoice, unrosteredErr)
	require.Equal(t, runtime.ReasonUnsupportedConfiguration, r.Reason)
}

// --- 3. the fallback is taken, and says what it costs ------------------------

// TestRosteredEntryThatDoesNotFitChoosesStreamingAndRecordsTheTradeoff.
//
// All three conditions hold: it does not fit memory, it is on the roster, and
// it meets the streaming runtime's own floors. The choice must also carry what
// was traded — a path chosen for feasibility at a large throughput cost is not
// the same offer as the fast one, and a caller that cannot see the difference
// cannot tell the user.
func TestRosteredEntryThatDoesNotFitChoosesStreamingAndRecordsTheTradeoff(t *testing.T) {
	host := withMemoryAvailable(fixtures.SingleAccelerator(), 12*capability.GiB)
	entry := catfixtures.StreamingRosterMemberEntry()

	require.Greater(t, entry.MemoryRequiredBytes, uint64(host.MemoryAvailable),
		"fixture precondition: the entry must NOT fit in memory, or the fallback is never reached")
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
