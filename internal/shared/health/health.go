// Package health wraps digital.vasic.observability/pkg/health with a thin
// Checker facade for HelixLLM services.
package health

import (
	"context"

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
	agg *obshealth.Aggregator
}

// NewChecker returns a Checker with default configuration (5 s timeout per
// component).
func NewChecker() *Checker {
	return &Checker{agg: obshealth.NewAggregator(obshealth.DefaultAggregatorConfig())}
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
// Report.
func (c *Checker) Check(ctx context.Context) *Report {
	return c.agg.Check(ctx)
}
