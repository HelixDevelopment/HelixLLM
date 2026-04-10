// Package hardware detects GPU, CPU, and RAM capabilities at startup and
// selects a named preset profile for llama.cpp inference configuration.
package hardware

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
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
	return &HardwareProfile{
		GPU:           gpu,
		CPU:           cpu,
		RAM:           ram,
		PresetProfile: preset,
	}
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
