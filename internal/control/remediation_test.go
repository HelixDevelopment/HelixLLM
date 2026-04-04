package control

import (
	"context"
	"errors"
	"testing"
	"time"
)

// mockRemediator records restart calls and can be configured to fail.
type mockRemediator struct {
	calls    []restartCall
	failFor  map[string]error // service → error to return
	succeedAfter map[string]int // service → succeed after N calls
}

type restartCall struct {
	Host    string
	Service string
}

func newMockRemediator() *mockRemediator {
	return &mockRemediator{
		failFor:      make(map[string]error),
		succeedAfter: make(map[string]int),
	}
}

func (r *mockRemediator) RestartContainer(
	_ context.Context, host, service string,
) error {
	r.calls = append(r.calls, restartCall{Host: host, Service: service})
	if threshold, ok := r.succeedAfter[service]; ok {
		if len(r.calls) > threshold {
			return nil
		}
	}
	if err, ok := r.failFor[service]; ok {
		return err
	}
	return nil
}

// makeMonitorWithStatus is a test helper that wires a Monitor with a
// pre-populated ClusterStatus so we can test Remediate in isolation.
func makeMonitorWithStatus(status *ClusterStatus, rem Remediator) *Monitor {
	mock := newMockSSHClient()
	prober := NewHostProber(mock, "user", "key")
	m := NewMonitor(prober, mock, 30*time.Second)
	m.status = status
	if rem != nil {
		m.WithRemediator(rem)
	}
	return m
}

// TestRemediate_NoStatus verifies that Remediate is a no-op when no
// status has been set yet.
func TestRemediate_NoStatus(t *testing.T) {
	m := makeMonitorWithStatus(nil, nil)
	actions := m.Remediate(context.Background())
	if len(actions) != 0 {
		t.Errorf("Remediate() = %v, want empty", actions)
	}
}

// TestRemediate_AllHealthy verifies no actions are taken when every
// deployment is running.
func TestRemediate_AllHealthy(t *testing.T) {
	status := &ClusterStatus{
		Hosts: []HostProfile{
			{Name: "host1", State: "online"},
		},
		Deployments: []DeploymentInfo{
			{ServiceName: "svc1", HostName: "host1", State: "running"},
		},
	}
	rem := newMockRemediator()
	m := makeMonitorWithStatus(status, rem)

	actions := m.Remediate(context.Background())
	if len(actions) != 0 {
		t.Errorf("Remediate() = %v, want empty for healthy deployment", actions)
	}
	if len(rem.calls) != 0 {
		t.Errorf("RestartContainer called %d times, want 0", len(rem.calls))
	}
}

// TestRemediate_RestartOnFirstFailure verifies that the first failed
// deployment triggers a "restart" action.
func TestRemediate_RestartOnFirstFailure(t *testing.T) {
	status := &ClusterStatus{
		Hosts: []HostProfile{
			{Name: "host1", State: "online"},
		},
		Deployments: []DeploymentInfo{
			{ServiceName: "svc1", HostName: "host1", State: "failed"},
		},
	}
	rem := newMockRemediator()
	m := makeMonitorWithStatus(status, rem)

	actions := m.Remediate(context.Background())
	if len(actions) != 1 {
		t.Fatalf("Remediate() returned %d actions, want 1", len(actions))
	}
	if actions[0].Type != "restart" {
		t.Errorf("action.Type = %q, want %q", actions[0].Type, "restart")
	}
	if actions[0].Service != "svc1" {
		t.Errorf("action.Service = %q, want %q", actions[0].Service, "svc1")
	}
	if len(rem.calls) != 1 {
		t.Errorf("RestartContainer called %d times, want 1", len(rem.calls))
	}
}

// TestRemediate_RescheduleAfterMaxRestarts verifies that after
// maxRestartAttempts consecutive failures the monitor emits a
// "reschedule" action targeting a healthy host.
func TestRemediate_RescheduleAfterMaxRestarts(t *testing.T) {
	// host1 is the failing host; host2 is healthy and available for
	// rescheduling.
	status := &ClusterStatus{
		Hosts: []HostProfile{
			{Name: "host1", State: "online"},
			{Name: "host2", State: "online"},
		},
		Deployments: []DeploymentInfo{
			{ServiceName: "svc1", HostName: "host1", State: "failed"},
		},
	}
	errFail := errors.New("container restart failed")
	rem := newMockRemediator()
	rem.failFor["svc1"] = errFail
	m := makeMonitorWithStatus(status, rem)

	// Drive the monitor to maxRestartAttempts failures.
	for i := 0; i < maxRestartAttempts; i++ {
		actions := m.Remediate(context.Background())
		if len(actions) != 1 {
			t.Fatalf("iteration %d: got %d actions, want 1", i, len(actions))
		}
		if actions[0].Type != "restart" {
			t.Errorf("iteration %d: action.Type = %q, want restart", i, actions[0].Type)
		}
	}

	// Next call should escalate to reschedule.
	actions := m.Remediate(context.Background())
	if len(actions) != 1 {
		t.Fatalf("escalation: got %d actions, want 1", len(actions))
	}
	if actions[0].Type != "reschedule" {
		t.Errorf("escalation: action.Type = %q, want reschedule", actions[0].Type)
	}
	if actions[0].Service != "svc1" {
		t.Errorf("reschedule action.Service = %q, want svc1", actions[0].Service)
	}
	// The rescheduled host must be one of the healthy hosts.
	if actions[0].Host != "host1" && actions[0].Host != "host2" {
		t.Errorf("reschedule action.Host = %q, want a healthy host", actions[0].Host)
	}
}

// TestRemediate_AlertWhenNoHealthyHosts verifies that an "alert" is
// generated when max restarts are reached and no healthy host is
// available for rescheduling.
func TestRemediate_AlertWhenNoHealthyHosts(t *testing.T) {
	status := &ClusterStatus{
		Hosts: []HostProfile{
			{Name: "host1", State: "offline"}, // unhealthy — can't reschedule here
		},
		Deployments: []DeploymentInfo{
			{ServiceName: "svc1", HostName: "host1", State: "failed"},
		},
	}
	errFail := errors.New("cannot restart")
	rem := newMockRemediator()
	rem.failFor["svc1"] = errFail
	m := makeMonitorWithStatus(status, rem)

	// Exhaust restart attempts.
	for i := 0; i < maxRestartAttempts; i++ {
		m.Remediate(context.Background()) //nolint:errcheck
	}

	// Should now emit alert.
	actions := m.Remediate(context.Background())
	if len(actions) != 1 {
		t.Fatalf("got %d actions, want 1", len(actions))
	}
	if actions[0].Type != "alert" {
		t.Errorf("action.Type = %q, want alert", actions[0].Type)
	}
}

// TestRemediate_CounterResetOnRecovery verifies that once a deployment
// returns to "running", its restart counter is cleared so a future
// failure starts fresh.
func TestRemediate_CounterResetOnRecovery(t *testing.T) {
	errFail := errors.New("fail")
	rem := newMockRemediator()
	rem.failFor["svc1"] = errFail

	failedStatus := &ClusterStatus{
		Hosts: []HostProfile{{Name: "host1", State: "online"}},
		Deployments: []DeploymentInfo{
			{ServiceName: "svc1", HostName: "host1", State: "failed"},
		},
	}
	m := makeMonitorWithStatus(failedStatus, rem)

	// One failed restart — counter = 1.
	m.Remediate(context.Background())

	// Deployment recovers.
	recoveredStatus := &ClusterStatus{
		Hosts: []HostProfile{{Name: "host1", State: "online"}},
		Deployments: []DeploymentInfo{
			{ServiceName: "svc1", HostName: "host1", State: "running"},
		},
	}
	m.status = recoveredStatus
	m.Remediate(context.Background())

	// Fail again — counter should be back at 0 before this call.
	m.status = failedStatus
	actions := m.Remediate(context.Background())
	if len(actions) != 1 || actions[0].Type != "restart" {
		t.Errorf("after recovery, expected fresh restart action, got %v", actions)
	}
}

// TestRemediate_NilRemediator verifies that Remediate does not panic
// when no Remediator has been attached, and still records an action.
func TestRemediate_NilRemediator(t *testing.T) {
	status := &ClusterStatus{
		Hosts: []HostProfile{{Name: "host1", State: "online"}},
		Deployments: []DeploymentInfo{
			{ServiceName: "svc1", HostName: "host1", State: "failed"},
		},
	}
	m := makeMonitorWithStatus(status, nil) // no remediator

	actions := m.Remediate(context.Background())
	if len(actions) != 1 {
		t.Fatalf("Remediate() = %d actions, want 1", len(actions))
	}
	if actions[0].Type != "restart" {
		t.Errorf("action.Type = %q, want restart", actions[0].Type)
	}
}
