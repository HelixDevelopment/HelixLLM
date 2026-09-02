package telemetry

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
	"sort"
	"sync"
	"time"
)

// Per-request latency and throughput for each running model (FR-031, SC-014).
//
// The question this answers is not "how fast is the model on average" but "is
// this model usable on this machine". Those differ, and the difference is the
// tail: a model that answers in 40ms nine times out of ten and stalls for six
// seconds on the tenth has a perfectly respectable mean and is unusable. So a
// DISTRIBUTION is kept, not a running average — p50 says what a typical request
// costs, p95/p99 say what the user actually notices.
//
// # What recording costs
//
// This code runs at the end of every request on the serving path, so it is
// built to be invisible:
//
//   - Zero allocations per recorded request. The histogram is a fixed array
//     inside the model's state, sized once when the model is first seen; a
//     record is a bucket index computed with two shifts and a bits.Len64, then
//     integer increments.
//   - Lock scope is per model, not per recorder. Two models never contend, and
//     the critical section holds no loop, no allocation and no call out.
//   - The model handle is resolvable once and reused (Recorder.Model), so a
//     caller that keeps it pays no map lookup at all on the hot path.
//   - Fixed memory per model: histogramBuckets counters plus a handful of
//     scalars, so cost does not grow with request count. Nothing is retained
//     per request, which is also why an unbounded request stream cannot make
//     the observer the thing that exhausts the host it is observing.
//
// The price of a fixed histogram is resolution: quantiles are bucket upper
// bounds, so they never understate and may overstate by at most
// QuantileRelativeError. Minimum and maximum are kept exactly, because the
// worst request a user actually waited through should be reported as it
// happened rather than rounded up.

// Measurement errors. Callers switch on these with errors.Is.
var (
	// ErrNegativeLatency is returned for a request whose measured duration is
	// below zero — a clock went backwards, and the reading means nothing.
	ErrNegativeLatency = errors.New("telemetry: request latency is negative")

	// ErrNegativeTokens is returned for a negative token count.
	ErrNegativeTokens = errors.New("telemetry: request token count is negative")
)

// Histogram shape. subBits sub-buckets per power of two bound the relative
// width of every bucket, and so the error of every quantile derived from it.
const (
	subBits  = 3
	subCount = 1 << subBits

	// histogramBuckets covers every uint64 nanosecond value, so no latency can
	// fall outside the histogram and be silently dropped.
	histogramBuckets = (64-subBits)*subCount + subCount

	// QuantileRelativeError is the most a reported quantile may exceed the
	// true value. Quantiles are bucket upper bounds, so they never understate;
	// this is the whole of the error in the other direction.
	QuantileRelativeError = 1.0 / float64(subCount)
)

// bucketIndex maps a nanosecond latency to its histogram bucket. Values below
// subCount are their own bucket, so sub-microsecond latencies are exact.
func bucketIndex(v uint64) int {
	if v < subCount {
		return int(v)
	}
	exp := bits.Len64(v) - 1
	mantissa := v >> uint(exp-subBits) // in [subCount, 2*subCount)
	return (exp-subBits)*subCount + int(mantissa-subCount) + subCount
}

// bucketUpperBound is the largest value that falls in bucket i. Reporting this
// value is what makes a quantile an upper bound of the truth.
func bucketUpperBound(i int) uint64 {
	if i < subCount {
		return uint64(i)
	}
	j := i - subCount
	exp := subBits + j/subCount
	sub := j % subCount
	shift := uint(exp - subBits)
	low := uint64(subCount+sub) << shift
	return low + (uint64(1) << shift) - 1
}

// PerfSummary is one model's serving performance over everything recorded for
// it since it started running.
type PerfSummary struct {
	ModelID string

	// Requests and Tokens are what the model has served and produced.
	Requests uint64
	Tokens   uint64

	// ServiceTime is the total time the model spent producing those tokens —
	// the sum of the per-request latencies, NOT wall-clock time since it
	// started. It is the denominator of TokensPerSecond, and the distinction
	// matters: wall clock includes idleness, which is not slowness.
	ServiceTime time.Duration

	// First and Last are when the earliest and latest recorded requests were
	// served. They date the sample so a reader can see whether it is current;
	// they are deliberately not used to compute a rate.
	First time.Time
	Last  time.Time

	// Min and Max are exact extremes, not bucketed.
	Min time.Duration
	Max time.Duration

	// P50, P95 and P99 are the latency distribution. They are upper bounds
	// within QuantileRelativeError. P50 is what a request typically costs;
	// P95 and P99 are what makes a model feel broken.
	P50 time.Duration
	P95 time.Duration
	P99 time.Duration
}

// TokensPerSecond is the rate at which the model produces tokens while it is
// working (FR-031). A request that produced nothing still consumed serving
// time and still counts against the rate, so a model that is failing slowly
// reads as slow rather than as absent.
//
// It is zero when the model has served nothing, rather than undefined.
func (s PerfSummary) TokensPerSecond() float64 {
	if s.ServiceTime <= 0 {
		return 0
	}
	return float64(s.Tokens) / s.ServiceTime.Seconds()
}

// MeanLatency is the average request duration. It is reported alongside the
// distribution, never instead of it.
func (s PerfSummary) MeanLatency() time.Duration {
	if s.Requests == 0 {
		return 0
	}
	return time.Duration(uint64(s.ServiceTime) / s.Requests)
}

// Age is how long ago the most recent recorded request was served. It is what
// tells a reader whether this performance picture is current.
func (s PerfSummary) Age(now time.Time) time.Duration {
	if s.Requests == 0 {
		return 0
	}
	return now.Sub(s.Last)
}

// ModelPerf accumulates one running model's request timings.
//
// It is safe for concurrent use and is the unit of lock contention: requests
// finishing on different models never wait on each other. Callers resolve a
// handle once with Recorder.Model and reuse it for the model's whole life.
type ModelPerf struct {
	modelID string

	mu           sync.Mutex
	buckets      [histogramBuckets]uint64
	requests     uint64
	tokens       uint64
	serviceNanos uint64
	minNanos     uint64
	maxNanos     uint64
	first        time.Time
	last         time.Time
}

// Record adds one completed request, timing it as of now. Prefer RecordAt when
// the caller already knows when the request finished.
func (p *ModelPerf) Record(latency time.Duration, tokens int64) error {
	return p.RecordAt(time.Now(), latency, tokens)
}

// RecordAt adds one completed request served at at, taking latency and
// producing tokens tokens.
//
// This is the hot path. It allocates nothing and holds the model's lock across
// a fixed, branch-light sequence of integer updates.
func (p *ModelPerf) RecordAt(at time.Time, latency time.Duration, tokens int64) error {
	if latency < 0 {
		return fmt.Errorf("%w: model=%s latency=%s", ErrNegativeLatency, p.modelID, latency)
	}
	if tokens < 0 {
		return fmt.Errorf("%w: model=%s tokens=%d", ErrNegativeTokens, p.modelID, tokens)
	}
	nanos := uint64(latency)
	idx := bucketIndex(nanos)

	p.mu.Lock()
	if p.requests == 0 || nanos < p.minNanos {
		p.minNanos = nanos
	}
	if nanos > p.maxNanos {
		p.maxNanos = nanos
	}
	if p.requests == 0 || at.Before(p.first) {
		p.first = at
	}
	if at.After(p.last) {
		p.last = at
	}
	p.buckets[idx]++
	p.requests++
	p.tokens += uint64(tokens)
	p.serviceNanos += nanos
	p.mu.Unlock()
	return nil
}

// Quantile returns the latency at or below which the given fraction of
// requests completed. The result is a bucket upper bound: it never understates
// and exceeds the true value by at most QuantileRelativeError.
//
// q is clamped to (0, 1]. A model that has served nothing returns zero.
func (p *ModelPerf) Quantile(q float64) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.quantileLocked(q)
}

func (p *ModelPerf) quantileLocked(q float64) time.Duration {
	if p.requests == 0 {
		return 0
	}
	if q > 1 {
		q = 1
	}
	rank := uint64(math.Ceil(q * float64(p.requests)))
	if rank == 0 {
		rank = 1
	}
	var cumulative uint64
	for i := 0; i < histogramBuckets; i++ {
		cumulative += p.buckets[i]
		if cumulative >= rank {
			upper := bucketUpperBound(i)
			// Never report a quantile above the largest request actually
			// served: the bucket's upper bound is an artefact of resolution,
			// the observed maximum is a fact.
			if upper > p.maxNanos {
				upper = p.maxNanos
			}
			return time.Duration(upper)
		}
	}
	return time.Duration(p.maxNanos)
}

// Summary is this model's performance picture. It is taken under one lock, so
// the quantiles, counters and extremes all describe the same set of requests
// rather than a set that shifted mid-read.
func (p *ModelPerf) Summary() PerfSummary {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := PerfSummary{
		ModelID:     p.modelID,
		Requests:    p.requests,
		Tokens:      p.tokens,
		ServiceTime: time.Duration(p.serviceNanos),
		First:       p.first,
		Last:        p.last,
		Min:         time.Duration(p.minNanos),
		Max:         time.Duration(p.maxNanos),
	}
	if p.requests > 0 {
		s.P50 = p.quantileLocked(0.50)
		s.P95 = p.quantileLocked(0.95)
		s.P99 = p.quantileLocked(0.99)
	}
	return s
}

// Recorder holds the per-request timings of every running model.
//
// Its own lock guards only the model map — which changes when a model starts
// or stops running, not per request — so it is never the thing requests queue
// behind.
type Recorder struct {
	mu     sync.Mutex
	models map[string]*ModelPerf
}

// NewRecorder returns an empty Recorder.
func NewRecorder() *Recorder {
	return &Recorder{models: make(map[string]*ModelPerf)}
}

// Model returns the handle that records modelID's requests, creating it on
// first use. Resolve it once when a model starts serving and reuse it: the
// handle is what keeps the per-request path free of map lookups.
func (r *Recorder) Model(modelID string) (*ModelPerf, error) {
	if modelID == "" {
		return nil, ErrNoModelID
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.models[modelID]; ok {
		return p, nil
	}
	p := &ModelPerf{modelID: modelID}
	r.models[modelID] = p
	return p, nil
}

// Record adds one completed request for modelID, timed as of now.
func (r *Recorder) Record(modelID string, latency time.Duration, tokens int64) error {
	return r.RecordAt(modelID, time.Now(), latency, tokens)
}

// RecordAt adds one completed request for modelID. It resolves the model on
// every call; a caller on a hot path should hold a Model handle instead.
//
// A rejected measurement is not counted: the model's handle is created, but
// nothing is added to its history.
func (r *Recorder) RecordAt(modelID string, at time.Time, latency time.Duration, tokens int64) error {
	p, err := r.Model(modelID)
	if err != nil {
		return err
	}
	return p.RecordAt(at, latency, tokens)
}

// Forget drops a model's timings. It is called when a model stops running: the
// performance of a model that is no longer loaded is not a current fact about
// the host.
func (r *Recorder) Forget(modelID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.models, modelID)
}

// Summary returns modelID's performance picture. The second result is false
// when the model has served no request — an absence of evidence, not a
// measured zero.
func (r *Recorder) Summary(modelID string) (PerfSummary, bool) {
	r.mu.Lock()
	p, ok := r.models[modelID]
	r.mu.Unlock()
	if !ok {
		return PerfSummary{}, false
	}
	s := p.Summary()
	if s.Requests == 0 {
		return PerfSummary{}, false
	}
	return s, true
}

// Summaries returns one entry per model that has served at least one request,
// ordered by model id so a rendering never depends on map iteration order.
func (r *Recorder) Summaries() []PerfSummary {
	r.mu.Lock()
	handles := make([]*ModelPerf, 0, len(r.models))
	for _, p := range r.models {
		handles = append(handles, p)
	}
	r.mu.Unlock()

	out := make([]PerfSummary, 0, len(handles))
	for _, p := range handles {
		if s := p.Summary(); s.Requests > 0 {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModelID < out[j].ModelID })
	return out
}
