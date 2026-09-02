package selection_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/capability/testdata/fixtures"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/selection"
	"github.com/stretchr/testify/require"
)

// These tests pin the ORDER offers come back in, which is part of Select's
// contract rather than each caller's business (see doc.go). The rule is
// cheapest-admissible-first — memory, then storage, then the catalogue
// identity — and it exists because a host serves several models at once: the
// memory the largest admissible option takes is memory the model beside it
// then cannot have. It mirrors select() in container/helix_model_gate.py so
// the Go path and the Python path choose alike on the same host.

// key renders an option the way the Python gate's tiebreak does, with no host
// prefix, so an expected order can be written down without naming a machine.
func key(o selection.Option) string {
	if o.Variant == "" {
		return o.ModelID
	}
	return o.ModelID + ":" + o.Variant
}

// TestOffersAreOrderedCheapestAdmissibleFirst sweeps every fixture host and
// every family: no offer may be cheaper than the offer in front of it. A
// largest-first ordering fails here on the first host that can serve more than
// one model, and the failure names the option that was wrongly preferred.
func TestOffersAreOrderedCheapestAdmissibleFirst(t *testing.T) {
	compared := 0
	for name, host := range fixtures.All() {
		res, err := selection.Select(request(host, catalogue.UsagePersonal))
		if err != nil {
			continue // hosts that are no basis for a choice are covered elsewhere
		}
		for _, fr := range res.Families {
			for i := 1; i < len(fr.Offered); i++ {
				prev, cur := fr.Offered[i-1], fr.Offered[i]
				compared++
				require.LessOrEqual(t, prev.Cost.MemoryRequiredBytes, cur.Cost.MemoryRequiredBytes,
					"host %s, family %s: %s (%d bytes) was preferred over the cheaper %s (%d bytes); "+
						"offers must be ordered cheapest-admissible-first so the models sharing this "+
						"host are left room",
					name, fr.Family, key(prev), prev.Cost.MemoryRequiredBytes,
					key(cur), cur.Cost.MemoryRequiredBytes)

				if prev.Cost.MemoryRequiredBytes != cur.Cost.MemoryRequiredBytes {
					continue
				}
				require.LessOrEqual(t, prev.Cost.StorageRequiredBytes, cur.Cost.StorageRequiredBytes,
					"host %s, family %s: %s (%d bytes of storage) was preferred over the cheaper %s "+
						"(%d bytes of storage) at equal memory; storage breaks the memory tie",
					name, fr.Family, key(prev), prev.Cost.StorageRequiredBytes,
					key(cur), cur.Cost.StorageRequiredBytes)

				if prev.Cost.StorageRequiredBytes != cur.Cost.StorageRequiredBytes {
					continue
				}
				require.Less(t, key(prev), key(cur),
					"host %s, family %s: %s and %s cost the same on both axes, so the catalogue "+
						"identity must order them; otherwise the same host answers differently twice",
					name, fr.Family, key(prev), key(cur))
			}
		}
	}
	require.Positive(t, compared,
		"no fixture host offered two options in one family, so this test compared nothing "+
			"and could not have failed — the fixtures no longer exercise the ordering")
}

// TestCheapestFirstIsTakenOverTheMostCapable is the concrete case the rule was
// decided on. On a host that can serve all three text models, the 6 GiB coder
// model comes first — not the 20 GiB ones — and the two 20 GiB models are
// separated by their storage, not by their names.
//
// Under a largest-first ordering the first offer is deepseek-r1-671b:q4_k_m,
// which is exactly the choice that leaves a co-resident vision model with
// nothing.
func TestCheapestFirstIsTakenOverTheMostCapable(t *testing.T) {
	res, err := selection.Select(request(fixtures.SingleAccelerator(), catalogue.UsagePersonal))
	require.NoError(t, err)

	fr := familyOf(t, res, catalogue.FamilyText)
	require.Equal(t, []string{
		"qwen2.5-coder-7b-instruct:q4_k_m", // 6 GiB — cheapest that genuinely runs
		"qwen3-30b-a3b:q4_k_m",             // 20 GiB, 18.6 GiB of storage
		"deepseek-r1-671b:q4_k_m",          // 20 GiB, but 372 GiB of storage
	}, offeredKeys(fr),
		"the lane takes the first offer, so this order IS the model that gets served")
}

// TestOrderingIsATiebreakNotARelaxation. Cheapest-first ranks among options
// that are already admissible; it can never promote one that was withheld.
// This host ties two models on memory and separates them on storage — the axis
// that is checked separately — and the one that does not fit storage stays out
// of the offers entirely rather than being ordered among them.
func TestOrderingIsATiebreakNotARelaxation(t *testing.T) {
	host := fixtures.SingleAccelerator()
	host.StorageAvailable = 40 * capability.GiB

	res, err := selection.Select(request(host, catalogue.UsagePersonal))
	require.NoError(t, err)

	fr := familyOf(t, res, catalogue.FamilyText)
	require.NotContains(t, offeredKeys(fr), "deepseek-r1-671b:q4_k_m",
		"it ties qwen3-30b-a3b on memory but needs 372 GiB of storage this host does not have; "+
			"a memory tie must not carry an option past the storage check")

	w := withheldFor(t, fr, "deepseek-r1-671b")
	require.Equal(t, selection.ReasonInsufficientResources, w.Reason)
	require.NotNil(t, w.Shortfall)
	require.Equal(t, selection.ResourceStorage, w.Shortfall.Resource,
		"the withholding must name storage, not the memory the two models tie on")

	require.Equal(t, []string{
		"qwen2.5-coder-7b-instruct:q4_k_m",
		"qwen3-30b-a3b:q4_k_m",
	}, offeredKeys(fr), "what remains admissible is still ordered cheapest-first")
}

func offeredKeys(fr selection.FamilyResult) []string {
	keys := make([]string, 0, len(fr.Offered))
	for _, o := range fr.Offered {
		keys = append(keys, key(o))
	}
	return keys
}
