package control

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestDeployer_RestartContainer_Success(t *testing.T) {
	mock := newDeployMockSSH()
	deployer := NewDeployer(mock, "user", "key")

	err := deployer.RestartContainer(context.Background(), "host1", "redis")
	if err != nil {
		t.Fatalf("RestartContainer: %v", err)
	}
	if len(mock.runs) != 1 {
		t.Fatalf("expected 1 SSH command, got %d", len(mock.runs))
	}
	if !strings.Contains(mock.runs[0].Command, "restart") {
		t.Errorf("command should contain 'restart': %q", mock.runs[0].Command)
	}
	if !strings.Contains(mock.runs[0].Command, "redis") {
		t.Errorf("command should contain service name 'redis': %q", mock.runs[0].Command)
	}
	if mock.runs[0].Host != "host1" {
		t.Errorf("host = %q, want %q", mock.runs[0].Host, "host1")
	}
}

func TestDeployer_RestartContainer_SSHError(t *testing.T) {
	mock := newDeployMockSSH()
	mock.errors["host1"] = fmt.Errorf("SSH timeout")

	deployer := NewDeployer(mock, "user", "key")

	err := deployer.RestartContainer(context.Background(), "host1", "redis")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "restart") {
		t.Errorf("error should mention 'restart': %q", err.Error())
	}
	if !strings.Contains(err.Error(), "redis") {
		t.Errorf("error should mention service 'redis': %q", err.Error())
	}
}

func TestDeployer_RestartContainer_UsesConfiguredRuntime(t *testing.T) {
	mock := newDeployMockSSH()
	deployer := NewDeployer(mock, "user", "key")
	deployer.SetRuntime("docker")

	err := deployer.RestartContainer(context.Background(), "host1", "app")
	if err != nil {
		t.Fatalf("RestartContainer: %v", err)
	}
	if len(mock.runs) != 1 {
		t.Fatalf("expected 1 SSH command, got %d", len(mock.runs))
	}
	if !strings.Contains(mock.runs[0].Command, "docker") {
		t.Errorf("command should use docker runtime: %q", mock.runs[0].Command)
	}
}
