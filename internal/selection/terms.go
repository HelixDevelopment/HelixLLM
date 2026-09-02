package selection

import "github.com/HelixDevelopment/HelixLLM/internal/catalogue"

// excludedBy reports the terms that withhold e for the declared usage, or nil
// when the terms permit it (FR-054).
//
// Two distinct situations both withhold, and the difference matters to whoever
// reads the result:
//
//   - a restriction excludes the declared purpose — the licence has a clause
//     that says so, and the clause can be cited;
//   - the licence simply never granted the purpose — there is no clause to
//     cite, only an absence of permission.
//
// The restriction that EXCLUDED the purpose is the one named. A licence may
// carry restrictions that constrain how output is used without withholding
// anything — attribution, share-alike — and those may appear first in the list.
// Reporting "the first restriction" names a term that had nothing to do with
// the decision, and points the user at a clause they could have complied with.
func excludedBy(terms catalogue.UsageTerms, declared catalogue.UsagePurpose) *Exclusion {
	if terms.Permits(declared) {
		return nil
	}

	exclusion := &Exclusion{
		Purpose:   declared,
		LicenseID: terms.LicenseID,
		Granted:   grants(terms, declared),
	}

	// RestrictionFor walks to the restriction whose Excludes actually names the
	// declared purpose, skipping the non-excluding ones.
	if r, restricted := terms.RestrictionFor(declared); restricted {
		exclusion.Term = r.Term
		exclusion.Threshold = r.Threshold
		exclusion.Reference = r.Reference
	}

	return exclusion
}

// grants reports whether the licence lists declared among its permitted
// purposes, disregarding restrictions. It separates "granted, then excluded by
// a clause" from "never granted at all".
func grants(terms catalogue.UsageTerms, declared catalogue.UsagePurpose) bool {
	for _, granted := range terms.Permitted {
		if granted == declared {
			return true
		}
	}
	return false
}
