//go:build linux

package capability

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Linux measurement primitives. Everything here reads the kernel's own report
// through procfs and sysfs — no shelling out for figures the kernel publishes
// directly, and no derived quantity presented as a measured one.

const (
	procCPUInfo    = "/proc/cpuinfo"
	procMemInfo    = "/proc/meminfo"
	sysCPUTopology = "/sys/devices/system/cpu"
)

// linuxFeatureSpellings maps the kernel's flag names onto the features that
// gate CPU-served inference. Both the x86 (`flags`) and arm64 (`Features`)
// spellings appear, because one kernel reports asimd where another reports neon
// for the same silicon capability.
var linuxFeatureSpellings = map[string]CPUFeature{
	"avx2":     FeatureAVX2,
	"avx512f":  FeatureAVX512F,
	"avx512bw": FeatureAVX512BW,
	"f16c":     FeatureF16C,
	"fma":      FeatureFMA,
	"neon":     FeatureNEON,
	"asimd":    FeatureNEON,
	"asimddp":  FeatureDotProd,
	"dotprod":  FeatureDotProd,
	"asimdhp":  FeatureFP16,
	"fphp":     FeatureFP16,
}

func platformCPU() (CPUProfile, error) {
	raw, err := os.ReadFile(procCPUInfo)
	if err != nil {
		return CPUProfile{}, fmt.Errorf("%w: reading %s: %v", ErrFigureUnavailable, procCPUInfo, err)
	}
	cpu := parseLinuxCPUInfo(string(raw))

	// Prefer sysfs topology: it is present on arm64 hosts whose /proc/cpuinfo
	// carries no physical-id/core-id pairs at all.
	if physical, ok := linuxPhysicalCoresFromSysfs(); ok {
		cpu.PhysicalCores = physical
	}
	if cpu.PhysicalCores <= 0 {
		// Say so rather than assuming one core per thread. An invented core
		// count is exactly the fabrication measurement exists to avoid; the
		// caller marks the measurement incomplete on this error.
		return cpu, fmt.Errorf("%w: physical core topology not reported by this kernel", ErrFigureUnavailable)
	}
	return cpu, nil
}

// parseLinuxCPUInfo extracts logical core count, physical core count and
// instruction-set features from the text of /proc/cpuinfo.
func parseLinuxCPUInfo(text string) CPUProfile {
	var cpu CPUProfile
	features := map[CPUFeature]struct{}{}
	cores := map[string]struct{}{}

	var pkg, core string
	flush := func() {
		if pkg != "" && core != "" {
			cores[pkg+"/"+core] = struct{}{}
		}
		pkg, core = "", ""
	}

	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			if strings.TrimSpace(line) == "" {
				flush() // blank line ends one processor block
			}
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		switch key {
		case "processor":
			cpu.LogicalCores++
		case "physical id":
			pkg = value
		case "core id":
			core = value
		case "flags", "Features":
			for _, f := range strings.Fields(value) {
				if feature, known := linuxFeatureSpellings[f]; known {
					features[feature] = struct{}{}
				}
			}
		}
	}
	flush()

	cpu.PhysicalCores = len(cores)
	cpu.Features = sortedFeatures(features)
	return cpu
}

// linuxPhysicalCoresFromSysfs counts distinct (package, core) pairs across
// every online CPU. It reports ok=false when the topology files are absent, so
// the caller can fall back rather than read a missing file as zero cores.
func linuxPhysicalCoresFromSysfs() (int, bool) {
	entries, err := filepath.Glob(filepath.Join(sysCPUTopology, "cpu[0-9]*", "topology"))
	if err != nil || len(entries) == 0 {
		return 0, false
	}
	pairs := map[string]struct{}{}
	for _, dir := range entries {
		pkg, errPkg := os.ReadFile(filepath.Join(dir, "physical_package_id"))
		core, errCore := os.ReadFile(filepath.Join(dir, "core_id"))
		if errPkg != nil || errCore != nil {
			continue
		}
		pairs[strings.TrimSpace(string(pkg))+"/"+strings.TrimSpace(string(core))] = struct{}{}
	}
	if len(pairs) == 0 {
		return 0, false
	}
	return len(pairs), true
}

func platformMemory() (MemoryReading, error) {
	raw, err := os.ReadFile(procMemInfo)
	if err != nil {
		return MemoryReading{}, fmt.Errorf("%w: reading %s: %v", ErrFigureUnavailable, procMemInfo, err)
	}
	fields := parseLinuxMemInfo(string(raw))

	total, haveTotal := fields["MemTotal"]
	if !haveTotal || total == 0 {
		return MemoryReading{}, fmt.Errorf("%w: %s reports no MemTotal", ErrFigureUnavailable, procMemInfo)
	}
	// MemAvailable is the kernel's own estimate of what a new workload can
	// claim without swapping. MemFree is a different, much smaller quantity
	// and substituting it would silently under-report; refuse instead.
	available, haveAvailable := fields["MemAvailable"]
	if !haveAvailable {
		return MemoryReading{Total: Bytes(total) * KiB},
			fmt.Errorf("%w: %s reports no MemAvailable", ErrFigureUnavailable, procMemInfo)
	}
	return MemoryReading{
		Total:     Bytes(total) * KiB,
		Available: Bytes(available) * KiB,
	}, nil
}

// parseLinuxMemInfo returns the kibibyte values of /proc/meminfo keyed by name.
func parseLinuxMemInfo(text string) map[string]uint64 {
	out := make(map[string]uint64, 8)
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		parts := strings.Fields(value)
		if len(parts) == 0 {
			continue
		}
		n, err := strconv.ParseUint(parts[0], 10, 64)
		if err != nil {
			continue
		}
		out[strings.TrimSpace(key)] = n
	}
	return out
}

// platformFreeStorage asks the kernel for the free-block count of the
// filesystem holding path. Bavail rather than Bfree: the difference is the
// reserve only root may spend, and a serving process is not root.
func platformFreeStorage(path string) (Bytes, error) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return 0, fmt.Errorf("%w: statfs %s: %v", ErrFigureUnavailable, path, err)
	}
	if fs.Bsize <= 0 {
		return 0, fmt.Errorf("%w: statfs %s reported block size %d", ErrFigureUnavailable, path, fs.Bsize)
	}
	return Bytes(fs.Bavail) * Bytes(fs.Bsize), nil
}

// --- accelerator sources ---

// linuxAcceleratorVendorIDs maps PCI vendor IDs to the acceleration stacks this
// package can actually reason about.
//
// Only vendors with a supported AccelerationAPI appear here, and that is a
// deliberate scope decision rather than an omission: an Intel integrated
// display adapter is not an inference accelerator this package offers, so
// counting it as "an accelerator vendor is present" would push every ordinary
// laptop into a permanent unknown state for hardware nothing would ever use.
var linuxAcceleratorVendorIDs = map[string]AcceleratorVendor{
	"0x10de": VendorNVIDIA, // NVIDIA
	"0x1002": VendorAMD,    // AMD/ATI
	"0x1022": VendorAMD,    // AMD
}

// linuxAcceleratorClassPrefixes are the PCI class codes of devices that can
// carry an acceleration stack: VGA controller, 3D controller, and the
// processing-accelerator class used by compute-only cards.
var linuxAcceleratorClassPrefixes = []string{"0x0300", "0x0302", "0x1200"}

func platformAcceleratorSources() (VendorPresence, []AcceleratorProbe, error) {
	return linuxPCIPresence{}, []AcceleratorProbe{nvidiaSMIProbe{}, rocmSMIProbe{}}, nil
}

// linuxPCIPresence answers "is there acceleration hardware attached?" from the
// PCI bus itself, which is true whether or not any vendor tooling is installed.
// That independence is what lets measurement distinguish an honestly
// accelerator-free host from one whose driver tools are simply missing.
type linuxPCIPresence struct{}

func (linuxPCIPresence) AcceleratorVendorsPresent(context.Context) (map[AcceleratorVendor]bool, error) {
	entries, err := filepath.Glob("/sys/bus/pci/devices/*/class")
	if err != nil {
		return nil, fmt.Errorf("%w: enumerating the PCI bus: %v", ErrFigureUnavailable, err)
	}
	present := map[AcceleratorVendor]bool{}
	for _, classPath := range entries {
		classRaw, err := os.ReadFile(classPath)
		if err != nil {
			continue
		}
		class := strings.TrimSpace(string(classRaw))
		isAccelClass := false
		for _, prefix := range linuxAcceleratorClassPrefixes {
			if strings.HasPrefix(class, prefix) {
				isAccelClass = true
				break
			}
		}
		if !isAccelClass {
			continue
		}
		vendorRaw, err := os.ReadFile(filepath.Join(filepath.Dir(classPath), "vendor"))
		if err != nil {
			continue
		}
		if vendor, known := linuxAcceleratorVendorIDs[strings.TrimSpace(string(vendorRaw))]; known {
			present[vendor] = true
		}
	}
	return present, nil
}

// nvidiaSMIProbe reads NVIDIA devices through the driver's own query tool.
//
// The identity is the driver-assigned GPU UUID, which survives reboots, slot
// changes and the addition of a second card — the property an enumeration index
// does not have (§11.4.111).
type nvidiaSMIProbe struct{}

func (nvidiaSMIProbe) Vendor() AcceleratorVendor { return VendorNVIDIA }

func (nvidiaSMIProbe) Available(context.Context) bool {
	_, err := exec.LookPath("nvidia-smi")
	return err == nil
}

func (p nvidiaSMIProbe) Probe(ctx context.Context) ([]Accelerator, error) {
	out, err := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=uuid,name,memory.total,memory.free",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return nil, fmt.Errorf("nvidia-smi query: %w", err)
	}
	return parseNVIDIASMI(string(out))
}

// parseNVIDIASMI reads the csv,noheader,nounits form: uuid, name, total MiB,
// free MiB — one line per device.
func parseNVIDIASMI(out string) ([]Accelerator, error) {
	var devices []Accelerator
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) != 4 {
			return nil, fmt.Errorf("nvidia-smi returned %d fields, want 4: %q", len(fields), line)
		}
		uuid := strings.TrimSpace(fields[0])
		if uuid == "" {
			// Without the UUID the device has no stable identity, so it could
			// only be bound positionally — which is the defect. Refuse.
			return nil, fmt.Errorf("nvidia-smi reported a device with no UUID: %q", line)
		}
		totalMiB, err := strconv.ParseUint(strings.TrimSpace(fields[2]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("nvidia-smi total memory %q: %w", fields[2], err)
		}
		freeMiB, err := strconv.ParseUint(strings.TrimSpace(fields[3]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("nvidia-smi free memory %q: %w", fields[3], err)
		}
		devices = append(devices, Accelerator{
			Identity:        DeviceIdentity(uuid),
			Model:           strings.TrimSpace(fields[1]),
			API:             APICUDA,
			MemoryTotal:     Bytes(totalMiB) * MiB,
			MemoryAvailable: Bytes(freeMiB) * MiB,
		})
	}
	return devices, nil
}

// rocmSMIProbe reads AMD devices through rocm-smi's JSON output, keyed by the
// device's Unique ID.
//
// UNVERIFIED: this path has not been exercised against real AMD hardware — no
// such device was available on the machine where it was written. It is written
// to fail closed: any output it cannot parse becomes a probe error, which makes
// the accelerator state unknown rather than silently reporting a card-free
// host. See parseROCmSMI.
type rocmSMIProbe struct{}

func (rocmSMIProbe) Vendor() AcceleratorVendor { return VendorAMD }

func (rocmSMIProbe) Available(context.Context) bool {
	_, err := exec.LookPath("rocm-smi")
	return err == nil
}

func (p rocmSMIProbe) Probe(ctx context.Context) ([]Accelerator, error) {
	out, err := exec.CommandContext(ctx, "rocm-smi",
		"--showuniqueid", "--showproductname", "--showmeminfo", "vram", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("rocm-smi query: %w", err)
	}
	return parseROCmSMI(out)
}

// parseROCmSMI reads rocm-smi's --json object, whose top-level keys are device
// handles ("card0", "card1", …) — handles that are themselves enumeration
// ordinals, which is exactly why the Unique ID is required as the identity and
// a device lacking one is refused rather than bound by its card number.
func parseROCmSMI(out []byte) ([]Accelerator, error) {
	var raw map[string]map[string]string
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("rocm-smi json: %w", err)
	}
	var devices []Accelerator
	for card, fields := range raw {
		uniqueID := firstNonEmptyValue(fields, "Unique ID", "Device ID")
		if uniqueID == "" {
			return nil, fmt.Errorf("rocm-smi reported %s with no Unique ID; it has no stable identity to bind", card)
		}
		totalStr := firstNonEmptyValue(fields, "VRAM Total Memory (B)")
		usedStr := firstNonEmptyValue(fields, "VRAM Total Used Memory (B)")
		total, err := strconv.ParseUint(totalStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("rocm-smi %s VRAM total %q: %w", card, totalStr, err)
		}
		used, err := strconv.ParseUint(usedStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("rocm-smi %s VRAM used %q: %w", card, usedStr, err)
		}
		if used > total {
			return nil, fmt.Errorf("rocm-smi %s reports used %d above total %d", card, used, total)
		}
		devices = append(devices, Accelerator{
			Identity:        DeviceIdentity(uniqueID),
			Model:           firstNonEmptyValue(fields, "Card Series", "Card Model", "Card SKU"),
			API:             APIROCm,
			MemoryTotal:     Bytes(total),
			MemoryAvailable: Bytes(total - used),
		})
	}
	return devices, nil
}

// firstNonEmptyValue returns the first of keys present with a non-empty value.
func firstNonEmptyValue(fields map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(fields[k]); v != "" {
			return v
		}
	}
	return ""
}
