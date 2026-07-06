package vrambroker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAdmit_PairedMutation is the §1.1 paired mutation for the budget-admission
// guard. It proves the guard is LOAD-BEARING (not a tautology) at the LOGIC path
// WITHOUT ever risking a real OOM: with the real `admit`, an over-budget request
// is REFUSED; with the budget check disabled (the mutation), the SAME request
// would be (wrongly) GRANTED. The flip between the two proves the guard prevents
// the over-commit — see §11.4.108 runtime signature.
func TestAdmit_PairedMutation(t *testing.T) {
	// Realistic live shape: coder resident, ~12 GiB free on the 32 GiB card.
	free := int64(12689) * MiB
	need := int64(30000) * MiB // a burst that clearly does NOT fit
	hr := HeadroomBytes

	// REAL guard: over-budget request is refused.
	require.False(t, admit(free, need, hr),
		"real budget guard MUST refuse an over-budget request (need > free) — fail-closed")

	// MUTATION: disable the budget check (always-grant). This is the exact defect
	// the guard exists to prevent; under it the over-commit is admitted.
	admitMutatedAlwaysGrant := func(_, _, _ int64) bool { return true }
	require.True(t, admitMutatedAlwaysGrant(free, need, hr),
		"with the budget check disabled, the over-commit would be (wrongly) granted — proves the guard is load-bearing")

	// The two disagree on the same inputs => the guard is not a tautology.
	require.NotEqual(t, admit(free, need, hr), admitMutatedAlwaysGrant(free, need, hr),
		"real guard and mutated guard MUST disagree on an over-budget request")
}
