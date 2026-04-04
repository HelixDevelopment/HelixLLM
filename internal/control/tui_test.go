package control

import (
	"context"
	"testing"
	"time"
)

func TestRunMonitor_ExitsOnContextCancel(t *testing.T) {
	mock := newMockSSHClient()
	mock.reachable["host1"] = true
	mock.outputs["host1"] = linuxProbeOutput()

	prober := NewHostProber(mock, "user", "key")
	cp := &ControlPlane{
		prober:    prober,
		scheduler: NewScheduler(""),
		deployer:  NewDeployer(mock, "user", "key"),
		monitor:   NewMonitor(prober, mock, 30*time.Second),
		hosts:     []string{"host1"},
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- RunMonitor(ctx, cp, 50*time.Millisecond)
	}()

	// Cancel almost immediately; monitor should unblock.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("RunMonitor() returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunMonitor() did not exit after context cancellation")
	}
}

func TestRunMonitor_RendersWithNoHosts(t *testing.T) {
	mock := newMockSSHClient()
	prober := NewHostProber(mock, "user", "key")
	cp := &ControlPlane{
		prober:    prober,
		scheduler: NewScheduler(""),
		deployer:  NewDeployer(mock, "user", "key"),
		monitor:   NewMonitor(prober, mock, 30*time.Second),
		hosts:     nil,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// Should return nil without panicking even with an empty host list.
	err := RunMonitor(ctx, cp, 100*time.Millisecond)
	if err != nil {
		t.Errorf("RunMonitor() error = %v, want nil", err)
	}
}
