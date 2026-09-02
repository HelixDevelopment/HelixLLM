package capability

import (
	"errors"
	"runtime"
	"slices"
)

// Errors shared by every measurement entry point.
//
// They are separate because they mean different things to the caller: an
// unsupported platform is a gap in this package, while an unavailable figure is
// a gap on this host. Both are honest refusals — neither is ever substituted
// with a default, because a plausible invented number is indistinguishable from
// a measured one once it reaches selection (FR-056).
var (
	// ErrPlatformUnsupported means this package has no measurement path for
	// the running operating system.
	ErrPlatformUnsupported = errors.New("capability: no measurement path for this platform")
	// ErrFigureUnavailable means the platform is supported but this particular
	// figure could not be obtained from it.
	ErrFigureUnavailable = errors.New("capability: figure unavailable on this host")
)

// MemoryReading is one reading of system memory. Total is nameplate size;
// Available is what is genuinely free at the instant of the reading, which is
// the figure selection spends.
type MemoryReading struct {
	Total     Bytes
	Available Bytes
}

// MeasureCPU reports the processor of the machine this process is running on.
//
// Architecture is taken from the running binary rather than parsed from the
// host, because that is the architecture code will actually execute as. Core
// counts and instruction-set features come from the platform.
func MeasureCPU() (CPUProfile, error) {
	cpu, err := platformCPU()
	cpu.Architecture = runtime.GOARCH
	if cpu.LogicalCores <= 0 {
		// The runtime always knows this much. Using it as a floor keeps a
		// partial platform reading from under-reporting, and it is a measured
		// figure in its own right rather than a default.
		cpu.LogicalCores = runtime.NumCPU()
	}
	if err != nil {
		return cpu, err
	}
	if cpu.LogicalCores <= 0 {
		return cpu, ErrFigureUnavailable
	}
	return cpu, nil
}

// MeasureMemory reports total and currently-available system memory.
func MeasureMemory() (MemoryReading, error) {
	return platformMemory()
}

// sortedFeatures renders a feature set in a stable order. The order carries no
// meaning — HasFeature is the only supported way to ask — but a deterministic
// one keeps two readings of the same machine comparable byte for byte.
func sortedFeatures(set map[CPUFeature]struct{}) []CPUFeature {
	out := make([]CPUFeature, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	slices.Sort(out)
	return out
}
