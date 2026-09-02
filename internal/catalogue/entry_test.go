package catalogue_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue/testdata"
	"github.com/stretchr/testify/require"
)

// TestStreamingEligibilityIsRosterMembershipNotArchitecture is the load-bearing
// test of this package.
//
// Both entries are mixture-of-experts. If eligibility were inferred from
// architecture they would agree; they must not. The roster member is eligible,
// the unrostered one is not, and the ONLY difference the implementation is
// permitted to read is roster membership (D1).
func TestStreamingEligibilityIsRosterMembershipNotArchitecture(t *testing.T) {
	member := testdata.StreamingRosterMemberEntry()
	ineligible := testdata.StreamingIneligibleMoEEntry()

	// Premise: the two fixtures are architecturally identical.
	require.Equal(t, catalogue.ArchitectureMixtureOfExperts, member.Architecture)
	require.Equal(t, catalogue.ArchitectureMixtureOfExperts, ineligible.Architecture)
	require.Equal(t, member.Architecture, ineligible.Architecture,
		"fixtures must share an architecture, or this test cannot distinguish "+
			"roster membership from architecture inference")

	// Conclusion: they disagree anyway.
	require.True(t, member.StreamingEligible(),
		"a rostered family must be eligible for the streaming runtime")
	require.False(t, ineligible.StreamingEligible(),
		"a mixture-of-experts model absent from the streaming roster must NOT be "+
			"eligible: inferring eligibility from architecture offers a model the "+
			"runtime cannot serve (D1)")
}

// TestRosterAdmitsByNameOnly proves the roster is a lookup of a declared name,
// so adding a supported family is a data change and never a code change.
func TestRosterAdmitsByNameOnly(t *testing.T) {
	roster := testdata.StreamingRoster()

	require.True(t, roster.Admits(testdata.StreamingFamilyDeepSeekR1))
	require.False(t, roster.Admits(testdata.StreamingFamilyQwen3MoE),
		"the roster is closed: a family it does not name is not admitted")
	require.False(t, roster.Admits(""), "an unnamed family is never admitted")
	require.False(t, roster.Admits("mixture-of-experts"),
		"an architecture is not a family name and must never match the roster")

	// A resolved membership carries the name that was looked up, so a reader can
	// tell "looked up, not listed" from "never looked up".
	missed := roster.Membership(testdata.StreamingFamilyQwen3MoE)
	require.Equal(t, testdata.StreamingFamilyQwen3MoE, missed.FamilyName)
	require.False(t, missed.Listed)
	require.False(t, missed.Eligible())

	hit := roster.Membership(testdata.StreamingFamilyDeepSeekR1)
	require.True(t, hit.Listed)
	require.True(t, hit.Eligible())

	// Resolving the fixtures through the roster reproduces the membership each
	// fixture already records — the fixtures are not asserting something the
	// roster data contradicts.
	for _, entry := range []catalogue.Entry{
		testdata.StreamingRosterMemberEntry(),
		testdata.StreamingIneligibleMoEEntry(),
	} {
		resolved := roster.Membership(entry.StreamingRoster.FamilyName)
		require.Equal(t, entry.StreamingEligible(), resolved.Eligible(),
			"fixture %q records an eligibility the roster does not support", entry.Identity())
	}
}

// TestRosterAddingAFamilyIsADataChange shows the same unrostered entry becoming
// eligible when the runtime's declared set grows — with no change to the entry
// and none to this package's code.
func TestRosterAddingAFamilyIsADataChange(t *testing.T) {
	ineligible := testdata.StreamingIneligibleMoEEntry()
	require.False(t, testdata.StreamingRoster().Admits(ineligible.StreamingRoster.FamilyName))

	widened := catalogue.NewRoster(
		testdata.StreamingFamilyDeepSeekR1,
		testdata.StreamingFamilyQwen3MoE,
	)
	require.Equal(t, 2, widened.Len())

	ineligible.StreamingRoster = widened.Membership(ineligible.StreamingRoster.FamilyName)
	require.True(t, ineligible.StreamingEligible())
	require.Equal(t, catalogue.ArchitectureMixtureOfExperts, ineligible.Architecture,
		"architecture did not change; only the roster data did")
}

// TestNonCommercialTermsNameTheRestrictingTerm proves a caller withholding this
// entry can name WHICH term restricted it — the difference between a usable
// explanation and a generic unavailability (FR-055, D4).
func TestNonCommercialTermsNameTheRestrictingTerm(t *testing.T) {
	entry := testdata.NonCommercialEntry()

	require.False(t, entry.UsageTerms.Permits(catalogue.UsageCommercial),
		"a non-commercial licence must not permit commercial use")

	restriction, restricted := entry.UsageTerms.RestrictionFor(catalogue.UsageCommercial)
	require.True(t, restricted, "the caller must be able to obtain the restricting term")
	require.Equal(t, catalogue.TermNonCommercial, restriction.Term,
		"the term named must be the one that actually excludes the usage, not "+
			"merely the first restriction on the entry")
	require.NotEmpty(t, restriction.Reference,
		"a withheld entry must be able to cite the clause it was withheld under")
	require.True(t, restriction.Excludable())

	// The same entry is still offerable for the usages its licence does grant:
	// the terms filter withholds per declared usage, not wholesale.
	for _, permitted := range []catalogue.UsagePurpose{
		catalogue.UsagePersonal,
		catalogue.UsageResearch,
		catalogue.UsageEvaluation,
	} {
		require.True(t, entry.UsageTerms.Permits(permitted),
			"licence grants %q and no restriction excludes it", permitted)
		_, blocked := entry.UsageTerms.RestrictionFor(permitted)
		require.False(t, blocked)
	}

	// Attribution constrains how output is used but withholds nothing, so it
	// must never be reported as a reason an entry was unavailable.
	var attribution catalogue.Restriction
	for _, r := range entry.UsageTerms.Restrictions {
		if r.Term == catalogue.TermAttributionRequired {
			attribution = r
		}
	}
	require.Equal(t, catalogue.TermAttributionRequired, attribution.Term,
		"fixture must carry a second, non-exclusionary restriction")
	require.False(t, attribution.Excludable(),
		"a non-exclusionary term must not be able to withhold an entry")
}

// TestCommercialSafeTermsPermitEveryDeclaredUsage is the counterpart: an entry
// the terms filter never withholds.
func TestCommercialSafeTermsPermitEveryDeclaredUsage(t *testing.T) {
	entry := testdata.CommercialSafeEntry()

	for _, purpose := range []catalogue.UsagePurpose{
		catalogue.UsageCommercial,
		catalogue.UsagePersonal,
		catalogue.UsageResearch,
		catalogue.UsageEvaluation,
	} {
		require.True(t, entry.UsageTerms.Permits(purpose), "purpose %q", purpose)
		_, restricted := entry.UsageTerms.RestrictionFor(purpose)
		require.False(t, restricted, "purpose %q must carry no restriction", purpose)
	}
	require.Empty(t, entry.UsageTerms.Restrictions)
	require.NotEmpty(t, entry.UsageTerms.LicenseID)
}

// TestRevenueCapThresholdIsInspectable proves a capped term carries its bound as
// data, so the caller states the actual ceiling rather than reducing every
// licence to permitted/forbidden.
func TestRevenueCapThresholdIsInspectable(t *testing.T) {
	entry := testdata.RevenueCappedEntry()

	require.False(t, entry.UsageTerms.Permits(catalogue.UsageCommercial))
	restriction, restricted := entry.UsageTerms.RestrictionFor(catalogue.UsageCommercial)
	require.True(t, restricted)
	require.Equal(t, catalogue.TermRevenueCap, restriction.Term)
	require.False(t, restriction.Threshold.Zero(), "a capped term must carry its bound")
	require.Equal(t, uint64(1_000_000), restriction.Threshold.Value)
	require.Equal(t, "USD", restriction.Threshold.Currency)
	require.NotEmpty(t, restriction.Threshold.Period)

	// An uncapped licence carries no bound to read.
	safe := testdata.CommercialSafeEntry()
	_, capped := safe.UsageTerms.RestrictionFor(catalogue.UsageCommercial)
	require.False(t, capped)
}

// TestUngrantedUsageIsNotAFabricatedRestriction guards the boundary between "the
// licence forbids this" and "the licence never granted this". Reporting the
// second as the first invents a restriction the licence does not state.
func TestUngrantedUsageIsNotAFabricatedRestriction(t *testing.T) {
	entry := testdata.NonCommercialEntry()
	unknown := catalogue.UsagePurpose("resale")

	require.False(t, entry.UsageTerms.Permits(unknown), "an ungranted purpose is not permitted")
	_, restricted := entry.UsageTerms.RestrictionFor(unknown)
	require.False(t, restricted,
		"no restriction excludes an ungranted purpose; absence of permission is "+
			"not a restricting term and must not be reported as one")
}

// TestStorageRequirementIsIndependentOfMemory pins D2: a model's disk footprint
// is not implied by its memory figure, and the streaming fixture makes that
// impossible to satisfy with a single headroom number.
func TestStorageRequirementIsIndependentOfMemory(t *testing.T) {
	streaming := testdata.StreamingRosterMemberEntry()
	require.Greater(t, streaming.StorageRequiredBytes, streaming.MemoryRequiredBytes*10,
		"the streaming fixture exists because weights vastly exceed memory; a "+
			"single conflated headroom figure would offer a model that cannot be stored")

	// Two entries can require identical memory and very different storage, so
	// memory alone can never stand in for a storage check.
	inMemoryMoE := testdata.StreamingIneligibleMoEEntry()
	require.Equal(t, streaming.MemoryRequiredBytes, inMemoryMoE.MemoryRequiredBytes,
		"fixtures must agree on memory for this comparison to isolate storage")
	require.NotEqual(t, streaming.StorageRequiredBytes, inMemoryMoE.StorageRequiredBytes)

	for _, entry := range testdata.Entries() {
		require.NotZero(t, entry.MemoryRequiredBytes, "entry %q", entry.Identity())
		require.NotZero(t, entry.StorageRequiredBytes, "entry %q", entry.Identity())
	}
}

// TestFixturesAreValid keeps the fixtures honest: a fixture that could not pass
// the catalogue's own validation would prove nothing about real entries.
func TestFixturesAreValid(t *testing.T) {
	entries := testdata.Entries()
	require.NotEmpty(t, entries)

	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		require.NoError(t, entry.Validate(), "fixture %q", entry.Identity())
		require.True(t, entry.Integrity.Complete(),
			"fixture %q must state a value its weight file is verified against", entry.Identity())
		require.True(t, entry.Family.Known())

		_, duplicate := seen[entry.Identity()]
		require.False(t, duplicate, "fixture identity %q is duplicated", entry.Identity())
		seen[entry.Identity()] = struct{}{}
	}
}

// TestValidateRejectsDefectiveEntries mutates a known-good fixture one field at a
// time and asserts the specific defect is reported. Distinct errors matter: a
// missing digest and an unrostered streaming entry have different remedies.
func TestValidateRejectsDefectiveEntries(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*catalogue.Entry)
		wantErr error
	}{
		{
			name:    "no model id",
			mutate:  func(e *catalogue.Entry) { e.ModelID = "" },
			wantErr: catalogue.ErrMissingModelID,
		},
		{
			name:    "unrecorded family",
			mutate:  func(e *catalogue.Entry) { e.Family = catalogue.CapabilityFamily("telepathy") },
			wantErr: catalogue.ErrUnknownFamily,
		},
		{
			name:    "unrecorded runtime",
			mutate:  func(e *catalogue.Entry) { e.Runtime = catalogue.RuntimeKind("guesswork") },
			wantErr: catalogue.ErrUnknownRuntime,
		},
		{
			name:    "no memory requirement",
			mutate:  func(e *catalogue.Entry) { e.MemoryRequiredBytes = 0 },
			wantErr: catalogue.ErrNoMemoryRequirement,
		},
		{
			name:    "storage requirement left to be inferred from memory",
			mutate:  func(e *catalogue.Entry) { e.StorageRequiredBytes = 0 },
			wantErr: catalogue.ErrNoStorageRequirement,
		},
		{
			name:    "no licence",
			mutate:  func(e *catalogue.Entry) { e.UsageTerms.LicenseID = "" },
			wantErr: catalogue.ErrNoLicense,
		},
		{
			name:    "no permitted usage",
			mutate:  func(e *catalogue.Entry) { e.UsageTerms.Permitted = nil },
			wantErr: catalogue.ErrNoPermittedUsage,
		},
		{
			name: "permits what its own restrictions exclude",
			mutate: func(e *catalogue.Entry) {
				e.UsageTerms.Restrictions = append(e.UsageTerms.Restrictions, catalogue.Restriction{
					Term:     catalogue.TermNonCommercial,
					Excludes: []catalogue.UsagePurpose{catalogue.UsageCommercial},
				})
			},
			wantErr: catalogue.ErrContradictoryTerms,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := testdata.CommercialSafeEntry()
			require.NoError(t, entry.Validate(), "fixture must be valid before mutation")

			tt.mutate(&entry)

			err := entry.Validate()
			require.Error(t, err)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// TestValidateRejectsStreamingEntryOffTheRoster is the catalogue-data half of
// D1: an entry the streaming runtime is said to serve, yet absent from that
// runtime's declared roster, is a defect caught here rather than at load time.
func TestValidateRejectsStreamingEntryOffTheRoster(t *testing.T) {
	entry := testdata.StreamingRosterMemberEntry()
	require.NoError(t, entry.Validate())

	entry.StreamingRoster.Listed = false

	err := entry.Validate()
	require.Error(t, err)
	require.ErrorIs(t, err, catalogue.ErrStreamingNotRostered)
	require.Contains(t, err.Error(), entry.Identity(),
		"a catalogue-wide sweep must be able to name the offending entry")
}

// TestIdentityCarriesTheVariant covers the naming value: the variant is part of
// the model's identity, not a detail dropped on the way to the user.
func TestIdentityCarriesTheVariant(t *testing.T) {
	withVariant := testdata.CommercialSafeEntry()
	require.NotEmpty(t, withVariant.Variant)
	require.Equal(t, withVariant.ModelID+":"+withVariant.Variant, withVariant.Identity())

	withoutVariant := testdata.NonCommercialEntry()
	require.Empty(t, withoutVariant.Variant)
	require.Equal(t, withoutVariant.ModelID, withoutVariant.Identity())
}

// Reconciled from TestValidateRejectsDefectiveEntries when the integrity
// requirement moved from Validate to ValidateForAcquisition.
//
// The two cases below previously asserted that Validate refuses an entry with no
// verifiable integrity expectation. That was correct while Validate was the only
// gate, but it made the catalogue unloadable in its ordinary initial state: a
// digest is captured AT first download, so requiring one to load meant nothing
// could load, nothing could be downloaded, and no digest could ever be captured.
//
// The requirement is not weakened — it is enforced at the gate that actually
// precedes touching a weight file. SC-011 governs loading a model FILE, not
// reading the catalogue. These cases are the proof that the check still bites.
func TestValidateForAcquisitionRejectsUnverifiableIntegrity(t *testing.T) {
	sourced := func() catalogue.Entry {
		e := testdata.CommercialSafeEntry()
		e.Source = "https://huggingface.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF"
		return e
	}

	tests := []struct {
		name   string
		mutate func(*catalogue.Entry)
	}{
		{
			name:   "no integrity expectation",
			mutate: func(e *catalogue.Entry) { e.Integrity = catalogue.IntegrityExpectation{} },
		},
		{
			name:   "integrity digest without a size to check first",
			mutate: func(e *catalogue.Entry) { e.Integrity.SizeBytes = 0 },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := sourced()
			tt.mutate(&e)

			// Still perfectly valid to OFFER: the model is researched, its weights
			// are simply not fetched yet.
			require.NoError(t, e.Validate(),
				"an unfetched entry must remain offerable")

			// But never acquirable: fetching weights with nothing to verify them
			// against is the defect SC-011 exists to prevent.
			require.ErrorIs(t, e.ValidateForAcquisition(), catalogue.ErrIncompleteIntegrity,
				"weights must not be fetched with no verifiable expectation")
		})
	}
}
