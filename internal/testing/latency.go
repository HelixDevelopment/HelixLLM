package testing

// Relative latency budgets, and the honest skip when the host cannot measure.
//
// # Why a fixed wall-clock ceiling was the wrong assertion
//
// `Concurrent Request Stress/500_concurrent_model_list` asserted that the
// slowest of 500 concurrent GET /v1/models stayed under a fixed 1000ms. On a
// shared machine that assertion measures the machine at least as much as the
// code, and it was measured doing exactly that — three runs against two
// binaries, one of which had a fix the endpoint's code path never touches:
//
//	run1 OLD  FAIL 1742.7ms   run1 NEW  FAIL 1037.3ms
//	run2 OLD  pass            run2 NEW  FAIL 1135.9ms
//	run3 OLD  FAIL 1016.7ms   run3 NEW  pass
//
// It flipped green/red run to run on BOTH binaries. A threshold that reports
// a different verdict for the same code depending on what else the host is
// doing is not a budget; it is a coin flip that costs somebody an
// investigation every time it lands red.
//
// Raising the ceiling until today's run passes is not a fix either. It
// re-arms on the next slower host and, worse, a ceiling generous enough to
// survive a loaded machine is too generous to catch the regression the test
// exists for.
//
// # What the test is actually for
//
// Two different kinds of claim live in this step, and they must not be
// treated alike — the same split `internal/lifecycle/evict_test.go` draws
// between an invariant and a scheduling precondition:
//
//   - The INVARIANT is a property of the CODE: under 500 simultaneous
//     requests every one is answered 200 — nothing is dropped, refused, or
//     answered 5xx. It is asserted on every attempt and is never skipped or
//     retried away.
//
//   - The BUDGET is a property of the CODE relative to the HOST: serving 500
//     at once should not cost disproportionately more per request than
//     serving one. Expressed as a ratio against a serial, uncontended
//     baseline of the SAME request measured moments earlier, it survives a
//     slow host — because a host that halves the concurrent throughput
//     halves the baseline too — while still catching a real concurrency
//     regression, which inflates the ratio and not the baseline.
//
//   - The PRECONDITION is a property of the HOST alone: the baseline itself
//     has to be steady enough for a ratio against it to mean anything. When
//     the uncontended series is already varying by more than the whole
//     budget, this host cannot distinguish a concurrency effect from its own
//     noise. That is neither a pass nor a defect, and it is reported as a
//     SKIP that quotes the measured numbers — never as a failure blamed on
//     the code, and never as a pass.

import (
	"context"
	"fmt"
)

// BaselineSpec asks for a short SERIAL, uncontended reference series of the
// step's own request, taken immediately before the real run.
type BaselineSpec struct {
	// Samples is how many serial requests to take. Enough for a median and a
	// spread to mean something, few enough to stay cheap.
	Samples int `yaml:"samples"`

	// MaxSpread is how unsteady the UNCONTENDED series may be — slowest
	// divided by median — before this host is declared unable to measure a
	// concurrency effect at all, and the step SKIPS instead of returning a
	// verdict it cannot support.
	//
	// It is deliberately a separate number from the concurrency budget. The
	// first draft of this file reused the budget for both, on the reasoning
	// that noise wider than the whole budget drowns the signal. Measurement
	// killed that: on this host the uncontended median of GET /v1/models is
	// 0.3-1.3ms, so its spread runs 1.1x-3.1x while the concurrency budget
	// is in the low tens — one number cannot be both, and using the budget
	// as the spread limit would have made the skip unreachable.
	MaxSpread float64 `yaml:"max_spread"`
}

// Baseline sample-count bounds. Below the minimum a median is not a median;
// above the maximum the reference series costs more than the step it serves.
const (
	minBaselineSamples = 3
	maxBaselineSamples = 50
)

// validateBaseline rejects an unusable baseline spec at LOAD time.
func validateBaseline(b *BaselineSpec) error {
	if b == nil {
		return nil
	}
	if b.Samples < minBaselineSamples || b.Samples > maxBaselineSamples {
		return fmt.Errorf("baseline.samples must be between %d and %d, got %d",
			minBaselineSamples, maxBaselineSamples, b.Samples)
	}
	if b.MaxSpread <= 1 {
		return fmt.Errorf(
			"baseline.max_spread must be greater than 1 (slowest / median of the "+
				"uncontended series); got %v, which no real series can satisfy",
			b.MaxSpread)
	}
	return nil
}

// skipError is returned by an assertion whose PRECONDITION could not be met
// on this host. evaluateStep turns it into a skipped step — but only after
// every other assertion has been evaluated, so an invariant breach always
// wins over a skip and can never be skipped away.
type skipError struct{ reason string }

func (e skipError) Error() string { return e.reason }

// baselineWarmupRequests is how many serial requests are issued and DISCARDED
// before the measured series begins.
//
// This is not padding. Without it the first sample pays the TLS handshake and
// connection setup that every later sample reuses, and on this project's own
// dev server that one sample dominated everything: measured against
// GET /v1/models, a cold baseline spread 27x-183x (slowest / median) while a
// warm one spread 1.1x-1.8x. Left uncorrected it read as "host too loaded to
// measure" on ten runs out of ten, on a host that was measuring fine.
//
// Excluding one-off connection setup from the DENOMINATOR is also the honest
// choice for what the ratio claims: it compares concurrency cost against
// steady-state per-request service time. The NUMERATOR keeps its handshakes,
// because 500 simultaneous clients really do each open a connection and that
// cost is part of what serving them concurrently costs.
const baselineWarmupRequests = 3

// collectBaseline issues the step's request serially, with no concurrency, to
// establish what this host does with the endpoint when nothing is contending
// for it. Warm-up requests are issued first and dropped.
func (r *Runner) collectBaseline(ctx context.Context, step ChallengeStep) []httpSample {
	if step.Baseline == nil {
		return nil
	}
	for i := 0; i < baselineWarmupRequests; i++ {
		r.doRequest(ctx, step.http)
	}
	out := make([]httpSample, step.Baseline.Samples)
	for i := range out {
		out[i] = r.doRequest(ctx, step.http)
	}
	return out
}

// evalConcurrentLatencyRatio checks the step's p99 against a multiple of the
// baseline median, and skips when the baseline is too unsteady to compare
// against.
//
// The skip threshold is not a second, independently-invented number: it is
// the SAME limit the assertion declares. The reasoning is that if an
// uncontended series already spreads wider than the entire budget allowed
// for contention, then any ratio measured under contention is dominated by
// host noise and proves nothing either way.
func evalConcurrentLatencyRatio(step ChallengeStep, a Assertion, samples, baseline []httpSample) error {
	limit, ok := numericOf(a.Max, a.Value, a.Expected)
	if !ok {
		return fmt.Errorf("no numeric ratio limit given (`max:`)")
	}
	if step.Baseline == nil || len(baseline) == 0 {
		return fmt.Errorf(
			"assertion needs a serial reference series but the step declares no " +
				"`baseline:` block; add `baseline: {samples: N, max_spread: X}`")
	}

	base := medianMS(baseline)
	if base <= 0 {
		return skipError{reason: fmt.Sprintf(
			"uncontended baseline median measured as %.3fms, at or below this host's "+
				"timer resolution — a ratio against it is arithmetic, not evidence. "+
				"Neither a pass nor a defect. The per-request invariants were still "+
				"checked and held.", base)}
	}

	// PRECONDITION (a property of the HOST): is the uncontended series steady
	// enough to compare against? When it is not, no concurrency verdict is
	// supportable and saying so is the honest answer.
	spread := maxLatencyMS(baseline) / base
	if spread > step.Baseline.MaxSpread {
		return skipError{reason: fmt.Sprintf(
			"host too loaded to measure concurrency: the UNCONTENDED baseline "+
				"(%d serial samples) already spread %.1fx (median %.2fms, slowest "+
				"%.2fms) against a %.1fx stability limit. A concurrency ratio measured "+
				"on top of that noise is neither a pass nor a defect. The "+
				"every-response-is-200 invariant was still checked and held. Re-run on "+
				"a quieter host.",
			len(baseline), spread, base, maxLatencyMS(baseline),
			step.Baseline.MaxSpread)}
	}

	// BUDGET (a property of the CODE, relative to this host): how much did
	// serving them all at once cost, compared with serving the same number one
	// after another at the uncontended median?
	//
	//	serialEquivalent = N x baseline median   -- what N requests cost serially
	//	overhead         = p99 / serialEquivalent
	//
	// A server with no concurrency at all scores about 1.0; real parallelism
	// pulls it below 1; accept/TLS/scheduling costs push it above. What it does
	// NOT do is move when the host gets slower, because a host that doubles the
	// concurrent p99 doubles the baseline median too. That is the whole reason
	// this replaced a fixed wall-clock ceiling.
	n := len(samples)
	serialEquivalent := base * float64(n)
	got := percentileMS(samples, 0.99)
	overhead := got / serialEquivalent
	if overhead > limit {
		return fmt.Errorf(
			"concurrency overhead %.2fx: p99 of %d concurrent samples is %.1fms, "+
				"against %.1fms for the same %d served one at a time at the uncontended "+
				"median of %.2fms (%d serial samples, spread %.1fx) — over the %.1fx budget",
			overhead, n, got, serialEquivalent, n, base, len(baseline), spread, limit)
	}
	return nil
}

// medianMS is the median observed latency across samples, in milliseconds.
func medianMS(samples []httpSample) float64 {
	return percentileMS(samples, 0.5)
}
