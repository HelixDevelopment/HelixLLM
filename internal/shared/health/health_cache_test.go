package health_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/health"
)

func TestChecker_CachedResult(t *testing.T) {
	var callCount atomic.Int64
	c := health.NewCheckerWithOptions(health.WithCacheDuration(100 * time.Millisecond))
	c.Register("test", func(_ context.Context) error {
		callCount.Add(1)
		return nil
	})

	// First call runs the check.
	r1 := c.Check(context.Background())
	if r1.Status != health.StatusHealthy {
		t.Fatalf("first check: status = %s, want healthy", r1.Status)
	}
	if callCount.Load() != 1 {
		t.Errorf("expected 1 call, got %d", callCount.Load())
	}

	// Immediate second call should use cache.
	_ = c.Check(context.Background())
	if callCount.Load() != 1 {
		t.Errorf("expected still 1 call (cached), got %d", callCount.Load())
	}

	// After cache expires, should call again.
	time.Sleep(150 * time.Millisecond)
	_ = c.Check(context.Background())
	if callCount.Load() != 2 {
		t.Errorf("expected 2 calls after cache expiry, got %d", callCount.Load())
	}
}

func TestChecker_NoCacheByDefault(t *testing.T) {
	var callCount atomic.Int64
	c := health.NewChecker()
	c.Register("test", func(_ context.Context) error {
		callCount.Add(1)
		return nil
	})

	_ = c.Check(context.Background())
	_ = c.Check(context.Background())

	if callCount.Load() != 2 {
		t.Errorf("expected 2 calls (no caching), got %d", callCount.Load())
	}
}

func TestCheckerWithOptions_ZeroDuration(t *testing.T) {
	var callCount atomic.Int64
	c := health.NewCheckerWithOptions(health.WithCacheDuration(0))
	c.Register("test", func(_ context.Context) error {
		callCount.Add(1)
		return nil
	})

	_ = c.Check(context.Background())
	_ = c.Check(context.Background())

	if callCount.Load() != 2 {
		t.Errorf("expected 2 calls (zero cache duration), got %d", callCount.Load())
	}
}
