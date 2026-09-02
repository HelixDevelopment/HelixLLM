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
	catalogue.FamilyVideoGeneration,
	catalogue.FamilySpeechToText,
	catalogue.FamilyTextToSpeech,
	catalogue.FamilyAudioGeneration,
	catalogue.FamilyAudioClassification,
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
		sortOffered(result.Offered)
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

// sortOffered puts a family's offers in the order a lane should try them: the
// cheapest option that genuinely runs on this host comes first.
//
// This is a TIE-BREAK AMONG ADMISSIBLE OPTIONS and never a relaxation of
// admissibility. Everything in Offered has already passed, separately,
// configuration support, fit on BOTH the memory and the storage axis with the
// responsiveness reserve held back, and the declared usage terms. Ordering
// only decides which of several genuine options is reached for first; it
// cannot promote something that was withheld.
//
// Cheapest-first rather than largest-first because this host does not serve
// one model alone. Several models share one accelerator — that is the whole
// reason internal/vrambroker exists — so memory taken by the largest
// admissible option is memory the vision or coder model beside it then cannot
// have. Picking the biggest thing that fits is how a host ends up unable to
// load the second model. Largest-first optimises one model in isolation;
// cheapest-that-works optimises the machine.
//
// The key mirrors select() in container/helix_model_gate.py exactly — memory,
// then storage, then the catalogue identity — so the same host reaches the
// same decision through the Go path and the Python path. Divergence between
// the two is a defect in whichever one drifted, and this is the seam it would
// show up at.
func sortOffered(offered []Option) {
	sort.SliceStable(offered, func(i, j int) bool {
		a, b := offered[i], offered[j]
		if a.Cost.MemoryRequiredBytes != b.Cost.MemoryRequiredBytes {
			return a.Cost.MemoryRequiredBytes < b.Cost.MemoryRequiredBytes
		}
		if a.Cost.StorageRequiredBytes != b.Cost.StorageRequiredBytes {
			return a.Cost.StorageRequiredBytes < b.Cost.StorageRequiredBytes
		}
		return catalogueKey(a) < catalogueKey(b)
	})
}

// catalogueKey renders an option in the catalogue's own identity form.
//
// It is the tiebreak the Python gate sorts on, and unlike Option.Identity it
// carries no host prefix — so two hosts running the same catalogue break ties
// the same way rather than by what the machines happen to be called.
func catalogueKey(o Option) string {
	if o.Variant == "" {
		return o.ModelID
	}
	return o.ModelID + ":" + o.Variant
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
