package control

import (
	"fmt"
	"math"
)

// Valid scheduling strategies.
const (
	StrategyAuto             = "auto"
	StrategyBinPack          = "bin_pack"
	StrategySpread           = "spread"
	StrategyGPUAffinity      = "gpu_affinity"
	StrategyMemoryFirst      = "memory_first"
	StrategyLatencyOptimized = "latency_optimized"
)

// Scheduler maps host profiles and service requirements to
// placement decisions. It uses a scoring approach based on
// available resources and the chosen strategy.
type Scheduler struct {
	strategy string
}

// NewScheduler creates a Scheduler with the given global strategy.
func NewScheduler(strategy string) *Scheduler {
	s := &Scheduler{}
	s.strategy = s.resolveStrategy(strategy)
	return s
}

// Schedule determines where to place each service across the
// available hosts. Returns one PlacementResult per service.
func (s *Scheduler) Schedule(
	hosts []HostProfile,
	services []ServiceRequirement,
) ([]PlacementResult, error) {
	if len(hosts) == 0 && len(services) > 0 {
		return nil, fmt.Errorf("no hosts available for scheduling")
	}
	if len(services) == 0 {
		return nil, nil
	}

	// Filter to online hosts only.
	online := filterOnlineHosts(hosts)
	if len(online) == 0 {
		return nil, fmt.Errorf(
			"no online hosts available for scheduling",
		)
	}

	// Track per-host load for bin-pack and spread.
	hostLoad := make(map[string]int, len(online))
	for _, h := range online {
		hostLoad[h.Name] = 0
	}

	results := make([]PlacementResult, 0, len(services))
	for _, svc := range services {
		strategy := s.effectiveStrategy(svc)
		best := s.pickHost(online, svc, strategy, hostLoad)
		hostLoad[best.HostName]++
		results = append(results, best)
	}

	return results, nil
}

// effectiveStrategy returns the strategy to use for a specific
// service, considering both per-service overrides and the global
// strategy.
func (s *Scheduler) effectiveStrategy(
	svc ServiceRequirement,
) string {
	if svc.Strategy != "" {
		return s.resolveStrategy(svc.Strategy)
	}
	return s.strategy
}

// pickHost selects the best host for a service using the given
// strategy.
func (s *Scheduler) pickHost(
	hosts []HostProfile,
	svc ServiceRequirement,
	strategy string,
	hostLoad map[string]int,
) PlacementResult {
	var bestHost string
	var bestScore float64
	var bestReason string

	for _, h := range hosts {
		score, reason := s.scoreHost(h, svc, strategy, hostLoad)
		if bestHost == "" || score > bestScore {
			bestHost = h.Name
			bestScore = score
			bestReason = reason
		}
	}

	return PlacementResult{
		Service:  svc,
		HostName: bestHost,
		Score:    bestScore,
		Reason:   bestReason,
	}
}

// scoreHost calculates a placement score (0.0-1.0) for a host
// given a service requirement and strategy.
func (s *Scheduler) scoreHost(
	host HostProfile,
	svc ServiceRequirement,
	strategy string,
	hostLoad map[string]int,
) (float64, string) {
	switch strategy {
	case StrategyGPUAffinity:
		return s.scoreGPUAffinity(host, svc)
	case StrategyMemoryFirst:
		return s.scoreMemoryFirst(host, svc)
	case StrategyBinPack:
		return s.scoreBinPack(host, svc, hostLoad)
	case StrategySpread:
		return s.scoreSpread(host, svc, hostLoad)
	case StrategyLatencyOptimized:
		return s.scoreLatencyOptimized(host, svc)
	default:
		return s.scoreResourceAware(host, svc)
	}
}

// scoreResourceAware is the default balanced scoring.
func (s *Scheduler) scoreResourceAware(
	host HostProfile, svc ServiceRequirement,
) (float64, string) {
	score := 0.0
	reasons := make([]string, 0, 4)

	// CPU fitness.
	cpuAvail := float64(host.CPU.Cores) *
		(100.0 - host.CPU.UsagePercent) / 100.0
	if svc.CPUCores > 0 {
		cpuFit := math.Min(cpuAvail/svc.CPUCores, 1.0)
		score += cpuFit * 0.3
		reasons = append(reasons,
			fmt.Sprintf("cpu=%.2f", cpuFit))
	} else {
		score += 0.3
	}

	// Memory fitness.
	memAvailMB := host.Memory.AvailableBytes / (1024 * 1024)
	if svc.MemoryMB > 0 {
		memFit := math.Min(
			float64(memAvailMB)/float64(svc.MemoryMB), 1.0,
		)
		score += memFit * 0.3
		reasons = append(reasons,
			fmt.Sprintf("mem=%.2f", memFit))
	} else {
		score += 0.3
	}

	// Disk fitness.
	diskAvailMB := (host.Disk.TotalBytes - host.Disk.UsedBytes) /
		(1024 * 1024)
	if svc.DiskMB > 0 {
		diskFit := math.Min(
			float64(diskAvailMB)/float64(svc.DiskMB), 1.0,
		)
		score += diskFit * 0.2
		reasons = append(reasons,
			fmt.Sprintf("disk=%.2f", diskFit))
	} else {
		score += 0.2
	}

	// GPU bonus.
	if svc.NeedsGPU {
		if host.HasGPU() {
			score += 0.2
			reasons = append(reasons, "gpu=yes")
		}
		// No bonus if no GPU.
	} else {
		score += 0.2
	}

	return score, fmt.Sprintf("resource_aware(%s)",
		joinReasons(reasons))
}

// scoreGPUAffinity heavily favors hosts with GPUs.
func (s *Scheduler) scoreGPUAffinity(
	host HostProfile, svc ServiceRequirement,
) (float64, string) {
	if host.HasGPU() {
		// Base score from resources + GPU bonus.
		base, _ := s.scoreResourceAware(host, svc)
		score := base*0.5 + 0.5
		return score, fmt.Sprintf(
			"gpu_affinity(gpu=%s, vram=%dMB)",
			host.GPU.Name, host.GPU.MemoryMB,
		)
	}
	// Penalize heavily if no GPU.
	base, _ := s.scoreResourceAware(host, svc)
	return base * 0.3, "gpu_affinity(no_gpu)"
}

// scoreMemoryFirst heavily favors hosts with more available memory.
// Unlike resource_aware, this uses absolute available memory so that
// hosts with more RAM always rank higher regardless of the
// requirement threshold.
func (s *Scheduler) scoreMemoryFirst(
	host HostProfile, svc ServiceRequirement,
) (float64, string) {
	memAvailMB := host.Memory.AvailableBytes / (1024 * 1024)

	// Use absolute available memory as the primary signal.
	// Normalize to a 0-1 range using 256 GB as the reference
	// ceiling; this ensures hosts with more memory always score
	// higher.
	const refMemMB = 256 * 1024 // 256 GB
	memScore := math.Min(float64(memAvailMB)/refMemMB, 1.0)

	// Apply a fitness check: if the requirement is not met, halve
	// the score.
	if svc.MemoryMB > 0 && memAvailMB < svc.MemoryMB {
		memScore *= 0.5
	}

	cpuAvail := float64(host.CPU.Cores) *
		(100.0 - host.CPU.UsagePercent) / 100.0
	cpuScore := 0.0
	if svc.CPUCores > 0 {
		cpuScore = math.Min(cpuAvail/svc.CPUCores, 1.0)
	} else {
		cpuScore = math.Min(cpuAvail/32.0, 1.0)
	}

	score := memScore*0.8 + cpuScore*0.2
	return score, fmt.Sprintf(
		"memory_first(avail=%dMB)", memAvailMB,
	)
}

// scoreBinPack favors hosts that already have containers (pack
// tightly).
func (s *Scheduler) scoreBinPack(
	host HostProfile,
	svc ServiceRequirement,
	hostLoad map[string]int,
) (float64, string) {
	base, _ := s.scoreResourceAware(host, svc)

	// Prefer hosts with more existing load (pack tightly).
	load := hostLoad[host.Name]
	loadBonus := math.Min(float64(load)*0.1, 0.3)

	score := base*0.7 + loadBonus + 0.0
	return math.Min(score, 1.0),
		fmt.Sprintf("bin_pack(load=%d)", load)
}

// scoreSpread favors hosts with fewer containers (spread evenly).
func (s *Scheduler) scoreSpread(
	host HostProfile,
	svc ServiceRequirement,
	hostLoad map[string]int,
) (float64, string) {
	base, _ := s.scoreResourceAware(host, svc)

	// Prefer hosts with less load (spread).
	load := hostLoad[host.Name]
	loadPenalty := math.Min(float64(load)*0.15, 0.5)

	score := math.Max(base-loadPenalty, 0.0)
	return score, fmt.Sprintf("spread(load=%d)", load)
}

// scoreLatencyOptimized favors the first host (assumed closest to
// clients). In production, this would use network latency data.
func (s *Scheduler) scoreLatencyOptimized(
	host HostProfile, svc ServiceRequirement,
) (float64, string) {
	base, _ := s.scoreResourceAware(host, svc)
	return base, "latency_optimized(proximity)"
}

// resolveStrategy validates and normalizes a strategy name.
func (s *Scheduler) resolveStrategy(strategy string) string {
	switch strategy {
	case StrategyAuto, StrategyBinPack, StrategySpread,
		StrategyGPUAffinity, StrategyMemoryFirst,
		StrategyLatencyOptimized:
		return strategy
	default:
		return StrategyAuto
	}
}

// filterOnlineHosts returns only hosts with state "online".
func filterOnlineHosts(hosts []HostProfile) []HostProfile {
	var online []HostProfile
	for _, h := range hosts {
		if h.IsHealthy() {
			online = append(online, h)
		}
	}
	return online
}

// joinReasons joins reason strings with commas.
func joinReasons(reasons []string) string {
	result := ""
	for i, r := range reasons {
		if i > 0 {
			result += ", "
		}
		result += r
	}
	return result
}
