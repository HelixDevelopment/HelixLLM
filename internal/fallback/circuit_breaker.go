// Package fallback provides the multi-provider fallback chain for HelixLLM.
//
// It includes:
//   - CircuitBreaker: tracks consecutive failures and stops routing to unhealthy providers
//   - RateLimitTracker: parses rate-limit headers and gates providers near exhaustion
//   - ChainEntry: combines provider metadata, circuit state, and cooldown into a single unit
//   - FallbackChain: ordered list of entries with selection logic (implemented in chain.go)
package fallback

import (
	"sync"
	"time"
)

// CircuitState represents the operating state of a CircuitBreaker.
type CircuitState int

const (
	// StateClosed is the normal operating state — requests are allowed through.
	StateClosed CircuitState = iota

	// StateOpen means the circuit has tripped due to too many failures.
	// Requests are rejected until the open timeout elapses.
	StateOpen

	// StateHalfOpen is a probe state entered after the open timeout expires.
	// One request is allowed through; success closes the circuit, failure reopens it.
	StateHalfOpen
)

// String returns a human-readable name for the state.
func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker guards a single downstream dependency (one LLM provider) by
// tracking consecutive failures.  Once the failure threshold is reached the
// circuit opens and all subsequent Allow() calls return false until the open
// timeout elapses, at which point the circuit enters the half-open probe state.
type CircuitBreaker struct {
	maxFailures int
	openTimeout time.Duration

	mu                sync.Mutex
	consecutiveErrors int
	state             CircuitState
	openedAt          time.Time
}

// NewCircuitBreaker creates a CircuitBreaker that opens after maxFailures
// consecutive failures and stays open for openTimeout before probing again.
func NewCircuitBreaker(maxFailures int, openTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures: maxFailures,
		openTimeout: openTimeout,
		state:       StateClosed,
	}
}

// State returns the current CircuitState.  If the circuit is open and the
// open timeout has elapsed, it transitions to StateHalfOpen automatically.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == StateOpen && time.Since(cb.openedAt) >= cb.openTimeout {
		cb.state = StateHalfOpen
	}
	return cb.state
}

// Allow returns true when the circuit is Closed or HalfOpen (one probe
// allowed).  Returns false when the circuit is Open.
func (cb *CircuitBreaker) Allow() bool {
	state := cb.State()
	return state == StateClosed || state == StateHalfOpen
}

// RecordSuccess resets the consecutive error counter and closes the circuit.
// It is idempotent: calling it on an already-closed circuit is safe.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveErrors = 0
	cb.state = StateClosed
}

// RecordFailure increments the consecutive error counter.  The circuit opens
// when the count reaches maxFailures, or immediately if the circuit is already
// in the HalfOpen probe state (the probe failed).
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveErrors++
	if cb.consecutiveErrors >= cb.maxFailures || cb.state == StateHalfOpen {
		cb.state = StateOpen
		cb.openedAt = time.Now()
	}
}
