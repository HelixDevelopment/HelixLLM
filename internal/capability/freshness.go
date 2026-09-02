package capability

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Measurement freshness.
//
// A refusal is the moment this package is most visible to a user: they asked
// for a model and were told no. FR-033 says that "no" must rest on a CURRENT
// reading — never on one taken at start-up and carried forward.
//
// The failure mode is quiet. A process measures once at boot, keeps the
// profile, and hours later declines a model on a memory figure from before the
// machine's whole workload changed. Nothing errors; the user is simply told
// something untrue about their own machine. So recency is checked explicitly,
// and the zero-value policy deliberately expires rather than defaulting to
// "never" — a forgotten configuration must fail closed.

// Freshness sentinels.
var (
	// ErrStaleMeasurement means the reading is too old to justify a decision.
	ErrStaleMeasurement = errors.New("capability: measurement is stale, not a basis for a current decision")
	// ErrMeasurementFromFuture means the reading is stamped ahead of now, so
	// its age cannot be established at all.
	ErrMeasurementFromFuture = errors.New("capability: measurement is stamped in the future, its age is unknowable")
)

// DefaultMaxMeasurementAge is the recency callers should ask for when they have
// no stricter requirement of their own: FreshnessPolicy{MaxAge: DefaultMaxMeasurementAge}.
//
// It is short on purpose. Available memory and free storage are the two figures
// selection spends, and both move continuously as other work runs on the host;
// a reading older than this describes a machine that has since changed.
//
// It is NOT what the zero-value policy demands — that one requires a reading
// taken at this instant, so a forgotten configuration fails closed rather than
// silently widening the window.
const DefaultMaxMeasurementAge = 5 * time.Second

// FreshnessPolicy is how recent a reading must be to justify a decision.
//
// The zero value is usable and strict: it demands a reading taken at this
// instant, so a caller that forgets to configure a policy gets the safest
// behaviour rather than an unbounded one.
type FreshnessPolicy struct {
	// MaxAge is the oldest reading accepted, inclusive. Zero means the reading
	// must be taken now.
	MaxAge time.Duration
}

// ValidateBasis reports whether p may justify a resource decision at now.
//
// Two conditions, in order of what they tell the user: the measurement must
// have actually succeeded (ErrNotMeasured — "we could not read your machine"),
// and it must be current (ErrStaleMeasurement — "what we know is out of date").
// Recency never repairs incompleteness, and completeness never excuses age.
func (fp FreshnessPolicy) ValidateBasis(p HostCapabilityProfile, now time.Time) error {
	if err := p.ValidateForSelection(); err != nil {
		return err
	}
	age := p.Age(now)
	if age < 0 {
		return fmt.Errorf("%w: measured at %s, now %s", ErrMeasurementFromFuture,
			p.MeasuredAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	}
	if age > fp.MaxAge {
		return fmt.Errorf("%w: %s old, limit %s", ErrStaleMeasurement, age, fp.MaxAge)
	}
	return nil
}

// IsCurrent reports whether p may justify a decision at now.
func (fp FreshnessPolicy) IsCurrent(p HostCapabilityProfile, now time.Time) bool {
	return fp.ValidateBasis(p, now) == nil
}

// EnsureFresh returns a profile that satisfies the policy, re-measuring the
// host when the one it was given does not.
//
// This is the mechanism that makes FR-033 operational rather than advisory: a
// caller holding a start-up reading cannot accidentally refuse a user on it,
// because asking for a usable profile takes a new reading instead.
func EnsureFresh(ctx context.Context, p HostCapabilityProfile, opts Options, policy FreshnessPolicy) (HostCapabilityProfile, error) {
	if policy.IsCurrent(p, time.Now()) {
		return p, nil
	}
	return Measure(ctx, opts)
}
