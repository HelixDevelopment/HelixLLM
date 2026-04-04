package control

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMonitor_CheckCluster_AllHealthy(t *testing.T) {
	mock := newMockSSHClient()
	mock.reachable["host1"] = true
	mock.reachable["host2"] = true
	mock.outputs["host1"] = linuxProbeOutput()
	mock.outputs["host2"] = linuxProbeOutputNoGPU()

	prober := NewHostProber(mock, "user", "key")
	mon := NewMonitor(prober, mock, 30*time.Second)

	status, err := mon.CheckCluster(
		context.Background(),
		[]string{"host1", "host2"},
	)
	if err != nil {
		t.Fatalf("CheckCluster() error: %v", err)
	}
	if !status.Healthy {
		t.Error("expected cluster to be healthy")
	}
	if len(status.Hosts) != 2 {
		t.Errorf("len(Hosts) = %d, want 2", len(status.Hosts))
	}
	if status.HealthyHostCount() != 2 {
		t.Errorf("HealthyHostCount() = %d, want 2",
			status.HealthyHostCount())
	}
}

func TestMonitor_CheckCluster_PartialFailure(t *testing.T) {
	mock := newMockSSHClient()
	mock.reachable["good"] = true
	mock.reachable["bad"] = false
	mock.outputs["good"] = linuxProbeOutput()

	prober := NewHostProber(mock, "user", "key")
	mon := NewMonitor(prober, mock, 30*time.Second)

	status, err := mon.CheckCluster(
		context.Background(),
		[]string{"good", "bad"},
	)
	if err != nil {
		t.Fatalf("CheckCluster() error: %v", err)
	}
	// Cluster is degraded, not fully healthy.
	if status.Healthy {
		t.Error("expected cluster to not be healthy with one offline host")
	}
	if len(status.Hosts) != 2 {
		t.Errorf("len(Hosts) = %d, want 2", len(status.Hosts))
	}
	if status.HealthyHostCount() != 1 {
		t.Errorf("HealthyHostCount() = %d, want 1",
			status.HealthyHostCount())
	}

	// Verify the bad host is marked offline.
	for _, h := range status.Hosts {
		if h.Name == "bad" && h.State != "offline" {
			t.Errorf("bad host State = %q, want %q",
				h.State, "offline")
		}
	}
}

func TestMonitor_CheckCluster_AllOffline(t *testing.T) {
	mock := newMockSSHClient()
	mock.reachable["h1"] = false
	mock.reachable["h2"] = false

	prober := NewHostProber(mock, "user", "key")
	mon := NewMonitor(prober, mock, 30*time.Second)

	status, err := mon.CheckCluster(
		context.Background(),
		[]string{"h1", "h2"},
	)
	if err != nil {
		t.Fatalf("CheckCluster() error: %v", err)
	}
	if status.Healthy {
		t.Error("expected cluster to not be healthy")
	}
	if status.HealthyHostCount() != 0 {
		t.Errorf("HealthyHostCount() = %d, want 0",
			status.HealthyHostCount())
	}
}

func TestMonitor_CheckHost(t *testing.T) {
	mock := newMockSSHClient()
	mock.reachable["host1"] = true
	mock.outputs["host1"] = linuxProbeOutput()

	prober := NewHostProber(mock, "user", "key")
	mon := NewMonitor(prober, mock, 30*time.Second)

	profile, err := mon.CheckHost(context.Background(), "host1")
	if err != nil {
		t.Fatalf("CheckHost() error: %v", err)
	}
	if profile.Name != "host1" {
		t.Errorf("Name = %q, want %q", profile.Name, "host1")
	}
	if !profile.IsHealthy() {
		t.Error("expected host to be healthy")
	}
}

func TestMonitor_CheckHost_Offline(t *testing.T) {
	mock := newMockSSHClient()
	mock.reachable["offline"] = false

	prober := NewHostProber(mock, "user", "key")
	mon := NewMonitor(prober, mock, 30*time.Second)

	_, err := mon.CheckHost(context.Background(), "offline")
	if err == nil {
		t.Fatal("expected error for offline host")
	}
	if !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("error = %q, want to contain %q",
			err.Error(), "unreachable")
	}
}

func TestMonitor_Status_CachedAfterCheck(t *testing.T) {
	mock := newMockSSHClient()
	mock.reachable["host1"] = true
	mock.outputs["host1"] = linuxProbeOutput()

	prober := NewHostProber(mock, "user", "key")
	mon := NewMonitor(prober, mock, 30*time.Second)

	// Status before any check should be nil.
	if s := mon.Status(); s != nil {
		t.Error("expected nil status before first check")
	}

	// Run a cluster check.
	_, err := mon.CheckCluster(
		context.Background(), []string{"host1"},
	)
	if err != nil {
		t.Fatalf("CheckCluster() error: %v", err)
	}

	// Status should now be cached.
	status := mon.Status()
	if status == nil {
		t.Fatal("expected non-nil status after check")
	}
	if len(status.Hosts) != 1 {
		t.Errorf("len(Hosts) = %d, want 1", len(status.Hosts))
	}
}

func TestMonitor_CheckCluster_Empty(t *testing.T) {
	mock := newMockSSHClient()
	prober := NewHostProber(mock, "user", "key")
	mon := NewMonitor(prober, mock, 30*time.Second)

	status, err := mon.CheckCluster(context.Background(), nil)
	if err != nil {
		t.Fatalf("CheckCluster() error: %v", err)
	}
	if len(status.Hosts) != 0 {
		t.Errorf("len(Hosts) = %d, want 0", len(status.Hosts))
	}
	// Empty cluster is healthy by default.
	if !status.Healthy {
		t.Error("expected empty cluster to be healthy")
	}
}

func TestMonitor_StartStop(t *testing.T) {
	mock := newMockSSHClient()
	mock.reachable["host1"] = true
	mock.outputs["host1"] = linuxProbeOutput()

	prober := NewHostProber(mock, "user", "key")
	mon := NewMonitor(prober, mock, 50*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	mon.Start(ctx, []string{"host1"})

	// Wait a bit for at least one check cycle.
	time.Sleep(150 * time.Millisecond)

	// Should have cached status.
	status := mon.Status()
	if status == nil {
		t.Fatal("expected non-nil status after monitoring started")
	}

	cancel()
	mon.Stop()

	// Should not panic after stop.
	mon.Stop()
}
