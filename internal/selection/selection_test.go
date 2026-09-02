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

// request builds a selection request over the full fixture catalogue for the
// given host and declared usage. Every test starts here so that a difference
// between two tests is visible in the call, not buried in setup.
func request(p capability.HostCapabilityProfile, usage catalogue.UsagePurpose) selection.Request {
	return selection.Request{
		Profile:       p,
		Entries:       catfixtures.Entries(),
		DeclaredUsage: usage,
		Now:           time.Now().UTC(),
		MaxProfileAge: time.Minute,
		Reserve:       selection.DefaultReserve(),
	}
}

func offeredIDs(fr selection.FamilyResult) []string {
	ids := make([]string, 0, len(fr.Offered))
	for _, o := range fr.Offered {
		ids = append(ids, o.ModelID)
	}
	return ids
}

func withheldFor(t *testing.T, fr selection.FamilyResult, modelID string) selection.Withheld {
	t.Helper()
	for _, w := range fr.Withheld {
		if w.ModelID == modelID {
			return w
		}
	}
	t.Fatalf("model %q was not withheld in family %q; offered=%v", modelID, fr.Family, offeredIDs(fr))
	return selection.Withheld{}
}

func familyOf(t *testing.T, r selection.Result, f catalogue.CapabilityFamily) selection.FamilyResult {
	t.Helper()
	fr, ok := r.Family(f)
	require.Truef(t, ok, "family %q absent from result", f)
	return fr
}

// --- Contract: the join is a pure function of (host, catalogue, usage) -------

// TestSelectDoesNotWriteBackIntoMeasurement is the load-bearing property of the
// package. Selection READS the measured host; it never writes into it. That
// one-directional join is what allows every path here — refusals included — to
// be driven from fixtures with no hardware present.
func TestSelectDoesNotWriteBackIntoMeasurement(t *testing.T) {
	for name, host := range fixtures.All() {
		t.Run(name, func(t *testing.T) {
			before := host
			entriesBefore := catfixtures.Entries()

			req := request(host, catalogue.UsagePersonal)
			_, _ = selection.Select(req)

			require.Equal(t, before, req.Profile, "selection mutated the measured profile")
			require.Equal(t, entriesBefore, catfixtures.Entries(), "selection mutated the catalogue")
		})
	}
}

// TestSelectIsDeterministic: the same inputs must yield the same answer. A
// selection that varies between identical calls cannot be reasoned about, and
// its refusals cannot be trusted.
func TestSelectIsDeterministic(t *testing.T) {
	req := request(fixtures.SingleAccelerator(), catalogue.UsageCommercial)

	first, errFirst := selection.Select(req)
	second, errSecond := selection.Select(req)

	require.Equal(t, errFirst, errSecond)
	require.Equal(t, first, second)
}

// --- Contract: every offered option fits the MEASURED host, both axes --------

// TestEveryOfferedOptionFitsTheMeasuredHost sweeps every fixture host and
// re-derives the fit independently of the implementation: an offer that does
// not fit its own host's measurement is the defect SC-002 forbids.
func TestEveryOfferedOptionFitsTheMeasuredHost(t *testing.T) {
	for name, host := range fixtures.All() {
		t.Run(name, func(t *testing.T) {
			res, err := selection.Select(request(host, catalogue.UsagePersonal))
			if err != nil {
				require.NotNil(t, res.Refusal, "an error must arrive with a stated host refusal")
				return
			}

			reserve := selection.DefaultReserve()
			memReserved := reserve.MemoryReserve(host.MemoryTotal)
			storeReserved := reserve.StorageReserve(host.StorageAvailable)

			for _, fr := range res.Families {
				for _, o := range fr.Offered {
					require.LessOrEqualf(t, o.Cost.MemoryRequiredBytes,
						uint64(host.MemoryAvailable)-uint64(memReserved),
						"%s offered but does not fit memory", o.ModelID)
					require.LessOrEqualf(t, o.Cost.StorageRequiredBytes,
						uint64(host.StorageAvailable)-uint64(storeReserved),
						"%s offered but does not fit storage", o.ModelID)
					if o.Cost.RequiresAccelerator {
						require.NotEmptyf(t, host.Accelerators,
							"%s requires an accelerator but the host has none", o.ModelID)
					}
				}
			}
		})
	}
}

// TestMemoryAndStorageAreSeparateAxes (D2). The low-storage host has 160 GiB of
// available memory and 2 GiB of free disk. A 4.7 GiB model fits memory more
// than twenty times over and does not fit disk at all, so the ONLY correct
// refusal names storage. A memory-named refusal here is provably wrong, and an
// implementation that checks a single combined axis cannot produce the right
// one.
func TestMemoryAndStorageAreSeparateAxes(t *testing.T) {
	host := fixtures.LowStorage()
	entry := catfixtures.CommercialSafeEntry()

	require.Less(t, entry.MemoryRequiredBytes, uint64(host.MemoryAvailable),
		"fixture precondition: the model must fit memory comfortably")
	require.Greater(t, entry.StorageRequiredBytes, uint64(host.StorageAvailable),
		"fixture precondition: the model must not fit storage")

	res, err := selection.Select(request(host, catalogue.UsagePersonal))
	require.NoError(t, err)

	fr := familyOf(t, res, catalogue.FamilyText)
	w := withheldFor(t, fr, entry.ModelID)

	require.Equal(t, selection.ReasonInsufficientResources, w.Reason)
	require.NotNil(t, w.Shortfall, "an insufficient-resources withholding must name the resource")
	require.Equal(t, selection.ResourceStorage, w.Shortfall.Resource,
		"memory fits %d times over here; naming memory is provably wrong",
		uint64(host.MemoryAvailable)/entry.MemoryRequiredBytes)
	require.Equal(t, entry.StorageRequiredBytes, w.Shortfall.RequiredBytes)
}

// TestHeadroomIsReEvaluatedAtSelectionTime (FR-006, FR-033). Two readings of
// the same host that differ only in free memory must produce different offers.
// An implementation that cached a first reading would answer identically.
func TestHeadroomIsReEvaluatedAtSelectionTime(t *testing.T) {
	roomy := fixtures.SingleAccelerator()

	tight := roomy
	// Leave less free memory than the largest fixture model needs, while the
	// nameplate total — and therefore the reserve — stays put.
	tight.MemoryAvailable = 12 * capability.GiB

	roomyRes, err := selection.Select(request(roomy, catalogue.UsagePersonal))
	require.NoError(t, err)
	tightRes, err := selection.Select(request(tight, catalogue.UsagePersonal))
	require.NoError(t, err)

	roomyText := familyOf(t, roomyRes, catalogue.FamilyText)
	tightText := familyOf(t, tightRes, catalogue.FamilyText)

	require.NotEqual(t, offeredIDs(roomyText), offeredIDs(tightText),
		"offers did not track the current reading of free memory")
	require.Less(t, len(tightText.Offered), len(roomyText.Offered))
}

// TestSelectionRefusesWhatWouldLeaveTheHostUnresponsive (FR-007, FR-008,
// SC-003). A model that fits raw free memory but would drive the host below its
// stated reserve must be withheld, not offered.
func TestSelectionRefusesWhatWouldLeaveTheHostUnresponsive(t *testing.T) {
	entry := catfixtures.CommercialSafeEntry()
	reserve := selection.DefaultReserve()

	host := fixtures.NoAccelerator()
	// Free memory exceeds the requirement, but only by less than the reserve:
	// serving here would consume the headroom SC-003 requires be left.
	host.MemoryAvailable = capability.Bytes(entry.MemoryRequiredBytes) + 1*capability.MiB

	require.Greater(t, uint64(host.MemoryAvailable), entry.MemoryRequiredBytes,
		"fixture precondition: raw free memory must exceed the requirement")
	require.Greater(t, uint64(reserve.MemoryReserve(host.MemoryTotal)), uint64(1*capability.MiB),
		"fixture precondition: the reserve must be the thing that bites")

	res, err := selection.Select(request(host, catalogue.UsagePersonal))
	require.NoError(t, err)

	fr := familyOf(t, res, catalogue.FamilyText)
	w := withheldFor(t, fr, entry.ModelID)
	require.Equal(t, selection.ReasonInsufficientResources, w.Reason)
	require.NotNil(t, w.Shortfall)
	require.Equal(t, selection.ResourceMemory, w.Shortfall.Resource)
	require.Positive(t, w.Shortfall.ReservedBytes, "the withholding must state the reserve it protected")
}

// TestOfferedOptionCarriesComparableCostAndCapability (FR-005). An option a
// non-expert can compare must carry its cost and its expected capability, not
// merely a model name.
func TestOfferedOptionCarriesComparableCostAndCapability(t *testing.T) {
	res, err := selection.Select(request(fixtures.SingleAccelerator(), catalogue.UsagePersonal))
	require.NoError(t, err)

	fr := familyOf(t, res, catalogue.FamilyText)
	require.NotEmpty(t, fr.Offered)

	for _, o := range fr.Offered {
		require.Positive(t, o.Cost.MemoryRequiredBytes)
		require.Positive(t, o.Cost.StorageRequiredBytes)
		require.Positive(t, o.Expected.ContextTokens, "%s states no usable context", o.ModelID)
		require.NotEmpty(t, o.Expected.Modalities)
		require.NotEmpty(t, o.Terms.LicenseID, "an offered option must carry the terms it may be used under")
		require.Positive(t, o.Headroom.MemoryRemainingBytes, "an offer must state what it leaves behind")
	}
}

// TestOfferIdentityIsHostQualified (D7, FR-014). The host-qualified identity is
// formed here, the only place that knows the host.
func TestOfferIdentityIsHostQualified(t *testing.T) {
	host := fixtures.SingleAccelerator()
	res, err := selection.Select(request(host, catalogue.UsagePersonal))
	require.NoError(t, err)

	fr := familyOf(t, res, catalogue.FamilyText)
	require.NotEmpty(t, fr.Offered)

	for _, o := range fr.Offered {
		require.Equal(t, host.HostIdentity, o.HostIdentity)
		require.Contains(t, o.Identity, host.HostIdentity)
		require.Contains(t, o.Identity, o.ModelID)
	}
}

// --- Contract: streaming eligibility is roster membership, not architecture --

// TestStreamingEligibilityIsRosterMembershipNotArchitecture (D1). Both fixture
// entries are mixture-of-experts. One is on the streaming runtime's roster and
// one is not, and they must not be treated alike. An implementation that reads
// Architecture cannot tell them apart — and the offer it wrongly makes fails at
// load time instead of here.
func TestStreamingEligibilityIsRosterMembershipNotArchitecture(t *testing.T) {
	rostered := catfixtures.StreamingRosterMemberEntry()
	unrostered := catfixtures.StreamingIneligibleMoEEntry()
	require.Equal(t, rostered.Architecture, unrostered.Architecture,
		"fixture precondition: the two entries must share an architecture")

	// A streaming-served entry whose roster lookup missed has no path at all.
	orphan := unrostered
	orphan.ModelID = "orphan-streaming-moe"
	orphan.Runtime = catalogue.RuntimeStreaming

	req := request(fixtures.DualAccelerator(), catalogue.UsagePersonal)
	req.Entries = append(req.Entries, orphan)

	res, err := selection.Select(req)
	require.NoError(t, err)

	fr := familyOf(t, res, catalogue.FamilyText)

	require.Contains(t, offeredIDs(fr), rostered.ModelID,
		"the rostered MoE entry is streaming-eligible and fits; it must be offered")
	require.Contains(t, offeredIDs(fr), unrostered.ModelID,
		"the unrostered MoE entry is served in memory; architecture must not withhold it")

	w := withheldFor(t, fr, orphan.ModelID)
	require.Equal(t, selection.ReasonUnsupportedConfiguration, w.Reason)
	require.NotNil(t, w.Unsupported)
	require.Equal(t, selection.RequirementStreamingRoster, w.Unsupported.Requirement)
	require.Equal(t, catfixtures.StreamingFamilyQwen3MoE, w.Unsupported.Detail,
		"the refusal must name the roster family that was looked up")
}

// --- Contract: a family is never silently empty (D5) ------------------------

// TestNoFamilyIsEverSilentlyEmpty. Across every fixture host and every declared
// usage, a family present in the result either offers something or states why
// it cannot — never an empty list with no reason.
func TestNoFamilyIsEverSilentlyEmpty(t *testing.T) {
	usages := []catalogue.UsagePurpose{
		catalogue.UsageCommercial,
		catalogue.UsagePersonal,
		catalogue.UsageResearch,
		catalogue.UsageEvaluation,
	}

	for name, host := range fixtures.All() {
		for _, usage := range usages {
			t.Run(name+"/"+string(usage), func(t *testing.T) {
				res, err := selection.Select(request(host, usage))
				if err != nil {
					require.NotNil(t, res.Refusal)
					return
				}
				require.NotEmpty(t, res.Families, "a measured host must report on the families it was asked about")

				for _, fr := range res.Families {
					if len(fr.Offered) > 0 {
						require.Nil(t, fr.Refusal, "family %q both offers and refuses", fr.Family)
						continue
					}
					require.NotNilf(t, fr.Refusal, "family %q is empty with no stated reason", fr.Family)
					require.Truef(t, fr.Refusal.Reason.Known(),
						"family %q refused with an unrecorded reason %q", fr.Family, fr.Refusal.Reason)
					require.NotEmptyf(t, fr.Refusal.Missing(),
						"family %q refusal names no missing requirement", fr.Family)
				}
			})
		}
	}
}

// TestEveryCandidateIsAccountedFor. Each catalogue entry in a requested family
// is either offered or withheld with exactly one reason — never dropped
// silently.
func TestEveryCandidateIsAccountedFor(t *testing.T) {
	for name, host := range fixtures.All() {
		t.Run(name, func(t *testing.T) {
			res, err := selection.Select(request(host, catalogue.UsageCommercial))
			if err != nil {
				return
			}

			seen := map[string]int{}
			for _, fr := range res.Families {
				for _, o := range fr.Offered {
					seen[o.ModelID]++
				}
				for _, w := range fr.Withheld {
					seen[w.ModelID]++
					require.Truef(t, w.Reason.Known(), "%s withheld with an unrecorded reason", w.ModelID)
				}
			}

			for _, e := range catfixtures.Entries() {
				require.Equalf(t, 1, seen[e.ModelID],
					"%s was accounted for %d times; each candidate is offered or withheld exactly once",
					e.ModelID, seen[e.ModelID])
			}
		})
	}
}

// --- Contract: usage terms gate offers (FR-054) ------------------------------

// TestDeclaredCommercialUsageWithholdsNonCommercialModels. The withholding must
// name the term that actually excluded the usage. The fixture deliberately
// carries a NON-excluding restriction first: an implementation that reports
// "the first restriction" names attribution-required and is wrong.
func TestDeclaredCommercialUsageWithholdsNonCommercialModels(t *testing.T) {
	entry := catfixtures.NonCommercialEntry()
	require.Equal(t, catalogue.TermAttributionRequired, entry.UsageTerms.Restrictions[0].Term,
		"fixture precondition: a non-excluding restriction must come first")

	res, err := selection.Select(request(fixtures.NoAccelerator(), catalogue.UsageCommercial))
	require.NoError(t, err)

	fr := familyOf(t, res, catalogue.FamilyTextToSpeech)
	w := withheldFor(t, fr, entry.ModelID)

	require.Equal(t, selection.ReasonExcludedByUsageTerms, w.Reason)
	require.NotNil(t, w.Exclusion)
	require.Equal(t, catalogue.TermNonCommercial, w.Exclusion.Term,
		"the restricting term must be named, not merely the first one encountered")
	require.Equal(t, catalogue.UsageCommercial, w.Exclusion.Purpose)
	require.Equal(t, entry.UsageTerms.LicenseID, w.Exclusion.LicenseID)
	require.NotEmpty(t, w.Exclusion.Reference, "the withholding must cite the clause it rests on")
}

// TestSameModelIsOfferedUnderAPermittedUsage. The same entry, same host — only
// the declared usage differs. Terms gate offers; they are not a property of the
// model alone.
func TestSameModelIsOfferedUnderAPermittedUsage(t *testing.T) {
	entry := catfixtures.NonCommercialEntry()
	host := fixtures.NoAccelerator()

	commercial, err := selection.Select(request(host, catalogue.UsageCommercial))
	require.NoError(t, err)
	personal, err := selection.Select(request(host, catalogue.UsagePersonal))
	require.NoError(t, err)

	require.NotContains(t, offeredIDs(familyOf(t, commercial, catalogue.FamilyTextToSpeech)), entry.ModelID)
	require.Contains(t, offeredIDs(familyOf(t, personal, catalogue.FamilyTextToSpeech)), entry.ModelID)
}

// TestRevenueCapIsReportedWithItsThreshold. A capped licence is not the same as
// a flat prohibition: the ceiling is what tells the user whether it applies to
// them, so it must survive to the withholding.
func TestRevenueCapIsReportedWithItsThreshold(t *testing.T) {
	entry := catfixtures.RevenueCappedEntry()

	res, err := selection.Select(request(fixtures.SingleAccelerator(), catalogue.UsageCommercial))
	require.NoError(t, err)

	fr := familyOf(t, res, catalogue.FamilyImageGeneration)
	w := withheldFor(t, fr, entry.ModelID)

	require.Equal(t, selection.ReasonExcludedByUsageTerms, w.Reason)
	require.NotNil(t, w.Exclusion)
	require.Equal(t, catalogue.TermRevenueCap, w.Exclusion.Term)
	require.False(t, w.Exclusion.Threshold.Zero(), "a capped term must carry its ceiling")
	require.Equal(t, entry.UsageTerms.Restrictions[0].Threshold, w.Exclusion.Threshold)
}

// --- Contract: pins constrain, never bypass (FR-056) -------------------------

// TestPinConstrainsSelectionToTheSinglePinnedModel.
func TestPinConstrainsSelectionToTheSinglePinnedModel(t *testing.T) {
	entry := catfixtures.CommercialSafeEntry()

	req := request(fixtures.SingleAccelerator(), catalogue.UsageCommercial)
	req.Pin = &selection.Pin{ModelID: entry.ModelID, Variant: entry.Variant}

	res, err := selection.Select(req)
	require.NoError(t, err)
	require.Len(t, res.Families, 1, "a pin narrows the answer to the pinned model's family")

	fr := res.Families[0]
	require.Equal(t, []string{entry.ModelID}, offeredIDs(fr))
	require.Empty(t, fr.Withheld, "a satisfied pin withholds nothing")
}

// TestPinIsStillMeasured. The pinned model does not fit this host's disk, and
// the pin does not make it fit. FR-056: a pin is a constraint on the choice,
// never a bypass of the measurement.
func TestPinIsStillMeasured(t *testing.T) {
	entry := catfixtures.CommercialSafeEntry()
	host := fixtures.LowStorage()

	req := request(host, catalogue.UsagePersonal)
	req.Pin = &selection.Pin{ModelID: entry.ModelID, Variant: entry.Variant}

	res, err := selection.Select(req)
	require.NoError(t, err)

	fr := res.Families[0]
	require.Empty(t, fr.Offered, "a pin the host cannot run must not be offered")
	require.NotNil(t, fr.Refusal)
	require.Equal(t, selection.ReasonInsufficientResources, fr.Refusal.Reason)

	w := withheldFor(t, fr, entry.ModelID)
	require.NotNil(t, w.Shortfall)
	require.Equal(t, selection.ResourceStorage, w.Shortfall.Resource,
		"the refusal must name which resource is insufficient")
}

// TestPinOnAnUnmeasurableHostIsRefused. The strongest form of the pin rule: a
// pin plus a host that could not be measured is still a refusal, because there
// is no measurement to check the pin against.
func TestPinOnAnUnmeasurableHostIsRefused(t *testing.T) {
	req := request(fixtures.Unmeasurable(), catalogue.UsagePersonal)
	req.Pin = &selection.Pin{ModelID: catfixtures.CommercialSafeEntry().ModelID}

	res, err := selection.Select(req)
	require.Error(t, err)
	require.NotNil(t, res.Refusal)
	require.Equal(t, selection.RefusalHostNotMeasured, res.Refusal.Kind)
	require.Empty(t, res.Families, "nothing may be offered against a host that was not measured")
}

// --- Contract: an unmeasurable host produces a refusal, never a guess --------

// TestUnmeasurableHostIsRefusedNotGuessed (FR-056, invariant 3).
func TestUnmeasurableHostIsRefusedNotGuessed(t *testing.T) {
	host := fixtures.Unmeasurable()
	require.Error(t, host.ValidateForSelection(), "fixture precondition")

	res, err := selection.Select(request(host, catalogue.UsagePersonal))

	require.ErrorIs(t, err, selection.ErrHostNotMeasured)
	require.NotNil(t, res.Refusal, "the response must be a refusal that states it")
	require.Equal(t, selection.RefusalHostNotMeasured, res.Refusal.Kind)
	require.Equal(t, host.HostIdentity, res.Refusal.HostIdentity)
	require.NotEmpty(t, res.Refusal.Cause, "the refusal must state what was missing")
	require.Empty(t, res.Families, "no fixed default model may stand in for a measurement")
}

// TestStaleMeasurementIsRefused (FR-033). A reading older than the request's
// bound is not a current measurement, and offers derived from it would not
// reflect current conditions.
func TestStaleMeasurementIsRefused(t *testing.T) {
	fresh := fixtures.SingleAccelerator()
	stale := fixtures.Staled(fresh, 2*time.Hour)

	req := request(stale, catalogue.UsagePersonal)
	req.MaxProfileAge = time.Minute

	res, err := selection.Select(req)
	require.ErrorIs(t, err, selection.ErrMeasurementStale)
	require.NotNil(t, res.Refusal)
	require.Equal(t, selection.RefusalMeasurementStale, res.Refusal.Kind)
	require.Greater(t, res.Refusal.AgeSeconds, res.Refusal.MaxAgeSeconds)
	require.Empty(t, res.Families)

	// The same host, freshly measured, is a usable basis.
	ok, err := selection.Select(request(fresh, catalogue.UsagePersonal))
	require.NoError(t, err)
	require.NotEmpty(t, ok.Families)
}

// TestDeclaredUsageIsRequired. Terms cannot be applied against an undeclared
// usage, and quietly assuming one would offer models the user may not use.
func TestDeclaredUsageIsRequired(t *testing.T) {
	req := request(fixtures.SingleAccelerator(), "")
	res, err := selection.Select(req)
	require.ErrorIs(t, err, selection.ErrNoDeclaredUsage)
	require.Empty(t, res.Families)
}

// --- Contract: the requested family is honoured ------------------------------

// TestSelectingASingleFamilyAnswersOnlyThatFamily.
func TestSelectingASingleFamilyAnswersOnlyThatFamily(t *testing.T) {
	req := request(fixtures.SingleAccelerator(), catalogue.UsagePersonal)
	req.Families = []catalogue.CapabilityFamily{catalogue.FamilyText}

	res, err := selection.Select(req)
	require.NoError(t, err)
	require.Len(t, res.Families, 1)
	require.Equal(t, catalogue.FamilyText, res.Families[0].Family)
	require.NotEmpty(t, res.Families[0].Offered)
}

// TestFamilyWithNoCatalogueEntryStatesSo. Asking for a family the catalogue has
// nothing for must still answer — a refusal naming the missing requirement, not
// an absent entry in the result.
func TestFamilyWithNoCatalogueEntryStatesSo(t *testing.T) {
	req := request(fixtures.SingleAccelerator(), catalogue.UsagePersonal)
	req.Families = []catalogue.CapabilityFamily{catalogue.FamilyVector}

	res, err := selection.Select(req)
	require.NoError(t, err)
	require.Len(t, res.Families, 1)

	fr := res.Families[0]
	require.Empty(t, fr.Offered)
	require.NotNil(t, fr.Refusal)
	require.Equal(t, selection.ReasonUnsupportedConfiguration, fr.Refusal.Reason)
	require.Contains(t, fr.Refusal.Missing(), string(selection.RequirementCatalogueEntry))
}

// TestFamilyOrderIsStable. Two calls over the same catalogue must report
// families in the same order, so a caller can diff two results.
func TestFamilyOrderIsStable(t *testing.T) {
	req := request(fixtures.DualAccelerator(), catalogue.UsagePersonal)

	first, err := selection.Select(req)
	require.NoError(t, err)

	shuffled := req
	entries := catfixtures.Entries()
	shuffled.Entries = []catalogue.Entry{entries[4], entries[0], entries[3], entries[2], entries[1]}
	second, err := selection.Select(shuffled)
	require.NoError(t, err)

	firstFamilies := make([]catalogue.CapabilityFamily, 0, len(first.Families))
	for _, fr := range first.Families {
		firstFamilies = append(firstFamilies, fr.Family)
	}
	secondFamilies := make([]catalogue.CapabilityFamily, 0, len(second.Families))
	for _, fr := range second.Families {
		secondFamilies = append(secondFamilies, fr.Family)
	}
	require.Equal(t, firstFamilies, secondFamilies, "family order must not depend on catalogue order")
}

// --- Contract: refusal text is composed from data (CONST-046) ---------------

// TestRefusalTextIsComposedFromMeasurementAndCatalogueData. Every token in a
// composed description must be a recorded machine key or a figure that appears
// in the inputs. A fixed English sentence cannot satisfy that.
func TestRefusalTextIsComposedFromMeasurementAndCatalogueData(t *testing.T) {
	host := fixtures.LowStorage()
	res, err := selection.Select(request(host, catalogue.UsagePersonal))
	require.NoError(t, err)

	fr := familyOf(t, res, catalogue.FamilyText)
	w := withheldFor(t, fr, catfixtures.CommercialSafeEntry().ModelID)

	fields := selection.DescribeWithheld(host, w)
	require.NotEmpty(t, fields)

	keys := map[selection.FieldKey]string{}
	for _, f := range fields {
		require.Truef(t, f.Key.Known(), "field key %q is not a recorded machine key", f.Key)
		require.NotEmptyf(t, f.Value, "field %q carries no value", f.Key)
		keys[f.Key] = f.Value
	}

	require.Equal(t, string(selection.ReasonInsufficientResources), keys[selection.FieldReason])
	require.Equal(t, string(selection.ResourceStorage), keys[selection.FieldResource])
	require.Equal(t, host.HostIdentity, keys[selection.FieldHost])
	require.Equal(t, catfixtures.CommercialSafeEntry().ModelID, keys[selection.FieldModel])
	require.Equal(t, string(catalogue.FamilyText), keys[selection.FieldFamily])
}
