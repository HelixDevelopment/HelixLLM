package control

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// deployMockSSH implements SSHClient for deployer tests.
type deployMockSSH struct {
	runs      []mockRunCall
	responses map[string]string
	errors    map[string]error
	reachable map[string]bool
}

func newDeployMockSSH() *deployMockSSH {
	return &deployMockSSH{
		responses: make(map[string]string),
		errors:    make(map[string]error),
		reachable: make(map[string]bool),
	}
}

func (m *deployMockSSH) Run(
	ctx context.Context, host, command string,
) (string, error) {
	m.runs = append(m.runs, mockRunCall{
		Host: host, Command: command,
	})
	key := host
	if err, ok := m.errors[key]; ok {
		return "", err
	}
	if resp, ok := m.responses[key]; ok {
		return resp, nil
	}
	return "", nil
}

func (m *deployMockSSH) IsReachable(
	ctx context.Context, host string,
) bool {
	if v, ok := m.reachable[host]; ok {
		return v
	}
	return true
}

func TestDeployer_Deploy_Success(t *testing.T) {
	mock := newDeployMockSSH()
	mock.reachable["host1"] = true

	deployer := NewDeployer(mock, "milosvasic", "~/.ssh/id_ed25519")
	placement := PlacementResult{
		Service: ServiceRequirement{
			Name:  "redis",
			Image: "redis:7-alpine",
		},
		HostName: "host1",
		Score:    0.85,
	}

	info, err := deployer.Deploy(context.Background(), placement)
	if err != nil {
		t.Fatalf("Deploy() error: %v", err)
	}
	if info.ServiceName != "redis" {
		t.Errorf("ServiceName = %q, want %q",
			info.ServiceName, "redis")
	}
	if info.HostName != "host1" {
		t.Errorf("HostName = %q, want %q",
			info.HostName, "host1")
	}
	if info.State != "running" {
		t.Errorf("State = %q, want %q", info.State, "running")
	}

	// Verify SSH commands were called.
	if len(mock.runs) < 2 {
		t.Fatalf("expected at least 2 SSH commands, got %d",
			len(mock.runs))
	}
	// First command should be the remove.
	if !strings.Contains(mock.runs[0].Command, "rm -f") {
		t.Errorf("first command should remove old container: %q",
			mock.runs[0].Command)
	}
	// Second command should be the run.
	if !strings.Contains(mock.runs[1].Command, "run -d") {
		t.Errorf("second command should run container: %q",
			mock.runs[1].Command)
	}
}

func TestDeployer_Deploy_SSHError(t *testing.T) {
	mock := newDeployMockSSH()
	mock.errors["badhost"] = fmt.Errorf("connection refused")

	deployer := NewDeployer(mock, "user", "key")
	placement := PlacementResult{
		Service: ServiceRequirement{
			Name:  "app",
			Image: "app:latest",
		},
		HostName: "badhost",
	}

	info, err := deployer.Deploy(context.Background(), placement)
	if err == nil {
		t.Fatal("expected error for SSH failure")
	}
	if info != nil && info.State != "failed" {
		t.Errorf("State = %q, want %q", info.State, "failed")
	}
}

func TestDeployer_Deploy_Unreachable(t *testing.T) {
	mock := newDeployMockSSH()
	mock.reachable["offline"] = false

	deployer := NewDeployer(mock, "user", "key")
	placement := PlacementResult{
		Service: ServiceRequirement{
			Name:  "app",
			Image: "app:latest",
		},
		HostName: "offline",
	}

	_, err := deployer.Deploy(context.Background(), placement)
	if err == nil {
		t.Fatal("expected error for unreachable host")
	}
	if !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("error = %q, want to contain %q",
			err.Error(), "unreachable")
	}
}

func TestDeployer_DeployAll(t *testing.T) {
	mock := newDeployMockSSH()
	mock.reachable["host1"] = true
	mock.reachable["host2"] = true

	deployer := NewDeployer(mock, "user", "key")
	placements := []PlacementResult{
		{
			Service:  ServiceRequirement{Name: "svc1", Image: "img1"},
			HostName: "host1",
		},
		{
			Service:  ServiceRequirement{Name: "svc2", Image: "img2"},
			HostName: "host2",
		},
	}

	infos, errs := deployer.DeployAll(
		context.Background(), placements,
	)
	if len(errs) != 0 {
		t.Errorf("len(errs) = %d, want 0: %v", len(errs), errs)
	}
	if len(infos) != 2 {
		t.Fatalf("len(infos) = %d, want 2", len(infos))
	}
	for _, info := range infos {
		if info.State != "running" {
			t.Errorf("State = %q, want %q",
				info.State, "running")
		}
	}
}

func TestDeployer_DeployAll_PartialFailure(t *testing.T) {
	mock := newDeployMockSSH()
	mock.reachable["good"] = true
	mock.errors["bad"] = fmt.Errorf("SSH failed")

	deployer := NewDeployer(mock, "user", "key")
	placements := []PlacementResult{
		{
			Service:  ServiceRequirement{Name: "ok", Image: "img1"},
			HostName: "good",
		},
		{
			Service:  ServiceRequirement{Name: "fail", Image: "img2"},
			HostName: "bad",
		},
	}

	infos, errs := deployer.DeployAll(
		context.Background(), placements,
	)
	if len(infos) != 1 {
		t.Errorf("len(infos) = %d, want 1", len(infos))
	}
	if len(errs) != 1 {
		t.Errorf("len(errs) = %d, want 1", len(errs))
	}
}

func TestDeployer_Undeploy(t *testing.T) {
	mock := newDeployMockSSH()
	deployer := NewDeployer(mock, "user", "key")

	dep := DeploymentInfo{
		ServiceName: "redis",
		HostName:    "host1",
		State:       "running",
	}

	err := deployer.Undeploy(context.Background(), dep)
	if err != nil {
		t.Fatalf("Undeploy() error: %v", err)
	}

	if len(mock.runs) != 1 {
		t.Fatalf("expected 1 SSH command, got %d", len(mock.runs))
	}
	if !strings.Contains(mock.runs[0].Command, "stop") ||
		!strings.Contains(mock.runs[0].Command, "rm") {
		t.Errorf("command should stop and remove: %q",
			mock.runs[0].Command)
	}
}

func TestDeployer_Deploy_UsesCorrectRuntime(t *testing.T) {
	mock := newDeployMockSSH()
	deployer := NewDeployer(mock, "user", "key")

	// Default runtime is podman.
	placement := PlacementResult{
		Service:  ServiceRequirement{Name: "svc", Image: "img"},
		HostName: "host1",
	}

	_, err := deployer.Deploy(context.Background(), placement)
	if err != nil {
		t.Fatalf("Deploy() error: %v", err)
	}

	// Commands should use podman by default.
	for _, run := range mock.runs {
		if !strings.Contains(run.Command, "podman") {
			t.Errorf("command should use podman: %q", run.Command)
		}
	}
}

func TestDeployer_SetRuntime(t *testing.T) {
	mock := newDeployMockSSH()
	deployer := NewDeployer(mock, "user", "key")

	deployer.SetRuntime("docker")

	placement := PlacementResult{
		Service:  ServiceRequirement{Name: "svc", Image: "img"},
		HostName: "host1",
	}

	_, err := deployer.Deploy(context.Background(), placement)
	if err != nil {
		t.Fatalf("Deploy() error: %v", err)
	}

	// Commands should use docker.
	for _, run := range mock.runs {
		if !strings.Contains(run.Command, "docker") {
			t.Errorf("command should use docker: %q", run.Command)
		}
	}
}

func TestDeployer_SetRuntime_EmptyIgnored(t *testing.T) {
	mock := newDeployMockSSH()
	deployer := NewDeployer(mock, "user", "key")

	deployer.SetRuntime("") // Should not change from default podman.

	placement := PlacementResult{
		Service:  ServiceRequirement{Name: "svc", Image: "img"},
		HostName: "host1",
	}

	_, err := deployer.Deploy(context.Background(), placement)
	if err != nil {
		t.Fatalf("Deploy() error: %v", err)
	}

	for _, run := range mock.runs {
		if !strings.Contains(run.Command, "podman") {
			t.Errorf("command should still use podman: %q", run.Command)
		}
	}
}

func TestDeployer_Undeploy_SSHError(t *testing.T) {
	mock := newDeployMockSSH()
	mock.errors["host1"] = fmt.Errorf("SSH connection lost")

	deployer := NewDeployer(mock, "user", "key")
	dep := DeploymentInfo{
		ServiceName: "redis",
		HostName:    "host1",
		State:       "running",
	}

	err := deployer.Undeploy(context.Background(), dep)
	if err == nil {
		t.Fatal("expected error for SSH failure during undeploy")
	}
	if !strings.Contains(err.Error(), "undeploy") {
		t.Errorf("error = %q, want to contain 'undeploy'", err.Error())
	}
}

func TestDeployer_DeployAll_Empty(t *testing.T) {
	mock := newDeployMockSSH()
	deployer := NewDeployer(mock, "user", "key")

	infos, errs := deployer.DeployAll(context.Background(), nil)
	if len(infos) != 0 {
		t.Errorf("len(infos) = %d, want 0", len(infos))
	}
	if len(errs) != 0 {
		t.Errorf("len(errs) = %d, want 0", len(errs))
	}
}
