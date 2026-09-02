package telemetry

import (
	"errors"
	"math"
	"sync"
	"testing"
	"time"
)

// SC-014: a model that is adequately resourced but too slow to use must be
// detectable from what is recorded. A mean cannot show that: 90 fast requests
// and 10 unusable ones average to a figure that describes neither. The tail is
// what a user feels, so the distribution — not just its centre — is kept.
func TestSummary_KeepsTheDistributionNotJustAMean(t *testing.T) {
	p := NewRecorder()
	base := testTime()
	for i := 0; i < 90; i++ {
		if err := p.RecordAt("slow-model", base.Add(time.Duration(i)*time.Second), 10*time.Millisecond, 50); err != nil {
			t.Fatalf("RecordAt: %v", err)
		}
	}
	for i := 0; i < 10; i++ {
		if err := p.RecordAt("slow-model", base.Add(time.Duration(90+i)*time.Second), 5*time.Second, 50); err != nil {
			t.Fatalf("RecordAt: %v", err)
		}
	}

	s, ok := p.Summary("slow-model")
	if !ok {
		t.Fatal("no summary for a model that served 100 requests")
	}
	if s.Requests != 100 {
		t.Fatalf("requests = %d, want 100", s.Requests)
	}

	// The mean of this set is ~509ms. A p50 near it means the distribution
	// collapsed; a p95/p99 near it means the tail was averaged away.
	mean := s.MeanLatency()
	if mean < 400*time.Millisecond || mean > 600*time.Millisecond {
		t.Fatalf("test fixture wrong: mean = %s, expected ~509ms", mean)
	}
	if s.P50 > 20*time.Millisecond {
		t.Errorf("p50 = %s, want the fast body of the distribution (<=20ms), not the mean %s", s.P50, mean)
	}
	if s.P95 < 4*time.Second {
		t.Errorf("p95 = %s, want the slow tail (>=4s), not the mean %s", s.P95, mean)
	}
	if s.P99 < 4*time.Second {
		t.Errorf("p99 = %s, want the slow tail (>=4s), not the mean %s", s.P99, mean)
	}
	if s.P50 >= s.P95 {
		t.Errorf("p50 %s >= p95 %s: the distribution is flat, so it carries no tail", s.P50, s.P95)
	}
	// Extremes are kept exactly rather than bucketed, so the worst request a
	// user actually waited through is reported as it happened.
	if s.Max != 5*time.Second {
		t.Errorf("max = %s, want exactly 5s", s.Max)
	}
	if s.Min != 10*time.Millisecond {
		t.Errorf("min = %s, want exactly 10ms", s.Min)
	}
}

// Quantiles come out of fixed buckets, so they are upper bounds within a stated
// relative error. That bound is part of the contract: a reader must know the
// figure never understates and by how much it may overstate.
func TestQuantile_IsAnUpperBoundWithinTheStatedError(t *testing.T) {
	for _, v := range []time.Duration{
		1 * time.Microsecond,
		37 * time.Millisecond,
		1500 * time.Millisecond,
		12 * time.Second,
	} {
		p := NewRecorder()
		for i := 0; i < 1000; i++ {
			if err := p.RecordAt("m", testTime(), v, 1); err != nil {
				t.Fatalf("RecordAt: %v", err)
			}
		}
		s, _ := p.Summary("m")
		upper := time.Duration(float64(v) * (1 + QuantileRelativeError))
		for name, got := range map[string]time.Duration{"p50": s.P50, "p95": s.P95, "p99": s.P99} {
			if got < v {
				t.Errorf("%s = %s for a set of %s: a quantile must never understate", name, got, v)
			}
			if got > upper {
				t.Errorf("%s = %s for a set of %s: above the stated %.1f%% bound (%s)",
					name, got, v, QuantileRelativeError*100, upper)
			}
		}
	}
}

// The bound must hold on a spread distribution too, where the quantile lands
// strictly inside the range and the observed maximum cannot stand in for it.
func TestQuantile_BoundHoldsAcrossASpreadDistribution(t *testing.T) {
	p := NewRecorder()
	for ms := 1; ms <= 1000; ms++ {
		if err := p.RecordAt("m", testTime(), time.Duration(ms)*time.Millisecond, 1); err != nil {
			t.Fatalf("RecordAt: %v", err)
		}
	}
	s, _ := p.Summary("m")

	// With values 1..1000ms the true quantiles are exactly the rank-th value.
	for _, tc := range []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"p50", s.P50, 500 * time.Millisecond},
		{"p95", s.P95, 950 * time.Millisecond},
		{"p99", s.P99, 990 * time.Millisecond},
	} {
		upper := time.Duration(float64(tc.want) * (1 + QuantileRelativeError))
		if tc.got < tc.want {
			t.Errorf("%s = %s, true value %s: a quantile must never understate", tc.name, tc.got, tc.want)
		}
		if tc.got > upper {
			t.Errorf("%s = %s, true value %s: above the stated %.1f%% bound (%s)",
				tc.name, tc.got, tc.want, QuantileRelativeError*100, upper)
		}
	}

	// p50 lands well inside the range, so the histogram — not the clamp to the
	// observed maximum — is what produced it. It must be a strict upper bound
	// of the true 500ms, and strictly below the largest request served.
	if s.P50 <= 500*time.Millisecond {
		t.Errorf("p50 = %s, want strictly above the true 500ms (a bucket upper bound)", s.P50)
	}
	if s.P50 >= s.Max {
		t.Errorf("p50 = %s reached the observed maximum %s", s.P50, s.Max)
	}
}

// FR-031: throughput is the rate at which a model produces tokens WHILE it is
// working. Dividing by wall-clock time since the first request would make an
// idle model look slow, which is exactly the misreading this figure exists to
// prevent.
func TestTokensPerSecond_MeasuresServingTimeNotWallClock(t *testing.T) {
	p := NewRecorder()
	base := testTime()
	if err := p.RecordAt("m", base, time.Second, 100); err != nil {
		t.Fatalf("RecordAt: %v", err)
	}
	// An hour of idleness, then one more identical request.
	if err := p.RecordAt("m", base.Add(time.Hour), time.Second, 100); err != nil {
		t.Fatalf("RecordAt: %v", err)
	}

	s, _ := p.Summary("m")
	if s.ServiceTime != 2*time.Second {
		t.Fatalf("service time = %s, want 2s (the time actually spent serving)", s.ServiceTime)
	}

	got := s.TokensPerSecond()
	const want = 100.0
	if math.Abs(got-want) > 0.5 {
		t.Errorf("tokens/s = %.4f, want %.1f", got, want)
	}
	// The wall-clock denominator would be ~3601s, giving ~0.056 tok/s. Guard
	// the denominator explicitly so a change to it cannot pass unnoticed.
	wallClock := float64(s.Tokens) / s.Last.Sub(s.First).Seconds()
	if math.Abs(got-wallClock) < 1.0 {
		t.Errorf("tokens/s = %.4f is the wall-clock rate %.4f: idle time is being counted as serving time",
			got, wallClock)
	}
}

// One request spans no wall-clock window at all. The rate must still be right,
// and must not divide by zero.
func TestTokensPerSecond_IsDefinedForASingleRequest(t *testing.T) {
	p := NewRecorder()
	if err := p.RecordAt("m", testTime(), 500*time.Millisecond, 250); err != nil {
		t.Fatalf("RecordAt: %v", err)
	}
	s, _ := p.Summary("m")
	got := s.TokensPerSecond()
	if math.IsNaN(got) || math.IsInf(got, 0) {
		t.Fatalf("tokens/s = %v for a single request", got)
	}
	if math.Abs(got-500.0) > 0.5 {
		t.Errorf("tokens/s = %.4f, want 500", got)
	}
}

// A request that produced nothing still occupied the model. Excluding its time
// would flatter a model that is failing slowly.
func TestTokensPerSecond_CountsTimeSpentProducingNothing(t *testing.T) {
	p := NewRecorder()
	base := testTime()
	if err := p.RecordAt("m", base, 2*time.Second, 0); err != nil {
		t.Fatalf("RecordAt: %v", err)
	}
	if err := p.RecordAt("m", base.Add(time.Second), time.Second, 100); err != nil {
		t.Fatalf("RecordAt: %v", err)
	}
	s, _ := p.Summary("m")
	if s.ServiceTime != 3*time.Second {
		t.Fatalf("service time = %s, want 3s", s.ServiceTime)
	}
	if got := s.TokensPerSecond(); math.Abs(got-100.0/3.0) > 0.5 {
		t.Errorf("tokens/s = %.4f, want %.4f", got, 100.0/3.0)
	}
}

func TestSummary_OfAModelThatHasServedNothing(t *testing.T) {
	p := NewRecorder()
	if _, ok := p.Summary("never-used"); ok {
		t.Fatal("a model that never served has a summary")
	}
	h, err := p.Model("known-but-idle")
	if err != nil {
		t.Fatalf("Model: %v", err)
	}
	s := h.Summary()
	if s.Requests != 0 {
		t.Errorf("requests = %d, want 0", s.Requests)
	}
	if got := s.TokensPerSecond(); got != 0 {
		t.Errorf("tokens/s = %v, want 0", got)
	}
	if got := s.MeanLatency(); got != 0 {
		t.Errorf("mean = %s, want 0", got)
	}
	if s.P50 != 0 || s.P99 != 0 {
		t.Errorf("quantiles of nothing = %s/%s, want 0", s.P50, s.P99)
	}
}

func TestRecord_RejectsUnusableMeasurements(t *testing.T) {
	p := NewRecorder()
	cases := []struct {
		name    string
		modelID string
		latency time.Duration
		tokens  int64
		want    error
	}{
		{"no model id", "", time.Second, 1, ErrNoModelID},
		{"negative latency", "m", -time.Nanosecond, 1, ErrNegativeLatency},
		{"negative tokens", "m", time.Second, -1, ErrNegativeTokens},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := p.RecordAt(tc.modelID, testTime(), tc.latency, tc.tokens)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
	if _, err := p.Model(""); !errors.Is(err, ErrNoModelID) {
		t.Fatalf("Model(\"\") err = %v, want %v", err, ErrNoModelID)
	}
	// A rejected measurement must not be counted.
	if s, ok := p.Summary("m"); ok && s.Requests != 0 {
		t.Fatalf("rejected measurements were recorded: requests = %d", s.Requests)
	}
}

func TestForget_DropsAnUnloadedModelsHistory(t *testing.T) {
	p := NewRecorder()
	if err := p.RecordAt("m", testTime(), time.Second, 10); err != nil {
		t.Fatalf("RecordAt: %v", err)
	}
	p.Forget("m")
	if _, ok := p.Summary("m"); ok {
		t.Fatal("a forgotten model still has a summary")
	}
}

func TestSummaries_AreOrderedByModel(t *testing.T) {
	p := NewRecorder()
	for _, id := range []string{"zeta", "alpha", "mid"} {
		if err := p.RecordAt(id, testTime(), time.Second, 1); err != nil {
			t.Fatalf("RecordAt(%s): %v", id, err)
		}
	}
	got := p.Summaries()
	want := []string{"alpha", "mid", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("summaries = %d, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ModelID != id {
			t.Fatalf("summaries[%d] = %s, want %s", i, got[i].ModelID, id)
		}
	}
}

// Requests finish on many goroutines at once. Nothing may be lost or
// double-counted, and readers must be able to summarise mid-flight.
func TestRecorder_ConcurrentRecordAndSummarise(t *testing.T) {
	p := NewRecorder()
	const goroutines, perGoroutine = 12, 500

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, err := p.Model("shared")
			if err != nil {
				t.Errorf("Model: %v", err)
				return
			}
			for i := 0; i < perGoroutine; i++ {
				if err := h.RecordAt(testTime(), 5*time.Millisecond, 7); err != nil {
					t.Errorf("RecordAt: %v", err)
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_ = p.Summaries()
		}
	}()
	wg.Wait()

	s, ok := p.Summary("shared")
	if !ok {
		t.Fatal("no summary after concurrent recording")
	}
	if want := uint64(goroutines * perGoroutine); s.Requests != want {
		t.Errorf("requests = %d, want %d", s.Requests, want)
	}
	if want := uint64(goroutines * perGoroutine * 7); s.Tokens != want {
		t.Errorf("tokens = %d, want %d", s.Tokens, want)
	}
	if want := time.Duration(goroutines*perGoroutine) * 5 * time.Millisecond; s.ServiceTime != want {
		t.Errorf("service time = %s, want %s", s.ServiceTime, want)
	}
}

// The observer must not distort what it observes. This benchmark is the
// evidence for what recording one request costs on the serving path.
func BenchmarkModelPerf_RecordAt(b *testing.B) {
	p := NewRecorder()
	h, err := p.Model("bench")
	if err != nil {
		b.Fatalf("Model: %v", err)
	}
	at := testTime()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := h.RecordAt(at, time.Duration(i%1000)*time.Millisecond, 64); err != nil {
			b.Fatal(err)
		}
	}
}

// Cost under contention: many goroutines finishing requests on ONE model, the
// worst case for a per-model lock.
func BenchmarkModelPerf_RecordAtContended(b *testing.B) {
	p := NewRecorder()
	h, err := p.Model("bench")
	if err != nil {
		b.Fatalf("Model: %v", err)
	}
	at := testTime()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			i++
			if err := h.RecordAt(at, time.Duration(i%1000)*time.Millisecond, 64); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// Cost of the whole hot path when the caller does NOT hold a handle and must
// resolve the model by id on every request.
func BenchmarkRecorder_RecordAtByModelID(b *testing.B) {
	p := NewRecorder()
	at := testTime()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := p.RecordAt("bench", at, time.Duration(i%1000)*time.Millisecond, 64); err != nil {
			b.Fatal(err)
		}
	}
}
