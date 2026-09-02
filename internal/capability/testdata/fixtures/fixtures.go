// Package fixtures provides measured-host profiles for tests.
//
// These five hosts are the reason the selection surface — every offer and every
// refusal path — can be exercised with no hardware present. Selection is a pure
// function of (host, catalogue, declared usage), so a fixture host is a
// complete, honest stand-in for a real one.
//
// It lives under testdata/ so it can never be linked into a production binary
// by accident, and is imported by explicit path from tests in the capability
// and selection packages.
//
// Every constructor returns a fresh value with freshly allocated slices, so a
// test may mutate what it receives without affecting any other test.
package fixtures

import (
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
)

// Stable device identities of the fixture accelerators. Tests bind to these
// rather than to a position in the Accelerators slice: that is the whole point
// of the dual-accelerator fixture (§11.4.111).
const (
	// IdentityWorkstationPrimary is the higher-memory card in the dual host,
	// and the sole card in the single-accelerator host.
	IdentityWorkstationPrimary capability.DeviceIdentity = "GPU-b2f8c1d4-3e5a-4c9b-9d2e-4f6a8b0c2d4e"
	// IdentityWorkstationSecondary is the lower-memory card in the dual host.
	IdentityWorkstationSecondary capability.DeviceIdentity = "GPU-7a1c9e60-2b84-4f13-8ac6-1e35d709b4f2"
)

// Model names of the fixture accelerators. They are descriptions, not
// identities — a two-card host may report the same model name twice.
const (
	ModelWorkstationPrimary   = "NVIDIA GeForce RTX 4090"
	ModelWorkstationSecondary = "NVIDIA GeForce RTX 3090"
)

// x86Features is the instruction-set set of a current server-class x86-64 CPU.
func x86Features() []capability.CPUFeature {
	return []capability.CPUFeature{
		capability.FeatureAVX2,
		capability.FeatureAVX512F,
		capability.FeatureAVX512BW,
		capability.FeatureF16C,
		capability.FeatureFMA,
	}
}

// measuredNow stamps a fixture as a fresh reading. Fixtures are fresh by
// default so a selection test is not incidentally exercising the staleness
// path; a test that wants a stale reading calls Staled.
func measuredNow() time.Time {
	return time.Now().UTC()
}

// NoAccelerator is a well-provisioned host with no acceleration device at all.
// Measurement completed: zero accelerators here is a positive finding, not a
// gap, and drives CPU-only offers (FR-002).
func NoAccelerator() capability.HostCapabilityProfile {
	return capability.HostCapabilityProfile{
		HostIdentity: "fixture-cpu-only",
		CPU: capability.CPUProfile{
			Architecture:  "amd64",
			PhysicalCores: 16,
			LogicalCores:  32,
			Features:      x86Features(),
		},
		MemoryTotal:         64 * capability.GiB,
		MemoryAvailable:     48 * capability.GiB,
		AcceleratorState:    capability.AcceleratorStateMeasured,
		Accelerators:        []capability.Accelerator{},
		StorageAvailable:    512 * capability.GiB,
		MeasuredAt:          measuredNow(),
		MeasurementComplete: true,
	}
}

// SingleAccelerator is a workstation with one discrete CUDA device.
func SingleAccelerator() capability.HostCapabilityProfile {
	return capability.HostCapabilityProfile{
		HostIdentity: "fixture-single-gpu",
		CPU: capability.CPUProfile{
			Architecture:  "amd64",
			PhysicalCores: 12,
			LogicalCores:  24,
			Features:      x86Features(),
		},
		MemoryTotal:      64 * capability.GiB,
		MemoryAvailable:  44 * capability.GiB,
		AcceleratorState: capability.AcceleratorStateMeasured,
		Accelerators: []capability.Accelerator{
			primaryCard(),
		},
		StorageAvailable:    1024 * capability.GiB,
		MeasuredAt:          measuredNow(),
		MeasurementComplete: true,
	}
}

// DualAccelerator is a workstation with two distinct CUDA devices of different
// memory sizes.
//
// This fixture exists so a test can prove that a binding follows device
// identity and not enumeration position: pair it with DualAcceleratorReversed,
// which reports the same two devices in the opposite order, and assert that
// resolving either identity yields the same device in both.
func DualAccelerator() capability.HostCapabilityProfile {
	p := dualBase()
	p.Accelerators = []capability.Accelerator{primaryCard(), secondaryCard()}
	return p
}

// DualAcceleratorReversed is DualAccelerator with enumeration order reversed —
// the same physical host as a different boot order would report it.
func DualAcceleratorReversed() capability.HostCapabilityProfile {
	p := dualBase()
	p.Accelerators = []capability.Accelerator{secondaryCard(), primaryCard()}
	return p
}

func dualBase() capability.HostCapabilityProfile {
	return capability.HostCapabilityProfile{
		HostIdentity: "fixture-dual-gpu",
		CPU: capability.CPUProfile{
			Architecture:  "amd64",
			PhysicalCores: 24,
			LogicalCores:  48,
			Features:      x86Features(),
		},
		MemoryTotal:         128 * capability.GiB,
		MemoryAvailable:     96 * capability.GiB,
		AcceleratorState:    capability.AcceleratorStateMeasured,
		StorageAvailable:    2048 * capability.GiB,
		MeasuredAt:          measuredNow(),
		MeasurementComplete: true,
	}
}

func primaryCard() capability.Accelerator {
	return capability.Accelerator{
		Identity:        IdentityWorkstationPrimary,
		Model:           ModelWorkstationPrimary,
		API:             capability.APICUDA,
		MemoryTotal:     24 * capability.GiB,
		MemoryAvailable: 23*capability.GiB + 200*capability.MiB,
	}
}

func secondaryCard() capability.Accelerator {
	return capability.Accelerator{
		Identity:        IdentityWorkstationSecondary,
		Model:           ModelWorkstationSecondary,
		API:             capability.APICUDA,
		MemoryTotal:     12 * capability.GiB,
		MemoryAvailable: 11*capability.GiB + 512*capability.MiB,
	}
}

// LowStorage is a host with abundant memory and almost no free disk.
//
// It exists so a storage-infeasible model can be shown to be withheld for
// storage specifically — memory here is deliberately far larger than any
// footprint that would fit the free disk, so a refusal that names memory is
// provably wrong (D2).
func LowStorage() capability.HostCapabilityProfile {
	return capability.HostCapabilityProfile{
		HostIdentity: "fixture-low-storage",
		CPU: capability.CPUProfile{
			Architecture:  "amd64",
			PhysicalCores: 16,
			LogicalCores:  32,
			Features:      x86Features(),
		},
		MemoryTotal:      192 * capability.GiB,
		MemoryAvailable:  160 * capability.GiB,
		AcceleratorState: capability.AcceleratorStateMeasured,
		Accelerators: []capability.Accelerator{
			primaryCard(),
		},
		StorageAvailable:    2 * capability.GiB,
		MeasuredAt:          measuredNow(),
		MeasurementComplete: true,
	}
}

// Unmeasurable is a host whose measurement did not complete.
//
// Accelerator state is unknown — which is not the same value as "no
// accelerator" — and MeasurementComplete is false, so selection must refuse
// rather than fall back to a default (FR-056). The figures that were obtained
// before the failure are kept; the point is that they are not enough.
func Unmeasurable() capability.HostCapabilityProfile {
	return capability.HostCapabilityProfile{
		HostIdentity: "fixture-unmeasurable",
		CPU: capability.CPUProfile{
			Architecture:  "amd64",
			PhysicalCores: 8,
			LogicalCores:  16,
			Features:      []capability.CPUFeature{capability.FeatureAVX2},
		},
		MemoryTotal:         32 * capability.GiB,
		MemoryAvailable:     20 * capability.GiB,
		AcceleratorState:    capability.AcceleratorStateUnknown,
		Accelerators:        nil,
		StorageAvailable:    0,
		MeasuredAt:          measuredNow(),
		MeasurementComplete: false,
	}
}

// All returns every fixture host, keyed by a short name, for table-driven tests
// that must hold across the whole set.
func All() map[string]capability.HostCapabilityProfile {
	return map[string]capability.HostCapabilityProfile{
		"no-accelerator":     NoAccelerator(),
		"single-accelerator": SingleAccelerator(),
		"dual-accelerator":   DualAccelerator(),
		"dual-reversed":      DualAcceleratorReversed(),
		"low-storage":        LowStorage(),
		"unmeasurable":       Unmeasurable(),
	}
}

// Staled returns p as it would look if its reading had been taken age ago,
// leaving every other field untouched. Freshness tests use this rather than
// hand-building a second profile, so the stale and fresh cases cannot drift.
func Staled(p capability.HostCapabilityProfile, age time.Duration) capability.HostCapabilityProfile {
	p.MeasuredAt = p.MeasuredAt.Add(-age)
	return p
}
