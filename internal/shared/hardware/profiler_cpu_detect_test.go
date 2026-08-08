package hardware

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// RED-baseline tests (§11.4.115) for two defects in the CPU hardware
// profiler shipped by 08de5c5 and running live in the deployed gateway.
//
// Both use the RED_MODE polarity switch:
//
//	RED_MODE=1 (default) — assert the defect IS present on the current
//	                       artifact. This is the reproduce-first baseline.
//	RED_MODE=0           — assert the defect is ABSENT. Flip to this after
//	                       a fix; the same source then becomes the standing
//	                       regression guard.
//
// Neither field currently drives a decision (L3CacheKB is logged only;
// MemoryBandwidthMBps has no consumer anywhere in the tree), so these are
// observability-correctness defects, not functional ones.

func redMode(t *testing.T) bool {
	t.Helper()
	return os.Getenv("RED_MODE") != "0"
}

// sysfsL3Truth returns the true total L3 size in KB, derived by summing one
// size per DISTINCT cache instance id, and the number of distinct instances.
func sysfsL3Truth(t *testing.T) (totalKB int64, instances int) {
	t.Helper()
	matches, err := filepath.Glob("/sys/devices/system/cpu/cpu*/cache/index3/id")
	if err != nil || len(matches) == 0 {
		t.Skip("SKIP-OK: no /sys/.../cache/index3/id on this host; L3 topology not introspectable")
	}
	seen := map[string]bool{}
	for _, idPath := range matches {
		idRaw, rErr := os.ReadFile(idPath)
		if rErr != nil {
			continue
		}
		id := strings.TrimSpace(string(idRaw))
		if seen[id] {
			continue
		}
		sizeRaw, sErr := os.ReadFile(filepath.Join(filepath.Dir(idPath), "size"))
		if sErr != nil {
			continue
		}
		seen[id] = true
		totalKB += parseCacheSize(string(sizeRaw))
	}
	return totalKB, len(seen)
}

// TestDetectL3Cache_SumsAllInstances.
//
// detectL3Cache dedupes candidate caches with `seen[key]` where key is the
// cache SIZE STRING ("32768K"), not the cache instance id. On any CPU whose
// L3 instances are equally sized — e.g. the deployment host, an AMD Ryzen
// Threadripper 7970X with 4x32 MiB L3 — the first instance is counted and
// every subsequent one is skipped as already "seen". The function returns
// one instance's size while its own doc comment promises the sum across all
// nodes. Measured live: reports 32768 KB, true total 131072 KB.
//
// The correct dedup key is index3/id (or shared_cpu_list).
func TestDetectL3Cache_SumsAllInstances(t *testing.T) {
	if err := exec.Command("dmidecode", "-t", "cache").Run(); err == nil {
		t.Skip("SKIP-OK: dmidecode succeeded (running privileged); detectL3Cache takes its DMI path, not the sysfs path under test")
	}

	truthKB, instances := sysfsL3Truth(t)
	if instances < 2 {
		t.Skipf("SKIP-OK: host has %d L3 instance(s); the equal-size dedup collision needs >=2", instances)
	}
	got := int64(detectL3Cache())

	if redMode(t) {
		if got >= truthKB {
			t.Fatalf("RED baseline did not reproduce: detectL3Cache()=%d KB >= true total %d KB "+
				"across %d instances. The under-count may already be fixed — rerun with RED_MODE=0.",
				got, truthKB, instances)
		}
		t.Logf("RED reproduced: detectL3Cache()=%d KB but true L3 total is %d KB across %d equally-sized instances",
			got, truthKB, instances)
		return
	}

	if got != truthKB {
		t.Fatalf("detectL3Cache()=%d KB; want %d KB (sum of %d distinct L3 instances)",
			got, truthKB, instances)
	}
}

// TestEstimateMemoryBandwidth_ReadsMemTotal.
//
// estimateMemoryBandwidth reads /proc/cpuinfo and then scans those same
// bytes for a line prefixed "MemTotal:". MemTotal lives in /proc/meminfo,
// never in /proc/cpuinfo, so memTotal is always 0 and both capacity-derived
// branches (>=128 GB -> 76800, >=64 GB -> 51200) are unreachable. Every
// host without a literal "ddr5"/"DDR5" string in /proc/cpuinfo therefore
// receives the 25000.0 single-channel DDR4 floor. Measured live: a 251 GB
// multi-channel host reports 25000.0.
func TestEstimateMemoryBandwidth_ReadsMemTotal(t *testing.T) {
	cpuinfo, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		t.Skip("SKIP-OK: /proc/cpuinfo unreadable on this host")
	}
	if strings.Contains(strings.ToLower(string(cpuinfo)), "ddr5") {
		t.Skip("SKIP-OK: /proc/cpuinfo advertises DDR5; the early-return branch fires before the MemTotal path under test")
	}

	meminfo, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		t.Skip("SKIP-OK: /proc/meminfo unreadable on this host")
	}
	var memTotalKB int64
	for _, line := range strings.Split(string(meminfo), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			memTotalKB = parseMemInfoKB(line)
			break
		}
	}
	if memTotalKB < 64*1024*1024 {
		t.Skipf("SKIP-OK: host has %d KB RAM; need >=64 GB for a capacity branch to be distinguishable", memTotalKB)
	}

	want := 51200.0
	if memTotalKB >= 128*1024*1024 {
		want = 76800.0
	}
	got := estimateMemoryBandwidth()

	if redMode(t) {
		if got != 25000.0 {
			t.Fatalf("RED baseline did not reproduce: estimateMemoryBandwidth()=%.0f, expected the "+
				"25000.0 floor. The /proc/cpuinfo-vs-/proc/meminfo bug may already be fixed — rerun with RED_MODE=0.",
				got)
		}
		if strings.Contains(string(cpuinfo), "MemTotal:") {
			t.Fatal("premise broken: /proc/cpuinfo unexpectedly contains MemTotal:")
		}
		t.Logf("RED reproduced: host has %d KB RAM (want %.0f MB/s) but estimateMemoryBandwidth()=%.0f "+
			"because MemTotal: is scanned in /proc/cpuinfo, where it never appears", memTotalKB, want, got)
		return
	}

	if got != want {
		t.Fatalf("estimateMemoryBandwidth()=%.0f; want %.0f for a host with %d KB RAM", got, want, memTotalKB)
	}
}
