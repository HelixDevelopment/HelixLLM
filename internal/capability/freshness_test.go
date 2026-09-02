package capability_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/capability/testdata/fixtures"
)

// T011 RED — FR-033. A resource refusal must rest on a CURRENT reading, never
// on one taken at start-up.
//
// The failure this guards against is quiet and specific: a process measures
// once at boot, holds that profile, and hours later refuses a model because
// "there is not enough memory" — using a figure from before the machine's whole
// workload changed. The user is told no on the strength of a stale number.

func TestFreshness_AStaleProfileIsNotAValidBasisForARefusal(t *testing.T) {
	policy := capability.FreshnessPolicy{MaxAge: 30 * time.Second}
	stale := fixtures.Staled(fixtures.NoAccelerator(), 10*time.Minute)

	err := policy.ValidateBasis(stale, time.Now())
	if !errors.Is(err, capability.ErrStaleMeasurement) {
		t.Fatalf("ValidateBasis(10-minute-old profile) = %v, want ErrStaleMeasurement", err)
	}
	// The profile is otherwise perfectly valid — that is the point. Structural
	// validity is not recency, and only recency makes a refusal honest.
	if err := stale.ValidateForSelection(); err != nil {
		t.Fatalf("precondition: the stale fixture should still be structurally selectable: %v", err)
	}
}

func TestFreshness_AFreshProfileIsAcceptedAsABasis(t *testing.T) {
	policy := capability.FreshnessPolicy{MaxAge: 30 * time.Second}
	if err := policy.ValidateBasis(fixtures.NoAccelerator(), time.Now()); err != nil {
		t.Errorf("ValidateBasis(fresh profile) = %v, want nil", err)
	}
}

func TestFreshness_BoundaryIsInclusiveOfMaxAge(t *testing.T) {
	policy := capability.FreshnessPolicy{MaxAge: time.Minute}
	base := fixtures.NoAccelerator()
	now := base.MeasuredAt.Add(time.Minute)

	if err := policy.ValidateBasis(base, now); err != nil {
		t.Errorf("a reading exactly MaxAge old was rejected: %v", err)
	}
	if err := policy.ValidateBasis(base, now.Add(time.Nanosecond)); !errors.Is(err, capability.ErrStaleMeasurement) {
		t.Errorf("a reading one nanosecond past MaxAge was accepted: %v", err)
	}
}

func TestFreshness_EveryStaleFixtureIsRefused(t *testing.T) {
	policy := capability.FreshnessPolicy{MaxAge: time.Second}
	for name, p := range fixtures.All() {
		t.Run(name, func(t *testing.T) {
			err := policy.ValidateBasis(fixtures.Staled(p, time.Hour), time.Now())
			if err == nil {
				t.Fatal("an hour-old reading was accepted as a basis for a resource decision")
			}
			// The unmeasurable host is refused for being incomplete; every
			// other host is refused for being stale. Both are refusals, and
			// the distinction is what a caller reports to the user.
			if !p.MeasurementComplete {
				if !errors.Is(err, capability.ErrNotMeasured) {
					t.Errorf("err = %v, want ErrNotMeasured for an incomplete profile", err)
				}
				return
			}
			if !errors.Is(err, capability.ErrStaleMeasurement) {
				t.Errorf("err = %v, want ErrStaleMeasurement", err)
			}
		})
	}
}

func TestFreshness_FreshButIncompleteIsStillRefused(t *testing.T) {
	// Recency does not repair an incomplete measurement. A brand-new reading
	// that failed half its axes is still not something to refuse a user on.
	policy := capability.FreshnessPolicy{MaxAge: time.Hour}
	err := policy.ValidateBasis(fixtures.Unmeasurable(), time.Now())
	if !errors.Is(err, capability.ErrNotMeasured) {
		t.Errorf("ValidateBasis(fresh but incomplete) = %v, want ErrNotMeasured", err)
	}
}

func TestFreshness_AReadingFromTheFutureIsRefused(t *testing.T) {
	// Negative age means the clock moved, so the reading's age is unknowable —
	// and an unknowable age cannot certify a current reading.
	policy := capability.FreshnessPolicy{MaxAge: time.Hour}
	future := fixtures.Staled(fixtures.NoAccelerator(), -time.Hour)
	if err := policy.ValidateBasis(future, time.Now()); err == nil {
		t.Error("a profile stamped an hour in the future was accepted as current")
	}
}

func TestFreshness_ZeroMaxAgeDemandsAReadingTakenNow(t *testing.T) {
	// The strictest caller: nothing older than this instant will do.
	policy := capability.FreshnessPolicy{MaxAge: 0}
	p := fixtures.NoAccelerator()
	if err := policy.ValidateBasis(p, p.MeasuredAt.Add(time.Millisecond)); !errors.Is(err, capability.ErrStaleMeasurement) {
		t.Errorf("MaxAge=0 accepted a one-millisecond-old reading: %v", err)
	}
}

func TestFreshness_EnsureFreshRemeasuresRatherThanReusingAStaleReading(t *testing.T) {
	// This is the mechanism that keeps a start-up reading from ever being the
	// basis of a later refusal: asking for a usable profile re-measures.
	policy := capability.FreshnessPolicy{MaxAge: 30 * time.Second}
	stale := fixtures.Staled(fixtures.NoAccelerator(), time.Hour)

	got, err := capability.EnsureFresh(context.Background(), stale, capability.Options{}, policy)
	if err != nil {
		t.Fatalf("EnsureFresh(): %v", err)
	}
	if got.MeasuredAt.Equal(stale.MeasuredAt) {
		t.Fatal("EnsureFresh returned the stale reading unchanged")
	}
	if err := policy.ValidateBasis(got, time.Now()); err != nil {
		t.Errorf("the profile EnsureFresh returned is still not a valid basis: %v", err)
	}
	if got.HostIdentity == stale.HostIdentity {
		t.Errorf("HostIdentity = %q — the fixture's identity survived, so nothing was actually re-measured", got.HostIdentity)
	}
}

func TestFreshness_EnsureFreshKeepsAProfileThatIsStillCurrent(t *testing.T) {
	policy := capability.FreshnessPolicy{MaxAge: time.Hour}
	fresh := fixtures.NoAccelerator()

	got, err := capability.EnsureFresh(context.Background(), fresh, capability.Options{}, policy)
	if err != nil {
		t.Fatalf("EnsureFresh(): %v", err)
	}
	if !got.MeasuredAt.Equal(fresh.MeasuredAt) {
		t.Error("EnsureFresh re-measured a profile that was still current")
	}
}

func TestFreshness_DefaultPolicyIsUsableWithoutConfiguration(t *testing.T) {
	// A zero-value policy must not silently mean "never expires" — that is the
	// exact behaviour FR-033 forbids.
	var zero capability.FreshnessPolicy
	old := fixtures.Staled(fixtures.NoAccelerator(), 365*24*time.Hour)
	if err := zero.ValidateBasis(old, time.Now()); err == nil {
		t.Error("a zero-value policy accepted a year-old reading as current")
	}
}
