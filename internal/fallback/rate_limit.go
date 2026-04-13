package fallback

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimitState holds the last-known rate-limit counters for one provider.
type RateLimitState struct {
	RemainingRequests int
	RemainingTokens   int
	ResetAt           time.Time
	LastUpdated       time.Time
}

// RateLimitTracker parses provider rate-limit response headers and decides
// whether a provider should be skipped on the next request because it is
// close to exhaustion.  It also provides an exponential-backoff helper for
// providers that return 429 responses.
type RateLimitTracker struct {
	minRequests int
	minTokens   int

	mu      sync.RWMutex
	states  map[string]*RateLimitState
	backoffs map[string]int // call count per provider (drives exponent)
}

// NewRateLimitTracker creates a tracker that considers a provider "near
// exhaustion" when its remaining requests drop below minRequests OR its
// remaining tokens drop below minTokens.
func NewRateLimitTracker(minRequests, minTokens int) *RateLimitTracker {
	return &RateLimitTracker{
		minRequests: minRequests,
		minTokens:   minTokens,
		states:      make(map[string]*RateLimitState),
		backoffs:    make(map[string]int),
	}
}

// UpdateFromHeaders parses the standard rate-limit headers returned by most
// OpenAI-compatible providers and stores the results.  At least one of the
// three recognised headers must be present for the state to be stored.
//
// Recognised headers:
//
//	x-ratelimit-remaining-requests
//	x-ratelimit-remaining-tokens
//	x-ratelimit-reset  (HTTP-date or integer seconds)
func (r *RateLimitTracker) UpdateFromHeaders(provider string, h http.Header) {
	reqStr := h.Get("x-ratelimit-remaining-requests")
	tokStr := h.Get("x-ratelimit-remaining-tokens")
	resetStr := h.Get("x-ratelimit-reset")

	// Nothing to store.
	if reqStr == "" && tokStr == "" && resetStr == "" {
		return
	}

	state := &RateLimitState{LastUpdated: time.Now()}

	if reqStr != "" {
		if v, err := strconv.Atoi(reqStr); err == nil {
			state.RemainingRequests = v
		}
	}
	if tokStr != "" {
		if v, err := strconv.Atoi(tokStr); err == nil {
			state.RemainingTokens = v
		}
	}
	if resetStr != "" {
		// Try integer seconds first, then HTTP-date format.
		if secs, err := strconv.ParseInt(resetStr, 10, 64); err == nil {
			state.ResetAt = time.Now().Add(time.Duration(secs) * time.Second)
		} else if t, err := http.ParseTime(resetStr); err == nil {
			state.ResetAt = t
		}
	}

	r.mu.Lock()
	r.states[provider] = state
	r.mu.Unlock()
}

// Get returns the last-known RateLimitState for provider.  If no state has
// been recorded, it returns the zero value (all fields at their zero values).
func (r *RateLimitTracker) Get(provider string) RateLimitState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if s, ok := r.states[provider]; ok {
		return *s
	}
	return RateLimitState{}
}

// ShouldSkip returns true when the provider's remaining quota is below the
// configured minimums.  An unknown provider (one that has never had headers
// recorded) is never skipped.
func (r *RateLimitTracker) ShouldSkip(provider string) bool {
	r.mu.RLock()
	s, ok := r.states[provider]
	r.mu.RUnlock()

	if !ok {
		return false
	}
	return s.RemainingRequests < r.minRequests || s.RemainingTokens < r.minTokens
}

// ParseRetryAfter reads the Retry-After header and converts it to a
// time.Duration.  Accepts both the integer-seconds form ("90") and the
// HTTP-date form ("Mon, 13 Apr 2026 12:00:00 GMT").  Returns 0 if the header
// is absent or unparseable.
func (r *RateLimitTracker) ParseRetryAfter(h http.Header) time.Duration {
	val := h.Get("Retry-After")
	if val == "" {
		return 0
	}
	// Try seconds integer.
	if secs, err := strconv.ParseInt(val, 10, 64); err == nil {
		return time.Duration(secs) * time.Second
	}
	// Try HTTP-date.
	if t, err := http.ParseTime(val); err == nil {
		d := time.Until(t)
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}

const (
	backoffBase    = 60 * time.Second
	backoffMaxCap  = 15 * time.Minute
)

// NextBackoff returns the next exponential-backoff duration for provider and
// advances the internal counter.  The sequence is: 60s, 120s, 240s, 480s,
// then capped at 15 minutes for all subsequent calls.
func (r *RateLimitTracker) NextBackoff(provider string) time.Duration {
	r.mu.Lock()
	n := r.backoffs[provider]
	r.backoffs[provider] = n + 1
	r.mu.Unlock()

	// d = 60s * 2^n
	d := backoffBase * (1 << uint(n))
	if d > backoffMaxCap {
		d = backoffMaxCap
	}
	return d
}

// ResetBackoff resets the backoff counter for provider to zero.
func (r *RateLimitTracker) ResetBackoff(provider string) {
	r.mu.Lock()
	delete(r.backoffs, provider)
	r.mu.Unlock()
}
