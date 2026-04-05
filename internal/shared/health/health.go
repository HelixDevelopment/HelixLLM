// Package health wraps digital.vasic.observability/pkg/health with a thin
// Checker facade for HelixLLM services.
package health

import (
	"context"
	"sync"
	"time"

	obshealth "digital.vasic.observability/pkg/health"
)

// Re-export status constants so callers only import this package.
const (
	StatusHealthy   = obshealth.StatusHealthy
	StatusDegraded  = obshealth.StatusDegraded
	StatusUnhealthy = obshealth.StatusUnhealthy
)

// Type aliases so callers work entirely through this package.
type (
	Status          = obshealth.Status
	Report          = obshealth.Report
	ComponentResult = obshealth.ComponentResult
	CheckFunc       = obshealth.CheckFunc
)

// Checker aggregates component health checks into an overall Report.
type Checker struct {
	agg           *obshealth.Aggregator
	cacheDuration time.Duration
	cachedReport  *Report
	cachedAt      time.Time
	cacheMu       sync.RWMutex
}

// Option configures a Checker.
type Option func(*Checker)

// WithCacheDuration sets how long health check results are cached.
// Zero means no caching (every Check call runs all checks).
func WithCacheDuration(d time.Duration) Option {
	return func(c *Checker) {
		c.cacheDuration = d
	}
}

// NewChecker returns a Checker with default configuration (5 s timeout per
// component, no caching).
func NewChecker() *Checker {
	return &Checker{agg: obshealth.NewAggregator(obshealth.DefaultAggregatorConfig())}
}

// NewCheckerWithOptions returns a Checker with the given options applied.
func NewCheckerWithOptions(opts ...Option) *Checker {
	c := &Checker{agg: obshealth.NewAggregator(obshealth.DefaultAggregatorConfig())}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Register adds a required component check. A failure marks the system
// as StatusUnhealthy.
func (c *Checker) Register(name string, fn CheckFunc) {
	c.agg.Register(name, fn)
}

// RegisterOptional adds an optional component check. A failure marks the
// system as StatusDegraded (not StatusUnhealthy).
func (c *Checker) RegisterOptional(name string, fn CheckFunc) {
	c.agg.RegisterOptional(name, fn)
}

// Check runs all registered checks in parallel and returns an aggregated
// Report. When a cache duration is configured, results are reused until the
// cache expires, avoiding redundant check execution on frequent calls.
func (c *Checker) Check(ctx context.Context) *Report {
	if c.cacheDuration > 0 {
		c.cacheMu.RLock()
		if c.cachedReport != nil && time.Since(c.cachedAt) < c.cacheDuration {
			r := c.cachedReport
			c.cacheMu.RUnlock()
			return r
		}
		c.cacheMu.RUnlock()
	}

	report := c.agg.Check(ctx)

	if c.cacheDuration > 0 {
		c.cacheMu.Lock()
		c.cachedReport = report
		c.cachedAt = time.Now()
		c.cacheMu.Unlock()
	}

	return report
}
