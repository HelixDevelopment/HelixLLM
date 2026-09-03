package testing

// Hermetic coverage for the relative latency budget.
//
// These drive real HTTP against a server whose timing this test dictates, so
// each case asserts a decision the harness makes rather than a number this
// host happened to produce. The margins are deliberately wide: a test written
// to cure a flake that fails when the machine is busy has not cured anything.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// timedServer answers every request after a delay chosen by delayFor, which
// receives the 1-based ordinal of the request. Counting requests is what lets
// a test place a spike inside the serial baseline, or slow only the
// concurrent phase, without any sleeping in the test itself.
func timedServer(t *testing.T, status int, delayFor func(n int64) time.Duration) *httptest.Server {
	t.Helper()
	var seq atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if d := delayFor(seq.Add(1)); d > 0 {
			time.Sleep(d)
		}
		w.WriteHeader(status)
		w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// latencyBank writes a bank whose single step measures a baseline and then
// fires `concurrent` requests, with the given budget and stability limit.
func latencyBank(t *testing.T, maxRatio, maxSpread float64, concurrent int) string {
	t.Helper()
	return writeBankFile(t, fmt.Sprintf(`
name: latency-bank
steps:
  - name: concurrent_burst
    method: GET
    path: /v1/models
    concurrent: %d
    baseline:
      samples: 12
      max_spread: %v
    assertions:
      - type: status
        value: 200
      - type: concurrent_latency_ratio
        max: %v
`, concurrent, maxSpread, maxRatio))
}

// latencyBankRatioFirst is latencyBank with the assertion order reversed, so
// the host-dependent ratio is evaluated BEFORE the every-response-is-200
// invariant. Ordering matters to what a test can prove: with the invariant
// first it fails and returns before the skip is ever considered, so a test
// written that way passes whether or not skip precedence is implemented at
// all. Putting the ratio first is what actually exercises the rule that a
// skip must never mask a failure — confirmed by the paired mutation, which
// this ordering catches and the other does not.
func latencyBankRatioFirst(t *testing.T, maxRatio, maxSpread float64, concurrent int) string {
	t.Helper()
	return writeBankFile(t, fmt.Sprintf(`
name: latency-bank-ratio-first
steps:
  - name: concurrent_burst
    method: GET
    path: /v1/models
    concurrent: %d
    baseline:
      samples: 12
      max_spread: %v
    assertions:
      - type: concurrent_latency_ratio
        max: %v
      - type: status
        value: 200
`, concurrent, maxSpread, maxRatio))
}

// runLatencyBank executes the bank against srv and returns the single result.
func runLatencyBank(t *testing.T, srvURL, bank string) ChallengeResult {
	t.Helper()
	r := NewRunner(srvURL)
	require.NoError(t, r.LoadBank(bank))
	results := r.RunAll(t.Context())
	require.Len(t, results, 1)
	return results[0]
}

// baselineOrdinals: the first baselineWarmupRequests are discarded warm-ups,
// so the measured serial series occupies the next 12 ordinals.
const (
	firstMeasuredBaseline = baselineWarmupRequests + 1
	lastMeasuredBaseline  = baselineWarmupRequests + 12
)

// TestConcurrentLatencyRatio_UnsteadyBaselineSkipsRatherThanFails is the
// whole reason the fixed 1000ms ceiling was replaced. When the uncontended
// series is too unsteady to divide by, the honest answer is "this host could
// not measure it" — not a failure attributed to code that did nothing wrong.
func TestConcurrentLatencyRatio_UnsteadyBaselineSkipsRatherThanFails(t *testing.T) {
	// One 300ms spike inside the measured serial series against a ~0ms
	// median: a spread far past any plausible stability limit.
	srv := timedServer(t, http.StatusOK, func(n int64) time.Duration {
		if n == firstMeasuredBaseline+5 {
			return 300 * time.Millisecond
		}
		return 0
	})

	res := runLatencyBank(t, srv.URL, latencyBank(t, 12, 8, 8))

	require.Equal(t, StatusSkipped, res.Status,
		"an unsteady host must skip, not fail: attributing host noise to the code "+
			"is the exact flake this budget replaced")
	reason := firstSkipReason(res.Steps)
	require.Contains(t, reason, "host too loaded to measure concurrency")
	require.Contains(t, reason, "UNCONTENDED baseline",
		"the skip must quote the measurement it made, not merely assert a verdict")
	require.Contains(t, reason, "invariant was still checked and held",
		"the skip must say which claim still ran, so it is not read as blanket silence")
}

// TestConcurrentLatencyRatio_DisproportionateCostFails proves the budget
// still catches something. Only the concurrent phase is slowed, so the
// baseline stays clean and the ratio is the only thing that moves.
func TestConcurrentLatencyRatio_DisproportionateCostFails(t *testing.T) {
	srv := timedServer(t, http.StatusOK, func(n int64) time.Duration {
		if n > lastMeasuredBaseline {
			return 400 * time.Millisecond // concurrent phase only
		}
		return 0
	})

	res := runLatencyBank(t, srv.URL, latencyBank(t, 12, 8, 8))

	require.Equal(t, StatusFailed, res.Status,
		"a server that costs hugely more under concurrency must still fail; a "+
			"budget that cannot fail is not a budget")
	require.Contains(t, res.Error, "concurrency overhead")
	require.Contains(t, res.Error, "over the 12.0x budget")
}

// TestConcurrentLatencyRatio_SteadyServerPasses guards the other direction: a
// server that behaves must not be reported broken.
func TestConcurrentLatencyRatio_SteadyServerPasses(t *testing.T) {
	// Uniform 2ms everywhere. Serial-equivalent for 8 requests is ~16ms and
	// the concurrent p99 is a few ms, so the ratio sits far under budget.
	// max_spread is loose here on purpose: this case is about the BUDGET, and
	// letting host noise skip it would prove nothing either way.
	srv := timedServer(t, http.StatusOK, func(int64) time.Duration {
		return 2 * time.Millisecond
	})

	res := runLatencyBank(t, srv.URL, latencyBank(t, 12, 200, 8))

	require.Equal(t, StatusPassed, res.Status,
		"steady server reported %q: %s", res.Status, res.Error+firstSkipReason(res.Steps))
}

// TestInvariantFailureWinsOverHostSkip is the ordering rule, and it is the
// one that keeps a skip from becoming a hiding place. A host too noisy to
// measure AND a server answering 500 must report the 500 — the property of
// the code — never the property of the host.
func TestInvariantFailureWinsOverHostSkip(t *testing.T) {
	srv := timedServer(t, http.StatusInternalServerError, func(n int64) time.Duration {
		if n == firstMeasuredBaseline+5 {
			return 300 * time.Millisecond // would skip on its own
		}
		return 0
	})

	// The ratio assertion is declared FIRST here on purpose. With the status
	// invariant first it returns before the skip is reached, and the test
	// passes no matter how precedence is implemented — which it demonstrably
	// did: the paired mutation that makes a skip return immediately left the
	// invariant-first version green.
	res := runLatencyBank(t, srv.URL, latencyBankRatioFirst(t, 12, 8, 8))

	require.Equal(t, StatusFailed, res.Status,
		"the 500 must win over the host skip even when the host-dependent "+
			"assertion is evaluated first: a skip that can mask a broken response "+
			"is worse than the flake it was written to cure. Got %s: %s",
		res.Status, res.Error+firstSkipReason(res.Steps))
	require.Contains(t, res.Error, "500")
}

// TestBaselineDiscardsConnectionWarmup guards the warm-up. The first
// requests a client makes pay connection setup that every later one reuses,
// and on the real dev server that single sample dominated the whole series —
// cold baselines spread 27x-183x where warm ones spread 1.1x-1.8x, reading as
// "host too loaded" on ten runs out of ten. Here the cost is injected into
// exactly the warm-up ordinals: discarded, the series is clean and the step
// passes; counted, it would skip.
func TestBaselineDiscardsConnectionWarmup(t *testing.T) {
	// The ordinal is a LITERAL 3, not baselineWarmupRequests. Deriving it
	// from the constant under test moves the injection point along with the
	// mutation, so setting the warm-up to zero also removed the cost and the
	// test stayed green — proving nothing. Measured: with the constant it
	// passed under mutation; with the literal it fails.
	const injectedSetupCostRequests = 3
	srv := timedServer(t, http.StatusOK, func(n int64) time.Duration {
		if n <= injectedSetupCostRequests {
			return 300 * time.Millisecond // "connection setup"
		}
		return 0
	})

	res := runLatencyBank(t, srv.URL, latencyBank(t, 12, 8, 8))

	require.Equal(t, StatusPassed, res.Status,
		"setup cost on the first requests leaked into the measured baseline and "+
			"skipped a step this host could measure perfectly well: %s",
		res.Error+firstSkipReason(res.Steps))
}

// TestBaselineSpecIsValidatedAtLoad proves an unusable baseline refuses to
// load rather than silently producing a meaningless ratio at run time.
func TestBaselineSpecIsValidatedAtLoad(t *testing.T) {
	for _, tc := range []struct{ name, spec, want string }{
		{"too few samples", "samples: 1\n      max_spread: 8", "baseline.samples must be between"},
		{"too many samples", "samples: 500\n      max_spread: 8", "baseline.samples must be between"},
		{"spread of one", "samples: 12\n      max_spread: 1", "max_spread must be greater than 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bank := writeBankFile(t, fmt.Sprintf(`
name: bad-baseline
steps:
  - name: s
    method: GET
    path: /v1/models
    concurrent: 4
    baseline:
      %s
    assertions:
      - type: status
        value: 200
`, tc.spec))
			err := NewRunner("http://127.0.0.1:1").LoadBank(bank)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestConcurrentLatencyRatioNeedsABaseline proves the assertion cannot be
// declared without the series it divides by — it fails loudly instead of
// treating a missing baseline as a pass.
func TestConcurrentLatencyRatioNeedsABaseline(t *testing.T) {
	srv := timedServer(t, http.StatusOK, func(int64) time.Duration { return 0 })
	bank := writeBankFile(t, `
name: no-baseline
steps:
  - name: s
    method: GET
    path: /v1/models
    concurrent: 4
    assertions:
      - type: concurrent_latency_ratio
        max: 12
`)
	res := runLatencyBank(t, srv.URL, bank)
	require.Equal(t, StatusFailed, res.Status)
	require.Contains(t, res.Error, "declares no `baseline:` block")
}

// TestShippedConcurrentChallengeUsesARelativeBudget guards the shipped bank
// itself: if someone reinstates a fixed wall-clock ceiling on the 500-way
// concurrency step, this says so.
func TestShippedConcurrentChallengeUsesARelativeBudget(t *testing.T) {
	r := NewRunner("http://127.0.0.1:1")
	require.NoError(t, r.LoadBank(filepath.Join("..", "..", "challenges", "banks", "stress", "concurrent.yaml")))

	var step *ChallengeStep
	for _, ch := range r.challenges() {
		if ch.Name != "500_concurrent_model_list" {
			continue
		}
		require.Len(t, ch.Steps, 1)
		step = &ch.Steps[0]
	}
	require.NotNil(t, step, "500_concurrent_model_list is gone from the shipped bank")

	require.NotNil(t, step.Baseline,
		"the step lost its `baseline:` block, so any latency claim it makes is "+
			"absolute again and will flip with host load")

	var sawRatio, sawStatus bool
	for _, a := range step.Assertions {
		switch a.Type {
		case "concurrent_latency_ratio":
			sawRatio = true
		case "status":
			sawStatus = true
		case "response_time_ms", "max_response_time_ms", "max_latency", "response_time_p99_ms":
			t.Fatalf("a fixed wall-clock latency assertion (%s) came back to this step; "+
				"it measures the host, and it was measured flipping green/red run to "+
				"run on two different binaries", a.Type)
		}
	}
	require.True(t, sawRatio, "the relative budget assertion is missing")
	require.True(t, sawStatus,
		"the every-response-is-200 invariant is missing; without it a skip would "+
			"leave the step asserting nothing at all")
}
