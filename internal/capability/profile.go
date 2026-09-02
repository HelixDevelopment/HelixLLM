package capability

import (
	"errors"
	"fmt"
	"time"
)

// Bytes is a byte quantity. Memory and storage are both counted in Bytes but
// are separate axes: a model's disk footprint is not implied by its memory
// figure (data-model.md D2).
type Bytes uint64

// Byte-quantity units, for readable declarations.
const (
	KiB Bytes = 1 << 10
	MiB Bytes = 1 << 20
	GiB Bytes = 1 << 30
)

// DeviceIdentity is the stable identity of an accelerator — a vendor UUID, a
// PCI address, a registry ID. It is deliberately a distinct string type so an
// enumeration index cannot be assigned to it: binding by position silently
// re-points when a second card is added or boot order changes (§11.4.111).
type DeviceIdentity string

// AccelerationAPI is the acceleration interface a device offers.
type AccelerationAPI string

// The acceleration interfaces measurement can report. APIUnknown is the zero
// value and is never a valid measured result.
const (
	APIUnknown AccelerationAPI = ""
	APICUDA    AccelerationAPI = "cuda"
	APIMetal   AccelerationAPI = "metal"
	APIROCm    AccelerationAPI = "rocm"
)

// Known reports whether the API was actually determined by measurement.
func (a AccelerationAPI) Known() bool {
	switch a {
	case APICUDA, APIMetal, APIROCm:
		return true
	default:
		return false
	}
}

// CPUFeature is an instruction-set feature that gates CPU-served options.
type CPUFeature string

// Instruction-set features that materially gate CPU-served inference.
const (
	FeatureAVX2     CPUFeature = "avx2"
	FeatureAVX512F  CPUFeature = "avx512f"
	FeatureAVX512BW CPUFeature = "avx512bw"
	FeatureF16C     CPUFeature = "f16c"
	FeatureFMA      CPUFeature = "fma"
	FeatureNEON     CPUFeature = "neon"
	FeatureDotProd  CPUFeature = "dotprod"
	FeatureFP16     CPUFeature = "fp16"
)

// CPUProfile is the processor as measured.
type CPUProfile struct {
	// Architecture as reported by the host, e.g. "amd64", "arm64".
	Architecture string
	// PhysicalCores is the count of physical cores; LogicalCores includes SMT
	// siblings. Both are reported because thread-count guidance differs.
	PhysicalCores int
	LogicalCores  int
	// Features are the instruction-set features present, unordered.
	Features []CPUFeature
}

// HasFeature reports whether the measured CPU offers f.
func (c CPUProfile) HasFeature(f CPUFeature) bool {
	for _, have := range c.Features {
		if have == f {
			return true
		}
	}
	return false
}

// Accelerator is one measured acceleration device.
type Accelerator struct {
	// Identity is the stable device identity. Never an enumeration index
	// (§11.4.111) — every binding to this device goes through this value.
	Identity DeviceIdentity
	// Model is the device's reported name, for description only. It is not an
	// identity: two identical cards share a model name.
	Model string
	// API is the acceleration interface this device offers.
	API AccelerationAPI
	// MemoryTotal and MemoryAvailable are usable device memory, not nameplate.
	MemoryTotal     Bytes
	MemoryAvailable Bytes
}

// AcceleratorMeasurement records whether accelerator enumeration actually
// completed. It exists so that "this host has no accelerator" and "this host's
// accelerator state could not be determined" are different values: the first is
// a valid, first-class state that drives CPU-only offers (FR-002), the second
// must produce a refusal rather than a guess (FR-056).
type AcceleratorMeasurement uint8

const (
	// AcceleratorStateUnknown means enumeration did not complete. Selection
	// MUST refuse on this; it must never be read as "there are none".
	AcceleratorStateUnknown AcceleratorMeasurement = iota
	// AcceleratorStateMeasured means enumeration completed. Zero accelerators
	// alongside this value is a positive finding: the host has none.
	AcceleratorStateMeasured
)

// String renders the measurement state for diagnostics.
func (m AcceleratorMeasurement) String() string {
	switch m {
	case AcceleratorStateMeasured:
		return "measured"
	case AcceleratorStateUnknown:
		return "unknown"
	default:
		return fmt.Sprintf("AcceleratorMeasurement(%d)", uint8(m))
	}
}

// HostCapabilityProfile is what a candidate serving machine can currently
// support. It is produced by measurement and never configured.
//
// Selection consumes this and nothing else about the host, which is what makes
// selection a pure function testable without hardware.
type HostCapabilityProfile struct {
	// HostIdentity is the stable identity of the machine, used in the naming
	// scheme (FR-014).
	HostIdentity string

	// CPU gates the CPU-served options.
	CPU CPUProfile

	// MemoryTotal is nameplate system memory; MemoryAvailable is what is
	// actually free right now. Selection uses MemoryAvailable.
	MemoryTotal     Bytes
	MemoryAvailable Bytes

	// AcceleratorState says whether Accelerators is a finding or an absence of
	// information. Read it before reading Accelerators.
	AcceleratorState AcceleratorMeasurement
	// Accelerators is zero or more measured devices. Empty with
	// AcceleratorStateMeasured is valid and first-class (FR-002).
	Accelerators []Accelerator

	// StorageAvailable is free disk, an axis independent of memory (D2).
	StorageAvailable Bytes

	// MeasuredAt is when this reading was taken. A stale profile must not
	// silently drive a fresh selection (FR-033).
	MeasuredAt time.Time

	// MeasurementComplete is false when measurement failed or was partial.
	// Selection MUST refuse rather than fall back to a default (FR-056).
	MeasurementComplete bool
}

// Errors returned by Validate and ValidateForSelection. They are distinct so a
// caller can tell a malformed profile from an honestly incomplete one.
var (
	ErrNoHostIdentity          = errors.New("capability: profile has no host identity")
	ErrNoCPUCores              = errors.New("capability: profile reports no logical CPU cores")
	ErrNoMemoryTotal           = errors.New("capability: profile reports no total memory")
	ErrMemoryAvailableExceeds  = errors.New("capability: available memory exceeds total memory")
	ErrAcceleratorNoIdentity   = errors.New("capability: accelerator has no stable device identity")
	ErrAcceleratorDuplicateID  = errors.New("capability: two accelerators share one device identity")
	ErrAcceleratorUnknownAPI   = errors.New("capability: accelerator reports no acceleration API")
	ErrAcceleratorMemoryExceed = errors.New("capability: accelerator available memory exceeds its total")
	ErrUnknownStateHasDevices  = errors.New("capability: accelerator state is unknown yet devices are listed")
	ErrCompleteButStateUnknown = errors.New("capability: measurement marked complete while accelerator state is unknown")
	ErrNotMeasured             = errors.New("capability: measurement incomplete, not a basis for selection")
	ErrNoMeasurementTime       = errors.New("capability: profile carries no measurement time")
)

// Validate reports whether the profile is structurally well-formed. It does not
// require the measurement to have succeeded — an honestly incomplete profile is
// well-formed. Use ValidateForSelection to additionally require a usable
// measurement.
func (p HostCapabilityProfile) Validate() error {
	if p.HostIdentity == "" {
		return ErrNoHostIdentity
	}
	if p.MeasuredAt.IsZero() {
		return ErrNoMeasurementTime
	}
	if p.MemoryAvailable > p.MemoryTotal {
		return fmt.Errorf("%w: %d > %d", ErrMemoryAvailableExceeds, p.MemoryAvailable, p.MemoryTotal)
	}
	if p.AcceleratorState == AcceleratorStateUnknown && len(p.Accelerators) > 0 {
		return fmt.Errorf("%w: %d listed", ErrUnknownStateHasDevices, len(p.Accelerators))
	}
	if p.MeasurementComplete && p.AcceleratorState != AcceleratorStateMeasured {
		return ErrCompleteButStateUnknown
	}
	if p.MeasurementComplete {
		if p.CPU.LogicalCores <= 0 {
			return ErrNoCPUCores
		}
		if p.MemoryTotal == 0 {
			return ErrNoMemoryTotal
		}
	}

	seen := make(map[DeviceIdentity]struct{}, len(p.Accelerators))
	for _, a := range p.Accelerators {
		if a.Identity == "" {
			return fmt.Errorf("%w: model %q", ErrAcceleratorNoIdentity, a.Model)
		}
		if _, dup := seen[a.Identity]; dup {
			return fmt.Errorf("%w: %s", ErrAcceleratorDuplicateID, a.Identity)
		}
		seen[a.Identity] = struct{}{}
		if !a.API.Known() {
			return fmt.Errorf("%w: %s", ErrAcceleratorUnknownAPI, a.Identity)
		}
		if a.MemoryAvailable > a.MemoryTotal {
			return fmt.Errorf("%w: %s: %d > %d", ErrAcceleratorMemoryExceed, a.Identity, a.MemoryAvailable, a.MemoryTotal)
		}
	}
	return nil
}

// ValidateForSelection reports whether the profile may be used as the basis of
// a selection. An incomplete profile is not a profile: it is refused here
// rather than completed with defaults (FR-056).
func (p HostCapabilityProfile) ValidateForSelection() error {
	if err := p.Validate(); err != nil {
		return err
	}
	if !p.MeasurementComplete || p.AcceleratorState != AcceleratorStateMeasured {
		return ErrNotMeasured
	}
	return nil
}

// AcceleratorStateKnown reports whether accelerator enumeration completed.
// False means the state is unknown and selection must refuse, not assume none.
func (p HostCapabilityProfile) AcceleratorStateKnown() bool {
	return p.AcceleratorState == AcceleratorStateMeasured
}

// HasNoAccelerator reports the positive finding that this host has no
// acceleration device. It is false when the state is merely unknown, which is
// the distinction that keeps a failed measurement from masquerading as a
// CPU-only host.
func (p HostCapabilityProfile) HasNoAccelerator() bool {
	return p.AcceleratorStateKnown() && len(p.Accelerators) == 0
}

// AcceleratorByIdentity resolves a device by its stable identity, independent
// of the order enumeration happened to return. This is the only supported way
// to reach a specific device.
func (p HostCapabilityProfile) AcceleratorByIdentity(id DeviceIdentity) (Accelerator, bool) {
	for _, a := range p.Accelerators {
		if a.Identity == id {
			return a, true
		}
	}
	return Accelerator{}, false
}

// Age reports how long ago the measurement was taken, relative to now.
func (p HostCapabilityProfile) Age(now time.Time) time.Duration {
	return now.Sub(p.MeasuredAt)
}
