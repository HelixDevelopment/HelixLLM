package vrambroker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// class_agent_test.go — RED-first (§11.4.115) coverage for the new ClassAgent
// warm-tier class (Serving-plan Task 1.4 / master plan §6.2 / danger zone D5:
// "if Lane B reuses ClassVLM instead of a new ClassAgent, a future genuine
// vision-serving workload would collide with Lane B for the same broker
// class"). ClassAgent MUST have warm-tier semantics IDENTICAL to ClassVLM —
// IsResident()==false, IsBurst()==false, admission-gated by Budget().free —
// and MUST NEVER be folded into ClassVLM.
//
// This file was authored BEFORE `ClassAgent` existed in broker.go: at that
// point `go build`/`go vet` failed with `undefined: ClassAgent` — the
// compile-time RED for a brand-new enum value (there is no "broken behavior"
// to exercise at runtime yet; the RED is the build failure itself, captured
// in docs/phase1_s1_broker_validator.md). Adding the constant makes this file
// compile and these tests pass — the GREEN.

// TestClassAgent_WarmTierSemantics_MatchesClassVLM asserts ClassAgent is a
// warm class, matching ClassVLM on BOTH discriminating predicates.
func TestClassAgent_WarmTierSemantics_MatchesClassVLM(t *testing.T) {
	require.False(t, ClassAgent.IsResident(), "ClassAgent MUST NOT be a resident (pinned, never-evicted) class")
	require.False(t, ClassAgent.IsBurst(), "ClassAgent MUST NOT be a burst (single-owner) class")

	require.Equal(t, ClassVLM.IsResident(), ClassAgent.IsResident(),
		"ClassAgent's IsResident() MUST match ClassVLM's (both warm-tier)")
	require.Equal(t, ClassVLM.IsBurst(), ClassAgent.IsBurst(),
		"ClassAgent's IsBurst() MUST match ClassVLM's (both warm-tier)")

	// D5 anti-regression: ClassAgent MUST be its OWN class value, never an
	// alias/reuse of ClassVLM — a future real vision workload must not collide
	// with a Lane-B agent instance for the same broker class.
	require.NotEqual(t, ClassVLM, ClassAgent, "ClassAgent MUST be a distinct class from ClassVLM (D5 mitigation)")
}

// TestAcquire_ClassAgent_AdmissionGated_LikeClassVLM mirrors
// TestAcquire_Warm_AdmissionGated (broker_test.go) for the new class: within
// budget -> granted; over budget -> ErrBudgetExceeded, fail-closed; NOT
// single-owner (a ClassAgent lease coexists with a live ClassVLM lease, unlike
// the burst classes).
func TestAcquire_ClassAgent_AdmissionGated_LikeClassVLM(t *testing.T) {
	b := newWithReader(fakeReader(gib(32), gib(20), gib(12), nil))
	ctx := context.Background()

	// Within budget -> granted, identically to ClassVLM's admission path.
	lease, err := b.Acquire(ctx, ClassAgent, gib(3))
	require.NoError(t, err)
	require.NotNil(t, lease)
	require.Equal(t, ClassAgent, lease.Class)

	// A live ClassVLM warm lease coexists with the ClassAgent lease — warm
	// classes are NOT single-owner (only burst classes are, §11.4.119).
	vlm, err := b.Acquire(ctx, ClassVLM, gib(2))
	require.NoError(t, err)
	require.NotNil(t, vlm)

	// Over budget -> refused with ErrBudgetExceeded, same failure mode as
	// ClassVLM/ClassTranslate (see TestAcquire_Warm_AdmissionGated).
	over, err := b.Acquire(ctx, ClassAgent, gib(40))
	require.Nil(t, over)
	require.ErrorIs(t, err, ErrBudgetExceeded)

	lease.Release()
	vlm.Release()
}

// TestClassAgent_PairedMutation_IsResidentFlip is the §1.1 paired mutation for
// ClassAgent's warm-tier discrimination. `acquireDecision` mirrors broker.go's
// Acquire resident-branch decision (broker.go:140-142: "if class.IsResident()
// { return b.grant(...), nil }") using an INJECTED isResident predicate — the
// exact seam a mutation would flip.
//
//   - REAL: ClassAgent.IsResident() == false -> falls through to the
//     budget-gated admission path -> a grossly over-budget request is refused.
//   - MUTATED: ClassAgent.IsResident() flipped to true (folded in with
//     ClassCoder) -> unconditionally granted regardless of measured free VRAM
//     — exactly the D5 defect class this test proves is caught.
//
// The two decisions MUST disagree on the same inputs, proving the test
// actually discriminates resident from warm-tier for ClassAgent (per the
// Serving-plan Task 1.4 acceptance criterion).
func acquireDecision(isResident func(Class) bool, class Class, free, need, headroom int64) bool {
	if isResident(class) {
		return true // resident: always granted, unconditionally (mirrors broker.go's resident branch)
	}
	return admit(free, need, headroom)
}

func TestClassAgent_PairedMutation_IsResidentFlip(t *testing.T) {
	free := gib(1)  // clearly insufficient free VRAM
	need := gib(30) // clearly over-budget request
	hr := HeadroomBytes

	// REAL guard: Class.IsResident is the actual method (a method expression,
	// func(Class) bool) — ClassAgent is warm, so this request MUST be refused.
	real := acquireDecision(Class.IsResident, ClassAgent, free, need, hr)
	require.False(t, real, "ClassAgent MUST be admission-gated (not unconditionally granted) — real IsResident()==false")

	// MUTATION: pretend IsResident() also returns true for ClassAgent (the
	// exact regression this test guards against — reusing/aliasing the
	// resident semantics for the new class). Under the mutation the SAME
	// grossly-over-budget request would be (wrongly) granted, unconditionally.
	mutatedIsResident := func(c Class) bool { return c == ClassCoder || c == ClassAgent }
	mutated := acquireDecision(mutatedIsResident, ClassAgent, free, need, hr)
	require.True(t, mutated, "under the mutation, ClassAgent would be (wrongly) granted unconditionally — proves the guard is load-bearing")

	require.NotEqual(t, real, mutated,
		"real vs. mutated IsResident() decisions MUST disagree on the same over-budget request — proves this test discriminates resident from warm-tier for ClassAgent")
}
