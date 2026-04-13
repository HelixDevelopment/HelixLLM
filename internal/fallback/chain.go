package fallback

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// Chain is the central fallback orchestrator.  It holds an ordered list of
// ChainEntry values and routes each request to the first available provider,
// automatically failing over on 429 / 5xx errors.
//
// Entries are tried in the order stored in the slice.  Local-fallback entries
// (IsLocalFallback=true) should be placed at the end of the slice by the
// caller (e.g. ScorerBridge) so cloud providers are always attempted first.
type Chain struct {
	providers   map[string]brain.Provider
	entries     []ChainEntry
	rateLimiter *RateLimitTracker
	mu          sync.RWMutex
}

// NewChain returns a Chain backed by the given provider map and rate limiter.
// providers maps brain.Provider.Name() → Provider instance.
// rl must not be nil.
func NewChain(providers map[string]brain.Provider, rl *RateLimitTracker) *Chain {
	return &Chain{
		providers:   providers,
		rateLimiter: rl,
	}
}

// SetEntries replaces the ordered entry list.  It is safe to call concurrently.
func (c *Chain) SetEntries(entries []ChainEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = entries
}

// Entries returns a snapshot copy of the current entry list.
// It is safe to call concurrently.
func (c *Chain) Entries() []ChainEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]ChainEntry, len(c.entries))
	copy(out, c.entries)
	return out
}

// Complete iterates the entry list in order and calls the first available
// provider.  On success it returns the response.  On 429 it marks the entry
// exhausted and tries the next one.  On any other error it records a circuit
// failure and tries the next one.
//
// If every entry is skipped or fails, Complete returns an error that wraps
// the last provider error and contains the string "all providers exhausted".
//
// State mutations (429 cooldowns, circuit-breaker failures) are written back
// directly into c.entries under a write lock so the changes persist across
// successive calls.  The old snapshot-based approach (Entries() returning a
// copy) silently discarded every mutation.
func (c *Chain) Complete(ctx context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	c.mu.RLock()
	numEntries := len(c.entries)
	c.mu.RUnlock()

	var lastErr error
	for i := 0; i < numEntries; i++ {
		// Read entry state under a read lock (copy for local use).
		c.mu.RLock()
		if i >= len(c.entries) {
			c.mu.RUnlock()
			break
		}
		entry := c.entries[i] // value copy for reading
		c.mu.RUnlock()

		if !c.entryAvailable(i) {
			slog.Debug("fallback: skipping unavailable entry", "provider", entry.ProviderName)
			continue
		}

		// Skip rate-limited cloud providers; local fallbacks always get a shot.
		if c.rateLimiter.ShouldSkip(entry.ProviderName) && !entry.IsLocalFallback {
			slog.Debug("fallback: skipping rate-limited entry", "provider", entry.ProviderName)
			continue
		}

		provider, ok := c.providers[entry.ProviderName]
		if !ok {
			slog.Warn("fallback: provider not registered", "provider", entry.ProviderName)
			continue
		}

		// Build a per-entry request: copy and override Model if specified.
		reqCopy := *req
		if entry.ModelID != "" {
			reqCopy.Model = entry.ModelID
		}

		resp, err := provider.Complete(ctx, &reqCopy)
		if err == nil {
			c.writeBackSuccess(i)
			c.rateLimiter.ResetBackoff(entry.ProviderName)
			return resp, nil
		}

		// Record the failure and decide on the next action.
		lastErr = err
		c.writeBackError(i, err)
		slog.Warn("fallback: provider error, trying next",
			"provider", entry.ProviderName,
			"error", err)
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all providers exhausted, last error: %w", lastErr)
	}
	return nil, fmt.Errorf("all providers exhausted: no entries available")
}

// CompleteStream iterates the entry list in order and calls the first
// available provider for streaming.  The failover logic mirrors Complete.
func (c *Chain) CompleteStream(ctx context.Context, req *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	c.mu.RLock()
	numEntries := len(c.entries)
	c.mu.RUnlock()

	var lastErr error
	for i := 0; i < numEntries; i++ {
		// Read entry state under a read lock (copy for local use).
		c.mu.RLock()
		if i >= len(c.entries) {
			c.mu.RUnlock()
			break
		}
		entry := c.entries[i] // value copy for reading
		c.mu.RUnlock()

		if !c.entryAvailable(i) {
			slog.Debug("fallback: skipping unavailable entry (stream)", "provider", entry.ProviderName)
			continue
		}

		if c.rateLimiter.ShouldSkip(entry.ProviderName) && !entry.IsLocalFallback {
			slog.Debug("fallback: skipping rate-limited entry (stream)", "provider", entry.ProviderName)
			continue
		}

		provider, ok := c.providers[entry.ProviderName]
		if !ok {
			slog.Warn("fallback: provider not registered (stream)", "provider", entry.ProviderName)
			continue
		}

		reqCopy := *req
		if entry.ModelID != "" {
			reqCopy.Model = entry.ModelID
		}

		ch, err := provider.CompleteStream(ctx, &reqCopy)
		if err == nil {
			c.writeBackSuccess(i)
			c.rateLimiter.ResetBackoff(entry.ProviderName)
			return ch, nil
		}

		lastErr = err
		c.writeBackError(i, err)
		slog.Warn("fallback: provider stream error, trying next",
			"provider", entry.ProviderName,
			"error", err)
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all providers exhausted, last error: %w", lastErr)
	}
	return nil, fmt.Errorf("all providers exhausted: no entries available")
}

// entryAvailable checks whether c.entries[idx] can accept a request, also
// performing the auto-recovery from Exhausted status when the cooldown has
// elapsed.  Mutations (status reset) are written back under a write lock so
// they persist across calls.
func (c *Chain) entryAvailable(idx int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if idx >= len(c.entries) {
		return false
	}
	e := &c.entries[idx]

	// Auto-recover from exhaustion once the cooldown window has elapsed.
	if e.Status == EntryExhausted {
		if !e.CooldownUntil.IsZero() && time.Now().After(e.CooldownUntil) {
			e.Status = EntryActive
			e.CooldownUntil = time.Time{}
		} else {
			return false
		}
	}

	// Defer to the circuit breaker if one is attached.
	if e.CircuitBreaker != nil && !e.CircuitBreaker.Allow() {
		return false
	}

	return true
}

// writeBackError inspects err and mutates c.entries[idx] directly under a
// write lock so the changes are visible to subsequent calls.
//
//   - HTTP 429: mark the entry Exhausted with a CooldownUntil derived from the
//     Retry-After header or the exponential-backoff sequence.
//   - HTTP 5xx: record a circuit-breaker failure.
//   - Any other error: record a circuit-breaker failure.
func (c *Chain) writeBackError(idx int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if idx >= len(c.entries) {
		return
	}
	entry := &c.entries[idx]

	var pe *brain.ProviderError
	if brain.AsProviderError(err, &pe) {
		switch {
		case pe.StatusCode == 429:
			backoff := c.rateLimiter.ParseRetryAfter(pe.Headers)
			if backoff <= 0 {
				backoff = c.rateLimiter.NextBackoff(entry.ProviderName)
			}
			entry.Status = EntryExhausted
			entry.CooldownUntil = time.Now().Add(backoff)
			slog.Warn("fallback: rate-limited, cooling down",
				"provider", entry.ProviderName,
				"cooldown", backoff)

		case pe.StatusCode >= 500:
			if entry.CircuitBreaker != nil {
				entry.CircuitBreaker.RecordFailure()
			}
			slog.Warn("fallback: upstream 5xx, recording circuit failure",
				"provider", entry.ProviderName,
				"status", pe.StatusCode)

		default:
			// 4xx other than 429: treat as a transient circuit failure.
			if entry.CircuitBreaker != nil {
				entry.CircuitBreaker.RecordFailure()
			}
		}
		return
	}

	// Non-ProviderError (network error, context cancelled, etc.).
	if entry.CircuitBreaker != nil {
		entry.CircuitBreaker.RecordFailure()
	}
}

// writeBackSuccess records a success on the circuit breaker for c.entries[idx]
// under a write lock.
func (c *Chain) writeBackSuccess(idx int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if idx >= len(c.entries) {
		return
	}
	if cb := c.entries[idx].CircuitBreaker; cb != nil {
		cb.RecordSuccess()
	}
}
