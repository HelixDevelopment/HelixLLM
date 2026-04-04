// Package analytics provides an event collection interface for HelixLLM.
//
// When ClickHouse is not configured the package silently discards all events
// via NoOpCollector.  A real ClickHouse-backed collector will be wired in once
// the ClickHouse container is available via
// digital.vasic.observability/pkg/analytics.
package analytics

import (
	"context"
	"time"
)

// Event represents a single analytics event.
type Event struct {
	// Name is the event identifier (e.g. "request.complete", "model.loaded").
	Name string `json:"name"`

	// Timestamp records when the event occurred.  If zero, callers should
	// substitute time.Now() before tracking.
	Timestamp time.Time `json:"timestamp"`

	// Properties holds arbitrary key-value pairs attached to the event.
	Properties map[string]interface{} `json:"properties,omitempty"`

	// Tags holds string-valued labels used for filtering in dashboards.
	Tags map[string]string `json:"tags,omitempty"`
}

// Collector collects analytics events.
type Collector interface {
	// Track records an event.  Implementations must be safe for concurrent use.
	Track(ctx context.Context, event Event) error

	// Close flushes any buffered events and releases resources.
	Close() error
}

// NoOpCollector silently discards all events.  It is used when no analytics
// backend is configured.
type NoOpCollector struct{}

// Track discards event and returns nil.
func (n *NoOpCollector) Track(_ context.Context, _ Event) error { return nil }

// Close is a no-op and returns nil.
func (n *NoOpCollector) Close() error { return nil }

// NewCollector creates an analytics Collector based on the supplied
// configuration.
//
// When clickhouseAddr is empty a NoOpCollector is returned so the caller can
// always use the Collector interface without nil-checks.
//
// When ClickHouse is available the implementation will delegate to
// digital.vasic.observability/pkg/analytics; until then it falls back to
// NoOpCollector.
func NewCollector(clickhouseAddr, _ string) Collector {
	if clickhouseAddr == "" {
		return &NoOpCollector{}
	}
	// TODO: wire real ClickHouse collector via
	// digital.vasic.observability/pkg/analytics once container is available.
	return &NoOpCollector{}
}
