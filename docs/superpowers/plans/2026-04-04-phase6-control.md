# Phase 6: Control Plane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Control Plane layer that manages the HelixLLM cluster. This covers host probing via SSH (detecting OS, CPU, RAM, GPU, disk, container runtime), capability profiling with benchmark results, container scheduling (which services go where), deployment (push containers via SSH), health monitoring across the cluster, and a Control API for managing operations. Complex auto-remediation and LLMOps/SelfImprove integration are deferred to later refinement.

**Architecture:** The Control Plane wraps `digital.vasic.containers` packages (remote, scheduler, distribution, monitor) with HelixLLM-specific types and logic. An `SSHClient` interface abstracts SSH command execution so tests use mocks while production uses real SSH. `HostProber` connects to each host in `HELIX_HOSTS` and collects a `HostProfile` (OS, CPU, memory, GPU, disk, container runtime). The `Scheduler` wraps `digital.vasic.containers/pkg/scheduler` to map host profiles and service requirements to placement decisions, supporting 5 strategies (BinPack, Spread, GPU-Affinity, Memory-First, Latency-Optimized) plus an `auto` strategy that selects per-service. The `Deployer` wraps `digital.vasic.containers/pkg/distribution` to push containers to hosts via SSH. The `Monitor` provides continuous health monitoring by probing hosts and checking container status. A Gin HTTP API exposes cluster operations under `/internal/cluster/`. Everything is wired into `main.go` and the server router.

**Tech Stack:** Go 1.26+, Gin Gonic, `digital.vasic.containers` (remote, scheduler, distribution, monitor packages -- already a submodule with replace directive), `sync` (concurrency), `time` (intervals, timestamps), `context` (cancellation), `net/http/httptest` (API testing), `encoding/json` (API serialization)

**Spec Reference:** `docs/superpowers/specs/2026-04-04-helixllm-master-design.md` -- Section 9 (Control Plane Design), Section 4.5 (Control Layer Submodules)

**Important notes:**
- The `digital.vasic.containers` submodule already exists with a replace directive in `go.mod`. It provides `pkg/remote` (SSH executor, host manager, prober), `pkg/scheduler` (5 strategies), `pkg/distribution` (distributor), and `pkg/monitor` (resource monitoring). We wrap these rather than reimplementing.
- Since we cannot SSH to real hosts in tests, all SSH interactions go through interfaces with mock implementations in tests. The `SSHClient` interface mirrors `remote.RemoteExecutor` from the Containers module.
- Benchmark, LLMOps, SelfImprove, Plugins, and Discovery are mentioned in the master spec as separate submodules but do not exist yet as Git submodules. For Phase 6, we note their absence and do not block on them. The Containers module already has built-in discovery (`pkg/discovery`) and monitoring (`pkg/monitor`).
- The `ScheduleStrategy` config field supports: `auto`, `bin_pack`, `spread`, `gpu_affinity`, `memory_first`, `latency_optimized`. The `auto` strategy maps to `resource_aware` in the Containers scheduler.
- Tests are written first (TDD) and must pass without any external services or SSH connectivity.
- The Control API is under `/internal/cluster/` (not public-facing) as it manages infrastructure.

---

## File Structure

```
helixllm/
  cmd/helixllm/
    main.go                                Updated to create ControlPlane and wire routes
  internal/
    control/
      types.go                             HostProfile, CPUInfo, MemoryInfo, GPUInfo, DiskInfo, etc.
      types_test.go
      prober.go                            HostProber with SSHClient interface
      prober_test.go
      scheduler.go                         Scheduler wrapping Containers scheduler
      scheduler_test.go
      deployer.go                          Deployer wrapping Containers distributor
      deployer_test.go
      monitor.go                           Health monitor for cluster
      monitor_test.go
      api.go                               Gin HTTP handlers for /internal/cluster/*
      api_test.go
  go.mod                                   Updated with digital.vasic.containers require
```

---

### Task 1: Verify Control Submodules

**Files:**
- Verify: `go.mod` (digital.vasic.containers replace directive exists)
- Verify: `submodules/Containers/` exists and has pkg/remote, pkg/scheduler, pkg/distribution, pkg/monitor

- [ ] **Step 1: Verify the Containers submodule is accessible**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
ls submodules/Containers/pkg/remote/
ls submodules/Containers/pkg/scheduler/
ls submodules/Containers/pkg/distribution/
ls submodules/Containers/pkg/monitor/
```

Expected: all directories exist with `.go` files.

- [ ] **Step 2: Verify go.mod has the replace directive**

```bash
grep "digital.vasic.containers" go.mod
```

Expected: `digital.vasic.containers => ./submodules/Containers` in replace block.

- [ ] **Step 3: Add digital.vasic.containers to require block if missing**

If `digital.vasic.containers` is not in the `require` block, add it:

```
require (
    // ... existing requirements ...
    digital.vasic.containers v0.0.0-00010101000000-000000000000
)
```

- [ ] **Step 4: Run go mod tidy and verify**

```bash
go mod tidy
go build ./...
```

Expected: no errors, containers module resolved via replace directive.

---

### Task 2: Host Profile Types

**Files:**
- Create: `internal/control/types.go`
- Create: `internal/control/types_test.go`

**Types:**

```go
// HostProfile contains the full profile of a probed host.
type HostProfile struct {
    Name             string
    Address          string
    OS               string
    Arch             string
    CPU              CPUInfo
    Memory           MemoryInfo
    GPU              *GPUInfo        // nil if no GPU detected
    Disk             DiskInfo
    ContainerRuntime ContainerRuntimeInfo
    Benchmark        *BenchmarkResult // nil if not benchmarked
    State            string           // "online", "offline", "degraded", "unknown"
    ProbedAt         time.Time
}

// CPUInfo holds CPU details from a probed host.
type CPUInfo struct {
    Model    string
    Cores    int
    Threads  int
    MHz      float64
    UsagePercent float64
}

// MemoryInfo holds memory details.
type MemoryInfo struct {
    TotalBytes     uint64
    AvailableBytes uint64
    UsagePercent   float64
}

// GPUInfo holds GPU details (nil when no GPU present).
type GPUInfo struct {
    Name       string
    MemoryMB   uint64
    Driver     string
    CUDAVersion string
}

// DiskInfo holds disk usage details.
type DiskInfo struct {
    TotalBytes     uint64
    UsedBytes      uint64
    UsagePercent   float64
}

// ContainerRuntimeInfo holds container runtime detection results.
type ContainerRuntimeInfo struct {
    Name    string // "podman", "docker", "none"
    Version string
}

// BenchmarkResult holds benchmark scores for a host (placeholder).
type BenchmarkResult struct {
    CPUScore    float64
    MemoryScore float64
    DiskScore   float64
    GPUScore    float64
    Timestamp   time.Time
}

// ServiceRequirement describes what a service needs from a host.
type ServiceRequirement struct {
    Name         string
    Image        string
    CPUCores     float64
    MemoryMB     uint64
    DiskMB       uint64
    NeedsGPU     bool
    Strategy     string // override per-service; empty = use global
    Labels       map[string]string
}

// PlacementResult records where a service was placed.
type PlacementResult struct {
    Service  ServiceRequirement
    HostName string
    Score    float64
    Reason   string
}

// ClusterStatus holds the overall cluster state.
type ClusterStatus struct {
    Hosts       []HostProfile
    Deployments []DeploymentInfo
    Healthy     bool
    CheckedAt   time.Time
}

// DeploymentInfo tracks a deployed container.
type DeploymentInfo struct {
    ServiceName string
    HostName    string
    State       string  // "running", "stopped", "failed", "deploying"
    DeployedAt  time.Time
    Error       string
}
```

- [ ] **Step 1: Write types_test.go** -- Test that HostProfile can be constructed, GPUInfo is optional (nil), ClusterStatus aggregates correctly, and helper methods work.

- [ ] **Step 2: Write types.go** -- Implement all types listed above with helper methods: `HostProfile.IsHealthy()`, `HostProfile.HasGPU()`, `MemoryInfo.UsedBytes()`, `ClusterStatus.HealthyHostCount()`.

- [ ] **Step 3: Verify tests pass**

```bash
go test ./internal/control/ -count=1 -run TestTypes
```

---

### Task 3: Host Prober

**Files:**
- Create: `internal/control/prober.go`
- Create: `internal/control/prober_test.go`

**Design:**

```go
// SSHClient abstracts SSH command execution for testing.
type SSHClient interface {
    // Run executes a command on the specified host and returns stdout.
    Run(ctx context.Context, host, command string) (string, error)
    // IsReachable checks if a host is reachable.
    IsReachable(ctx context.Context, host string) bool
}

// HostProber probes hosts via SSH to collect system profiles.
type HostProber struct {
    ssh     SSHClient
    sshUser string
    sshKey  string
}

func NewHostProber(ssh SSHClient, sshUser, sshKey string) *HostProber

// ProbeHost connects to a single host and returns its profile.
func (p *HostProber) ProbeHost(ctx context.Context, host string) (*HostProfile, error)

// ProbeAll probes all hosts concurrently and returns profiles.
func (p *HostProber) ProbeAll(ctx context.Context, hosts []string) ([]HostProfile, []error)
```

The prober runs these commands via SSH:
1. `uname -s -m` -- OS and architecture
2. `cat /proc/cpuinfo | grep 'model name' | head -1` + `nproc` -- CPU info
3. `free -b` -- Memory info
4. `nvidia-smi --query-gpu=name,memory.total,driver_version --format=csv,noheader,nounits 2>/dev/null` -- GPU info
5. `df -B1 --output=size,used / | tail -1` -- Disk info
6. `podman version --format '{{.Client.Version}}' 2>/dev/null || docker version --format '{{.Client.Version}}' 2>/dev/null || echo none` -- Container runtime

These are combined into a single compound command for efficiency.

- [ ] **Step 1: Write prober_test.go** -- Test with a mock SSHClient that returns canned output for each scenario: healthy Linux host with GPU, healthy Linux host without GPU, macOS host, unreachable host, parse errors.

- [ ] **Step 2: Write prober.go** -- Implement HostProber with SSH command execution and output parsing.

- [ ] **Step 3: Verify tests pass**

```bash
go test ./internal/control/ -count=1 -run TestProber
```

---

### Task 4: Scheduler

**Files:**
- Create: `internal/control/scheduler.go`
- Create: `internal/control/scheduler_test.go`

**Design:**

```go
// Scheduler maps host profiles and service requirements to placement decisions.
type Scheduler struct {
    strategy string // from config: auto, bin_pack, spread, etc.
}

func NewScheduler(strategy string) *Scheduler

// Schedule takes host profiles and service requirements, returns placement.
func (s *Scheduler) Schedule(
    hosts []HostProfile,
    services []ServiceRequirement,
) ([]PlacementResult, error)

// mapStrategy converts HelixLLM strategy names to Containers scheduler strategies.
func (s *Scheduler) mapStrategy(strategy string) string
```

Strategy mapping:
- `auto` / `resource_aware` -> `StrategyResourceAware`
- `bin_pack` -> `StrategyBinPack`
- `spread` -> `StrategySpread`
- `gpu_affinity` -> `StrategyAffinity` with GPU labels
- `memory_first` -> `StrategyResourceAware` with high memory weight
- `latency_optimized` -> `StrategyResourceAware` with high network weight

For Phase 6, the scheduler uses a simple scoring approach based on available resources rather than requiring the full Containers scheduler dependency (which needs SSH-based HostManager). This keeps tests self-contained.

- [ ] **Step 1: Write scheduler_test.go** -- Test scheduling with multiple hosts and services, GPU affinity placement, bin-pack behavior, empty hosts, single host.

- [ ] **Step 2: Write scheduler.go** -- Implement scheduling logic.

- [ ] **Step 3: Verify tests pass**

```bash
go test ./internal/control/ -count=1 -run TestScheduler
```

---

### Task 5: Deployer

**Files:**
- Create: `internal/control/deployer.go`
- Create: `internal/control/deployer_test.go`

**Design:**

```go
// Deployer deploys containers to hosts via SSH.
type Deployer struct {
    ssh     SSHClient
    sshUser string
    sshKey  string
}

func NewDeployer(ssh SSHClient, sshUser, sshKey string) *Deployer

// Deploy deploys a service to a specific host.
func (d *Deployer) Deploy(
    ctx context.Context,
    placement PlacementResult,
) (*DeploymentInfo, error)

// DeployAll deploys multiple placements concurrently.
func (d *Deployer) DeployAll(
    ctx context.Context,
    placements []PlacementResult,
) ([]DeploymentInfo, []error)

// Undeploy stops and removes a container on a host.
func (d *Deployer) Undeploy(
    ctx context.Context,
    deployment DeploymentInfo,
) error
```

Deploy runs via SSH:
1. `{runtime} rm -f {name} 2>/dev/null || true` -- Remove existing
2. `{runtime} pull {image}` -- Pull image
3. `{runtime} run -d --name {name} {image}` -- Start container

- [ ] **Step 1: Write deployer_test.go** -- Test deploy success, deploy failure, undeploy, concurrent deploy, missing runtime.

- [ ] **Step 2: Write deployer.go** -- Implement deployment logic.

- [ ] **Step 3: Verify tests pass**

```bash
go test ./internal/control/ -count=1 -run TestDeployer
```

---

### Task 6: Monitor

**Files:**
- Create: `internal/control/monitor.go`
- Create: `internal/control/monitor_test.go`

**Design:**

```go
// Monitor provides health monitoring for the cluster.
type Monitor struct {
    prober   *HostProber
    ssh      SSHClient
    interval time.Duration
    mu       sync.RWMutex
    status   *ClusterStatus
}

func NewMonitor(prober *HostProber, ssh SSHClient, interval time.Duration) *Monitor

// CheckCluster probes all hosts and returns cluster status.
func (m *Monitor) CheckCluster(ctx context.Context, hosts []string) (*ClusterStatus, error)

// CheckHost probes a single host.
func (m *Monitor) CheckHost(ctx context.Context, host string) (*HostProfile, error)

// Status returns the last known cluster status.
func (m *Monitor) Status() *ClusterStatus

// Start begins periodic monitoring in the background.
func (m *Monitor) Start(ctx context.Context, hosts []string)

// Stop halts periodic monitoring.
func (m *Monitor) Stop()
```

- [ ] **Step 1: Write monitor_test.go** -- Test cluster check with mixed healthy/unhealthy hosts, status caching, single host check.

- [ ] **Step 2: Write monitor.go** -- Implement monitoring logic.

- [ ] **Step 3: Verify tests pass**

```bash
go test ./internal/control/ -count=1 -run TestMonitor
```

---

### Task 7: Control API

**Files:**
- Create: `internal/control/api.go`
- Create: `internal/control/api_test.go`

**Endpoints:**

| Method | Path | Description |
|--------|------|-------------|
| GET | `/internal/cluster/status` | Returns current cluster status |
| POST | `/internal/cluster/probe` | Triggers a probe of all hosts |
| POST | `/internal/cluster/deploy` | Deploys services based on placement |
| POST | `/internal/cluster/rebalance` | Re-evaluates and rebalances placement |

**Design:**

```go
// ControlPlane coordinates probing, scheduling, deploying, and monitoring.
type ControlPlane struct {
    prober    *HostProber
    scheduler *Scheduler
    deployer  *Deployer
    monitor   *Monitor
    hosts     []string
    mu        sync.RWMutex
    profiles  []HostProfile
    deployments []DeploymentInfo
}

func NewControlPlane(opts ControlPlaneOptions) *ControlPlane

// RegisterRoutes registers the control API routes on the Gin engine.
func RegisterRoutes(r *gin.Engine, cp *ControlPlane)
```

Request/response types:
- `GET /internal/cluster/status` -> `ClusterStatus` JSON
- `POST /internal/cluster/probe` -> `{ "hosts": [...HostProfile] }` JSON
- `POST /internal/cluster/deploy` -> `{ "services": [...ServiceRequirement] }` body -> `{ "deployments": [...DeploymentInfo] }` JSON
- `POST /internal/cluster/rebalance` -> `{ "placements": [...PlacementResult] }` JSON

- [ ] **Step 1: Write api_test.go** -- Test all 4 endpoints with httptest, mock SSH, verify JSON responses and status codes.

- [ ] **Step 2: Write api.go** -- Implement ControlPlane struct and Gin handlers.

- [ ] **Step 3: Verify tests pass**

```bash
go test ./internal/control/ -count=1 -run TestAPI
```

---

### Task 8: Wire into Server

**Files:**
- Modify: `cmd/helixllm/main.go`

- [ ] **Step 1: Update main.go** -- Import `internal/control`, create ControlPlane, register routes on the server router.

```go
import "github.com/HelixDevelopment/HelixLLM/internal/control"

// After server creation:
cp := control.NewControlPlane(control.ControlPlaneOptions{
    Hosts:    cfg.HostList(),
    SSHUser:  cfg.SSHUser,
    SSHKey:   cfg.SSHKey,
    Strategy: cfg.ScheduleStrategy,
})
control.RegisterRoutes(srv.Router(), cp)
```

- [ ] **Step 2: Verify full build and tests pass**

```bash
go build ./...
go test ./... -count=1
```

---

## Completion Criteria

- [ ] All 8 tasks implemented with tests passing
- [ ] `go test ./... -count=1` passes with zero failures
- [ ] `go build ./...` succeeds
- [ ] Each task committed separately
- [ ] Code pushed to `origin main`
