// Package hardware provides hardware detection and profiling for HelixLLM.
package hardware

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseNvidiaSMI_ValidOutput verifies that a well-formed nvidia-smi CSV line
// (with --format=csv,noheader,nounits so units are already stripped) is parsed
// into a correct GPUProfile.
func TestParseNvidiaSMI_ValidOutput(t *testing.T) {
	input := "6144, 5800, NVIDIA GeForce RTX 3060, 8.6"
	profile, err := parseNvidiaSMI(input)
	require.NoError(t, err)
	assert.True(t, profile.Available)
	assert.Equal(t, "NVIDIA GeForce RTX 3060", profile.Name)
	// 6144 MiB in bytes
	assert.Equal(t, int64(6144)*1024*1024, profile.VRAMTotal)
	// 5800 MiB in bytes
	assert.Equal(t, int64(5800)*1024*1024, profile.VRAMFree)
	assert.Equal(t, "8.6", profile.ComputeCap)
}

// TestParseNvidiaSMI_Empty verifies that an empty string results in
// Available=false with no error (nvidia-smi not found / no GPU).
func TestParseNvidiaSMI_Empty(t *testing.T) {
	profile, err := parseNvidiaSMI("")
	require.NoError(t, err)
	assert.False(t, profile.Available)
	assert.Equal(t, "", profile.Name)
	assert.Equal(t, int64(0), profile.VRAMTotal)
	assert.Equal(t, int64(0), profile.VRAMFree)
}

// TestParseProcCPUInfo_ValidOutput verifies that a realistic /proc/cpuinfo
// snippet is parsed correctly, picking up model name, cores, threads, and
// extension flags.
func TestParseProcCPUInfo_ValidOutput(t *testing.T) {
	// Minimal representative excerpt from an AMD Ryzen 9 system.
	// Two processor blocks; the second is included to ensure we count siblings
	// from a single block (not double-count).
	input := `processor	: 0
vendor_id	: AuthenticAMD
cpu family	: 25
model name	: AMD Ryzen 9 5900X 12-Core Processor
cpu cores	: 16
siblings	: 32
flags		: fpu vme de pse tsc avx2 cx8 apic

processor	: 1
vendor_id	: AuthenticAMD
cpu family	: 25
model name	: AMD Ryzen 9 5900X 12-Core Processor
cpu cores	: 16
siblings	: 32
flags		: fpu vme de pse tsc avx2 cx8 apic
`
	profile, err := parseProcCPUInfo(input)
	require.NoError(t, err)
	assert.Equal(t, "AMD Ryzen 9 5900X 12-Core Processor", profile.Model)
	assert.Equal(t, 16, profile.Cores)
	assert.Equal(t, 32, profile.Threads)
	assert.True(t, profile.AVX2)
	assert.False(t, profile.AVX512)
}

// TestSelectPresetProfile is table-driven and validates the VRAM thresholds
// that map to preset profile names.
func TestSelectPresetProfile(t *testing.T) {
	const MiB = int64(1024 * 1024)
	tests := []struct {
		name      string
		vramBytes int64
		want      string
	}{
		{"zero vram → cpu_only", 0, "cpu_only"},
		{"1 GiB → cpu_only", 1024 * MiB, "cpu_only"},
		{"4095 MiB boundary → cpu_only", 4095 * MiB, "cpu_only"},
		{"4096 MiB → consumer_6gb", 4096 * MiB, "consumer_6gb"},
		{"5120 MiB → consumer_6gb", 5120 * MiB, "consumer_6gb"},
		{"6143 MiB boundary → consumer_6gb", 6143 * MiB, "consumer_6gb"},
		{"6144 MiB → consumer_8gb", 6144 * MiB, "consumer_8gb"},
		{"7168 MiB → consumer_8gb", 7168 * MiB, "consumer_8gb"},
		{"8191 MiB boundary → consumer_8gb", 8191 * MiB, "consumer_8gb"},
		{"8192 MiB → high_end", 8192 * MiB, "high_end"},
		{"12288 MiB → high_end", 12288 * MiB, "high_end"},
		{"24 GiB → high_end", 24 * 1024 * MiB, "high_end"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectPresetProfile(tt.vramBytes)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestInferenceThreads validates the thread count heuristic.
func TestInferenceThreads(t *testing.T) {
	tests := []struct {
		cores int
		want  int
	}{
		{cores: 16, want: 14},
		{cores: 2, want: 2},
		{cores: 1, want: 2},
		{cores: 4, want: 2},
		{cores: 8, want: 6},
	}
	for _, tt := range tests {
		got := inferenceThreads(tt.cores)
		assert.Equal(t, tt.want, got, "cores=%d", tt.cores)
	}
}

// TestHardwareProfile_InferenceAndBatchThreads validates the HardwareProfile
// convenience methods.
func TestHardwareProfile_InferenceAndBatchThreads(t *testing.T) {
	p := &HardwareProfile{
		CPU: CPUProfile{Cores: 16, Threads: 32},
	}
	assert.Equal(t, 14, p.InferenceThreads())
	assert.Equal(t, 16, p.BatchThreads())
}

// TestDetect_ReturnsProfile verifies that Detect() returns a non-nil profile
// and a valid preset string without crashing (regardless of actual hardware).
func TestDetect_ReturnsProfile(t *testing.T) {
	profile, err := Detect()
	require.NoError(t, err)
	require.NotNil(t, profile)
	validPresets := map[string]bool{
		"cpu_only":     true,
		"consumer_6gb": true,
		"consumer_8gb": true,
		"high_end":     true,
	}
	assert.True(t, validPresets[profile.PresetProfile],
		"unexpected preset: %q", profile.PresetProfile)
	assert.Greater(t, profile.CPU.Cores, 0)
}
