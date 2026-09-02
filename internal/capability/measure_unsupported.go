//go:build !linux && !darwin

package capability

import "fmt"

// Measurement primitives for platforms this package has no path for yet.
//
// Every one of them refuses. That is the point: a platform without a
// measurement path must produce an incomplete profile that selection declines
// to act on, never a plausible-looking default that selection would spend
// (§11.4.81 honest per-OS gap, FR-056).

func platformCPU() (CPUProfile, error) {
	return CPUProfile{}, fmt.Errorf("%w: CPU measurement", ErrPlatformUnsupported)
}

func platformMemory() (MemoryReading, error) {
	return MemoryReading{}, fmt.Errorf("%w: system-memory measurement", ErrPlatformUnsupported)
}

func platformFreeStorage(string) (Bytes, error) {
	return 0, fmt.Errorf("%w: free-storage measurement", ErrPlatformUnsupported)
}

func platformAcceleratorSources() (VendorPresence, []AcceleratorProbe, error) {
	return nil, nil, fmt.Errorf("%w: accelerator measurement", ErrPlatformUnsupported)
}
