package control

import (
	"testing"
	"time"
)

func makeHost(name string, cores int, memMB uint64, diskMB uint64, gpu bool) HostProfile {
	hp := HostProfile{
		Name:    name,
		Address: name,
		OS:      "Linux",
		Arch:    "x86_64",
		CPU: CPUInfo{
			Cores:        cores,
			UsagePercent: 20.0,
		},
		Memory: MemoryInfo{
			TotalBytes:     memMB * 1024 * 1024,
			AvailableBytes: (memMB * 80 / 100) * 1024 * 1024,
			UsagePercent:   20.0,
		},
		Disk: DiskInfo{
			TotalBytes:   diskMB * 1024 * 1024,
			UsedBytes:    (diskMB * 20 / 100) * 1024 * 1024,
			UsagePercent: 20.0,
		},
		ContainerRuntime: ContainerRuntimeInfo{
			Name:    "podman",
			Version: "4.9.0",
		},
		State:    "online",
		ProbedAt: time.Now(),
	}
	if gpu {
		hp.GPU = &GPUInfo{
			Name:     "NVIDIA RTX 4090",
			MemoryMB: 24576,
			Driver:   "535.129.03",
		}
	}
	return hp
}

func TestScheduler_Schedule_SingleHost(t *testing.T) {
	hosts := []HostProfile{
		makeHost("host1", 16, 65536, 500000, true),
	}
	services := []ServiceRequirement{
		{Name: "redis", Image: "redis:7", CPUCores: 2, MemoryMB: 4096},
	}

	sched := NewScheduler("auto")
	results, err := sched.Schedule(hosts, services)
	if err != nil {
		t.Fatalf("Schedule() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].HostName != "host1" {
		t.Errorf("HostName = %q, want %q",
			results[0].HostName, "host1")
	}
	if results[0].Score <= 0 {
		t.Errorf("Score = %f, want > 0", results[0].Score)
	}
}

func TestScheduler_Schedule_MultipleHosts(t *testing.T) {
	hosts := []HostProfile{
		makeHost("small", 4, 8192, 100000, false),
		makeHost("large", 32, 131072, 2000000, true),
	}
	services := []ServiceRequirement{
		{Name: "qdrant", Image: "qdrant/qdrant", CPUCores: 4,
			MemoryMB: 65536},
	}

	sched := NewScheduler("auto")
	results, err := sched.Schedule(hosts, services)
	if err != nil {
		t.Fatalf("Schedule() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	// Should pick the large host since small doesn't have enough
	// memory.
	if results[0].HostName != "large" {
		t.Errorf("HostName = %q, want %q",
			results[0].HostName, "large")
	}
}

func TestScheduler_Schedule_GPUAffinity(t *testing.T) {
	hosts := []HostProfile{
		makeHost("cpu-only", 32, 131072, 2000000, false),
		makeHost("gpu-host", 16, 65536, 1000000, true),
	}
	services := []ServiceRequirement{
		{Name: "llama-cpp",
			Image:    "ghcr.io/ggml-org/llama.cpp:server-cuda",
			CPUCores: 8, MemoryMB: 32768, NeedsGPU: true,
			Strategy: "gpu_affinity"},
	}

	sched := NewScheduler("auto")
	results, err := sched.Schedule(hosts, services)
	if err != nil {
		t.Fatalf("Schedule() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].HostName != "gpu-host" {
		t.Errorf("HostName = %q, want %q (GPU affinity)",
			results[0].HostName, "gpu-host")
	}
}

func TestScheduler_Schedule_GPUAffinity_NoGPUHost(t *testing.T) {
	hosts := []HostProfile{
		makeHost("cpu1", 16, 65536, 1000000, false),
		makeHost("cpu2", 32, 131072, 2000000, false),
	}
	services := []ServiceRequirement{
		{Name: "llama-cpp", Image: "llama:latest",
			NeedsGPU: true, Strategy: "gpu_affinity"},
	}

	sched := NewScheduler("auto")
	results, err := sched.Schedule(hosts, services)
	if err != nil {
		t.Fatalf("Schedule() error: %v", err)
	}
	// Should still place somewhere even without GPU, but score
	// should be low.
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Score >= 0.5 {
		t.Errorf("Score = %f, want < 0.5 for no-GPU host",
			results[0].Score)
	}
}

func TestScheduler_Schedule_MemoryFirst(t *testing.T) {
	hosts := []HostProfile{
		makeHost("low-mem", 32, 16384, 2000000, false),
		makeHost("high-mem", 16, 262144, 1000000, false),
	}
	services := []ServiceRequirement{
		{Name: "vectordb", Image: "qdrant/qdrant",
			MemoryMB: 8192, Strategy: "memory_first"},
	}

	sched := NewScheduler("auto")
	results, err := sched.Schedule(hosts, services)
	if err != nil {
		t.Fatalf("Schedule() error: %v", err)
	}
	if results[0].HostName != "high-mem" {
		t.Errorf("HostName = %q, want %q (memory first)",
			results[0].HostName, "high-mem")
	}
}

func TestScheduler_Schedule_Spread(t *testing.T) {
	hosts := []HostProfile{
		makeHost("host1", 16, 65536, 1000000, false),
		makeHost("host2", 16, 65536, 1000000, false),
		makeHost("host3", 16, 65536, 1000000, false),
	}
	services := []ServiceRequirement{
		{Name: "svc1", Image: "app:1", CPUCores: 2, MemoryMB: 2048},
		{Name: "svc2", Image: "app:2", CPUCores: 2, MemoryMB: 2048},
		{Name: "svc3", Image: "app:3", CPUCores: 2, MemoryMB: 2048},
	}

	sched := NewScheduler("spread")
	results, err := sched.Schedule(hosts, services)
	if err != nil {
		t.Fatalf("Schedule() error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}

	// With spread, services should be distributed across hosts.
	hostCounts := make(map[string]int)
	for _, r := range results {
		hostCounts[r.HostName]++
	}
	// Each host should have exactly 1 service.
	for host, count := range hostCounts {
		if count != 1 {
			t.Errorf("host %q has %d services, want 1",
				host, count)
		}
	}
}

func TestScheduler_Schedule_BinPack(t *testing.T) {
	hosts := []HostProfile{
		makeHost("host1", 16, 65536, 1000000, false),
		makeHost("host2", 16, 65536, 1000000, false),
	}
	services := []ServiceRequirement{
		{Name: "svc1", Image: "app:1", CPUCores: 2, MemoryMB: 2048},
		{Name: "svc2", Image: "app:2", CPUCores: 2, MemoryMB: 2048},
		{Name: "svc3", Image: "app:3", CPUCores: 2, MemoryMB: 2048},
	}

	sched := NewScheduler("bin_pack")
	results, err := sched.Schedule(hosts, services)
	if err != nil {
		t.Fatalf("Schedule() error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}

	// With bin-pack, services should be packed onto fewer hosts.
	hostCounts := make(map[string]int)
	for _, r := range results {
		hostCounts[r.HostName]++
	}
	// First host should have most services.
	if len(hostCounts) > 2 {
		t.Errorf("services spread across %d hosts, want <= 2",
			len(hostCounts))
	}
}

func TestScheduler_Schedule_EmptyHosts(t *testing.T) {
	services := []ServiceRequirement{
		{Name: "svc1", Image: "app:1"},
	}

	sched := NewScheduler("auto")
	_, err := sched.Schedule(nil, services)
	if err == nil {
		t.Fatal("expected error for empty hosts")
	}
}

func TestScheduler_Schedule_EmptyServices(t *testing.T) {
	hosts := []HostProfile{
		makeHost("host1", 16, 65536, 1000000, false),
	}

	sched := NewScheduler("auto")
	results, err := sched.Schedule(hosts, nil)
	if err != nil {
		t.Fatalf("Schedule() error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("len(results) = %d, want 0", len(results))
	}
}

func TestScheduler_Schedule_OfflineHostSkipped(t *testing.T) {
	hosts := []HostProfile{
		makeHost("offline", 32, 131072, 2000000, true),
		makeHost("online", 16, 65536, 1000000, false),
	}
	hosts[0].State = "offline"

	services := []ServiceRequirement{
		{Name: "svc1", Image: "app:1", CPUCores: 2, MemoryMB: 2048},
	}

	sched := NewScheduler("auto")
	results, err := sched.Schedule(hosts, services)
	if err != nil {
		t.Fatalf("Schedule() error: %v", err)
	}
	if results[0].HostName != "online" {
		t.Errorf("HostName = %q, want %q (offline host skipped)",
			results[0].HostName, "online")
	}
}

func TestScheduler_Schedule_InsufficientResources(t *testing.T) {
	hosts := []HostProfile{
		makeHost("tiny", 2, 1024, 10000, false),
	}
	services := []ServiceRequirement{
		{Name: "big", Image: "app:1", CPUCores: 64, MemoryMB: 999999},
	}

	sched := NewScheduler("auto")
	results, err := sched.Schedule(hosts, services)
	if err != nil {
		t.Fatalf("Schedule() error: %v", err)
	}
	// Should still place but with low score.
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Score >= 0.5 {
		t.Errorf("Score = %f, want < 0.5 for insufficient resources",
			results[0].Score)
	}
}

func TestScheduler_Schedule_LatencyOptimized(t *testing.T) {
	hosts := []HostProfile{
		makeHost("host1", 16, 65536, 1000000, false),
		makeHost("host2", 16, 65536, 1000000, false),
	}
	services := []ServiceRequirement{
		{Name: "gateway", Image: "gateway:latest", CPUCores: 2,
			MemoryMB: 2048, Strategy: "latency_optimized"},
	}

	sched := NewScheduler("auto")
	results, err := sched.Schedule(hosts, services)
	if err != nil {
		t.Fatalf("Schedule() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	// Latency optimized should still produce a valid placement.
	if results[0].Score <= 0 {
		t.Errorf("Score = %f, want > 0", results[0].Score)
	}
}

func TestScheduler_Schedule_AllOffline(t *testing.T) {
	hosts := []HostProfile{
		makeHost("h1", 16, 65536, 1000000, false),
		makeHost("h2", 16, 65536, 1000000, false),
	}
	hosts[0].State = "offline"
	hosts[1].State = "offline"

	services := []ServiceRequirement{
		{Name: "svc", Image: "app:1"},
	}

	sched := NewScheduler("auto")
	_, err := sched.Schedule(hosts, services)
	if err == nil {
		t.Fatal("expected error when all hosts are offline")
	}
}

func TestScheduler_Schedule_ResourceAware_NoCPUNoDisk(t *testing.T) {
	// Service with no CPU/disk requirements hits the "else" branches in
	// scoreResourceAware, awarding the full 0.3/0.2 bonuses.
	hosts := []HostProfile{
		makeHost("host1", 16, 65536, 1000000, false),
	}
	services := []ServiceRequirement{
		{Name: "nocpu", Image: "app:1", CPUCores: 0, MemoryMB: 0, DiskMB: 0},
	}
	sched := NewScheduler("auto")
	results, err := sched.Schedule(hosts, services)
	if err != nil {
		t.Fatalf("Schedule() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	// Full bonuses for zero requirements → score should be 1.0.
	if results[0].Score < 0.9 {
		t.Errorf("Score = %f, want >= 0.9 for zero-requirement service", results[0].Score)
	}
}

func TestScheduler_Schedule_ResourceAware_NeedsGPU_HostHasGPU(t *testing.T) {
	hosts := []HostProfile{
		makeHost("gpu-host", 16, 65536, 1000000, true),
	}
	services := []ServiceRequirement{
		{Name: "llm", Image: "llm:latest", NeedsGPU: true},
	}
	sched := NewScheduler("resource_aware")
	results, err := sched.Schedule(hosts, services)
	if err != nil {
		t.Fatalf("Schedule() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Score <= 0 {
		t.Errorf("Score = %f, want > 0 for GPU service on GPU host", results[0].Score)
	}
}

func TestScheduler_Schedule_MemoryFirst_InsufficientMemory(t *testing.T) {
	// Service requires more memory than available → memScore is halved.
	hosts := []HostProfile{
		makeHost("low-mem", 32, 1024, 100000, false), // only 1 GB
	}
	services := []ServiceRequirement{
		{Name: "bigmem", Image: "app:1", MemoryMB: 65536, Strategy: "memory_first"},
	}
	sched := NewScheduler("auto")
	results, err := sched.Schedule(hosts, services)
	if err != nil {
		t.Fatalf("Schedule() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	// Halved score due to insufficient memory.
	if results[0].Score >= 0.5 {
		t.Errorf("Score = %f, expected lower score for insufficient memory with memory_first", results[0].Score)
	}
}

func TestScheduler_Schedule_MemoryFirst_ZeroCPU(t *testing.T) {
	// Service with CPUCores = 0 hits the cpuScore else-branch in scoreMemoryFirst.
	hosts := []HostProfile{
		makeHost("host1", 64, 65536, 1000000, false),
	}
	services := []ServiceRequirement{
		{Name: "nocpu", Image: "app:1", CPUCores: 0, MemoryMB: 4096, Strategy: "memory_first"},
	}
	sched := NewScheduler("auto")
	results, err := sched.Schedule(hosts, services)
	if err != nil {
		t.Fatalf("Schedule() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Score <= 0 {
		t.Errorf("Score = %f, want > 0", results[0].Score)
	}
}

func TestScheduler_MapStrategy(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"auto", "auto"},
		{"bin_pack", "bin_pack"},
		{"spread", "spread"},
		{"gpu_affinity", "gpu_affinity"},
		{"memory_first", "memory_first"},
		{"latency_optimized", "latency_optimized"},
		{"unknown", "auto"},
		{"", "auto"},
	}
	sched := NewScheduler("auto")
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sched.resolveStrategy(tt.input)
			if got != tt.want {
				t.Errorf("resolveStrategy(%q) = %q, want %q",
					tt.input, got, tt.want)
			}
		})
	}
}
