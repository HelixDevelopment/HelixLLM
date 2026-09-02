package catalogue_test

import (
	"os"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/stretchr/testify/require"
)

// EX-12: a family can pass every visible check and still be unbootable.
//
// No entry in video.yaml recorded `source:`, so the videogen lane measured the
// host, chose a model, and THEN refused to boot it (exit 24, "records no weight
// source"). Nothing was wrong with the measurement, the selection, or the entry's
// validity — Validate() does not require a source, because an entry is legitimate
// to OFFER before anyone decides where its weights come from. The gap opened
// between "offerable" and "acquirable", which is exactly where nothing was
// looking.
//
// Source is also the field the FR-012/SC-011 acquisition allowlist is checked
// against, so it cannot be substituted from annotations (unvalidated, and
// contractually "carried and shown, never read to make a decision"). Absent a
// source there is nothing to route the lane to, and correctly nothing happens.
//
// WHY A NAMED LIST, NOT "EVERY FAMILY": speech-to-text records no source on any
// entry today, and that is a fact about the data rather than a regression —
// asserting otherwise would fail on a state nobody introduced. This names the
// families where a bootable option is known to exist and fails when one stops
// existing, in the same shape as TestFamiliesWithACommerciallyUsableOption.
func TestFamiliesWithAnAcquirableOption(t *testing.T) {
	if _, err := os.Stat("data"); os.IsNotExist(err) {
		t.Skip("no data/ directory in this checkout")
	}
	cat, err := catalogue.Load("data")
	require.NoError(t, err)

	// Families whose lane can obtain weights today. Losing the last sourced
	// entry in one of these is EX-12 recurring.
	expected := []catalogue.CapabilityFamily{
		catalogue.FamilyText,
		catalogue.FamilyVision,
		catalogue.FamilyImageGeneration,
		catalogue.FamilyVideoGeneration,
		catalogue.FamilyTextToSpeech,
	}

	acquirable := map[catalogue.CapabilityFamily][]string{}
	for _, e := range cat.Entries() {
		if e.Source != "" {
			acquirable[e.Family] = append(acquirable[e.Family], e.ModelID)
		}
	}

	for _, f := range expected {
		t.Run(string(f), func(t *testing.T) {
			require.NotEmptyf(t, acquirable[f],
				"family %q offers no entry recording a weight source: the lane can measure the "+
					"host, choose a model, and then refuse to boot it because there is no validated "+
					"repository to pull from. Nothing errors at selection time — the refusal happens "+
					"one layer down, after the choice looks like it succeeded (EX-12)", f)
		})
	}
	t.Logf("entries recording a weight source, per family: %v", acquirable)
}

// The other half of EX-12, and the reason it is stated as its own assertion: a
// source that PASSES the allowlist and then fails at load is worse than no
// source, because the refusal moves from boot time to download time and the gate
// signed off on it. LTX is deliberately left without one for exactly this reason
// — the obvious URL (huggingface.co/Lightricks/LTX-Video) contains no .gguf at
// all (EX-2) — so this asserts the SHAPE of every source that IS recorded, and
// says nothing about entries that honestly record none.
func TestRecordedSourcesAreRepositoryReferences(t *testing.T) {
	if _, err := os.Stat("data"); os.IsNotExist(err) {
		t.Skip("no data/ directory in this checkout")
	}
	cat, err := catalogue.Load("data")
	require.NoError(t, err)

	const host = "https://huggingface.co/"
	checked := 0
	for _, e := range cat.Entries() {
		if e.Source == "" {
			continue // honestly unsourced; TestFamiliesWithAnAcquirableOption covers the cost
		}
		checked++
		require.Truef(t, len(e.Source) > len(host) && e.Source[:len(host)] == host,
			"entry %s records source %q, which the runtime cannot resolve to a weight "+
				"repository: the acquisition gate would pass it and the load would then fail",
			e.Identity(), e.Source)

		repo := e.Source[len(host):]
		slashes := 0
		for _, c := range repo {
			if c == '/' {
				slashes++
			}
		}
		require.Equalf(t, 1, slashes,
			"entry %s records source %q, which is not an <owner>/<model> repository reference",
			e.Identity(), e.Source)
	}
	require.Positive(t, checked, "no sourced entries were checked; the guard proved nothing")
	t.Logf("checked %d recorded weight sources", checked)
}
