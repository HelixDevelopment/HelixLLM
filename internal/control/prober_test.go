package control

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// mockSSHClient implements SSHClient for testing.
type mockSSHClient struct {
	// outputs maps "host:command-prefix" to stdout responses.
	outputs   map[string]string
	reachable map[string]bool
	runCalls  []mockRunCall
}

type mockRunCall struct {
	Host    string
	Command string
}

func newMockSSHClient() *mockSSHClient {
	return &mockSSHClient{
		outputs:   make(map[string]string),
		reachable: make(map[string]bool),
	}
}

func (m *mockSSHClient) Run(
	ctx context.Context, host, command string,
) (string, error) {
	m.runCalls = append(m.runCalls, mockRunCall{
		Host: host, Command: command,
	})
	key := host
	if out, ok := m.outputs[key]; ok {
		return out, nil
	}
	return "", fmt.Errorf("ssh: connection refused to %s", host)
}

func (m *mockSSHClient) IsReachable(
	ctx context.Context, host string,
) bool {
	if v, ok := m.reachable[host]; ok {
		return v
	}
	return false
}

// linuxProbeOutput returns a realistic compound command output for
// a Linux host with GPU.
func linuxProbeOutput() string {
	return strings.Join([]string{
		"Linux x86_64",                                             // uname -s -m
		"---PROBE_SEP---",                                          //
		"model name\t: AMD Ryzen 9 7950X 16-Core Processor",       // cpuinfo
		"---PROBE_SEP---",                                          //
		"32",                                                       // nproc
		"---PROBE_SEP---",                                          //
		"              total        used        free      shared  buff/cache   available", //
		"Mem:    67108864000  16777216000  33554432000    1048576  16777216000  50331648000", // free -b
		"---PROBE_SEP---",                                          //
		"NVIDIA RTX 4090, 24576, 535.129.03",                       // nvidia-smi
		"---PROBE_SEP---",                                          //
		"  2000000000000   500000000000",                            // df -B1
		"---PROBE_SEP---",                                          //
		"podman 4.9.0",                                             // runtime
	}, "\n")
}

// linuxProbeOutputNoGPU returns output for a Linux host without GPU.
func linuxProbeOutputNoGPU() string {
	return strings.Join([]string{
		"Linux x86_64",
		"---PROBE_SEP---",
		"model name\t: Intel Core i7-12700K",
		"---PROBE_SEP---",
		"20",
		"---PROBE_SEP---",
		"              total        used        free      shared  buff/cache   available",
		"Mem:    33554432000   8388608000  16777216000     524288   8388608000  25165824000",
		"---PROBE_SEP---",
		"",
		"---PROBE_SEP---",
		"  1000000000000   250000000000",
		"---PROBE_SEP---",
		"docker 24.0.7",
	}, "\n")
}

// darwinProbeOutput returns output for a macOS host.
func darwinProbeOutput() string {
	return strings.Join([]string{
		"Darwin arm64",
		"---PROBE_SEP---",
		"Apple M2 Max",
		"---PROBE_SEP---",
		"12",
		"---PROBE_SEP---",
		"              total        used        free      shared  buff/cache   available",
		"Mem:    34359738368   8589934592  17179869184     524288   8589934592  25769803776",
		"---PROBE_SEP---",
		"",
		"---PROBE_SEP---",
		"  500000000000   125000000000",
		"---PROBE_SEP---",
		"docker 24.0.6",
	}, "\n")
}

func TestProber_ProbeHost_LinuxWithGPU(t *testing.T) {
	mock := newMockSSHClient()
	mock.reachable["nezha.local"] = true
	mock.outputs["nezha.local"] = linuxProbeOutput()

	prober := NewHostProber(mock, "milosvasic", "~/.ssh/id_ed25519")
	profile, err := prober.ProbeHost(context.Background(), "nezha.local")
	if err != nil {
		t.Fatalf("ProbeHost() error: %v", err)
	}

	if profile.Name != "nezha.local" {
		t.Errorf("Name = %q, want %q", profile.Name, "nezha.local")
	}
	if profile.OS != "Linux" {
		t.Errorf("OS = %q, want %q", profile.OS, "Linux")
	}
	if profile.Arch != "x86_64" {
		t.Errorf("Arch = %q, want %q", profile.Arch, "x86_64")
	}
	if profile.CPU.Cores != 32 {
		t.Errorf("CPU.Cores = %d, want 32", profile.CPU.Cores)
	}
	if !strings.Contains(profile.CPU.Model, "AMD Ryzen 9 7950X") {
		t.Errorf("CPU.Model = %q, want to contain %q",
			profile.CPU.Model, "AMD Ryzen 9 7950X")
	}
	if profile.Memory.TotalBytes != 67108864000 {
		t.Errorf("Memory.TotalBytes = %d, want 67108864000",
			profile.Memory.TotalBytes)
	}
	if profile.Memory.AvailableBytes != 50331648000 {
		t.Errorf("Memory.AvailableBytes = %d, want 50331648000",
			profile.Memory.AvailableBytes)
	}
	if !profile.HasGPU() {
		t.Error("expected HasGPU() = true")
	}
	if profile.GPU.Name != "NVIDIA RTX 4090" {
		t.Errorf("GPU.Name = %q, want %q",
			profile.GPU.Name, "NVIDIA RTX 4090")
	}
	if profile.GPU.MemoryMB != 24576 {
		t.Errorf("GPU.MemoryMB = %d, want 24576", profile.GPU.MemoryMB)
	}
	if profile.Disk.TotalBytes != 2000000000000 {
		t.Errorf("Disk.TotalBytes = %d, want 2000000000000",
			profile.Disk.TotalBytes)
	}
	if profile.ContainerRuntime.Name != "podman" {
		t.Errorf("ContainerRuntime.Name = %q, want %q",
			profile.ContainerRuntime.Name, "podman")
	}
	if profile.ContainerRuntime.Version != "4.9.0" {
		t.Errorf("ContainerRuntime.Version = %q, want %q",
			profile.ContainerRuntime.Version, "4.9.0")
	}
	if profile.State != "online" {
		t.Errorf("State = %q, want %q", profile.State, "online")
	}
}

func TestProber_ProbeHost_LinuxNoGPU(t *testing.T) {
	mock := newMockSSHClient()
	mock.reachable["worker1"] = true
	mock.outputs["worker1"] = linuxProbeOutputNoGPU()

	prober := NewHostProber(mock, "milosvasic", "~/.ssh/id_ed25519")
	profile, err := prober.ProbeHost(context.Background(), "worker1")
	if err != nil {
		t.Fatalf("ProbeHost() error: %v", err)
	}

	if profile.OS != "Linux" {
		t.Errorf("OS = %q, want %q", profile.OS, "Linux")
	}
	if profile.HasGPU() {
		t.Error("expected HasGPU() = false for no-GPU host")
	}
	if profile.CPU.Cores != 20 {
		t.Errorf("CPU.Cores = %d, want 20", profile.CPU.Cores)
	}
	if profile.ContainerRuntime.Name != "docker" {
		t.Errorf("ContainerRuntime.Name = %q, want %q",
			profile.ContainerRuntime.Name, "docker")
	}
}

func TestProber_ProbeHost_Darwin(t *testing.T) {
	mock := newMockSSHClient()
	mock.reachable["machost"] = true
	mock.outputs["machost"] = darwinProbeOutput()

	prober := NewHostProber(mock, "milosvasic", "~/.ssh/id_ed25519")
	profile, err := prober.ProbeHost(context.Background(), "machost")
	if err != nil {
		t.Fatalf("ProbeHost() error: %v", err)
	}

	if profile.OS != "Darwin" {
		t.Errorf("OS = %q, want %q", profile.OS, "Darwin")
	}
	if profile.Arch != "arm64" {
		t.Errorf("Arch = %q, want %q", profile.Arch, "arm64")
	}
	if profile.HasGPU() {
		t.Error("expected HasGPU() = false for macOS without GPU")
	}
}

func TestProber_ProbeHost_Unreachable(t *testing.T) {
	mock := newMockSSHClient()
	mock.reachable["offline.local"] = false

	prober := NewHostProber(mock, "milosvasic", "~/.ssh/id_ed25519")
	profile, err := prober.ProbeHost(context.Background(), "offline.local")
	if err == nil {
		t.Fatal("expected error for unreachable host")
	}
	if profile != nil {
		t.Error("expected nil profile for unreachable host")
	}
	if !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("error = %q, want to contain %q",
			err.Error(), "unreachable")
	}
}

func TestProber_ProbeHost_SSHError(t *testing.T) {
	mock := newMockSSHClient()
	mock.reachable["badhost"] = true
	// No output configured -> Run returns error

	prober := NewHostProber(mock, "milosvasic", "~/.ssh/id_ed25519")
	_, err := prober.ProbeHost(context.Background(), "badhost")
	if err == nil {
		t.Fatal("expected error for SSH failure")
	}
}

func TestProber_ProbeHost_MalformedOutput(t *testing.T) {
	mock := newMockSSHClient()
	mock.reachable["broken"] = true
	mock.outputs["broken"] = "some garbage output"

	prober := NewHostProber(mock, "milosvasic", "~/.ssh/id_ed25519")
	_, err := prober.ProbeHost(context.Background(), "broken")
	if err == nil {
		t.Fatal("expected error for malformed output")
	}
}

func TestProber_ProbeAll(t *testing.T) {
	mock := newMockSSHClient()
	mock.reachable["host1"] = true
	mock.reachable["host2"] = true
	mock.reachable["host3"] = false
	mock.outputs["host1"] = linuxProbeOutput()
	mock.outputs["host2"] = linuxProbeOutputNoGPU()

	prober := NewHostProber(mock, "milosvasic", "~/.ssh/id_ed25519")
	profiles, errs := prober.ProbeAll(
		context.Background(),
		[]string{"host1", "host2", "host3"},
	)

	if len(profiles) != 2 {
		t.Errorf("len(profiles) = %d, want 2", len(profiles))
	}
	if len(errs) != 1 {
		t.Errorf("len(errs) = %d, want 1", len(errs))
	}

	// Verify we got the expected hosts.
	names := make(map[string]bool)
	for _, p := range profiles {
		names[p.Name] = true
	}
	if !names["host1"] || !names["host2"] {
		t.Errorf("expected host1 and host2 in profiles, got %v", names)
	}
}

func TestProber_ProbeAll_Empty(t *testing.T) {
	mock := newMockSSHClient()
	prober := NewHostProber(mock, "milosvasic", "~/.ssh/id_ed25519")

	profiles, errs := prober.ProbeAll(context.Background(), nil)
	if len(profiles) != 0 {
		t.Errorf("len(profiles) = %d, want 0", len(profiles))
	}
	if len(errs) != 0 {
		t.Errorf("len(errs) = %d, want 0", len(errs))
	}
}

func TestProber_ProbeHost_NoRuntime(t *testing.T) {
	output := strings.Join([]string{
		"Linux x86_64",
		"---PROBE_SEP---",
		"model name\t: Generic CPU",
		"---PROBE_SEP---",
		"4",
		"---PROBE_SEP---",
		"              total        used        free      shared  buff/cache   available",
		"Mem:     4294967296   2147483648   1073741824     524288   1073741824   2147483648",
		"---PROBE_SEP---",
		"",
		"---PROBE_SEP---",
		"  100000000000   50000000000",
		"---PROBE_SEP---",
		"none",
	}, "\n")

	mock := newMockSSHClient()
	mock.reachable["minimal"] = true
	mock.outputs["minimal"] = output

	prober := NewHostProber(mock, "user", "key")
	profile, err := prober.ProbeHost(context.Background(), "minimal")
	if err != nil {
		t.Fatalf("ProbeHost() error: %v", err)
	}
	if profile.ContainerRuntime.Name != "none" {
		t.Errorf("ContainerRuntime.Name = %q, want %q",
			profile.ContainerRuntime.Name, "none")
	}
	if profile.ContainerRuntime.Version != "" {
		t.Errorf("ContainerRuntime.Version = %q, want empty",
			profile.ContainerRuntime.Version)
	}
}
