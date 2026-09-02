package capability

import (
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// T006 RED. Measurement must report the CPU and system memory of the machine
// the test is running on. The assertions below are cross-checked against an
// independent reading — the Go runtime for core counts, and a direct read of
// the kernel's own memory report — so a fabricated constant cannot pass.

func TestMeasureCPU_ReportsThisMachine(t *testing.T) {
	cpu, err := MeasureCPU()
	if err != nil {
		t.Fatalf("MeasureCPU() on the live host: %v", err)
	}

	if cpu.Architecture != runtime.GOARCH {
		t.Errorf("Architecture = %q, want the running architecture %q", cpu.Architecture, runtime.GOARCH)
	}
	if cpu.LogicalCores <= 0 {
		t.Errorf("LogicalCores = %d, want a positive count", cpu.LogicalCores)
	}
	if cpu.PhysicalCores <= 0 {
		t.Errorf("PhysicalCores = %d, want a positive count", cpu.PhysicalCores)
	}
	if cpu.PhysicalCores > cpu.LogicalCores {
		t.Errorf("PhysicalCores = %d exceeds LogicalCores = %d", cpu.PhysicalCores, cpu.LogicalCores)
	}
	// runtime.NumCPU is an independent view of the same quantity. It can be
	// narrowed by affinity or a cgroup quota, so it is a ceiling-free lower
	// bound check rather than an equality: a measurement reporting fewer
	// logical cores than the runtime can already see is wrong either way.
	if cpu.LogicalCores < runtime.NumCPU() {
		t.Errorf("LogicalCores = %d is below runtime.NumCPU() = %d", cpu.LogicalCores, runtime.NumCPU())
	}
}

func TestMeasureCPU_FeaturesMatchTheKernelReport(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("SKIP-OK: the kernel flag report used as the independent oracle here is Linux-specific; this host is %s", runtime.GOOS)
	}
	raw, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		t.Skipf("SKIP-OK: no independent CPU oracle available on this host: %v", err)
	}
	flags := kernelFlagSetForTest(string(raw))
	if len(flags) == 0 {
		t.Skip("SKIP-OK: kernel reported no CPU flags to cross-check against")
	}

	cpu, err := MeasureCPU()
	if err != nil {
		t.Fatalf("MeasureCPU(): %v", err)
	}

	// Every feature claimed must be one the kernel actually reports, under
	// either the x86 or the arm64 spelling. A claimed feature the host does
	// not have is the fabrication this asserts against.
	for _, f := range cpu.Features {
		if !featureBackedByKernelForTest(f, flags) {
			t.Errorf("measurement claims feature %q that the kernel does not report", f)
		}
	}
	// And the converse for one feature we can name concretely: if the kernel
	// says avx2, measurement must not have dropped it.
	if flags["avx2"] && !cpu.HasFeature(FeatureAVX2) {
		t.Error("kernel reports avx2 but measurement omitted FeatureAVX2")
	}
}

func TestMeasureMemory_MatchesTheKernelReport(t *testing.T) {
	mem, err := MeasureMemory()
	if err != nil {
		t.Fatalf("MeasureMemory() on the live host: %v", err)
	}
	if mem.Total == 0 {
		t.Fatal("Total = 0, want the machine's real memory size")
	}
	if mem.Available == 0 {
		t.Error("Available = 0, want the memory actually free right now")
	}
	if mem.Available > mem.Total {
		t.Errorf("Available = %d exceeds Total = %d", mem.Available, mem.Total)
	}

	if runtime.GOOS != "linux" {
		t.Skipf("SKIP-OK: cross-check oracle is Linux-specific; remaining assertions skipped on %s", runtime.GOOS)
	}
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		t.Skipf("SKIP-OK: no independent memory oracle on this host: %v", err)
	}
	wantTotal := kernelMemFieldForTest(string(raw), "MemTotal:")
	if wantTotal == 0 {
		t.Skip("SKIP-OK: kernel reported no MemTotal to cross-check against")
	}
	if mem.Total != wantTotal {
		t.Errorf("Total = %d, kernel reports %d", mem.Total, wantTotal)
	}
}

func TestMeasureMemory_AvailableIsNotTotal(t *testing.T) {
	// A measurement that reports free memory by simply echoing total memory
	// would satisfy every bound above while being useless to selection, which
	// spends the Available figure. On any machine actually running this test
	// some memory is in use, so the two must differ.
	mem, err := MeasureMemory()
	if err != nil {
		t.Fatalf("MeasureMemory(): %v", err)
	}
	if mem.Available == mem.Total {
		t.Errorf("Available == Total == %d; free memory was not measured, only copied", mem.Total)
	}
}

func TestMeasureSystem_UnsupportedPlatformIsHonest(t *testing.T) {
	// Whatever this platform is, the two entry points either measure or say
	// they cannot. Neither may invent a figure. This is the assertion that
	// holds on a platform with no implementation at all.
	_, cpuErr := MeasureCPU()
	_, memErr := MeasureMemory()
	for name, err := range map[string]error{"MeasureCPU": cpuErr, "MeasureMemory": memErr} {
		if err == nil {
			continue
		}
		if !errors.Is(err, ErrPlatformUnsupported) && !errors.Is(err, ErrFigureUnavailable) {
			t.Errorf("%s failed with %v, which is neither ErrPlatformUnsupported nor ErrFigureUnavailable; "+
				"a failure must name why the figure is missing", name, err)
		}
	}
}

// --- independent oracles, deliberately written differently from the code under test ---

func kernelFlagSetForTest(cpuinfo string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(cpuinfo, "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "flags", "Features":
			for _, f := range strings.Fields(val) {
				out[f] = true
			}
		}
	}
	return out
}

func featureBackedByKernelForTest(f CPUFeature, flags map[string]bool) bool {
	// Accept any kernel spelling that legitimately backs the feature.
	for _, spelling := range map[CPUFeature][]string{
		FeatureAVX2:     {"avx2"},
		FeatureAVX512F:  {"avx512f"},
		FeatureAVX512BW: {"avx512bw"},
		FeatureF16C:     {"f16c"},
		FeatureFMA:      {"fma"},
		FeatureNEON:     {"neon", "asimd"},
		FeatureDotProd:  {"asimddp", "dotprod"},
		FeatureFP16:     {"asimdhp", "fphp"},
	}[f] {
		if flags[spelling] {
			return true
		}
	}
	return false
}

func kernelMemFieldForTest(meminfo, field string) Bytes {
	for _, line := range strings.Split(meminfo, "\n") {
		if !strings.HasPrefix(line, field) {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, field))
		if len(fields) == 0 {
			return 0
		}
		n, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return 0
		}
		return Bytes(n) * KiB // /proc/meminfo reports kibibytes
	}
	return 0
}
