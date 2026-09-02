package selection

import "github.com/HelixDevelopment/HelixLLM/internal/catalogue"

// Pin is a model a user has deliberately chosen.
//
// A pin is a CONSTRAINT on the choice, never a bypass of the measurement
// (FR-056). It narrows the candidate set to one entry; that entry then goes
// through exactly the same configuration, fit and terms checks as any other,
// and is refused — with the insufficient resource named — when the host cannot
// run it. Nothing about a pin makes a model fit.
type Pin struct {
	ModelID string
	// Variant selects among builds of the same model. Empty matches an entry
	// that declares no variant, and also matches by model id alone when the
	// catalogue records exactly one build.
	Variant string
}

// Matches reports whether e is the entry this pin names.
func (p Pin) Matches(e catalogue.Entry) bool {
	if e.ModelID != p.ModelID {
		return false
	}
	if p.Variant == "" {
		return true
	}
	return e.Variant == p.Variant
}

// Identity renders the pinned model in the catalogue's own identity form, so a
// refusal can name what was asked for even when nothing matched it.
func (p Pin) Identity() string {
	if p.Variant == "" {
		return p.ModelID
	}
	return p.ModelID + ":" + p.Variant
}

// constrain narrows entries to those the pin names, preserving order.
func (p Pin) constrain(entries []catalogue.Entry) []catalogue.Entry {
	matched := make([]catalogue.Entry, 0, 1)
	for _, e := range entries {
		if p.Matches(e) {
			matched = append(matched, e)
		}
	}
	return matched
}

// unmatchedFamily is the answer to a pin that names a model the catalogue does
// not record.
//
// It is an unsupported configuration rather than a resource shortfall: there is
// no entry to measure against, so there is no resource to be short of. The
// remedy is a different pin, not a bigger machine — and critically, a name that
// resolves to nothing is refused rather than started (FR-056).
func unmatchedFamily(p Pin, family catalogue.CapabilityFamily) FamilyResult {
	withheld := Withheld{
		ModelID: p.ModelID,
		Variant: p.Variant,
		Family:  family,
		Reason:  ReasonUnsupportedConfiguration,
		Unsupported: &Unsupported{
			Requirement: RequirementCatalogueEntry,
			Detail:      p.Identity(),
		},
	}
	return FamilyResult{
		Family:   family,
		Withheld: []Withheld{withheld},
		Refusal: &FamilyRefusal{
			Family:     family,
			Reason:     ReasonUnsupportedConfiguration,
			Candidates: []Withheld{withheld},
		},
	}
}
