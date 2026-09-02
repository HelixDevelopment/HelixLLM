package catalogue_test

import (
	"errors"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue/testdata"
)

// An entry whose weights have not been fetched yet cannot have a digest: the
// digest is captured AT first download. Requiring one to VALIDATE therefore made
// the catalogue unloadable in its ordinary initial state — no entry could load,
// so no download could be triggered, so no digest could ever be captured. Four
// independently-researched data files hit this simultaneously.
//
// SC-011 says no model FILE is LOADED whose integrity was not verified. It
// governs fetching weights, not reading the catalogue. So the requirement moves
// to the acquisition gate, where it actually bites, and offering stays possible.
// This mirrors Source: valid to offer, not yet valid to acquire.
func TestDigestlessEntryIsOfferableButNotAcquirable(t *testing.T) {
	e := testdata.CommercialSafeEntry()
	e.Source = "https://huggingface.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF"
	e.Integrity = catalogue.IntegrityExpectation{} // not yet fetched

	if err := e.Validate(); err != nil {
		t.Fatalf("a researched-but-unfetched entry must be OFFERABLE: Validate() = %v", err)
	}

	err := e.ValidateForAcquisition()
	if !errors.Is(err, catalogue.ErrIncompleteIntegrity) {
		t.Fatalf("ValidateForAcquisition() = %v, want ErrIncompleteIntegrity — "+
			"weights must never be fetched with nothing to verify them against (SC-011)", err)
	}
}

func TestFullyRecordedEntryIsAcquirable(t *testing.T) {
	e := testdata.CommercialSafeEntry() // fixtures carry a complete integrity expectation
	e.Source = "https://huggingface.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF"

	if err := e.ValidateForAcquisition(); err != nil {
		t.Fatalf("ValidateForAcquisition() = %v, want nil", err)
	}
}
