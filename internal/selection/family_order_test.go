package selection

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
)

// familyOrder decides which families a Families-empty request enumerates. It was
// written when the catalogue recorded eight families; two more were added later
// (video-generation when it entered scope, audio-classification when research
// showed it must stay separate from audio-generation because one is
// processor-viable and the other is not).
//
// Neither was added here. The effect is silent: a caller asking for "everything"
// gets no video-generation family at all, even with three video entries loaded —
// no error, no empty family with a stated reason, just an absence. That is the
// one outcome the per-family guarantee exists to prevent.
//
// This test binds the two lists together so the next family added cannot be
// forgotten in the same way.
func TestFamilyOrderCoversEveryRecordedFamily(t *testing.T) {
	recorded := []catalogue.CapabilityFamily{
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

	// Every family the catalogue can hold must be reachable by a request that
	// names no family.
	inOrder := map[catalogue.CapabilityFamily]bool{}
	for _, f := range familyOrder {
		inOrder[f] = true
	}
	for _, f := range recorded {
		if !inOrder[f] {
			t.Errorf("family %q is recorded by the catalogue but absent from familyOrder: "+
				"a Families-empty request will never enumerate it", f)
		}
	}

	// And nothing in the order may be a family the catalogue does not know, which
	// would enumerate a family that can never have entries.
	for _, f := range familyOrder {
		if !f.Known() {
			t.Errorf("familyOrder contains %q, which is not a recorded family", f)
		}
	}
}
