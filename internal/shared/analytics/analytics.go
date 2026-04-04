// Package analytics provides an event collection interface for HelixLLM.
//
// When ClickHouse is not configured the package silently discards all events
// via NoOpCollector.  When an address is provided the package delegates to
// digital.vasic.observability/pkg/analytics for real ClickHouse-backed
// collection, with automatic fallback to NoOpCollector if the connection fails.
package analytics

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	obsanalytics "digital.vasic.observability/pkg/analytics"
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

// clickHouseAdapter wraps the observability ClickHouseCollector, adapting its
// richer interface to the local Collector interface.
type clickHouseAdapter struct {
	inner *obsanalytics.ClickHouseCollector
}

// Track translates the local Event to the observability Event and delegates.
func (a *clickHouseAdapter) Track(ctx context.Context, event Event) error {
	return a.inner.Track(ctx, obsanalytics.Event{
		Name:       event.Name,
		Timestamp:  event.Timestamp,
		Properties: event.Properties,
		Tags:       event.Tags,
	})
}

// Close releases the underlying ClickHouse connection.
func (a *clickHouseAdapter) Close() error {
	return a.inner.Close()
}

// NewCollector creates an analytics Collector based on the supplied
// configuration.
//
// When clickhouseAddr is empty a NoOpCollector is returned so the caller can
// always use the Collector interface without nil-checks.
//
// When clickhouseAddr is provided the implementation attempts to connect to
// ClickHouse via digital.vasic.observability/pkg/analytics.  If the connection
// fails it logs a warning and falls back to NoOpCollector so the application
// continues to operate without analytics.
//
// clickhouseAddr must be in "host:port" format.  database names the target
// ClickHouse database (defaults to "helixllm" when empty).
func NewCollector(clickhouseAddr, database string) Collector {
	if clickhouseAddr == "" {
		return &NoOpCollector{}
	}

	if database == "" {
		database = "helixllm"
	}

	host, portStr, err := net.SplitHostPort(clickhouseAddr)
	if err != nil {
		fmt.Printf("[analytics] invalid ClickHouse address %q: %v — using no-op collector\n",
			clickhouseAddr, err)
		return &NoOpCollector{}
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		fmt.Printf("[analytics] invalid ClickHouse port in address %q: %v — using no-op collector\n",
			clickhouseAddr, err)
		return &NoOpCollector{}
	}

	cfg := obsanalytics.ClickHouseConfig{
		Host:     host,
		Port:     port,
		Database: database,
		Table:    "events",
	}

	collector, err := obsanalytics.NewClickHouseCollector(cfg, nil)
	if err != nil {
		fmt.Printf("[analytics] ClickHouse unavailable (%v) — using no-op collector\n", err)
		return &NoOpCollector{}
	}

	return &clickHouseAdapter{inner: collector}
}
