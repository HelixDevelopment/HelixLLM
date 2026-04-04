package control

import (
	"testing"
	"time"
)

func TestHostProfile_IsHealthy(t *testing.T) {
	tests := []struct {
		name  string
		state string
		want  bool
	}{
		{"online", "online", true},
		{"offline", "offline", false},
		{"degraded", "degraded", false},
		{"unknown", "unknown", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hp := HostProfile{State: tt.state}
			if got := hp.IsHealthy(); got != tt.want {
				t.Errorf("IsHealthy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHostProfile_HasGPU(t *testing.T) {
	t.Run("with GPU", func(t *testing.T) {
		hp := HostProfile{
			GPU: &GPUInfo{Name: "NVIDIA RTX 4090"},
		}
		if !hp.HasGPU() {
			t.Error("HasGPU() = false, want true")
		}
	})

	t.Run("without GPU", func(t *testing.T) {
		hp := HostProfile{}
		if hp.HasGPU() {
			t.Error("HasGPU() = true, want false")
		}
	})
}

func TestMemoryInfo_UsedBytes(t *testing.T) {
	mi := MemoryInfo{
		TotalBytes:     16 * 1024 * 1024 * 1024,
		AvailableBytes: 8 * 1024 * 1024 * 1024,
	}
	want := uint64(8 * 1024 * 1024 * 1024)
	if got := mi.UsedBytes(); got != want {
		t.Errorf("UsedBytes() = %d, want %d", got, want)
	}
}

func TestMemoryInfo_UsedBytes_ZeroTotal(t *testing.T) {
	mi := MemoryInfo{}
	if got := mi.UsedBytes(); got != 0 {
		t.Errorf("UsedBytes() = %d, want 0", got)
	}
}

func TestMemoryInfo_UsedBytes_AvailableExceedsTotal(t *testing.T) {
	// When AvailableBytes > TotalBytes, UsedBytes() should return 0 (no underflow).
	mi := MemoryInfo{
		TotalBytes:     1000,
		AvailableBytes: 2000,
	}
	if got := mi.UsedBytes(); got != 0 {
		t.Errorf("UsedBytes() = %d, want 0 when available > total", got)
	}
}

func TestClusterStatus_HealthyHostCount(t *testing.T) {
	cs := ClusterStatus{
		Hosts: []HostProfile{
			{Name: "host1", State: "online"},
			{Name: "host2", State: "offline"},
			{Name: "host3", State: "online"},
			{Name: "host4", State: "degraded"},
		},
	}
	if got := cs.HealthyHostCount(); got != 2 {
		t.Errorf("HealthyHostCount() = %d, want 2", got)
	}
}

func TestClusterStatus_HealthyHostCount_Empty(t *testing.T) {
	cs := ClusterStatus{}
	if got := cs.HealthyHostCount(); got != 0 {
		t.Errorf("HealthyHostCount() = %d, want 0", got)
	}
}

func TestHostProfile_Construction(t *testing.T) {
	now := time.Now()
	hp := HostProfile{
		Name:    "nezha.local",
		Address: "192.168.1.10",
		OS:      "Linux",
		Arch:    "x86_64",
		CPU: CPUInfo{
			Model:        "AMD Ryzen 9 7950X",
			Cores:        16,
			Threads:      32,
			MHz:          5700,
			UsagePercent: 25.5,
		},
		Memory: MemoryInfo{
			TotalBytes:     64 * 1024 * 1024 * 1024,
			AvailableBytes: 48 * 1024 * 1024 * 1024,
			UsagePercent:   25.0,
		},
		GPU: &GPUInfo{
			Name:        "NVIDIA RTX 4090",
			MemoryMB:    24576,
			Driver:      "535.129.03",
			CUDAVersion: "12.2",
		},
		Disk: DiskInfo{
			TotalBytes:   2 * 1024 * 1024 * 1024 * 1024,
			UsedBytes:    500 * 1024 * 1024 * 1024,
			UsagePercent: 24.4,
		},
		ContainerRuntime: ContainerRuntimeInfo{
			Name:    "podman",
			Version: "4.9.0",
		},
		State:    "online",
		ProbedAt: now,
	}

	if hp.Name != "nezha.local" {
		t.Errorf("Name = %q, want %q", hp.Name, "nezha.local")
	}
	if hp.CPU.Cores != 16 {
		t.Errorf("CPU.Cores = %d, want 16", hp.CPU.Cores)
	}
	if !hp.HasGPU() {
		t.Error("expected HasGPU() = true")
	}
	if !hp.IsHealthy() {
		t.Error("expected IsHealthy() = true")
	}
	if hp.GPU.Name != "NVIDIA RTX 4090" {
		t.Errorf("GPU.Name = %q, want %q", hp.GPU.Name, "NVIDIA RTX 4090")
	}
	if hp.ContainerRuntime.Name != "podman" {
		t.Errorf("ContainerRuntime.Name = %q, want %q",
			hp.ContainerRuntime.Name, "podman")
	}
}

func TestServiceRequirement_Construction(t *testing.T) {
	sr := ServiceRequirement{
		Name:     "llama-cpp",
		Image:    "ghcr.io/ggml-org/llama.cpp:server-cuda",
		CPUCores: 8,
		MemoryMB: 32768,
		DiskMB:   50000,
		NeedsGPU: true,
		Strategy: "gpu_affinity",
		Labels:   map[string]string{"type": "inference"},
	}
	if sr.Name != "llama-cpp" {
		t.Errorf("Name = %q, want %q", sr.Name, "llama-cpp")
	}
	if !sr.NeedsGPU {
		t.Error("NeedsGPU should be true")
	}
}

func TestDeploymentInfo_Construction(t *testing.T) {
	di := DeploymentInfo{
		ServiceName: "redis",
		HostName:    "nezha.local",
		State:       "running",
		DeployedAt:  time.Now(),
	}
	if di.ServiceName != "redis" {
		t.Errorf("ServiceName = %q, want %q", di.ServiceName, "redis")
	}
	if di.State != "running" {
		t.Errorf("State = %q, want %q", di.State, "running")
	}
}

func TestBenchmarkResult_Construction(t *testing.T) {
	br := BenchmarkResult{
		CPUScore:    95.2,
		MemoryScore: 88.1,
		DiskScore:   72.0,
		GPUScore:    99.5,
		Timestamp:   time.Now(),
	}
	if br.CPUScore != 95.2 {
		t.Errorf("CPUScore = %f, want 95.2", br.CPUScore)
	}
}

func TestPlacementResult_Construction(t *testing.T) {
	pr := PlacementResult{
		Service: ServiceRequirement{
			Name:  "qdrant",
			Image: "qdrant/qdrant",
		},
		HostName: "thinker.local",
		Score:    0.85,
		Reason:   "highest available memory",
	}
	if pr.HostName != "thinker.local" {
		t.Errorf("HostName = %q, want %q", pr.HostName, "thinker.local")
	}
	if pr.Score != 0.85 {
		t.Errorf("Score = %f, want 0.85", pr.Score)
	}
}
