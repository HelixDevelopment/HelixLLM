package health_test

import (
	"context"
	"errors"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/health"
)

func TestNewChecker(t *testing.T) {
	checker := health.NewChecker()
	if checker == nil {
		t.Fatal("NewChecker() returned nil")
	}
}

func TestHealthyReport(t *testing.T) {
	checker := health.NewChecker()
	checker.Register("test-service", func(ctx context.Context) error {
		return nil
	})
	report := checker.Check(context.Background())
	if report.Status != health.StatusHealthy {
		t.Errorf("Status = %q, want %q", report.Status, health.StatusHealthy)
	}
	if len(report.Components) != 1 {
		t.Errorf("Components count = %d, want 1", len(report.Components))
	}
}

func TestUnhealthyReport(t *testing.T) {
	checker := health.NewChecker()
	checker.Register("failing-service", func(ctx context.Context) error {
		return errors.New("connection refused")
	})
	report := checker.Check(context.Background())
	if report.Status != health.StatusUnhealthy {
		t.Errorf("Status = %q, want %q", report.Status, health.StatusUnhealthy)
	}
}

func TestDegradedReport(t *testing.T) {
	checker := health.NewChecker()
	checker.Register("required-service", func(ctx context.Context) error {
		return nil
	})
	checker.RegisterOptional("optional-service", func(ctx context.Context) error {
		return errors.New("unavailable")
	})
	report := checker.Check(context.Background())
	if report.Status != health.StatusDegraded {
		t.Errorf("Status = %q, want %q", report.Status, health.StatusDegraded)
	}
}
