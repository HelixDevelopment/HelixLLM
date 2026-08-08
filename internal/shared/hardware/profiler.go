// Package hardware detects GPU, CPU, and RAM capabilities at startup and
// selects a named preset profile for llama.cpp inference configuration.
package hardware

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/metrics"
)

// GPUProfile describes the detected GPU (if any).
type GPUProfile struct {
	Available  bool   `json:"available"`
	Name       string `json:"name"`
	VRAMTotal  int64  `json:"vram_total"` // bytes
	VRAMFree   int64  `json:"vram_free"`  // bytes
	ComputeCap string `json:"compute_cap"`
}

// CPUProfile describes the detected CPU.
type CPUProfile struct {
	Model   string `json:"model"`
	Cores   int    `json:"cores"`
	Threads int    `json:"threads"`
	AVX2    bool   `json:"avx2"`
	AVX512  bool   `json:"avx512"`
}

// RAMProfile describes detected system memory.
type RAMProfile struct {
	Total     int64 `json:"total"`     // bytes
	Available int64 `json:"available"` // bytes
}

// HardwareProfile is the aggregated result of hardware detection. PresetProfile
// is one of: "cpu_only", "consumer_6gb", "consumer_8gb", "high_end".
type HardwareProfile struct {
	GPU           GPUProfile `json:"gpu"`
	CPU           CPUProfile `json:"cpu"`
	RAM           RAMProfile `json:"ram"`
	PresetProfile string     `json:"preset_profile"`
	// NUMAEnabled indicates whether NUMA topology was detected.
	NUMAEnabled bool `json:"numa_enabled"`
	// NUMAIndices lists the detected NUMA node indices.
	NUMAIndices []int `json:"numa_indices"`
	// MemoryBandwidthMBps is the estimated memory bandwidth in MB/s.
	MemoryBandwidthMBps float64 `json:"memory_bandwidth_mbps"`
	// L3CacheKB is the L3 cache size in KB (summed across all nodes).
	L3CacheKB int `json:"l3_cache_kb"`
	// SIMDCapabilities lists detected SIMD ISA extensions (e.g. avx512f, avx2, neon).
	SIMDCapabilities []string `json:"simd_capabilities"`
}

// InferenceThreads returns the recommended thread count for llama.cpp inference
// (leaves 2 threads free for OS / other tasks).
func (p *HardwareProfile) InferenceThreads() int {
	return inferenceThreads(p.CPU.Cores)
}

// BatchThreads returns the recommended thread count for batch processing
// (uses all available CPU cores).
func (p *HardwareProfile) BatchThreads() int {
	return p.CPU.Cores
}

// Detect probes GPU/CPU/RAM. Returns a valid profile even when detection partially fails (graceful degradation).
func Detect() *HardwareProfile {
	gpu := detectGPU()
	cpu := detectCPU()
	ram := detectRAM()
	preset := selectPresetProfile(gpu.VRAMTotal)
	numa, numaIndices := detectNUMA()
	return &HardwareProfile{
		GPU:                 gpu,
		CPU:                 cpu,
		RAM:                 ram,
		PresetProfile:       preset,
		NUMAEnabled:         numa,
		NUMAIndices:         numaIndices,
		MemoryBandwidthMBps: estimateMemoryBandwidth(),
		L3CacheKB:           detectL3Cache(),
		SIMDCapabilities:    detectSIMDCapabilities(cpu),
	}
}

// UpdateSystemMetrics sets the VRAM and RAM Prometheus gauges from a detected
// hardware profile. Call once at boot (or periodically) to keep the gauges
// current.
func UpdateSystemMetrics(p *HardwareProfile) {
	if p.GPU.Available {
		metrics.VRAMUsageBytes.Set(float64(p.GPU.VRAMTotal - p.GPU.VRAMFree))
	}
	metrics.RAMUsageBytes.Set(float64(p.RAM.Total - p.RAM.Available))
}

// ---------------------------------------------------------------------------
// Internal detection helpers
// ---------------------------------------------------------------------------

// detectGPU runs nvidia-smi and delegates to parseNvidiaSMI.
// NOTE: only the first GPU is used in multi-GPU systems
func detectGPU() GPUProfile {
	out, err := exec.Command(
		"nvidia-smi",
		"--query-gpu=memory.total,memory.free,name,compute_cap",
		"--format=csv,noheader,nounits",
	).Output()
	if err != nil {
		return GPUProfile{Available: false}
	}
	profile, err := parseNvidiaSMI(strings.TrimSpace(string(out)))
	if err != nil {
		return GPUProfile{Available: false}
	}
	return profile
}

// detectCPU reads /proc/cpuinfo and delegates to parseProcCPUInfo.
// Falls back to runtime.NumCPU() when /proc/cpuinfo is unavailable.
func detectCPU() CPUProfile {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		// Fallback: minimal profile from Go runtime
		n := runtime.NumCPU()
		return CPUProfile{
			Cores:   n,
			Threads: n,
		}
	}
	profile, err := parseProcCPUInfo(string(data))
	if err != nil {
		n := runtime.NumCPU()
		return CPUProfile{
			Cores:   n,
			Threads: n,
		}
	}
	return profile
}

// detectRAM reads /proc/meminfo and returns total/available RAM in bytes.
func detectRAM() RAMProfile {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return RAMProfile{}
	}
	var total, available int64
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			total = parseMemInfoKB(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			available = parseMemInfoKB(line)
		}
	}
	return RAMProfile{
		Total:     total * 1024,
		Available: available * 1024,
	}
}

// ---------------------------------------------------------------------------
// Parsers (exported for testing via same package)
// ---------------------------------------------------------------------------

// parseNvidiaSMI parses a single CSV line produced by:
//
//	nvidia-smi --query-gpu=memory.total,memory.free,name,compute_cap \
//	           --format=csv,noheader,nounits
//
// The nounits flag means values are plain integers (already in MiB).
// An empty string input returns Available=false with no error.
func parseNvidiaSMI(output string) (GPUProfile, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return GPUProfile{Available: false}, nil
	}

	// Expected format: "6144, 5800, NVIDIA GeForce RTX 3060, 8.6"
	// Split on ", " but only for the first three commas; the GPU name may
	// contain commas itself, so we split into at most 4 fields.
	parts := strings.SplitN(output, ", ", 4)
	if len(parts) != 4 {
		// Try splitting on "," without space as a fallback
		parts = strings.SplitN(output, ",", 4)
		if len(parts) != 4 {
			return GPUProfile{}, fmt.Errorf("hardware: unexpected nvidia-smi output: %q", output)
		}
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
	}

	totalMiB, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return GPUProfile{}, fmt.Errorf("hardware: parsing vram_total: %w", err)
	}
	freeMiB, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil {
		return GPUProfile{}, fmt.Errorf("hardware: parsing vram_free: %w", err)
	}

	return GPUProfile{
		Available:  true,
		Name:       strings.TrimSpace(parts[2]),
		VRAMTotal:  totalMiB * 1024 * 1024,
		VRAMFree:   freeMiB * 1024 * 1024,
		ComputeCap: strings.TrimSpace(parts[3]),
	}, nil
}

// parseProcCPUInfo parses the content of /proc/cpuinfo. It reads all processor
// blocks and extracts model name, cpu cores, siblings (threads), and CPU
// extension flags from the first block that provides them.
func parseProcCPUInfo(output string) (CPUProfile, error) {
	var profile CPUProfile

	// Track which fields we have found so we stop after the first occurrence.
	gotModel, gotCores, gotThreads, gotFlags := false, false, false, false

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])

		switch key {
		case "model name":
			if !gotModel {
				profile.Model = val
				gotModel = true
			}
		case "cpu cores":
			if !gotCores {
				n, err := strconv.Atoi(val)
				if err == nil {
					profile.Cores = n
					gotCores = true
				}
			}
		case "siblings":
			if !gotThreads {
				n, err := strconv.Atoi(val)
				if err == nil {
					profile.Threads = n
					gotThreads = true
				}
			}
		case "flags":
			if !gotFlags {
				flags := " " + val + " "
				profile.AVX2 = strings.Contains(flags, " avx2 ")
				// Detects any AVX-512 variant (avx512f, avx512bw, etc.)
				profile.AVX512 = strings.Contains(flags, " avx512")
				gotFlags = true
			}
		}

		if gotModel && gotCores && gotThreads && gotFlags {
			break
		}
	}

	if profile.Cores == 0 {
		profile.Cores = runtime.NumCPU()
	}
	if profile.Threads == 0 {
		profile.Threads = profile.Cores
	}

	return profile, nil
}

// ---------------------------------------------------------------------------
// Profile selection and thread helpers
// ---------------------------------------------------------------------------

// selectPresetProfile maps VRAM size (bytes) to a named preset. Thresholds:
//
//	0 – 4095 MiB  → "cpu_only"
//	4096 – 6143 MiB → "consumer_6gb"
//	6144 – 8191 MiB → "consumer_8gb"
//	8192+ MiB       → "high_end"
func selectPresetProfile(vramBytes int64) string {
	const MiB = int64(1024 * 1024)
	switch {
	case vramBytes < 4096*MiB:
		return "cpu_only"
	case vramBytes < 6144*MiB:
		return "consumer_6gb"
	case vramBytes < 8192*MiB:
		return "consumer_8gb"
	default:
		return "high_end"
	}
}

const (
	reservedOSThreads   = 2
	minInferenceThreads = 2
)

// inferenceThreads returns max(minInferenceThreads, cores-reservedOSThreads) so
// at least reservedOSThreads OS threads remain free for background work.
func inferenceThreads(cores int) int {
	n := cores - reservedOSThreads
	if n < minInferenceThreads {
		return minInferenceThreads
	}
	return n
}

// ---------------------------------------------------------------------------
// CPU platform detection (NUMA, bandwidth, L3, SIMD)
// ---------------------------------------------------------------------------

// detectNUMA checks /sys/devices/system/node/ for NUMA nodes. Returns
// enabled status and the list of node indices.
func detectNUMA() (bool, []int) {
	entries, err := os.ReadDir("/sys/devices/system/node/")
	if err != nil {
		return false, nil
	}
	var indices []int
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "node") {
			continue
		}
		n, parseErr := strconv.Atoi(name[4:])
		if parseErr != nil {
			continue
		}
		indices = append(indices, n)
	}
	if len(indices) < 2 {
		return false, indices
	}
	return true, indices
}

const (
	procCPUInfoPath = "/proc/cpuinfo"
	procMemInfoPath = "/proc/meminfo"
)

// estimateMemoryBandwidth returns an estimated memory bandwidth in MB/s
// based on DDR generation and installed capacity. Safe default: ~25 GB/s
// (DDR4 single-channel).
func estimateMemoryBandwidth() float64 {
	return estimateMemoryBandwidthFrom(procCPUInfoPath, procMemInfoPath)
}

// estimateMemoryBandwidthFrom is estimateMemoryBandwidth with its two input
// files injected, so the capacity branches are reachable under test.
func estimateMemoryBandwidthFrom(cpuinfoPath, meminfoPath string) float64 {
	// Try to detect DDR generation from /proc/cpuinfo
	data, err := os.ReadFile(cpuinfoPath)
	if err != nil {
		return 25000.0
	}
	content := string(data)
	if strings.Contains(content, "ddr5") || strings.Contains(content, "DDR5") {
		return 51200.0 // DDR5-6400 single channel
	}
	// Installed capacity comes from /proc/meminfo — MemTotal does not exist in
	// /proc/cpuinfo, so reading it there left memTotal permanently 0 and made
	// both capacity branches below unreachable.
	memData, memErr := os.ReadFile(meminfoPath)
	if memErr != nil {
		return 25000.0
	}
	var memTotal int64
	for _, line := range strings.Split(string(memData), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "MemTotal:") {
			memTotal = parseMemInfoKB(line)
			break
		}
	}
	if memTotal >= 128*1024*1024 { // >= 128 GB → likely multi-channel DDR4/DDR5
		return 76800.0
	}
	if memTotal >= 64*1024*1024 { // >= 64 GB → dual-channel DDR4
		return 51200.0
	}
	return 25000.0 // conservative DDR4 single-channel
}

// sysfsCPURoot is the sysfs directory enumerating the system's logical CPUs.
// Tests bypass it by passing a synthetic root to sumL3FromSysfs directly.
const sysfsCPURoot = "/sys/devices/system/cpu"

// detectL3Cache reports the total L3 cache size in KB, summed across all L3
// cache instances on the system. Falls back to 0 on error.
func detectL3Cache() int {
	// Try dmidecode first
	out, err := exec.Command(
		"dmidecode", "-t", "cache",
	).Output()
	if err == nil {
		total := parseL3FromDMI(string(out))
		if total > 0 {
			return total
		}
	}
	// Fallback: walk /sys
	return sumL3FromSysfs(sysfsCPURoot)
}

// sumL3FromSysfs walks <cpuRoot>/cpu*/cache/index3/ and sums the size of each
// DISTINCT L3 cache instance. Returns 0 when the tree is unreadable.
//
// Every logical CPU exposes the index3 node of the cache instance it is
// attached to, so a naive walk would count one instance once per sharing CPU.
// Instances are therefore de-duplicated by cache-instance identity, drawn from
// the kernel cacheinfo ABI:
//
//  1. index3/id — documented as "a unique identifier for a specific cache
//     instance of a particular type" (values may be non-contiguous).
//  2. index3/shared_cpu_list — the set of CPUs sharing the instance, which is
//     identical for every CPU of one instance and differs between instances.
//     Used when id is exposed nowhere in the tree.
//
// ONE of those attributes is chosen for the WHOLE walk (see l3IdentityAttr),
// never per directory: mixing them would let a single instance yield an id key
// for some of its CPUs and a shared_cpu_list key for the rest, counting that
// instance twice. With one identity space per walk, an entry that does not
// expose the chosen attribute is skipped rather than counted, so the result can
// under-count on a host with irregular cacheinfo but can never over-count.
func sumL3FromSysfs(cpuRoot string) int {
	entries, err := os.ReadDir(cpuRoot)
	if err != nil {
		return 0
	}
	attr := l3IdentityAttr(cpuRoot, entries)
	var totalL3 int64
	seen := make(map[string]bool)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "cpu") {
			continue
		}
		indexDir := filepath.Join(cpuRoot, name, "cache", "index3")
		data, fErr := os.ReadFile(filepath.Join(indexDir, "size"))
		if fErr != nil {
			continue
		}
		key, ok := readCacheAttr(indexDir, attr)
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		totalL3 += parseCacheSize(string(data))
	}
	return int(totalL3)
}

// l3IdentityAttr picks the single cacheinfo attribute used to identify L3
// instances for an entire walk: "id" when any index3 node in the tree exposes
// a non-blank one, otherwise "shared_cpu_list". Deciding once — rather than
// per directory — is what makes the de-duplication incapable of over-counting.
func l3IdentityAttr(cpuRoot string, entries []os.DirEntry) string {
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "cpu") {
			continue
		}
		indexDir := filepath.Join(cpuRoot, e.Name(), "cache", "index3")
		if _, ok := readCacheAttr(indexDir, "id"); ok {
			return "id"
		}
	}
	return "shared_cpu_list"
}

// readCacheAttr reads one cacheinfo attribute, reporting ok=false when it is
// missing, unreadable, or blank.
func readCacheAttr(indexDir, attr string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(indexDir, attr))
	if err != nil {
		return "", false
	}
	val := strings.TrimSpace(string(raw))
	return val, val != ""
}

// parseL3FromDMI extracts the L3 cache size from dmidecode output.
func parseL3FromDMI(output string) int {
	var totalL3 int
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "L3") && strings.Contains(line, "KB") {
			fields := strings.Fields(line)
			for _, f := range fields {
				if strings.HasSuffix(f, "KB") {
					val := strings.TrimSuffix(f, "KB")
					if n, err := strconv.Atoi(val); err == nil {
						totalL3 += n
					}
				}
			}
		}
	}
	return totalL3
}

// parseCacheSize extracts the KB value from a sysfs cache size string
// like "32768K\n" or "32M\n".
func parseCacheSize(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	// Handle "32M" or "32768K" format
	var multiplier int64 = 1
	if strings.HasSuffix(raw, "M") || strings.HasSuffix(raw, "m") {
		multiplier = 1024
		raw = strings.TrimSuffix(strings.TrimSuffix(raw, "M"), "m")
	} else {
		raw = strings.TrimSuffix(strings.TrimSuffix(raw, "K"), "k")
	}
	val, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return val * multiplier
}

// detectSIMDCapabilities returns the SIMD extensions available on this
// system, derived from both /proc/cpuinfo flags and runtime detection.
func detectSIMDCapabilities(cpu CPUProfile) []string {
	var caps []string
	if cpu.AVX512 {
		caps = append(caps, "avx512f")
	}
	if cpu.AVX2 {
		caps = append(caps, "avx2")
	}
	// Check for AVX (non-2, non-512) from /proc/cpuinfo flags
	data, err := os.ReadFile("/proc/cpuinfo")
	if err == nil {
		content := string(data)
		if strings.Contains(content, " avx ") {
			caps = append(caps, "avx")
		}
		if strings.Contains(content, " sse4_2 ") {
			caps = append(caps, "sse4_2")
		}
		if strings.Contains(content, " sse4_1 ") {
			caps = append(caps, "sse4_1")
		}
		if strings.Contains(content, " neon ") {
			caps = append(caps, "neon")
		}
		if strings.Contains(content, " fma ") {
			caps = append(caps, "fma")
		}
	}
	if len(caps) == 0 {
		// ARM fallback
		if strings.Contains(runtime.GOARCH, "arm") {
			caps = append(caps, "neon")
		}
	}
	return caps
}

// ---------------------------------------------------------------------------
// Internal utility
// ---------------------------------------------------------------------------

// parseMemInfoKB extracts the kB value from a /proc/meminfo line such as:
//
//	MemTotal:       65536000 kB
func parseMemInfoKB(line string) int64 {
	// Remove key prefix up to ":"
	idx := strings.Index(line, ":")
	if idx < 0 {
		return 0
	}
	rest := strings.TrimSpace(line[idx+1:])
	// Strip " kB" suffix
	rest = strings.TrimSuffix(rest, " kB")
	rest = strings.TrimSpace(rest)
	val, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		return 0
	}
	return val
}
