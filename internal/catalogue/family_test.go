package catalogue_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/stretchr/testify/require"
)

// allFamilies is every capability family the catalogue records. A family added
// to the constant block but omitted from this list will not be checked below, so
// this list is deliberately exhaustive and its length is asserted.
var allFamilies = []catalogue.CapabilityFamily{
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

// TestEveryDeclaredFamilyIsKnown closes the defect that a family constant can be
// declared while its Known() arm is forgotten.
//
// That gap is silent and total: Entry.Validate rejects every entry in the family,
// the loader refuses them, and the user is never offered those models — with no
// message anywhere saying a whole capability went missing. It is the exact
// silent-drop failure the catalogue exists to prevent, one level up.
func TestEveryDeclaredFamilyIsKnown(t *testing.T) {
	require.Len(t, allFamilies, 10, "update this list when a family is added")
	for _, family := range allFamilies {
		require.NotEmpty(t, string(family))
		require.True(t, family.Known(), "family %q is declared but not recognised by Known()", family)
	}
}

// TestUnrecordedFamilyIsNotKnown is the negative control: Known() must
// discriminate, not simply return true.
func TestUnrecordedFamilyIsNotKnown(t *testing.T) {
	for _, family := range []catalogue.CapabilityFamily{"", "txet", "video", "audio", "TEXT"} {
		require.False(t, family.Known(), "family %q must not be recognised", family)
	}
}

// TestVideoGenerationAndAudioClassificationAreDistinctFamilies pins the two
// splits that carry a research decision, so neither is later folded into a
// neighbour as a tidy-up.
//
// Audio classification is processor-viable on every host tier; audio generation
// has no processor-viable option at all. Merged, a host without an accelerator
// must either be offered generation it cannot run, or lose classification it
// can — and the per-family non-empty guarantee (D5) is measured per family, so
// merging also erases the reason one of them is withheld.
func TestVideoGenerationAndAudioClassificationAreDistinctFamilies(t *testing.T) {
	require.NotEqual(t, catalogue.FamilyAudioGeneration, catalogue.FamilyAudioClassification)
	require.NotEqual(t, catalogue.FamilyImageGeneration, catalogue.FamilyVideoGeneration)
	require.True(t, catalogue.FamilyVideoGeneration.Known())
	require.True(t, catalogue.FamilyAudioClassification.Known())

	// Distinctness must be visible in the recorded values too: these keys reach
	// data files and consumer-facing composition.
	seen := map[string]bool{}
	for _, family := range allFamilies {
		require.False(t, seen[string(family)], "family key %q is duplicated", family)
		seen[string(family)] = true
	}
}

// TestVideoOutputIsRecordedInItsOwnTerms — a video option's capability is a
// frame size, a frame count and a rate. Expressed as tokens per second it is not
// merely imprecise, it is a different quantity, and FR-005 requires options to be
// comparable on evidence rather than on a coerced number.
func TestVideoOutputIsRecordedInItsOwnTerms(t *testing.T) {
	capability := catalogue.ExpectedCapability{
		Modalities: []string{"text", "image"},
		Video: catalogue.VideoOutput{
			FrameWidth:      832,
			FrameHeight:     480,
			FrameCount:      49,
			FramesPerSecond: 16,
		},
	}
	require.True(t, capability.Video.Recorded())
	require.InDelta(t, 3.0625, capability.Video.DurationSeconds(), 0.0001,
		"duration is derived from frames and rate, so it cannot drift from them")

	// A text option records no video output, and must not read as a zero-length clip.
	require.False(t, catalogue.ExpectedCapability{ContextTokens: 32768}.Video.Recorded())
	require.Zero(t, catalogue.VideoOutput{}.DurationSeconds())
}
