package fallback

import "time"

// EntryStatus describes the operational status of a ChainEntry.
type EntryStatus int

const (
	// EntryActive means the entry is eligible to receive requests.
	EntryActive EntryStatus = iota

	// EntryExhausted means the entry has been rate-limited or its quota was
	// depleted.  It may become active again once CooldownUntil passes.
	EntryExhausted

	// EntryCircuitOpen means the entry's circuit breaker has tripped.
	// The circuit breaker itself manages the transition back to usable.
	EntryCircuitOpen
)

// String returns a human-readable label for the status.
func (s EntryStatus) String() string {
	switch s {
	case EntryActive:
		return "active"
	case EntryExhausted:
		return "exhausted"
	case EntryCircuitOpen:
		return "circuit_open"
	default:
		return "unknown"
	}
}

// ChainEntry is one slot in the fallback chain.  It pairs a provider/model
// identity with its operational state (circuit breaker, cooldown, score).
//
// Local-inference entries (llama.cpp) set IsLocalFallback=true; the chain
// sorts them last so cloud providers are tried first.
type ChainEntry struct {
	// ProviderName is the brain.Provider name (e.g. "openai", "chutes").
	ProviderName string

	// ModelID is the specific model to request (e.g. "gpt-4o-mini").
	ModelID string

	// Score is the LLMsVerifier quality score used for ordering the chain.
	// Higher is better.
	Score float64

	// Status is the current operational status of this entry.
	Status EntryStatus

	// CooldownUntil, when non-zero, is the earliest time the entry may be
	// considered active again after being marked Exhausted.
	CooldownUntil time.Time

	// CircuitBreaker for this specific provider+model pair.
	CircuitBreaker *CircuitBreaker

	// IsLocalFallback marks llama.cpp / local-inference entries that should
	// always be sorted after cloud providers regardless of score.
	IsLocalFallback bool
}

// Available reports whether this entry can accept a request right now.
//
// Decision logic:
//  1. If the entry is Exhausted but its cooldown has expired, reset it to Active.
//  2. If the entry is still Exhausted (cooldown not yet elapsed), return false.
//  3. If the circuit breaker does not allow the request, return false.
//  4. Otherwise return true.
func (e *ChainEntry) Available() bool {
	// Step 1: auto-recover from exhaustion once cooldown expires.
	if e.Status == EntryExhausted {
		if !e.CooldownUntil.IsZero() && time.Now().After(e.CooldownUntil) {
			e.Status = EntryActive
			e.CooldownUntil = time.Time{}
		} else {
			return false
		}
	}

	// Step 2: defer to the circuit breaker if one is attached.
	if e.CircuitBreaker != nil && !e.CircuitBreaker.Allow() {
		return false
	}

	return true
}
