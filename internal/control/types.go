// Package control implements the Control Plane layer for HelixLLM,
// managing host probing, scheduling, deployment, and monitoring
// across the cluster.
package control

import "time"

// HostProfile contains the full profile of a probed host.
type HostProfile struct {
	// Name is the hostname or DNS name.
	Name string `json:"name"`
	// Address is the IP address or hostname used for SSH.
	Address string `json:"address"`
	// OS is the operating system (e.g. "Linux", "Darwin").
	OS string `json:"os"`
	// Arch is the CPU architecture (e.g. "x86_64", "aarch64").
	Arch string `json:"arch"`
	// CPU holds CPU details.
	CPU CPUInfo `json:"cpu"`
	// Memory holds memory details.
	Memory MemoryInfo `json:"memory"`
	// GPU holds GPU details; nil if no GPU detected.
	GPU *GPUInfo `json:"gpu,omitempty"`
	// Disk holds disk usage details.
	Disk DiskInfo `json:"disk"`
	// ContainerRuntime holds container runtime detection results.
	ContainerRuntime ContainerRuntimeInfo `json:"container_runtime"`
	// Benchmark holds benchmark scores; nil if not benchmarked.
	Benchmark *BenchmarkResult `json:"benchmark,omitempty"`
	// State is the current host state: "online", "offline",
	// "degraded", or "unknown".
	State string `json:"state"`
	// ProbedAt is the timestamp of the last probe.
	ProbedAt time.Time `json:"probed_at"`
}

// IsHealthy returns true if the host state is "online".
func (hp *HostProfile) IsHealthy() bool {
	return hp.State == "online"
}

// HasGPU returns true if a GPU was detected on this host.
func (hp *HostProfile) HasGPU() bool {
	return hp.GPU != nil
}

// CPUInfo holds CPU details from a probed host.
type CPUInfo struct {
	// Model is the CPU model name.
	Model string `json:"model"`
	// Cores is the number of physical CPU cores.
	Cores int `json:"cores"`
	// Threads is the number of logical threads.
	Threads int `json:"threads"`
	// MHz is the CPU clock speed.
	MHz float64 `json:"mhz"`
	// UsagePercent is the current CPU usage (0-100).
	UsagePercent float64 `json:"usage_percent"`
}

// MemoryInfo holds memory details.
type MemoryInfo struct {
	// TotalBytes is the total installed memory.
	TotalBytes uint64 `json:"total_bytes"`
	// AvailableBytes is the available memory.
	AvailableBytes uint64 `json:"available_bytes"`
	// UsagePercent is the current memory usage (0-100).
	UsagePercent float64 `json:"usage_percent"`
}

// UsedBytes returns the used memory in bytes.
func (mi *MemoryInfo) UsedBytes() uint64 {
	if mi.TotalBytes < mi.AvailableBytes {
		return 0
	}
	return mi.TotalBytes - mi.AvailableBytes
}

// GPUInfo holds GPU details. A nil GPUInfo means no GPU detected.
type GPUInfo struct {
	// Name is the GPU model name.
	Name string `json:"name"`
	// MemoryMB is the GPU memory in megabytes.
	MemoryMB uint64 `json:"memory_mb"`
	// Driver is the GPU driver version.
	Driver string `json:"driver"`
	// CUDAVersion is the CUDA version if available.
	CUDAVersion string `json:"cuda_version,omitempty"`
}

// DiskInfo holds disk usage details.
type DiskInfo struct {
	// TotalBytes is the total disk space.
	TotalBytes uint64 `json:"total_bytes"`
	// UsedBytes is the used disk space.
	UsedBytes uint64 `json:"used_bytes"`
	// UsagePercent is the disk usage (0-100).
	UsagePercent float64 `json:"usage_percent"`
}

// ContainerRuntimeInfo holds container runtime detection results.
type ContainerRuntimeInfo struct {
	// Name is the runtime name: "podman", "docker", or "none".
	Name string `json:"name"`
	// Version is the runtime version string.
	Version string `json:"version"`
}

// BenchmarkResult holds benchmark scores for a host.
type BenchmarkResult struct {
	// CPUScore is the CPU benchmark score (0-100).
	CPUScore float64 `json:"cpu_score"`
	// MemoryScore is the memory benchmark score (0-100).
	MemoryScore float64 `json:"memory_score"`
	// DiskScore is the disk benchmark score (0-100).
	DiskScore float64 `json:"disk_score"`
	// GPUScore is the GPU benchmark score (0-100).
	GPUScore float64 `json:"gpu_score"`
	// Timestamp is when the benchmark was run.
	Timestamp time.Time `json:"timestamp"`
}

// ServiceRequirement describes what a service needs from a host.
type ServiceRequirement struct {
	// Name is the service name.
	Name string `json:"name"`
	// Image is the container image.
	Image string `json:"image"`
	// CPUCores is the minimum CPU cores required.
	CPUCores float64 `json:"cpu_cores"`
	// MemoryMB is the minimum memory in megabytes.
	MemoryMB uint64 `json:"memory_mb"`
	// DiskMB is the minimum disk space in megabytes.
	DiskMB uint64 `json:"disk_mb"`
	// NeedsGPU indicates whether a GPU is required.
	NeedsGPU bool `json:"needs_gpu"`
	// Strategy overrides the global scheduling strategy for this
	// service. Empty means use the global strategy.
	Strategy string `json:"strategy,omitempty"`
	// Labels are arbitrary key-value metadata for scheduling.
	Labels map[string]string `json:"labels,omitempty"`
}

// PlacementResult records where a service was placed.
type PlacementResult struct {
	// Service is the original requirement.
	Service ServiceRequirement `json:"service"`
	// HostName is the selected host.
	HostName string `json:"host_name"`
	// Score is the placement score (0.0-1.0).
	Score float64 `json:"score"`
	// Reason explains why this host was chosen.
	Reason string `json:"reason"`
}

// ClusterStatus holds the overall cluster state.
type ClusterStatus struct {
	// Hosts is the list of probed host profiles.
	Hosts []HostProfile `json:"hosts"`
	// Deployments is the list of deployed containers.
	Deployments []DeploymentInfo `json:"deployments"`
	// Healthy indicates whether the cluster is overall healthy.
	Healthy bool `json:"healthy"`
	// CheckedAt is when the status was last checked.
	CheckedAt time.Time `json:"checked_at"`
}

// HealthyHostCount returns the number of healthy hosts.
func (cs *ClusterStatus) HealthyHostCount() int {
	count := 0
	for i := range cs.Hosts {
		if cs.Hosts[i].IsHealthy() {
			count++
		}
	}
	return count
}

// DeploymentInfo tracks a deployed container.
type DeploymentInfo struct {
	// ServiceName is the name of the deployed service.
	ServiceName string `json:"service_name"`
	// HostName is the host where the container runs.
	HostName string `json:"host_name"`
	// State is the deployment state: "running", "stopped",
	// "failed", "deploying".
	State string `json:"state"`
	// DeployedAt is when the container was deployed.
	DeployedAt time.Time `json:"deployed_at"`
	// Error holds the last error message if state is "failed".
	Error string `json:"error,omitempty"`
}
