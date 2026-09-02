package catalogue_test

import (
	"os"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/stretchr/testify/require"
)

// A family whose every entry forbids commercial use serves a commercial caller
// NOTHING. That is a correct refusal — the licences really do exclude it — but
// it is an empty family, and an empty family is exactly what the per-family
// guarantee exists to surface rather than let pass quietly.
//
// This was real: for a period, the only loading text-to-speech entry was xtts-v2
// under CPML — non-commercial, and unlicensable at all since its vendor shut
// down. Nothing failed. Nothing warned. A commercial caller asking for speech
// synthesis simply received an empty set, and the catalogue looked healthy.
//
// This test names the families where a commercially-usable option is expected to
// exist and fails when one stops existing. It is deliberately a list rather than
// "every family": some families genuinely may have no commercially-safe option
// yet, and asserting otherwise would be asserting a wish.
func TestFamiliesWithACommerciallyUsableOption(t *testing.T) {
	if _, err := os.Stat("data"); os.IsNotExist(err) {
		t.Skip("no data/ directory in this checkout")
	}
	cat, err := catalogue.Load("data")
	require.NoError(t, err)

	// Families where a commercially-safe option is known to exist and whose
	// disappearance would be a regression, not a fact about the world.
	expected := []catalogue.CapabilityFamily{
		catalogue.FamilyText,
		catalogue.FamilyVision,
		catalogue.FamilyImageGeneration,
		catalogue.FamilySpeechToText,
		catalogue.FamilyTextToSpeech,
		catalogue.FamilyVideoGeneration,
	}

	usable := map[catalogue.CapabilityFamily][]string{}
	for _, e := range cat.Entries() {
		if e.UsageTerms.Permits(catalogue.UsageCommercial) {
			usable[e.Family] = append(usable[e.Family], e.ModelID)
		}
	}

	for _, f := range expected {
		t.Run(string(f), func(t *testing.T) {
			require.NotEmpty(t, usable[f],
				"family %q offers a commercial caller NOTHING: every loading entry "+
					"excludes commercial use. The refusal is correct, but the family is "+
					"empty, and that is a state worth knowing about rather than discovering", f)
		})
	}
	t.Logf("commercially-usable options per family: %v", usable)
}
