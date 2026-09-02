package selection_test

import (
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/capability/testdata/fixtures"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	catfixtures "github.com/HelixDevelopment/HelixLLM/internal/catalogue/testdata"
	"github.com/HelixDevelopment/HelixLLM/internal/selection"
	"github.com/stretchr/testify/require"
)

// The three withheld reasons have three different remedies:
//
//	insufficient_resources    → change the host, or pick something smaller
//	unsupported_configuration → a different approach entirely
//	excluded_by_usage_terms   → a different model, or a different declared usage
//
// Collapsing them into one generic unavailability destroys the remedy, which is
// the whole point of FR-055. These tests reach each reason from a fixture host
// and then assert the three cannot be confused with one another.

// --- insufficient_resources --------------------------------------------------

// TestReasonInsufficientResourcesIsReachedAndNamesTheResource. The host is
// otherwise perfectly capable — the resource it lacks is the finding.
func TestReasonInsufficientResourcesIsReachedAndNamesTheResource(t *testing.T) {
	host := fixtures.LowStorage()
	entry := catfixtures.CommercialSafeEntry()

	res, err := selection.Select(request(host, catalogue.UsagePersonal))
	require.NoError(t, err)

	fr := familyOf(t, res, catalogue.FamilyText)
	w := withheldFor(t, fr, entry.ModelID)

	require.Equal(t, selection.ReasonInsufficientResources, w.Reason)
	require.Equal(t, selection.RemedyChangeHostOrPickSmaller, w.Reason.Remedy())

	require.NotNil(t, w.Shortfall, "the reason is only actionable with the resource named")
	require.Nil(t, w.Unsupported, "exactly one reason, with only its own detail")
	require.Nil(t, w.Exclusion, "exactly one reason, with only its own detail")

	require.Equal(t, selection.ResourceStorage, w.Shortfall.Resource)
	require.Equal(t, entry.StorageRequiredBytes, w.Shortfall.RequiredBytes)
	require.Less(t, w.Shortfall.AvailableBytes, w.Shortfall.RequiredBytes,
		"a shortfall must actually be short")
}

// TestInsufficientResourcesDistinguishesMemoryFromStorage. Two hosts, the same
// model, two different named resources. An implementation with one combined
// axis cannot produce both.
func TestInsufficientResourcesDistinguishesMemoryFromStorage(t *testing.T) {
	entry := catfixtures.CommercialSafeEntry()

	starvedOfDisk := fixtures.LowStorage()

	starvedOfMemory := fixtures.NoAccelerator()
	starvedOfMemory.MemoryAvailable = 1 * capability.GiB

	diskRes, err := selection.Select(request(starvedOfDisk, catalogue.UsagePersonal))
	require.NoError(t, err)
	memRes, err := selection.Select(request(starvedOfMemory, catalogue.UsagePersonal))
	require.NoError(t, err)

	diskW := withheldFor(t, familyOf(t, diskRes, catalogue.FamilyText), entry.ModelID)
	memW := withheldFor(t, familyOf(t, memRes, catalogue.FamilyText), entry.ModelID)

	require.Equal(t, selection.ReasonInsufficientResources, diskW.Reason)
	require.Equal(t, selection.ReasonInsufficientResources, memW.Reason)

	require.Equal(t, selection.ResourceStorage, diskW.Shortfall.Resource)
	require.Equal(t, selection.ResourceMemory, memW.Shortfall.Resource)
	require.NotEqual(t, diskW.Shortfall.Resource, memW.Shortfall.Resource,
		"the same reason with two different resources must not report the same detail")
}

// --- unsupported_configuration ----------------------------------------------

// TestReasonUnsupportedConfigurationIsReachedAndNamesTheRequirement. A model
// that mandates an accelerator on a host measured to have none is not a
// resource shortfall: no amount of free memory fixes it, so the remedy is a
// different approach.
func TestReasonUnsupportedConfigurationIsReachedAndNamesTheRequirement(t *testing.T) {
	host := fixtures.NoAccelerator()
	require.True(t, host.HasNoAccelerator(), "fixture precondition: a positive finding of no accelerator")

	entry := catfixtures.StreamingRosterMemberEntry()
	require.True(t, entry.RequiresAccelerator, "fixture precondition")

	res, err := selection.Select(request(host, catalogue.UsagePersonal))
	require.NoError(t, err)

	fr := familyOf(t, res, catalogue.FamilyText)
	w := withheldFor(t, fr, entry.ModelID)

	require.Equal(t, selection.ReasonUnsupportedConfiguration, w.Reason)
	require.Equal(t, selection.RemedyDifferentApproach, w.Reason.Remedy())

	require.NotNil(t, w.Unsupported, "the reason is only actionable with the requirement named")
	require.Nil(t, w.Shortfall, "an absent accelerator is not a byte shortfall")
	require.Nil(t, w.Exclusion)

	require.Equal(t, selection.RequirementAccelerator, w.Unsupported.Requirement)
}

// TestUnsupportedConfigurationCoversAMissingStreamingPath. The other way a host
// can be unsupported: the entry is served only by the streaming runtime, and
// that runtime does not list its family. More memory does not help.
func TestUnsupportedConfigurationCoversAMissingStreamingPath(t *testing.T) {
	orphan := catfixtures.StreamingIneligibleMoEEntry()
	orphan.ModelID = "orphan-streaming-moe"
	orphan.Runtime = catalogue.RuntimeStreaming

	req := request(fixtures.DualAccelerator(), catalogue.UsagePersonal)
	req.Entries = append(req.Entries, orphan)

	res, err := selection.Select(req)
	require.NoError(t, err)

	w := withheldFor(t, familyOf(t, res, catalogue.FamilyText), orphan.ModelID)
	require.Equal(t, selection.ReasonUnsupportedConfiguration, w.Reason)
	require.Equal(t, selection.RequirementStreamingRoster, w.Unsupported.Requirement)
	require.Nil(t, w.Shortfall, "a missing runtime path is not a resource shortfall")
}

// --- excluded_by_usage_terms -------------------------------------------------

// TestReasonExcludedByUsageTermsIsReachedAndNamesTheTerm. The model fits and the
// host supports it; only the terms stand in the way, so the remedy is a
// different model or a different declared usage — never a bigger machine.
func TestReasonExcludedByUsageTermsIsReachedAndNamesTheTerm(t *testing.T) {
	host := fixtures.NoAccelerator()
	entry := catfixtures.NonCommercialEntry()

	res, err := selection.Select(request(host, catalogue.UsageCommercial))
	require.NoError(t, err)

	fr := familyOf(t, res, catalogue.FamilyTextToSpeech)
	w := withheldFor(t, fr, entry.ModelID)

	require.Equal(t, selection.ReasonExcludedByUsageTerms, w.Reason)
	require.Equal(t, selection.RemedyDifferentModelOrUsage, w.Reason.Remedy())

	require.NotNil(t, w.Exclusion, "the reason is only actionable with the term named")
	require.Nil(t, w.Shortfall, "the model fits; naming a resource would send the user to buy hardware")
	require.Nil(t, w.Unsupported)

	require.Equal(t, catalogue.TermNonCommercial, w.Exclusion.Term)
	require.Equal(t, catalogue.UsageCommercial, w.Exclusion.Purpose)
}

// --- the three are distinct --------------------------------------------------

// TestTheThreeReasonsAreDistinct. All three reached from fixtures in one run,
// then compared. This is the test that fails if the reasons are ever collapsed
// into a single generic unavailability.
func TestTheThreeReasonsAreDistinct(t *testing.T) {
	orphan := catfixtures.StreamingIneligibleMoEEntry()
	orphan.ModelID = "orphan-streaming-moe"
	orphan.Runtime = catalogue.RuntimeStreaming

	req := request(fixtures.LowStorage(), catalogue.UsageCommercial)
	req.Entries = append(req.Entries, orphan)

	res, err := selection.Select(req)
	require.NoError(t, err)

	reasons := map[selection.WithheldReason]int{}
	for _, fr := range res.Families {
		for _, w := range fr.Withheld {
			reasons[w.Reason]++
		}
	}

	require.Positive(t, reasons[selection.ReasonInsufficientResources],
		"the low-storage host must yield a resource shortfall")
	require.Positive(t, reasons[selection.ReasonUnsupportedConfiguration],
		"the orphaned streaming entry must yield an unsupported configuration")
	require.Positive(t, reasons[selection.ReasonExcludedByUsageTerms],
		"a commercial declaration must yield a terms exclusion")

	require.Len(t, reasons, 3, "all three reasons must be reachable and separate")
}

// TestReasonsHaveDistinctRemedies. Identical remedies would make the reasons
// interchangeable in practice even if their names differed.
func TestReasonsHaveDistinctRemedies(t *testing.T) {
	all := []selection.WithheldReason{
		selection.ReasonInsufficientResources,
		selection.ReasonUnsupportedConfiguration,
		selection.ReasonExcludedByUsageTerms,
	}

	remedies := map[selection.Remedy]selection.WithheldReason{}
	for _, r := range all {
		require.True(t, r.Known(), "%q is not a recorded reason", r)
		remedy := r.Remedy()
		require.NotEmpty(t, remedy, "%q implies no remedy", r)
		if other, clash := remedies[remedy]; clash {
			t.Fatalf("%q and %q share the remedy %q; they are not distinguishable to a user", r, other, remedy)
		}
		remedies[remedy] = r
	}
	require.Len(t, remedies, 3)
}

// TestUnrecordedReasonIsNotKnown. The enum is closed: a fourth value invented
// downstream must not pass as a reason.
func TestUnrecordedReasonIsNotKnown(t *testing.T) {
	require.False(t, selection.WithheldReason("unavailable").Known(),
		"a generic unavailability must not be a recorded reason")
	require.False(t, selection.WithheldReason("").Known())
	require.Empty(t, selection.WithheldReason("unavailable").Remedy())
}

// TestExactlyOneReasonPerWithheldOption. Each withheld option carries one
// reason and only the detail belonging to it.
func TestExactlyOneReasonPerWithheldOption(t *testing.T) {
	orphan := catfixtures.StreamingIneligibleMoEEntry()
	orphan.ModelID = "orphan-streaming-moe"
	orphan.Runtime = catalogue.RuntimeStreaming

	for name, host := range fixtures.All() {
		t.Run(name, func(t *testing.T) {
			req := request(host, catalogue.UsageCommercial)
			req.Entries = append(req.Entries, orphan)

			res, err := selection.Select(req)
			if err != nil {
				return
			}

			for _, fr := range res.Families {
				for _, w := range fr.Withheld {
					details := 0
					if w.Shortfall != nil {
						details++
						require.Equal(t, selection.ReasonInsufficientResources, w.Reason)
					}
					if w.Unsupported != nil {
						details++
						require.Equal(t, selection.ReasonUnsupportedConfiguration, w.Reason)
					}
					if w.Exclusion != nil {
						details++
						require.Equal(t, selection.ReasonExcludedByUsageTerms, w.Reason)
					}
					require.Equalf(t, 1, details,
						"%s carries %d reason details; exactly one is required", w.ModelID, details)
				}
			}
		})
	}
}

// --- host-level refusals are a separate kind ---------------------------------

// TestHostRefusalIsNotOneOfTheWithheldReasons. "This host could not be
// measured" is not "this option is unavailable": there are no options to speak
// about yet. Keeping the kinds separate is what stops a failed measurement from
// being reported as a resource shortfall.
func TestHostRefusalIsNotOneOfTheWithheldReasons(t *testing.T) {
	res, err := selection.Select(request(fixtures.Unmeasurable(), catalogue.UsagePersonal))
	require.Error(t, err)
	require.NotNil(t, res.Refusal)

	require.Equal(t, selection.RefusalHostNotMeasured, res.Refusal.Kind)
	require.True(t, res.Refusal.Kind.Known())

	require.False(t, selection.WithheldReason(res.Refusal.Kind).Known(),
		"a host refusal kind must not double as a withheld reason")
}

// TestStaleAndUnmeasuredAreDifferentHostRefusals. A reading that is merely old
// is not the same as one that never completed: one is refreshed, the other
// investigated.
func TestStaleAndUnmeasuredAreDifferentHostRefusals(t *testing.T) {
	unmeasured, errUnmeasured := selection.Select(request(fixtures.Unmeasurable(), catalogue.UsagePersonal))

	staleReq := request(fixtures.Staled(fixtures.SingleAccelerator(), 24*time.Hour), catalogue.UsagePersonal)
	staleReq.MaxProfileAge = time.Minute
	stale, errStale := selection.Select(staleReq)

	require.ErrorIs(t, errUnmeasured, selection.ErrHostNotMeasured)
	require.ErrorIs(t, errStale, selection.ErrMeasurementStale)
	require.NotErrorIs(t, errStale, selection.ErrHostNotMeasured)

	require.NotEqual(t, unmeasured.Refusal.Kind, stale.Refusal.Kind)
}

// --- family refusals name what is missing ------------------------------------

// TestFamilyRefusalNamesTheMissingRequirement (D5). A family that cannot be
// served states why and names what the host lacks — never an empty list.
func TestFamilyRefusalNamesTheMissingRequirement(t *testing.T) {
	res, err := selection.Select(request(fixtures.NoAccelerator(), catalogue.UsagePersonal))
	require.NoError(t, err)

	fr := familyOf(t, res, catalogue.FamilyImageGeneration)
	require.Empty(t, fr.Offered, "the only image model needs an accelerator this host lacks")
	require.NotNil(t, fr.Refusal)
	require.Equal(t, selection.ReasonUnsupportedConfiguration, fr.Refusal.Reason)
	require.Contains(t, fr.Refusal.Missing(), string(selection.RequirementAccelerator))
}

// TestFamilyRefusalPrefersTheClosestRemedy. When candidates fail for different
// reasons, the family's stated reason is the one closest to being satisfiable —
// telling a user to buy a machine when the real obstacle is a licence sends
// them to spend money that will not help.
func TestFamilyRefusalPrefersTheClosestRemedy(t *testing.T) {
	// This host runs the image model perfectly well; only the declared usage
	// stands in the way. A second candidate in the same family cannot run here
	// at all.
	unrunnable := catfixtures.RevenueCappedEntry()
	unrunnable.ModelID = "oversized-image-model"
	unrunnable.MemoryRequiredBytes = 4096 * uint64(capability.GiB)
	unrunnable.StorageRequiredBytes = 4096 * uint64(capability.GiB)
	unrunnable.Integrity.SizeBytes = unrunnable.StorageRequiredBytes

	req := request(fixtures.SingleAccelerator(), catalogue.UsageCommercial)
	req.Entries = append(req.Entries, unrunnable)

	res, err := selection.Select(req)
	require.NoError(t, err)

	fr := familyOf(t, res, catalogue.FamilyImageGeneration)
	require.Empty(t, fr.Offered)
	require.NotNil(t, fr.Refusal)
	require.Equal(t, selection.ReasonExcludedByUsageTerms, fr.Refusal.Reason,
		"a licence obstacle must not be reported as a hardware one")
	require.Contains(t, fr.Refusal.Missing(), string(catalogue.TermRevenueCap))
}

// TestFamilyRefusalCarriesItsCandidates. A refusal a user can act on has to say
// what was considered, not merely that nothing worked.
func TestFamilyRefusalCarriesItsCandidates(t *testing.T) {
	res, err := selection.Select(request(fixtures.LowStorage(), catalogue.UsagePersonal))
	require.NoError(t, err)

	fr := familyOf(t, res, catalogue.FamilyText)
	require.Empty(t, fr.Offered)
	require.NotNil(t, fr.Refusal)
	require.NotEmpty(t, fr.Refusal.Candidates, "the refusal names nothing it considered")
	require.Equal(t, len(fr.Withheld), len(fr.Refusal.Candidates))
}
