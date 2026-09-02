//go:build darwin

package capability

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// macOS measurement primitives (§11.4.81 per-OS dispatch).
//
// NOT YET EXECUTED ON HARDWARE. This file was written and cross-compiled on a
// Linux host with no Mac available, so nothing here has been run. It is
// therefore written to fail closed everywhere: every figure comes from a real
// kernel query, and any query that fails or returns something unexpected
// produces an error — which the orchestrator turns into an incomplete profile
// that selection refuses to act on. There is no code path here that invents a
// number, so the worst outcome of an unverified assumption is an honest refusal
// rather than a wrong offer.
//
// The parsing this depends on is in measure_darwin_parse.go, outside the build
// tag, and is unit-tested from any host.

// darwinFeatureSysctls are the arm64 capability leaves worth asking for. Each
// answers "1" or "0"; a missing leaf means this kernel does not report it,
// which is not the same as the capability being absent — so it simply yields
// no feature rather than a negative claim.
var darwinFeatureSysctls = []string{
	"hw.optional.neon",
	"hw.optional.AdvSIMD",
	"hw.optional.arm.FEAT_DotProd",
	"hw.optional.arm.FEAT_FP16",
}

func platformCPU() (CPUProfile, error) {
	physical, errPhysical := darwinSysctlUint("hw.physicalcpu")
	logical, errLogical := darwinSysctlUint("hw.logicalcpu")

	leaves := make(map[string]string, len(darwinFeatureSysctls))
	for _, key := range darwinFeatureSysctls {
		if v, err := darwinSysctl(key); err == nil {
			leaves[key] = v
		}
	}
	// Intel Macs report capabilities as name lists across two sysctls; arm64
	// Macs have neither, and an error here is simply "this is not that kind of
	// Mac" rather than a measurement failure.
	names, _ := darwinSysctl("machdep.cpu.features")
	leaf7, _ := darwinSysctl("machdep.cpu.leaf7_features")

	cpu := CPUProfile{
		Architecture:  runtime.GOARCH,
		PhysicalCores: int(physical),
		LogicalCores:  int(logical),
		Features:      parseDarwinFeatures(leaves, names+" "+leaf7),
	}
	if errLogical != nil {
		return cpu, fmt.Errorf("%w: hw.logicalcpu: %v", ErrFigureUnavailable, errLogical)
	}
	if errPhysical != nil {
		// Say so rather than assuming one core per thread — an invented core
		// count is exactly what measurement exists to avoid.
		return cpu, fmt.Errorf("%w: hw.physicalcpu: %v", ErrFigureUnavailable, errPhysical)
	}
	return cpu, nil
}

func platformMemory() (MemoryReading, error) {
	total, err := darwinSysctlUint("hw.memsize")
	if err != nil {
		return MemoryReading{}, fmt.Errorf("%w: hw.memsize: %v", ErrFigureUnavailable, err)
	}
	reading := MemoryReading{Total: Bytes(total)}

	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return reading, fmt.Errorf("%w: vm_stat: %v", ErrFigureUnavailable, err)
	}
	available, err := parseVMStat(string(out))
	if err != nil {
		return reading, err
	}
	if available > reading.Total {
		// A composed figure that exceeds nameplate memory means one of the
		// assumptions behind the composition is wrong on this release. Refuse
		// rather than report it (§11.4.6).
		return reading, fmt.Errorf("%w: vm_stat reports %d available above %d total",
			ErrFigureUnavailable, available, reading.Total)
	}
	reading.Available = available
	return reading, nil
}

func platformFreeStorage(path string) (Bytes, error) {
	// Statfs_t differs from Linux's here — Bsize is unsigned on darwin — which
	// is why this cannot be shared and lives behind the build tag.
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return 0, fmt.Errorf("%w: statfs %s: %v", ErrFigureUnavailable, path, err)
	}
	if fs.Bsize == 0 {
		return 0, fmt.Errorf("%w: statfs %s reported a zero block size", ErrFigureUnavailable, path)
	}
	// Bavail rather than Bfree: the difference is the reserve only root may
	// spend, and a serving process is not root.
	return Bytes(fs.Bavail) * Bytes(fs.Bsize), nil
}

// --- accelerator sources ---

func platformAcceleratorSources() (VendorPresence, []AcceleratorProbe, error) {
	return darwinGPUPresence{}, []AcceleratorProbe{metalProbe{}}, nil
}

// darwinGPUPresence reports whether this Mac has an accelerator this package
// can actually serve on.
//
// Apple Silicon has an integrated Metal GPU on every model, so hw.optional.arm64
// is a definitive presence signal. An Intel Mac's discrete GPU is reported
// present too — but no supported acceleration API reaches it from here (CUDA
// and ROCm do not exist on macOS), so the probe below refuses and the host's
// accelerator state becomes UNKNOWN with a named gap. That is deliberate: an
// Intel Mac genuinely has a GPU, and reporting "no accelerator" would be a
// fabricated finding, while UNKNOWN is the truth plus an explanation.
type darwinGPUPresence struct{}

func (darwinGPUPresence) AcceleratorVendorsPresent(context.Context) (map[AcceleratorVendor]bool, error) {
	return map[AcceleratorVendor]bool{VendorApple: true}, nil
}

// metalProbe reports the integrated Apple Silicon GPU.
//
// Its identity is the machine's own platform UUID rather than any enumeration
// position (§11.4.111). There is exactly one integrated GPU per Apple Silicon
// Mac, so the platform UUID identifies it uniquely and survives reboots, OS
// upgrades and peripheral changes — which "the first display device" does not.
type metalProbe struct{}

func (metalProbe) Vendor() AcceleratorVendor { return VendorApple }

func (metalProbe) Available(context.Context) bool {
	v, err := darwinSysctl("hw.optional.arm64")
	return err == nil && strings.TrimSpace(v) == "1"
}

func (p metalProbe) Probe(ctx context.Context) ([]Accelerator, error) {
	identity, err := darwinPlatformUUID(ctx)
	if err != nil {
		return nil, err
	}
	// Apple Silicon has unified memory: the GPU's pool IS system memory, so
	// the accelerator's figures are the host's rather than a separate VRAM
	// budget. Reporting a made-up dedicated figure here would misstate the
	// single most important number in a fit check.
	mem, err := platformMemory()
	if err != nil {
		return nil, fmt.Errorf("metal: unified memory unreadable: %w", err)
	}
	model, err := darwinSysctl("machdep.cpu.brand_string")
	if err != nil {
		model = "Apple Silicon GPU"
	}
	return []Accelerator{{
		Identity:        DeviceIdentity("METAL-" + identity),
		Model:           strings.TrimSpace(model),
		API:             APIMetal,
		MemoryTotal:     mem.Total,
		MemoryAvailable: mem.Available,
	}}, nil
}

// darwinPlatformUUID reads the machine's stable platform UUID from the IO
// registry. A device with no readable identity is refused rather than bound by
// position (§11.4.111).
func darwinPlatformUUID(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return "", fmt.Errorf("%w: ioreg: %v", ErrFigureUnavailable, err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "IOPlatformUUID") {
			continue
		}
		fields := strings.Split(line, "=")
		if len(fields) < 2 {
			continue
		}
		if uuid := strings.Trim(strings.TrimSpace(fields[1]), `"`); uuid != "" {
			return uuid, nil
		}
	}
	return "", fmt.Errorf("%w: ioreg reported no IOPlatformUUID, so the GPU has no stable identity to bind", ErrFigureUnavailable)
}

// darwinSysctl reads one sysctl value.
func darwinSysctl(key string) (string, error) {
	out, err := exec.Command("/usr/sbin/sysctl", "-n", key).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// darwinSysctlUint reads one sysctl value as an unsigned integer.
func darwinSysctlUint(key string) (uint64, error) {
	v, err := darwinSysctl(key)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(v, 10, 64)
}
