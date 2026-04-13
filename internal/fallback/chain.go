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
func (c *Chain) Complete(ctx context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	entries := c.Entries()

	var lastErr error
	for i := range entries {
		entry := &entries[i]

		if !entry.Available() {
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
			if entry.CircuitBreaker != nil {
				entry.CircuitBreaker.RecordSuccess()
			}
			c.rateLimiter.ResetBackoff(entry.ProviderName)
			return resp, nil
		}

		// Record the failure and decide on the next action.
		lastErr = err
		c.handleError(entry, err)
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
	entries := c.Entries()

	var lastErr error
	for i := range entries {
		entry := &entries[i]

		if !entry.Available() {
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
			if entry.CircuitBreaker != nil {
				entry.CircuitBreaker.RecordSuccess()
			}
			c.rateLimiter.ResetBackoff(entry.ProviderName)
			return ch, nil
		}

		lastErr = err
		c.handleError(entry, err)
		slog.Warn("fallback: provider stream error, trying next",
			"provider", entry.ProviderName,
			"error", err)
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all providers exhausted, last error: %w", lastErr)
	}
	return nil, fmt.Errorf("all providers exhausted: no entries available")
}

// handleError inspects err and updates the entry's circuit breaker and
// cooldown state accordingly.
//
//   - HTTP 429: mark the entry Exhausted, set CooldownUntil from Retry-After
//     header (or exponential backoff if no header is present).
//   - HTTP 5xx: record a circuit-breaker failure.
//   - Any other error (network, context, etc.): record a circuit-breaker failure.
func (c *Chain) handleError(entry *ChainEntry, err error) {
	var pe *brain.ProviderError
	if brain.AsProviderError(err, &pe) {
		switch {
		case pe.StatusCode == 429:
			// Parse Retry-After from the headers attached to the error.
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
