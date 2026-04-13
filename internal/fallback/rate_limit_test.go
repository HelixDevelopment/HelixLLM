package fallback

import (
	"net/http"
	"testing"
	"time"
)

// makeHeaders is a small helper to build an http.Header from key-value pairs.
func makeHeaders(pairs ...string) http.Header {
	h := http.Header{}
	for i := 0; i+1 < len(pairs); i += 2 {
		h.Set(pairs[i], pairs[i+1])
	}
	return h
}

// ---------------------------------------------------------------------------
// UpdateFromHeaders / Get
// ---------------------------------------------------------------------------

func TestRateLimitTracker_ParsesStandardHeaders(t *testing.T) {
	tracker := NewRateLimitTracker(5, 100)

	resetTime := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)
	h := makeHeaders(
		"x-ratelimit-remaining-requests", "42",
		"x-ratelimit-remaining-tokens", "8000",
		"x-ratelimit-reset", resetTime,
	)
	tracker.UpdateFromHeaders("openai", h)

	state := tracker.Get("openai")
	if state.RemainingRequests != 42 {
		t.Errorf("RemainingRequests: want 42, got %d", state.RemainingRequests)
	}
	if state.RemainingTokens != 8000 {
		t.Errorf("RemainingTokens: want 8000, got %d", state.RemainingTokens)
	}
	if state.ResetAt.IsZero() {
		t.Error("ResetAt should not be zero when header is present")
	}
}

func TestRateLimitTracker_NoHeadersDoesNotStore(t *testing.T) {
	tracker := NewRateLimitTracker(5, 100)
	tracker.UpdateFromHeaders("ghost", http.Header{})

	state := tracker.Get("ghost")
	if !state.LastUpdated.IsZero() {
		t.Error("expected zero-value state when no headers were present")
	}
}

func TestRateLimitTracker_PartialHeadersStored(t *testing.T) {
	tracker := NewRateLimitTracker(5, 100)
	h := makeHeaders("x-ratelimit-remaining-requests", "10")
	tracker.UpdateFromHeaders("partial", h)

	state := tracker.Get("partial")
	if state.RemainingRequests != 10 {
		t.Errorf("want 10, got %d", state.RemainingRequests)
	}
}

// ---------------------------------------------------------------------------
// ShouldSkip
// ---------------------------------------------------------------------------

func TestRateLimitTracker_ShouldSkipOnLowRequests(t *testing.T) {
	tracker := NewRateLimitTracker(5, 100)

	h := makeHeaders(
		"x-ratelimit-remaining-requests", "3",
		"x-ratelimit-remaining-tokens", "5000",
	)
	tracker.UpdateFromHeaders("provider1", h)

	if !tracker.ShouldSkip("provider1") {
		t.Error("expected ShouldSkip == true when remaining requests < minRequests")
	}
}

func TestRateLimitTracker_ShouldSkipOnLowTokens(t *testing.T) {
	tracker := NewRateLimitTracker(5, 100)

	h := makeHeaders(
		"x-ratelimit-remaining-requests", "50",
		"x-ratelimit-remaining-tokens", "80",
	)
	tracker.UpdateFromHeaders("provider2", h)

	if !tracker.ShouldSkip("provider2") {
		t.Error("expected ShouldSkip == true when remaining tokens < minTokens")
	}
}

func TestRateLimitTracker_UnknownProviderNotSkipped(t *testing.T) {
	tracker := NewRateLimitTracker(5, 100)

	if tracker.ShouldSkip("unknown-provider") {
		t.Error("expected ShouldSkip == false for unknown provider")
	}
}

func TestRateLimitTracker_NoHeadersNotSkipped(t *testing.T) {
	tracker := NewRateLimitTracker(5, 100)
	tracker.UpdateFromHeaders("noheaders", http.Header{})

	if tracker.ShouldSkip("noheaders") {
		t.Error("expected ShouldSkip == false when no headers were stored")
	}
}

func TestRateLimitTracker_ShouldNotSkipWhenAboveThresholds(t *testing.T) {
	tracker := NewRateLimitTracker(5, 100)

	h := makeHeaders(
		"x-ratelimit-remaining-requests", "100",
		"x-ratelimit-remaining-tokens", "50000",
	)
	tracker.UpdateFromHeaders("healthy", h)

	if tracker.ShouldSkip("healthy") {
		t.Error("expected ShouldSkip == false when both counters are above thresholds")
	}
}

// ---------------------------------------------------------------------------
// ParseRetryAfter
// ---------------------------------------------------------------------------

func TestRateLimitTracker_ParseRetryAfterSeconds(t *testing.T) {
	tracker := NewRateLimitTracker(5, 100)

	h := makeHeaders("Retry-After", "90")
	d := tracker.ParseRetryAfter(h)
	if d != 90*time.Second {
		t.Errorf("want 90s, got %v", d)
	}
}

func TestRateLimitTracker_ParseRetryAfterHTTPDate(t *testing.T) {
	tracker := NewRateLimitTracker(5, 100)

	future := time.Now().Add(2 * time.Minute).UTC()
	h := makeHeaders("Retry-After", future.Format(http.TimeFormat))
	d := tracker.ParseRetryAfter(h)

	// Allow a 2-second window for execution time.
	if d < 118*time.Second || d > 122*time.Second {
		t.Errorf("want ~2m, got %v", d)
	}
}

func TestRateLimitTracker_ParseRetryAfterMissing(t *testing.T) {
	tracker := NewRateLimitTracker(5, 100)
	d := tracker.ParseRetryAfter(http.Header{})
	if d != 0 {
		t.Errorf("want 0 for missing header, got %v", d)
	}
}

// ---------------------------------------------------------------------------
// NextBackoff / ResetBackoff
// ---------------------------------------------------------------------------

func TestRateLimitTracker_ExponentialBackoff(t *testing.T) {
	tracker := NewRateLimitTracker(5, 100)

	expected := []time.Duration{
		60 * time.Second,
		120 * time.Second,
		240 * time.Second,
		480 * time.Second,
		15 * time.Minute, // cap
		15 * time.Minute, // cap again
	}

	for i, want := range expected {
		got := tracker.NextBackoff("prov")
		if got != want {
			t.Errorf("iteration %d: want %v, got %v", i, want, got)
		}
	}
}

func TestRateLimitTracker_ResetBackoff(t *testing.T) {
	tracker := NewRateLimitTracker(5, 100)

	tracker.NextBackoff("prov")
	tracker.NextBackoff("prov")
	tracker.ResetBackoff("prov")

	got := tracker.NextBackoff("prov")
	if got != 60*time.Second {
		t.Errorf("after reset want 60s, got %v", got)
	}
}
