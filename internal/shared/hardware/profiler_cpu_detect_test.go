package hardware

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Regression guards (§11.4.115 / §11.4.135) for two defects in the CPU hardware
// profiler that ran live in the deployed gateway, plus the §11.4.146 STEP-3
// fan-out across the full case space of both functions.
//
// Polarity switch (§11.4.115) — ONE source, TWO roles:
//
//	RED_MODE unset / RED_MODE=0 (DEFAULT, post-fix) — the standing regression
//	                 guard: assert the defect is ABSENT.
//	RED_MODE=1     — reproduction mode: assert the defect IS PRESENT. On fixed
//	                 code it FAILS, which is what proves the guard is not blind.
//
// The default is the guard because the defects are fixed; RED_MODE=1 is kept
// so the reproduction stays replayable.
//
// What each polarity is replayable AGAINST (§11.4.6 — stated precisely rather
// than loosely):
//
//   - The two LIVE-host tests (TestDetectL3Cache_SumsAllInstances,
//     TestEstimateMemoryBandwidth_ReadsMemTotal) call only pre-existing
//     symbols, so they compile and reproduce against the genuine pre-fix
//     ARTIFACT. That artifact-level RED capture is commit f476267, where
//     redMode defaulted to RED.
//   - The four SEAM-level tests (TestSumL3FromSysfs_*,
//     TestEstimateMemoryBandwidthFrom_*) reference sumL3FromSysfs,
//     l3IdentityAttr and estimateMemoryBandwidthFrom, which do not exist at
//     f476267. They therefore reproduce against the pre-fix LOGIC transplanted
//     into these seams, not against the pre-fix artifact — the file does not
//     compile against f476267 at all.
//
// Neither field drives a decision today — L3CacheKB is logged once at boot
// (cmd/helixllm/main.go), MemoryBandwidthMBps has no consumer anywhere in the
// tree — so these are observability-correctness defects, not functional ones.
// They matter because they are the intended inputs to future CPU tuning.

// redMode reports whether the reproduction polarity is selected.
func redMode(t *testing.T) bool {
	t.Helper()
	return os.Getenv("RED_MODE") == "1"
}

// ---------------------------------------------------------------------------
// Synthetic sysfs fixtures — host-independent
// ---------------------------------------------------------------------------

// fakeL3 describes one logical CPU's index3 node in a synthetic sysfs tree.
// An empty string means the attribute file is absent entirely.
type fakeL3 struct {
	id            string // index3/id
	sharedCPUList string // index3/shared_cpu_list
	size          string // index3/size ("" => no index3 directory at all)
	unreadable    bool   // chmod 000 the size file
}

// writeFakeCPURoot builds a synthetic <root>/cpu<N>/cache/index3/ tree and
// returns the root. extraDirs are created verbatim under the root (used to
// prove non-CPU siblings such as cpufreq/cpuidle are ignored).
func writeFakeCPURoot(t *testing.T, cpus []fakeL3, extraDirs ...string) string {
	t.Helper()
	root := t.TempDir()
	for i, c := range cpus {
		cpuDir := filepath.Join(root, fmt.Sprintf("cpu%d", i))
		if c.size == "" {
			// CPU present but exposes no index3 node.
			if err := os.MkdirAll(cpuDir, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", cpuDir, err)
			}
			continue
		}
		idxDir := filepath.Join(cpuDir, "cache", "index3")
		if err := os.MkdirAll(idxDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", idxDir, err)
		}
		writeAttr := func(name, val string) {
			if val == "" {
				return
			}
			p := filepath.Join(idxDir, name)
			if err := os.WriteFile(p, []byte(val+"\n"), 0o644); err != nil {
				t.Fatalf("write %s: %v", p, err)
			}
		}
		writeAttr("size", c.size)
		writeAttr("id", c.id)
		writeAttr("shared_cpu_list", c.sharedCPUList)

		if c.unreadable {
			sizePath := filepath.Join(idxDir, "size")
			if err := os.Chmod(sizePath, 0o000); err != nil {
				t.Fatalf("chmod %s: %v", sizePath, err)
			}
			// Restore so TempDir cleanup and any re-read cannot be blocked.
			t.Cleanup(func() { _ = os.Chmod(sizePath, 0o644) })
		}
	}
	for _, d := range extraDirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("mkdir extra %s: %v", d, err)
		}
	}
	return root
}

// writeTempFile writes content to a uniquely named file and returns its path.
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// meminfoWithTotal renders a realistic /proc/meminfo carrying the given
// MemTotal in kB, with sibling keys so prefix matching is exercised.
func meminfoWithTotal(kb int64) string {
	return fmt.Sprintf(
		"MemTotal:       %d kB\nMemFree:         1234567 kB\nMemAvailable:    2345678 kB\nBuffers:            9999 kB\n",
		kb,
	)
}

// ---------------------------------------------------------------------------
// DEFECT 1 — detectL3Cache under-reported L3 by de-duplicating on SIZE STRING
// ---------------------------------------------------------------------------

// TestSumL3FromSysfs_DistinctInstances is the polarity-switched guard for the
// core defect: N equally-sized L3 instances collapsed to ONE because the dedup
// key was the size string ("32768K") rather than the cache-instance identity.
//
// Host-independent: the topology and the expected total are both derived from
// the synthetic fixture, never from this machine's numbers.
func TestSumL3FromSysfs_DistinctInstances(t *testing.T) {
	const (
		instances    = 4
		cpusPerCache = 16
		sizeStr      = "32768K"
		sizeKB       = 32768
	)
	var cpus []fakeL3
	for inst := 0; inst < instances; inst++ {
		for c := 0; c < cpusPerCache; c++ {
			cpus = append(cpus, fakeL3{
				id:            fmt.Sprintf("%d", inst),
				sharedCPUList: fmt.Sprintf("%d-%d", inst*cpusPerCache, (inst+1)*cpusPerCache-1),
				size:          sizeStr,
			})
		}
	}
	root := writeFakeCPURoot(t, cpus)

	want := instances * sizeKB
	got := sumL3FromSysfs(root)

	if redMode(t) {
		// Pre-fix signature: every instance shares one size string, so exactly
		// one is counted and the other instances-1 are discarded as "seen".
		if got != sizeKB {
			t.Fatalf("RED baseline did not reproduce: sumL3FromSysfs()=%d KB, expected the "+
				"single-instance under-count %d KB (true total %d KB across %d instances). "+
				"The size-string dedup may already be fixed — rerun with RED_MODE=0.",
				got, sizeKB, want, instances)
		}
		t.Logf("RED reproduced: %d equally-sized L3 instances of %d KB collapsed to %d KB "+
			"(true total %d KB) because the dedup key was the size string",
			instances, sizeKB, got, want)
		return
	}

	if got != want {
		t.Fatalf("sumL3FromSysfs()=%d KB; want %d KB (%d distinct instances x %d KB)",
			got, want, instances, sizeKB)
	}
}

// TestSumL3FromSysfs_CaseSpace is the §11.4.146 STEP-3 fan-out: the enumerated
// case space of L3 summation, each case with its expected outcome. These are
// standing guards (no polarity switch) — they encode correct behaviour, so
// against the pre-fix LOGIC the instance-collision cases fail, which is the RED
// capture for the fan-out.
func TestSumL3FromSysfs_CaseSpace(t *testing.T) {
	tests := []struct {
		name      string
		cpus      []fakeL3
		extraDirs []string
		want      int
		why       string
	}{
		{
			name: "single_instance",
			cpus: []fakeL3{
				{id: "0", sharedCPUList: "0-7", size: "16384K"},
				{id: "0", sharedCPUList: "0-7", size: "16384K"},
			},
			want: 16384,
			why:  "one instance shared by two CPUs is counted once",
		},
		{
			name: "two_equal_instances",
			cpus: []fakeL3{
				{id: "0", sharedCPUList: "0-1", size: "16384K"},
				{id: "0", sharedCPUList: "0-1", size: "16384K"},
				{id: "1", sharedCPUList: "2-3", size: "16384K"},
				{id: "1", sharedCPUList: "2-3", size: "16384K"},
			},
			want: 32768,
			why:  "equal sizes must NOT collapse — this is the reported defect",
		},
		{
			name: "three_unequal_instances_with_one_duplicate_size",
			cpus: []fakeL3{
				{id: "0", sharedCPUList: "0-1", size: "16384K"},
				{id: "1", sharedCPUList: "2-3", size: "16384K"},
				{id: "2", sharedCPUList: "4-5", size: "8192K"},
			},
			want: 40960,
			why:  "partial collision: the two 16384K instances collapsed pre-fix",
		},
		{
			name: "megabyte_unit_form",
			cpus: []fakeL3{
				{id: "0", sharedCPUList: "0-1", size: "32M"},
				{id: "1", sharedCPUList: "2-3", size: "32M"},
			},
			want: 65536,
			why:  "parseCacheSize converts M to KB; instances still distinct",
		},
		{
			name: "no_index3_anywhere",
			cpus: []fakeL3{{}, {}, {}},
			want: 0,
			why:  "CPUs without an L3 node contribute nothing",
		},
		{
			name: "some_cpus_lack_index3",
			cpus: []fakeL3{
				{id: "0", sharedCPUList: "0-1", size: "4096K"},
				{},
				{id: "1", sharedCPUList: "2-3", size: "4096K"},
			},
			want: 8192,
			why:  "heterogeneous topology: only CPUs exposing L3 are summed",
		},
		{
			name: "malformed_size_string",
			cpus: []fakeL3{
				{id: "0", sharedCPUList: "0-1", size: "not-a-number"},
				{id: "1", sharedCPUList: "2-3", size: "8192K"},
			},
			want: 8192,
			why:  "unparseable size contributes 0 and never poisons the total",
		},
		{
			name: "empty_size_file",
			cpus: []fakeL3{
				{id: "0", sharedCPUList: "0-1", size: "\n"},
				{id: "1", sharedCPUList: "2-3", size: "8192K"},
			},
			want: 8192,
			why:  "blank size contributes 0",
		},
		{
			name: "id_absent_dedupes_by_shared_cpu_list",
			cpus: []fakeL3{
				{sharedCPUList: "0-7,32-39", size: "32768K"},
				{sharedCPUList: "0-7,32-39", size: "32768K"},
				{sharedCPUList: "8-15,40-47", size: "32768K"},
			},
			want: 65536,
			why:  "older kernels lack index3/id; shared_cpu_list identifies the instance",
		},
		{
			name: "id_and_shared_cpu_list_both_absent",
			cpus: []fakeL3{
				{size: "32768K"},
				{size: "32768K"},
			},
			want: 0,
			why:  "unidentifiable instances are skipped, never multiplied per-CPU",
		},
		{
			// Both entries describe ONE instance (identical shared_cpu_list),
			// but id is exposed on only one of its CPUs. Because the identity
			// attribute is chosen once for the whole walk, id wins and the
			// id-less CPU is skipped => counted once.
			//
			// Choosing the attribute PER DIRECTORY instead would key the first
			// CPU by id ("0") and the second by shared_cpu_list ("0-1"), two
			// different keys for the same instance => counted twice (16384).
			// This fixture is what makes that regression detectable; note the
			// shared_cpu_list value must NOT equal the id value, or the two
			// keys collide by accident and the case stops discriminating.
			name: "mixed_attribute_exposure_never_double_counts",
			cpus: []fakeL3{
				{id: "0", sharedCPUList: "0-1", size: "8192K"},
				{sharedCPUList: "0-1", size: "8192K"},
			},
			want: 8192,
			why:  "one instance with id exposed on only some of its CPUs is counted once, never twice",
		},
		{
			name: "blank_id_everywhere_falls_back_to_shared_cpu_list",
			cpus: []fakeL3{
				{id: "   ", sharedCPUList: "0-1", size: "4096K"},
				{id: "   ", sharedCPUList: "0-1", size: "4096K"},
				{id: "   ", sharedCPUList: "2-3", size: "4096K"},
			},
			want: 8192,
			why:  "a present-but-blank id is not an identity; the walk falls back to shared_cpu_list",
		},
		{
			name: "blank_id_and_no_cpulist_skips_entry",
			cpus: []fakeL3{
				{id: "   ", size: "4096K"},
			},
			want: 0,
			why:  "blank id with no shared_cpu_list leaves the instance unidentifiable",
		},
		{
			name: "non_cpu_siblings_ignored",
			cpus: []fakeL3{
				{id: "0", sharedCPUList: "0-1", size: "8192K"},
			},
			extraDirs: []string{"cpufreq", "cpuidle", "power", "hotplug"},
			want:      8192,
			why:       "cpufreq/cpuidle start with 'cpu' but expose no index3",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := writeFakeCPURoot(t, tc.cpus, tc.extraDirs...)
			got := sumL3FromSysfs(root)
			if got != tc.want {
				t.Fatalf("sumL3FromSysfs()=%d KB; want %d KB (%s)", got, tc.want, tc.why)
			}
		})
	}

	t.Run("unreadable_size_node", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("SKIP-OK: running as root; chmod 000 does not deny access, so the unreadable path is not exercisable")
		}
		root := writeFakeCPURoot(t, []fakeL3{
			{id: "0", sharedCPUList: "0-1", size: "8192K", unreadable: true},
			{id: "1", sharedCPUList: "2-3", size: "8192K"},
		})
		got := sumL3FromSysfs(root)
		if got != 8192 {
			t.Fatalf("sumL3FromSysfs()=%d KB; want 8192 KB (unreadable node skipped, readable one counted)", got)
		}
	})

	t.Run("nonexistent_root", func(t *testing.T) {
		got := sumL3FromSysfs(filepath.Join(t.TempDir(), "definitely-absent"))
		if got != 0 {
			t.Fatalf("sumL3FromSysfs()=%d KB; want 0 KB for an unreadable cpu root", got)
		}
	})
}

// sysfsL3Truth returns this host's true total L3 in KB, derived by summing one
// size per DISTINCT cache instance id read live from sysfs, plus the instance
// count. The expectation is computed from the machine at runtime — no number
// from any particular CPU is baked into the assertion.
//
// It keys on index3/id ONLY, deliberately: this is the differential ORACLE for
// the production walk, so it must not re-implement production's attribute
// selection (an oracle that mirrors the code under test cannot falsify it).
// The cost is that on an id-less kernel this live test skips even though
// sumL3FromSysfs would work there via shared_cpu_list; that path is covered
// host-independently by the id_absent_dedupes_by_shared_cpu_list and
// blank_id_everywhere_falls_back_to_shared_cpu_list fixture cases instead.
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

// TestDetectL3Cache_SumsAllInstances is the live-host counterpart of
// TestSumL3FromSysfs_DistinctInstances: it exercises the real exported entry
// point against this machine's real topology, asserting the RELATION between
// the reported value and the runtime-derived truth.
func TestDetectL3Cache_SumsAllInstances(t *testing.T) {
	// Skip only when the DMI path would actually WIN inside detectL3Cache —
	// i.e. dmidecode both succeeds AND yields a positive total. A privileged
	// host whose dmidecode parses to 0 still falls through to sysfs, so it
	// keeps live coverage rather than skipping it away.
	if out, err := exec.Command("dmidecode", "-t", "cache").Output(); err == nil && parseL3FromDMI(string(out)) > 0 {
		t.Skip("SKIP-OK: dmidecode yields a positive L3 total; detectL3Cache returns via its DMI path, not the sysfs path under test")
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

// ---------------------------------------------------------------------------
// DEFECT 2 — estimateMemoryBandwidth scanned MemTotal in the WRONG FILE
// ---------------------------------------------------------------------------

// TestEstimateMemoryBandwidthFrom_ReadsMemInfo is the polarity-switched guard
// for the core defect: MemTotal was scanned in /proc/cpuinfo, where the key
// never appears, so memTotal was always 0 and BOTH capacity branches were
// structurally unreachable.
//
// Host-independent: capacity and expectation both come from the fixture.
func TestEstimateMemoryBandwidthFrom_ReadsMemInfo(t *testing.T) {
	const memTotalKB = int64(256) * 1024 * 1024 // 256 GiB -> the >=128 GB branch
	cpuinfo := writeTempFile(t, "cpuinfo", "processor\t: 0\nmodel name\t: Synthetic CPU\nflags\t\t: avx2 fma\n")
	meminfo := writeTempFile(t, "meminfo", meminfoWithTotal(memTotalKB))

	got := estimateMemoryBandwidthFrom(cpuinfo, meminfo)

	if redMode(t) {
		if got != 25000.0 {
			t.Fatalf("RED baseline did not reproduce: estimateMemoryBandwidthFrom()=%.0f, expected the "+
				"25000 floor. The wrong-file read may already be fixed — rerun with RED_MODE=0.", got)
		}
		t.Logf("RED reproduced: fixture declares %d KB RAM (want 76800 MB/s) but got %.0f "+
			"because MemTotal: was scanned in the cpuinfo file, where it never appears", memTotalKB, got)
		return
	}

	if got != 76800.0 {
		t.Fatalf("estimateMemoryBandwidthFrom()=%.0f; want 76800 for a %d KB host", got, memTotalKB)
	}
}

// TestEstimateMemoryBandwidthFrom_CaseSpace is the §11.4.146 STEP-3 fan-out:
// the enumerated case space of bandwidth estimation, including both capacity
// thresholds at and around their boundaries, and every degraded-input path.
func TestEstimateMemoryBandwidthFrom_CaseSpace(t *testing.T) {
	const (
		gib     = int64(1024 * 1024) // 1 GiB expressed in kB
		plainCP = "processor\t: 0\nmodel name\t: Synthetic CPU\nflags\t\t: avx2\n"
	)

	tests := []struct {
		name    string
		cpuinfo string // "" => file absent
		meminfo string // "" => file absent
		want    float64
		why     string
	}{
		{
			name:    "capacity_far_above_128gib",
			cpuinfo: plainCP,
			meminfo: meminfoWithTotal(251 * gib),
			want:    76800.0,
			why:     "multi-channel branch",
		},
		{
			name:    "capacity_exactly_128gib",
			cpuinfo: plainCP,
			meminfo: meminfoWithTotal(128 * gib),
			want:    76800.0,
			why:     "boundary is inclusive (>=)",
		},
		{
			name:    "capacity_one_kb_below_128gib",
			cpuinfo: plainCP,
			meminfo: meminfoWithTotal(128*gib - 1),
			want:    51200.0,
			why:     "off-by-one below the 128 GiB boundary falls to dual-channel",
		},
		{
			name:    "capacity_exactly_64gib",
			cpuinfo: plainCP,
			meminfo: meminfoWithTotal(64 * gib),
			want:    51200.0,
			why:     "boundary is inclusive (>=)",
		},
		{
			name:    "capacity_one_kb_below_64gib",
			cpuinfo: plainCP,
			meminfo: meminfoWithTotal(64*gib - 1),
			want:    25000.0,
			why:     "off-by-one below the 64 GiB boundary falls to the floor",
		},
		{
			name:    "capacity_small_host",
			cpuinfo: plainCP,
			meminfo: meminfoWithTotal(8 * gib),
			want:    25000.0,
			why:     "single-channel floor",
		},
		{
			name:    "capacity_zero",
			cpuinfo: plainCP,
			meminfo: meminfoWithTotal(0),
			want:    25000.0,
			why:     "zero capacity falls to the floor",
		},
		{
			name:    "cpuinfo_advertises_ddr5_short_circuits",
			cpuinfo: plainCP + "memory\t: ddr5-6400\n",
			meminfo: meminfoWithTotal(251 * gib),
			want:    51200.0,
			why:     "the DDR5 branch returns before capacity is consulted",
		},
		{
			name:    "cpuinfo_advertises_DDR5_uppercase",
			cpuinfo: plainCP + "memory\t: DDR5-6400\n",
			meminfo: meminfoWithTotal(251 * gib),
			want:    51200.0,
			why:     "uppercase spelling matches too",
		},
		{
			name:    "meminfo_absent",
			cpuinfo: plainCP,
			meminfo: "",
			want:    25000.0,
			why:     "unreadable meminfo degrades to the floor, never panics",
		},
		{
			name:    "meminfo_without_memtotal_key",
			cpuinfo: plainCP,
			meminfo: "MemFree:         1234567 kB\nBuffers:            9999 kB\n",
			want:    25000.0,
			why:     "absent MemTotal degrades to the floor",
		},
		{
			name:    "meminfo_malformed_memtotal_value",
			cpuinfo: plainCP,
			meminfo: "MemTotal:       not-a-number kB\n",
			want:    25000.0,
			why:     "unparseable MemTotal degrades to the floor",
		},
		{
			name:    "meminfo_memtotal_missing_unit_suffix",
			cpuinfo: plainCP,
			meminfo: fmt.Sprintf("MemTotal:       %d\n", 251*gib),
			want:    76800.0,
			why:     "parseMemInfoKB tolerates a missing ' kB' suffix",
		},
		{
			name:    "meminfo_memtotal_indented",
			cpuinfo: plainCP,
			meminfo: fmt.Sprintf("   MemTotal:       %d kB\n", 251*gib),
			want:    76800.0,
			why:     "leading whitespace is trimmed, matching detectRAM's parsing of the same file",
		},
		{
			name:    "meminfo_memtotal_not_first_line",
			cpuinfo: plainCP,
			meminfo: "MemFree:  1 kB\nBuffers:  2 kB\n" + meminfoWithTotal(251*gib),
			want:    76800.0,
			why:     "MemTotal is found wherever it appears",
		},
		{
			name:    "meminfo_duplicate_memtotal_first_wins",
			cpuinfo: plainCP,
			meminfo: meminfoWithTotal(251*gib) + "MemTotal:       8388608 kB\n",
			want:    76800.0,
			why:     "the scan breaks on the first MemTotal; a later duplicate cannot override it",
		},
		{
			name:    "memtotal_prefix_lookalike_key_ignored",
			cpuinfo: plainCP,
			meminfo: "MemTotalFoo:    999999999 kB\nMemTotal:       8388608 kB\n",
			want:    25000.0,
			why:     "'MemTotalFoo:' must not satisfy the 'MemTotal:' prefix match",
		},
		{
			name:    "cpuinfo_absent",
			cpuinfo: "",
			meminfo: meminfoWithTotal(251 * gib),
			want:    25000.0,
			why:     "PRESERVED pre-existing behaviour: unreadable cpuinfo short-circuits to the floor",
		},
		{
			name:    "both_inputs_absent",
			cpuinfo: "",
			meminfo: "",
			want:    25000.0,
			why:     "fully degraded input still yields the safe default",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cpuPath := filepath.Join(dir, "absent-cpuinfo")
			if tc.cpuinfo != "" {
				cpuPath = filepath.Join(dir, "cpuinfo")
				if err := os.WriteFile(cpuPath, []byte(tc.cpuinfo), 0o644); err != nil {
					t.Fatalf("write cpuinfo: %v", err)
				}
			}
			memPath := filepath.Join(dir, "absent-meminfo")
			if tc.meminfo != "" {
				memPath = filepath.Join(dir, "meminfo")
				if err := os.WriteFile(memPath, []byte(tc.meminfo), 0o644); err != nil {
					t.Fatalf("write meminfo: %v", err)
				}
			}

			got := estimateMemoryBandwidthFrom(cpuPath, memPath)
			if got != tc.want {
				t.Fatalf("estimateMemoryBandwidthFrom()=%.0f; want %.0f (%s)", got, tc.want, tc.why)
			}
		})
	}
}

// TestEstimateMemoryBandwidth_ReadsMemTotal is the live-host counterpart: it
// exercises the real entry point against this machine's real /proc, asserting
// the RELATION between the reported bandwidth and the runtime-read capacity.
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
		if strings.HasPrefix(strings.TrimSpace(line), "MemTotal:") {
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
