package selection

import (
	"sort"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
)

// familyOrder is the order families are reported in. It is fixed rather than
// derived from catalogue order so two results over the same host can be
// compared line for line even when the catalogue was loaded differently.
var familyOrder = []catalogue.CapabilityFamily{
	catalogue.FamilyText,
	catalogue.FamilyVision,
	catalogue.FamilyImageGeneration,
	catalogue.FamilySpeechToText,
	catalogue.FamilyTextToSpeech,
	catalogue.FamilyAudioGeneration,
	catalogue.FamilyEmbedding,
	catalogue.FamilyVector,
}

func familyRank(f catalogue.CapabilityFamily) int {
	for i, known := range familyOrder {
		if known == f {
			return i
		}
	}
	// An unrecorded family still gets an answer; it simply sorts last.
	return len(familyOrder)
}

// sortFamilies puts results in the fixed order, breaking ties on the family key
// so unrecorded families are ordered deterministically too.
func sortFamilies(results []FamilyResult) {
	sort.SliceStable(results, func(i, j int) bool {
		ri, rj := familyRank(results[i].Family), familyRank(results[j].Family)
		if ri != rj {
			return ri < rj
		}
		return results[i].Family < results[j].Family
	})
}

// evaluateFamily joins every candidate in one family against the measured host
// and the declared usage.
//
// The result is never silently empty (D5): a family that can be served offers
// something, and a family that cannot states why and names what is missing.
func evaluateFamily(
	family catalogue.CapabilityFamily,
	candidates []catalogue.Entry,
	p capability.HostCapabilityProfile,
	declared catalogue.UsagePurpose,
	reserve Reserve,
) FamilyResult {
	result := FamilyResult{Family: family}

	for _, e := range candidates {
		offered, withheld := evaluateEntry(e, p, declared, reserve)
		if withheld != nil {
			result.Withheld = append(result.Withheld, *withheld)
			continue
		}
		result.Offered = append(result.Offered, *offered)
	}

	if len(result.Offered) > 0 {
		return result
	}

	// Nothing could be offered. A family with no candidates at all is not a
	// gap in the answer — it is an answer: the catalogue records nothing that
	// could serve this request here.
	if len(result.Withheld) == 0 {
		return noCandidatesFamily(family)
	}

	result.Refusal = &FamilyRefusal{
		Family:     family,
		Reason:     closestReason(result.Withheld),
		Candidates: result.Withheld,
	}
	return result
}

// evaluateEntry decides one candidate. Exactly one of the two results is
// non-nil.
//
// Order is deliberate. Configuration comes first because it asks whether this
// host can run the entry at all — a question no quantity of free memory
// answers. Fit comes next, because a model that cannot fit is not a licensing
// question. Terms come last, so an entry withheld for its licence is one that
// genuinely would have run: that is what makes "a different model, or a
// different declared usage" the honest remedy rather than a guess.
func evaluateEntry(
	e catalogue.Entry,
	p capability.HostCapabilityProfile,
	declared catalogue.UsagePurpose,
	reserve Reserve,
) (*Option, *Withheld) {
	base := Withheld{
		ModelID: e.ModelID,
		Variant: e.Variant,
		Family:  e.Family,
	}

	if unsupported := supports(p, e); unsupported != nil {
		base.Reason = ReasonUnsupportedConfiguration
		base.Unsupported = unsupported
		return nil, &base
	}

	headroom, shortfall := fits(p, e, reserve)
	if shortfall != nil {
		base.Reason = ReasonInsufficientResources
		base.Shortfall = shortfall
		return nil, &base
	}

	if exclusion := excludedBy(e.UsageTerms, declared); exclusion != nil {
		base.Reason = ReasonExcludedByUsageTerms
		base.Exclusion = exclusion
		return nil, &base
	}

	return &Option{
		ModelID:      e.ModelID,
		Variant:      e.Variant,
		Identity:     hostQualifiedIdentity(p.HostIdentity, e),
		HostIdentity: p.HostIdentity,
		Family:       e.Family,
		Runtime:      e.Runtime,
		Cost: ResourceCost{
			MemoryRequiredBytes:  e.MemoryRequiredBytes,
			StorageRequiredBytes: e.StorageRequiredBytes,
			RequiresAccelerator:  e.RequiresAccelerator,
		},
		Headroom:   headroom,
		Expected:   e.ExpectedCapability,
		Descriptor: e.Descriptor,
		Terms:      e.UsageTerms,
	}, nil
}

// closestReason picks the family's stated reason from among its candidates'.
//
// The ordering is by how close the candidate came to being offered, because
// that is the remedy worth stating. An entry withheld only by its licence would
// have run on this machine; reporting the family as a hardware problem sends
// the user to spend money that will not help. An entry that cannot run here at
// all is the furthest from an offer, so it only becomes the family's reason
// when nothing came closer.
func closestReason(withheld []Withheld) WithheldReason {
	byCloseness := []WithheldReason{
		ReasonExcludedByUsageTerms,
		ReasonInsufficientResources,
		ReasonUnsupportedConfiguration,
	}
	for _, reason := range byCloseness {
		for _, w := range withheld {
			if w.Reason == reason {
				return reason
			}
		}
	}
	return ReasonUnsupportedConfiguration
}

// noCandidatesFamily answers a family the catalogue records nothing for. The
// missing requirement is named so the answer is actionable, rather than an
// empty list the caller has to interpret.
func noCandidatesFamily(family catalogue.CapabilityFamily) FamilyResult {
	withheld := Withheld{
		Family: family,
		Reason: ReasonUnsupportedConfiguration,
		Unsupported: &Unsupported{
			Requirement: RequirementCatalogueEntry,
			Detail:      string(family),
		},
	}
	return FamilyResult{
		Family: family,
		Refusal: &FamilyRefusal{
			Family:     family,
			Reason:     ReasonUnsupportedConfiguration,
			Candidates: []Withheld{withheld},
		},
	}
}

// hostQualifiedIdentity forms helixllm/<host>/<model>[:<variant>]. This is the
// only place that knows the host, so it is the only place the host-qualified
// identity can be formed (D7).
const identityPrefix = "helixllm"

func hostQualifiedIdentity(host string, e catalogue.Entry) string {
	return identityPrefix + "/" + host + "/" + e.Identity()
}
