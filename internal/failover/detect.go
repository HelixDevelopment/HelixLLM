package failover

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Detection errors. Callers switch on these with errors.Is.
var (
	// ErrInvalidConfig is returned when a failover policy is unusable.
	ErrInvalidConfig = errors.New("failover: invalid configuration")

	// ErrNoLivenessProbe is returned when a Detector is built without an
	// independent liveness probe. Without one the detector could only GUESS
	// that a host died from the symptoms of the request itself — and the
	// symptom of a dead host is indistinguishable from the symptom of a slow
	// one. A guess here costs a needless retry against a struggling fleet.
	ErrNoLivenessProbe = errors.New("failover: a LivenessProbe is required — host loss must be corroborated out of band, not inferred from a timeout (FR-048)")

	// ErrUnknownRequest is returned for an assessment of a request this
	// detector is not watching. It is deliberately an error rather than a
	// cheerful "alive": a request nobody is watching has no known state.
	ErrUnknownRequest = errors.New("failover: request is not being watched")

	// ErrDuplicateRequest is returned when a second watch is opened for a
	// request id already in flight. Two watches would corroborate the same
	// host loss twice and reach the threshold on half the evidence.
	ErrDuplicateRequest = errors.New("failover: request is already being watched")

	// ErrHostLost is the explicit failure FR-048 demands: the serving host
	// became unreachable while the request was in flight. It is never returned
	// for a host that is merely slow, and it always names the host (SC-016).
	ErrHostLost = errors.New("failover: serving host became unreachable while the request was in flight (FR-048)")
)

// HostLostError names the host that went. SC-016 requires the user to receive
// an explicit failure identifying the lost host rather than a truncated answer
// presented as complete, so the host is a structured field and not merely a
// fragment of a message string.
type HostLostError struct {
	Host      string // the serving host proven unreachable
	RequestID string // the request that was in flight when it went
	ModelID   string // the model that was answering
	Cause     error  // the last corroborating probe failure, if any
}

// NewHostLostError builds the FR-048 failure for a proven-unreachable host.
func NewHostLostError(requestID string, inst Instance, cause error) *HostLostError {
	return &HostLostError{Host: inst.Host, RequestID: requestID, ModelID: inst.ModelID, Cause: cause}
}

func (e *HostLostError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: host=%s model=%s request=%s: %v",
			ErrHostLost.Error(), e.Host, e.ModelID, e.RequestID, e.Cause)
	}
	return fmt.Sprintf("%s: host=%s model=%s request=%s",
		ErrHostLost.Error(), e.Host, e.ModelID, e.RequestID)
}

// Is makes errors.Is(err, ErrHostLost) succeed for this concrete type.
func (e *HostLostError) Is(target error) bool { return target == ErrHostLost }

// Unwrap exposes the corroborating probe failure for further inspection.
func (e *HostLostError) Unwrap() error { return e.Cause }

// Instance identifies one serving instance of one model on one host. It is the
// unit failover reasons about: an answer comes from exactly one of these, and a
// retry moves the request from one to another.
type Instance struct {
	// Host is the serving host. It is what the user is told (FR-050, SC-016).
	Host string

	// ModelID is the model as the user selected it.
	ModelID string

	// Fingerprint is the model's IDENTITY — family, variant and quantisation
	// collapsed into one comparable value. Two instances sharing a fingerprint
	// hold the same weights at the same precision, so swapping between them
	// changes the host and nothing else. See Equivalent.
	Fingerprint string

	// Capabilities is what this instance is actually able to serve (tools,
	// vision, embeddings, ...). Identical weights on a host whose runtime is
	// missing a capability is not an equivalent substitute.
	Capabilities []string
}

// Verdict is the assessed state of the HOST — not of the request. A request can
// be doomed (its connection broke) while its host is demonstrably alive, and
// only VerdictLost authorises the FR-048 failure and the FR-049 retry.
type Verdict string

const (
	// VerdictAlive: the request is progressing, or has been silent only briefly.
	VerdictAlive Verdict = "alive"

	// VerdictSlow: the request is not progressing, but the host has NOT been
	// proven unreachable. A timeout lands here. This is the verdict that keeps
	// a struggling fleet from being retried into collapse.
	VerdictSlow Verdict = "slow"

	// VerdictLost: independent probes have corroborated that the host is
	// unreachable the configured number of consecutive times.
	VerdictLost Verdict = "lost"
)

// LivenessProbe answers, out of band from the in-flight request, whether a host
// is reachable at all. It is the independent second opinion that separates a
// dead host from a slow one; a nil error means the host answered.
type LivenessProbe interface {
	Probe(ctx context.Context, host string) error
}

// LivenessProbeFunc adapts a plain function to LivenessProbe.
type LivenessProbeFunc func(ctx context.Context, host string) error

// Probe implements LivenessProbe.
func (f LivenessProbeFunc) Probe(ctx context.Context, host string) error { return f(ctx, host) }

// DetectConfig is the loss-detection policy. Both values are CONFIGURATION:
// how patient to be with a quiet host, and how much independent evidence is
// required before declaring it dead, are deployment decisions.
type DetectConfig struct {
	// SilenceGrace is how long a request may produce no output before the host
	// is even suspected. Below this, gaps between tokens are normal and cost no
	// probe traffic. Must be > 0.
	SilenceGrace time.Duration

	// ProbeFailuresForLoss is how many CONSECUTIVE independent probe failures
	// are required before a host is declared lost. Must be >= 1. A higher value
	// buys resistance to a transient blip at the cost of detecting a real death
	// later.
	ProbeFailuresForLoss int
}

// Validate reports whether the policy is usable.
func (c DetectConfig) Validate() error {
	if c.SilenceGrace <= 0 {
		return fmt.Errorf("%w: SilenceGrace=%s must be > 0", ErrInvalidConfig, c.SilenceGrace)
	}
	if c.ProbeFailuresForLoss < 1 {
		return fmt.Errorf("%w: ProbeFailuresForLoss=%d must be >= 1", ErrInvalidConfig, c.ProbeFailuresForLoss)
	}
	return nil
}

// Assessment is one decision about one in-flight request's host.
type Assessment struct {
	RequestID     string
	Instance      Instance
	Verdict       Verdict
	SinceProgress time.Duration // how long the request has produced nothing
	ProbeFailures int           // consecutive independent probe failures so far
	ProbeErr      error         // the most recent probe failure, if any
	TransportErr  error         // the last failure reported by the request channel
	At            time.Time
}

// Lost reports whether the host has been proven unreachable.
func (a Assessment) Lost() bool { return a.Verdict == VerdictLost }

// Err returns the FR-048 explicit failure when — and only when — the host is
// lost. A slow host yields no error: slowness is not a fault to surface as one.
func (a Assessment) Err() error {
	if !a.Lost() {
		return nil
	}
	return NewHostLostError(a.RequestID, a.Instance, a.ProbeErr)
}

// watch is the tracked state of one in-flight request.
//
// state is guarded by Detector.mu; assess serialises assessment of THIS watch
// across goroutines so the probe call and the corroboration count it feeds stay
// consistent — two concurrent probes must not both increment the same counter
// from the same observation.
type watch struct {
	assess sync.Mutex

	requestID     string
	instance      Instance
	startedAt     time.Time
	lastProgress  time.Time
	suspect       bool  // the request channel reported a failure
	transportErr  error // ... and this is what it reported
	probeFailures int
	probeErr      error
	closed        bool
}

// Detector decides whether a serving host was LOST while a request was in
// flight, or is merely SLOW.
//
// The distinction is the whole point of this type. The symptoms available from
// the request itself — a deadline expiring, a stream falling silent, a
// connection resetting — are produced identically by a dead host and by an
// overloaded one. So this detector NEVER concludes loss from the request alone:
// loss requires the configured number of consecutive failures of an INDEPENDENT
// liveness probe. Treating slowness as death costs a needless retry, which adds
// load to a fleet that is by construction already struggling.
type Detector struct {
	mu      sync.Mutex
	watches map[string]*watch

	cfg   DetectConfig
	probe LivenessProbe
	clock func() time.Time
}

// DetectorOption configures a Detector.
type DetectorOption func(*Detector)

// WithClock injects the time source. Production uses time.Now; tests drive a
// controllable clock so grace periods are exercised without sleeping.
func WithClock(now func() time.Time) DetectorOption {
	return func(d *Detector) {
		if now != nil {
			d.clock = now
		}
	}
}

// NewDetector builds a Detector. An independent liveness probe is REQUIRED.
func NewDetector(cfg DetectConfig, probe LivenessProbe, opts ...DetectorOption) (*Detector, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if probe == nil {
		return nil, ErrNoLivenessProbe
	}
	d := &Detector{
		watches: make(map[string]*watch),
		cfg:     cfg,
		probe:   probe,
		clock:   time.Now,
	}
	for _, o := range opts {
		o(d)
	}
	return d, nil
}

// Watch registers an in-flight request against the instance serving it and
// returns the handle the caller feeds progress and transport failures into.
func (d *Detector) Watch(requestID string, inst Instance) (*Watch, error) {
	if requestID == "" {
		return nil, fmt.Errorf("%w: request id is empty", ErrInvalidConfig)
	}
	if inst.Host == "" {
		return nil, fmt.Errorf("%w: instance has no host — a loss that cannot be named cannot be reported (SC-016)", ErrInvalidConfig)
	}
	now := d.clock()

	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.watches[requestID]; exists {
		return nil, fmt.Errorf("%w: request=%s", ErrDuplicateRequest, requestID)
	}
	d.watches[requestID] = &watch{
		requestID:    requestID,
		instance:     inst,
		startedAt:    now,
		lastProgress: now,
	}
	return &Watch{d: d, requestID: requestID}, nil
}

// Watch is the handle for one in-flight request.
type Watch struct {
	d         *Detector
	requestID string
}

// RequestID returns the watched request's id.
func (w *Watch) RequestID() string { return w.requestID }

// Progress records that the request produced output just now. Progress is
// positive evidence of life and resets the silence clock.
func (w *Watch) Progress() { w.d.progress(w.requestID) }

// TransportFailure records a failure reported by the request channel itself —
// a deadline, a reset, an unexpected EOF. It makes the host a SUSPECT, which
// permits corroboration to begin immediately rather than waiting out the
// silence grace. It is never, on its own, a conclusion.
func (w *Watch) TransportFailure(err error) { w.d.transportFailure(w.requestID, err) }

// Assess evaluates the watched request's host now.
func (w *Watch) Assess(ctx context.Context) (Assessment, error) {
	return w.d.Assess(ctx, w.requestID)
}

// Close stops watching the request. It is idempotent.
func (w *Watch) Close() { w.d.close(w.requestID) }

func (d *Detector) progress(requestID string) {
	now := d.clock()
	d.mu.Lock()
	defer d.mu.Unlock()
	if wt, ok := d.watches[requestID]; ok {
		wt.lastProgress = now
	}
}

func (d *Detector) transportFailure(requestID string, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if wt, ok := d.watches[requestID]; ok {
		wt.suspect = true
		if err != nil {
			wt.transportErr = err
		}
	}
}

func (d *Detector) close(requestID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if wt, ok := d.watches[requestID]; ok {
		wt.closed = true
		delete(d.watches, requestID)
	}
}

// Watching reports whether requestID is currently being watched.
func (d *Detector) Watching(requestID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.watches[requestID]
	return ok
}

// Assess evaluates whether the host serving requestID is alive, slow, or lost.
//
// The order of the checks IS the policy:
//
//  1. Progressing, or silent only briefly, and unsuspected → alive, no probe.
//     A healthy fleet must not pay probe traffic for every in-flight request.
//  2. Otherwise the host is corroborated by ONE independent probe. A reachable
//     host resets the consecutive-failure count to zero: a blip is not a death.
//  3. Only when consecutive failures reach the configured threshold is the host
//     declared lost. Every other outcome is VerdictSlow — a timeout inclusive.
func (d *Detector) Assess(ctx context.Context, requestID string) (Assessment, error) {
	wt, err := d.lookup(requestID)
	if err != nil {
		return Assessment{}, err
	}

	// Serialise assessment of this one watch: the probe below runs WITHOUT the
	// detector lock (it is network I/O and must not block every other request),
	// so this per-watch lock is what keeps one observation from being counted
	// twice by two concurrent assessors.
	wt.assess.Lock()
	defer wt.assess.Unlock()

	now := d.clock()
	snap := d.snapshot(wt, now)
	if snap.gone {
		return Assessment{}, fmt.Errorf("%w: request=%s", ErrUnknownRequest, requestID)
	}

	// (1) Progressing or only briefly quiet, and nothing has gone wrong on the
	// request channel: no corroboration needed, and no probe traffic spent.
	if !snap.suspect && snap.sinceProgress < d.cfg.SilenceGrace {
		return d.assessment(wt, VerdictAlive, snap, now), nil
	}

	// (2) Corroborate out of band. This is the ONLY source of a loss verdict.
	probeErr := d.probe.Probe(ctx, snap.instance.Host)
	d.recordProbe(wt, probeErr)

	// (3) Consecutive-failure threshold, re-read under the lock.
	snap = d.snapshot(wt, now)
	if snap.gone {
		return Assessment{}, fmt.Errorf("%w: request=%s", ErrUnknownRequest, requestID)
	}
	verdict := VerdictSlow
	if snap.probeFailures >= d.cfg.ProbeFailuresForLoss {
		verdict = VerdictLost
	}
	return d.assessment(wt, verdict, snap, now), nil
}

// watchSnapshot is a consistent copy of a watch's mutable state, taken under
// the detector lock so no field is read while another goroutine writes it.
type watchSnapshot struct {
	instance      Instance
	sinceProgress time.Duration
	suspect       bool
	transportErr  error
	probeFailures int
	probeErr      error
	gone          bool
}

func (d *Detector) lookup(requestID string) (*watch, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	wt, ok := d.watches[requestID]
	if !ok {
		return nil, fmt.Errorf("%w: request=%s", ErrUnknownRequest, requestID)
	}
	return wt, nil
}

func (d *Detector) snapshot(wt *watch, now time.Time) watchSnapshot {
	d.mu.Lock()
	defer d.mu.Unlock()
	return watchSnapshot{
		instance:      wt.instance,
		sinceProgress: now.Sub(wt.lastProgress),
		suspect:       wt.suspect,
		transportErr:  wt.transportErr,
		probeFailures: wt.probeFailures,
		probeErr:      wt.probeErr,
		gone:          wt.closed,
	}
}

// recordProbe folds one probe result into the corroboration count. A reachable
// host resets the count to zero — the failures that justify declaring a host
// dead must be CONSECUTIVE, or a host that blips once an hour would eventually
// accumulate a death sentence it never earned.
func (d *Detector) recordProbe(wt *watch, probeErr error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if probeErr == nil {
		wt.probeFailures = 0
		wt.probeErr = nil
		return
	}
	wt.probeFailures++
	wt.probeErr = probeErr
}

func (d *Detector) assessment(wt *watch, v Verdict, snap watchSnapshot, now time.Time) Assessment {
	return Assessment{
		RequestID:     wt.requestID,
		Instance:      snap.instance,
		Verdict:       v,
		SinceProgress: snap.sinceProgress,
		ProbeFailures: snap.probeFailures,
		ProbeErr:      snap.probeErr,
		TransportErr:  snap.transportErr,
		At:            now,
	}
}
