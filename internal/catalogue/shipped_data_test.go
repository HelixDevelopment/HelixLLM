package catalogue_test

import (
	"os"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/stretchr/testify/require"
)

// The SHIPPED catalogue must load. Every other test in this package uses
// testdata/, so the real data/ directory was never exercised — which is precisely
// how four mutually incompatible file shapes landed there and sat unnoticed. A
// loader proven correct against fixtures says nothing about the data actually
// shipped beside it.
func TestShippedCatalogueLoads(t *testing.T) {
	if _, err := os.Stat("data"); os.IsNotExist(err) {
		t.Skip("no data/ directory in this checkout")
	}

	cat, err := catalogue.Load("data")
	require.NoError(t, err, "the shipped catalogue must load")
	require.NotEmpty(t, cat.Entries(), "a catalogue that loads but offers nothing is not loaded")

	families := map[catalogue.CapabilityFamily]int{}
	withSource := 0
	for _, e := range cat.Entries() {
		require.NoError(t, e.Validate(), "shipped entry %s must be offerable", e.Identity())
		families[e.Family]++
		if e.Source != "" {
			withSource++
		}
	}
	t.Logf("shipped catalogue: %d entries across %d families, %d carrying a source",
		len(cat.Entries()), len(families), withSource)

	require.Greater(t, len(families), 1,
		"a single-family catalogue means the other family files failed to contribute")
}
