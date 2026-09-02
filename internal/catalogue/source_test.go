package catalogue_test

import (
	"errors"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue/testdata"
)

// FR-012 requires an entry to record its source ALONGSIDE its expected integrity
// value. Without the source on the entry, the allowlist gate has to be handed one
// from somewhere else, and SC-011's "no model is obtained from a source outside
// the allowlist" is only half enforceable: the check exists but nothing supplies
// the value it checks.
//
// Source is deliberately NOT required by Validate. An entry can be perfectly
// valid to OFFER while its weights have not been located yet — that is the
// ordinary state of a researched-but-unacquired model. It becomes required at
// acquisition, which is the moment the allowlist actually gates something. That
// split mirrors Validate/ValidateForSelection already in this package.
func TestEntryRecordsItsSource(t *testing.T) {
	e := testdata.CommercialSafeEntry()
	e.Source = "https://huggingface.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF"

	if e.Source == "" {
		t.Fatal("Entry must carry a Source field (FR-012)")
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("an entry with a source must still validate: %v", err)
	}
}

func TestSourcelessEntryIsOfferableButNotAcquirable(t *testing.T) {
	e := testdata.CommercialSafeEntry() // no Source set

	if err := e.Validate(); err != nil {
		t.Fatalf("a sourceless entry must remain valid to offer: %v", err)
	}

	err := e.ValidateForAcquisition()
	if !errors.Is(err, catalogue.ErrNoSource) {
		t.Fatalf("ValidateForAcquisition() = %v, want ErrNoSource — the allowlist "+
			"cannot gate an acquisition whose source is unknown", err)
	}
}

func TestSourcedEntryIsAcquirable(t *testing.T) {
	e := testdata.CommercialSafeEntry()
	e.Source = "https://huggingface.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF"

	if err := e.ValidateForAcquisition(); err != nil {
		t.Fatalf("ValidateForAcquisition() = %v, want nil", err)
	}
}
