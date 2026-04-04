package control

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SSHClient abstracts SSH command execution for testing.
// Production implementations wrap digital.vasic.containers
// remote.RemoteExecutor; tests use a mock.
type SSHClient interface {
	// Run executes a command on the specified host and returns
	// the combined stdout output.
	Run(ctx context.Context, host, command string) (string, error)
	// IsReachable checks if a host is reachable via SSH.
	IsReachable(ctx context.Context, host string) bool
}

// probeSeparator is the marker used to split compound command
// output into sections.
const probeSeparator = "---PROBE_SEP---"

// HostProber probes hosts via SSH to collect system profiles.
type HostProber struct {
	ssh     SSHClient
	sshUser string
	sshKey  string
}

// NewHostProber creates a HostProber that uses the given SSH client.
func NewHostProber(
	ssh SSHClient, sshUser, sshKey string,
) *HostProber {
	return &HostProber{
		ssh:     ssh,
		sshUser: sshUser,
		sshKey:  sshKey,
	}
}

// probeCommand returns the compound SSH command that collects all
// system information in a single round-trip.
func probeCommand() string {
	parts := []string{
		"uname -s -m",
		fmt.Sprintf("echo '%s'", probeSeparator),
		"cat /proc/cpuinfo 2>/dev/null | grep 'model name' | head -1 || sysctl -n machdep.cpu.brand_string 2>/dev/null || echo unknown",
		fmt.Sprintf("echo '%s'", probeSeparator),
		"nproc 2>/dev/null || sysctl -n hw.logicalcpu 2>/dev/null || echo 1",
		fmt.Sprintf("echo '%s'", probeSeparator),
		"free -b 2>/dev/null || vm_stat 2>/dev/null || echo 'Mem: 0 0 0 0 0 0'",
		fmt.Sprintf("echo '%s'", probeSeparator),
		"nvidia-smi --query-gpu=name,memory.total,driver_version --format=csv,noheader,nounits 2>/dev/null || echo ''",
		fmt.Sprintf("echo '%s'", probeSeparator),
		"df -B1 --output=size,used / 2>/dev/null | tail -1 || df -b / 2>/dev/null | tail -1 || echo '0 0'",
		fmt.Sprintf("echo '%s'", probeSeparator),
		"podman version --format '{{.Client.Version}}' 2>/dev/null && echo podman || docker version --format '{{.Client.Version}}' 2>/dev/null && echo docker || echo none",
	}
	return strings.Join(parts, " && ")
}

// ProbeHost connects to a single host via SSH and returns its
// profile.
func (p *HostProber) ProbeHost(
	ctx context.Context, host string,
) (*HostProfile, error) {
	if !p.ssh.IsReachable(ctx, host) {
		return nil, fmt.Errorf("host %q is unreachable", host)
	}

	output, err := p.ssh.Run(ctx, host, probeCommand())
	if err != nil {
		return nil, fmt.Errorf("probe %s: %w", host, err)
	}

	profile, err := parseProbeOutput(host, output)
	if err != nil {
		return nil, fmt.Errorf("parse probe %s: %w", host, err)
	}

	return profile, nil
}

// ProbeAll probes all hosts concurrently and returns successful
// profiles and any errors encountered.
func (p *HostProber) ProbeAll(
	ctx context.Context, hosts []string,
) ([]HostProfile, []error) {
	if len(hosts) == 0 {
		return nil, nil
	}

	type result struct {
		profile *HostProfile
		err     error
	}

	results := make([]result, len(hosts))
	var wg sync.WaitGroup

	for i, host := range hosts {
		wg.Add(1)
		go func(idx int, h string) {
			defer wg.Done()
			profile, err := p.ProbeHost(ctx, h)
			results[idx] = result{profile: profile, err: err}
		}(i, host)
	}

	wg.Wait()

	var profiles []HostProfile
	var errs []error
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, r.err)
		} else if r.profile != nil {
			profiles = append(profiles, *r.profile)
		}
	}

	return profiles, errs
}

// parseProbeOutput parses the compound command output into a
// HostProfile.
func parseProbeOutput(
	host, output string,
) (*HostProfile, error) {
	sections := strings.Split(output, probeSeparator)
	if len(sections) < 7 {
		return nil, fmt.Errorf(
			"unexpected probe output: got %d sections, want 7",
			len(sections),
		)
	}

	profile := &HostProfile{
		Name:     host,
		Address:  host,
		State:    "online",
		ProbedAt: time.Now(),
	}

	// Section 0: uname -s -m -> OS and Arch.
	parseUname(strings.TrimSpace(sections[0]), profile)

	// Section 1: CPU model name.
	profile.CPU.Model = parseCPUModel(
		strings.TrimSpace(sections[1]),
	)

	// Section 2: nproc -> CPU cores.
	profile.CPU.Cores = parseInt(strings.TrimSpace(sections[2]), 1)

	// Section 3: free -b -> Memory.
	parseMemory(strings.TrimSpace(sections[3]), profile)

	// Section 4: nvidia-smi -> GPU (optional).
	parseGPU(strings.TrimSpace(sections[4]), profile)

	// Section 5: df -> Disk.
	parseDisk(strings.TrimSpace(sections[5]), profile)

	// Section 6: Container runtime.
	parseRuntime(strings.TrimSpace(sections[6]), profile)

	return profile, nil
}

// parseUname extracts OS and architecture from "uname -s -m" output.
func parseUname(output string, profile *HostProfile) {
	fields := strings.Fields(output)
	if len(fields) >= 1 {
		profile.OS = fields[0]
	}
	if len(fields) >= 2 {
		profile.Arch = fields[1]
	}
}

// parseCPUModel extracts the CPU model name from /proc/cpuinfo
// grep output or sysctl output.
func parseCPUModel(output string) string {
	// /proc/cpuinfo format: "model name\t: Some CPU Name"
	if idx := strings.Index(output, ":"); idx >= 0 {
		return strings.TrimSpace(output[idx+1:])
	}
	return strings.TrimSpace(output)
}

// parseMemory extracts memory info from "free -b" output.
func parseMemory(output string, profile *HostProfile) {
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "Mem:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 7 {
			profile.Memory.TotalBytes = parseUint64(fields[1])
			available := parseUint64(fields[6])
			profile.Memory.AvailableBytes = available
			if profile.Memory.TotalBytes > 0 {
				used := profile.Memory.TotalBytes -
					profile.Memory.AvailableBytes
				profile.Memory.UsagePercent =
					float64(used) /
						float64(profile.Memory.TotalBytes) * 100.0
			}
		}
		break
	}
}

// parseGPU extracts GPU info from nvidia-smi CSV output.
func parseGPU(output string, profile *HostProfile) {
	output = strings.TrimSpace(output)
	if output == "" {
		return
	}
	// Format: "name, memory_mb, driver_version"
	parts := strings.SplitN(output, ",", 3)
	if len(parts) < 3 {
		return
	}
	name := strings.TrimSpace(parts[0])
	if name == "" {
		return
	}
	profile.GPU = &GPUInfo{
		Name:     name,
		MemoryMB: parseUint64(strings.TrimSpace(parts[1])),
		Driver:   strings.TrimSpace(parts[2]),
	}
}

// parseDisk extracts disk info from "df -B1" output.
func parseDisk(output string, profile *HostProfile) {
	fields := strings.Fields(output)
	if len(fields) >= 2 {
		profile.Disk.TotalBytes = parseUint64(fields[0])
		profile.Disk.UsedBytes = parseUint64(fields[1])
		if profile.Disk.TotalBytes > 0 {
			profile.Disk.UsagePercent =
				float64(profile.Disk.UsedBytes) /
					float64(profile.Disk.TotalBytes) * 100.0
		}
	}
}

// parseRuntime extracts container runtime info from the detection
// command output.
func parseRuntime(output string, profile *HostProfile) {
	output = strings.TrimSpace(output)
	if output == "none" || output == "" {
		profile.ContainerRuntime = ContainerRuntimeInfo{
			Name: "none",
		}
		return
	}

	// The output format is either:
	//   "version\npodman" or "version\ndocker"
	// or simply "podman version" or "docker version"
	lines := strings.Split(output, "\n")

	if len(lines) >= 2 {
		version := strings.TrimSpace(lines[0])
		name := strings.TrimSpace(lines[1])
		profile.ContainerRuntime = ContainerRuntimeInfo{
			Name:    name,
			Version: version,
		}
		return
	}

	// Single-line format: "podman 4.9.0" or "docker 24.0.7"
	parts := strings.Fields(output)
	if len(parts) >= 2 {
		profile.ContainerRuntime = ContainerRuntimeInfo{
			Name:    parts[0],
			Version: parts[1],
		}
		return
	}

	profile.ContainerRuntime = ContainerRuntimeInfo{
		Name: output,
	}
}

// parseInt parses a string as an integer, returning the default
// value on failure.
func parseInt(s string, def int) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

// parseUint64 parses a string as a uint64, returning 0 on failure.
func parseUint64(s string) uint64 {
	v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return v
}
