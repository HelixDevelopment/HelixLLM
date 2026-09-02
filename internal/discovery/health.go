package discovery

// Health is liveness tracking, and it exists for one reason: an instance that
// is not answering must not be exported as available (T060). A model listed as
// usable on a host that is down is worse than one that was never listed — the
// user selects it, the request fails, and the failure surfaces somewhere far
// from the listing that caused it.

import (
	"sync"
	"time"
)

// Reasons an endpoint is not reachable. They are machine keys, not sentences:
// user-facing wording is composed at the presentation boundary in the user's
// language (CONST-046), and a key is what lets a caller distinguish "the host
// is down" from "the host is up but failed authentication" — different problems
// with different remedies.
const (
	// ReasonUnreachable: the endpoint did not answer at all.
	ReasonUnreachable = "unreachable"
	// ReasonAuthenticationFailed: the endpoint answered but could not present
	// the pre-shared secret (FR-024).
	ReasonAuthenticationFailed = "authentication-failed"
	// ReasonMalformedResponse: the endpoint answered with something this
	// package could not read as an attestation.
	ReasonMalformedResponse = "malformed-response"
	// ReasonRefused: the probe was refused before it left this process, e.g.
	// because no secret is configured for a mode that requires one.
	ReasonRefused = "refused"
)

// DefaultHealthTTL is how long an observation stays fresh. Beyond it an
// instance is stale rather than live: nothing has said it is down, but nothing
// has confirmed it is up either, and the two must not be conflated.
const DefaultHealthTTL = 2 * time.Minute

// Health is what is known about one endpoint's liveness.
//
// Reachable and LastSeen are separate fields because they answer separate
// questions — "did the last observation succeed" and "how old is that
// observation" — and an instance can fail the second while passing the first.
type Health struct {
	// Reachable is the outcome of the most recent observation.
	Reachable bool
	// LastSeen is when the endpoint last answered successfully.
	LastSeen time.Time
	// LastFailure is when it last failed to.
	LastFailure time.Time
	// ConsecutiveFailures counts failures since the last success, so a caller
	// can distinguish a blip from a host that has gone away.
	ConsecutiveFailures int
	// Reason is the machine key for the most recent failure, empty when the
	// endpoint is reachable. It never carries a credential or a remote-supplied
	// string: it is drawn from the closed set above.
	Reason string
}

// Live reports whether the endpoint is both reachable and freshly observed.
func (h Health) Live(now time.Time, ttl time.Duration) bool {
	if !h.Reachable || h.LastSeen.IsZero() {
		return false
	}
	return now.Sub(h.LastSeen) <= ttl
}

// Tracker records liveness per endpoint.
//
// now is injected so staleness is testable without sleeping: a test that has to
// wait out a TTL is a test that will one day be made flaky by a slow machine.
type Tracker struct {
	mu     sync.RWMutex
	states map[string]Health
	ttl    time.Duration
	now    func() time.Time
}

// NewTracker builds a tracker. A zero or negative ttl takes DefaultHealthTTL,
// and a nil clock takes time.Now.
func NewTracker(ttl time.Duration, now func() time.Time) *Tracker {
	if ttl <= 0 {
		ttl = DefaultHealthTTL
	}
	if now == nil {
		now = time.Now
	}
	return &Tracker{states: make(map[string]Health), ttl: ttl, now: now}
}

// Success records that endpoint answered, clearing any failure streak.
func (t *Tracker) Success(endpoint string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.states[endpoint]
	state.Reachable = true
	state.LastSeen = t.now()
	state.ConsecutiveFailures = 0
	state.Reason = ""
	t.states[endpoint] = state
}

// Failure records that endpoint did not answer usefully. reason must be one of
// the keys above; a remote-supplied string must never reach this argument.
func (t *Tracker) Failure(endpoint, reason string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.states[endpoint]
	state.Reachable = false
	state.LastFailure = t.now()
	state.ConsecutiveFailures++
	state.Reason = reason
	t.states[endpoint] = state
}

// Health returns what is known about endpoint. An endpoint never observed comes
// back as the zero Health, which is not reachable — the safe reading, since an
// unobserved host is not a healthy one.
func (t *Tracker) Health(endpoint string) Health {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.states[endpoint]
}

// TTL is the freshness window this tracker applies.
func (t *Tracker) TTL() time.Duration { return t.ttl }

// Available reports whether an instance may be exported to users. Both grounds
// are checked, and they are independent: an untrusted instance is withheld
// however healthy it is (FR-024), and an unreachable one is withheld however
// well it authenticated (T060).
func (t *Tracker) Available(inst Instance) bool {
	return inst.Trusted && inst.Health.Live(t.now(), t.ttl)
}

// Filter keeps only the instances that may be exported, preserving order.
func (t *Tracker) Filter(instances []Instance) []Instance {
	kept := make([]Instance, 0, len(instances))
	for _, inst := range instances {
		if t.Available(inst) {
			kept = append(kept, inst)
		}
	}
	return kept
}
