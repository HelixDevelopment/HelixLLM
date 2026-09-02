package capability_test

import (
	"errors"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/capability/testdata/fixtures"
)

// Every fixture must be structurally well-formed, including the unmeasurable
// one: an honest report of a failed measurement is well-formed, it is merely
// not a basis for selection.
func TestFixturesAreStructurallyWellFormed(t *testing.T) {
	for name, p := range fixtures.All() {
		t.Run(name, func(t *testing.T) {
			if err := p.Validate(); err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
			if p.HostIdentity == "" {
				t.Error("host identity is empty")
			}
			if p.CPU.LogicalCores < p.CPU.PhysicalCores {
				t.Errorf("logical cores %d < physical cores %d", p.CPU.LogicalCores, p.CPU.PhysicalCores)
			}
			if p.MemoryAvailable > p.MemoryTotal {
				t.Errorf("available memory %d exceeds total %d", p.MemoryAvailable, p.MemoryTotal)
			}
			if p.MeasuredAt.IsZero() {
				t.Error("measured-at is the zero time")
			}
			for _, a := range p.Accelerators {
				if a.Identity == "" {
					t.Errorf("accelerator %q has no stable identity", a.Model)
				}
				if !a.API.Known() {
					t.Errorf("accelerator %s reports unknown API %q", a.Identity, a.API)
				}
				if a.MemoryAvailable > a.MemoryTotal {
					t.Errorf("accelerator %s available %d exceeds total %d", a.Identity, a.MemoryAvailable, a.MemoryTotal)
				}
				if a.MemoryTotal == 0 {
					t.Errorf("accelerator %s reports no memory", a.Identity)
				}
			}
		})
	}
}

// The load-bearing distinction: "this host has no accelerator" and "this host's
// accelerator state is unknown" must not be the same value.
//
// Both profiles carry zero accelerators, so a consumer that reads only
// len(Accelerators) cannot tell them apart — which is precisely the bug this
// type is shaped to make impossible. Every discriminating predicate must
// disagree between the two.
func TestNoAcceleratorIsDistinctFromUnknownAcceleratorState(t *testing.T) {
	none := fixtures.NoAccelerator()
	unknown := fixtures.Unmeasurable()

	if got := len(none.Accelerators); got != 0 {
		t.Fatalf("no-accelerator fixture lists %d accelerators, want 0", got)
	}
	if got := len(unknown.Accelerators); got != 0 {
		t.Fatalf("unmeasurable fixture lists %d accelerators, want 0", got)
	}

	if !none.HasNoAccelerator() {
		t.Error("no-accelerator host: HasNoAccelerator() = false, want true — zero devices is a positive finding")
	}
	if unknown.HasNoAccelerator() {
		t.Error("unmeasurable host: HasNoAccelerator() = true, want false — unknown state must not read as 'none'")
	}

	if !none.AcceleratorStateKnown() {
		t.Error("no-accelerator host: AcceleratorStateKnown() = false, want true")
	}
	if unknown.AcceleratorStateKnown() {
		t.Error("unmeasurable host: AcceleratorStateKnown() = true, want false")
	}

	if none.AcceleratorState == unknown.AcceleratorState {
		t.Fatalf("both hosts carry AcceleratorState %v — the two states have collapsed into one value", none.AcceleratorState)
	}

	// A consumer branching on these predicates must take different branches.
	if branch(none) == branch(unknown) {
		t.Fatalf("both hosts drive branch %q — a failed measurement is masquerading as a CPU-only host", branch(none))
	}
}

// branch models the decision selection has to make: serve CPU-only, or refuse.
func branch(p capability.HostCapabilityProfile) string {
	if err := p.ValidateForSelection(); err != nil {
		return "refuse"
	}
	if p.HasNoAccelerator() {
		return "cpu-only"
	}
	return "accelerated"
}

func TestValidateForSelection(t *testing.T) {
	tests := []struct {
		name    string
		profile capability.HostCapabilityProfile
		wantErr error
	}{
		{"no-accelerator", fixtures.NoAccelerator(), nil},
		{"single-accelerator", fixtures.SingleAccelerator(), nil},
		{"dual-accelerator", fixtures.DualAccelerator(), nil},
		{"dual-reversed", fixtures.DualAcceleratorReversed(), nil},
		{"low-storage", fixtures.LowStorage(), nil},
		{"unmeasurable", fixtures.Unmeasurable(), capability.ErrNotMeasured},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.profile.ValidateForSelection()
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateForSelection() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateForSelection() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// Validate must have teeth: each of these malformed profiles is a real failure
// mode, and each must be caught by its own error rather than passing silently.
func TestValidateRejectsMalformedProfiles(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(capability.HostCapabilityProfile) capability.HostCapabilityProfile
		wantErr error
	}{
		{
			name: "claims complete while accelerator state is unknown",
			mutate: func(p capability.HostCapabilityProfile) capability.HostCapabilityProfile {
				p.AcceleratorState = capability.AcceleratorStateUnknown
				p.Accelerators = nil
				p.MeasurementComplete = true
				return p
			},
			wantErr: capability.ErrCompleteButStateUnknown,
		},
		{
			name: "unknown accelerator state yet lists devices",
			mutate: func(p capability.HostCapabilityProfile) capability.HostCapabilityProfile {
				p.AcceleratorState = capability.AcceleratorStateUnknown
				p.MeasurementComplete = false
				return p
			},
			wantErr: capability.ErrUnknownStateHasDevices,
		},
		{
			name: "accelerator without a stable identity",
			mutate: func(p capability.HostCapabilityProfile) capability.HostCapabilityProfile {
				p.Accelerators[0].Identity = ""
				return p
			},
			wantErr: capability.ErrAcceleratorNoIdentity,
		},
		{
			name: "two accelerators sharing one identity",
			mutate: func(p capability.HostCapabilityProfile) capability.HostCapabilityProfile {
				p.Accelerators[1].Identity = p.Accelerators[0].Identity
				return p
			},
			wantErr: capability.ErrAcceleratorDuplicateID,
		},
		{
			name: "accelerator reporting no acceleration API",
			mutate: func(p capability.HostCapabilityProfile) capability.HostCapabilityProfile {
				p.Accelerators[0].API = capability.APIUnknown
				return p
			},
			wantErr: capability.ErrAcceleratorUnknownAPI,
		},
		{
			name: "accelerator with more available memory than it has",
			mutate: func(p capability.HostCapabilityProfile) capability.HostCapabilityProfile {
				p.Accelerators[0].MemoryAvailable = p.Accelerators[0].MemoryTotal + capability.GiB
				return p
			},
			wantErr: capability.ErrAcceleratorMemoryExceed,
		},
		{
			name: "more available system memory than total",
			mutate: func(p capability.HostCapabilityProfile) capability.HostCapabilityProfile {
				p.MemoryAvailable = p.MemoryTotal + capability.GiB
				return p
			},
			wantErr: capability.ErrMemoryAvailableExceeds,
		},
		{
			name: "no host identity",
			mutate: func(p capability.HostCapabilityProfile) capability.HostCapabilityProfile {
				p.HostIdentity = ""
				return p
			},
			wantErr: capability.ErrNoHostIdentity,
		},
		{
			name: "no measurement time",
			mutate: func(p capability.HostCapabilityProfile) capability.HostCapabilityProfile {
				p.MeasuredAt = time.Time{}
				return p
			},
			wantErr: capability.ErrNoMeasurementTime,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Start from a host that is valid, so the mutation is the only cause.
			base := fixtures.DualAccelerator()
			if err := base.Validate(); err != nil {
				t.Fatalf("precondition: unmutated fixture is invalid: %v", err)
			}
			got := tt.mutate(base).Validate()
			if !errors.Is(got, tt.wantErr) {
				t.Fatalf("Validate() = %v, want %v", got, tt.wantErr)
			}
		})
	}
}

// §11.4.111: a binding must follow stable device identity, never enumeration
// position. The two dual fixtures are the same physical host reported in
// opposite order; resolving by identity must yield the same device in both,
// while resolving by position must not.
func TestAcceleratorBindingFollowsIdentityNotEnumerationOrder(t *testing.T) {
	forward := fixtures.DualAccelerator()
	reversed := fixtures.DualAcceleratorReversed()

	if len(forward.Accelerators) != 2 || len(reversed.Accelerators) != 2 {
		t.Fatalf("dual fixtures must both list 2 devices, got %d and %d",
			len(forward.Accelerators), len(reversed.Accelerators))
	}

	// Precondition: the orders genuinely differ, or this test proves nothing.
	if forward.Accelerators[0].Identity == reversed.Accelerators[0].Identity {
		t.Fatal("DualAcceleratorReversed reports the same order as DualAccelerator — the fixture cannot detect index binding")
	}

	for _, id := range []capability.DeviceIdentity{
		fixtures.IdentityWorkstationPrimary,
		fixtures.IdentityWorkstationSecondary,
	} {
		a, ok := forward.AcceleratorByIdentity(id)
		if !ok {
			t.Fatalf("forward host does not carry device %s", id)
		}
		b, ok := reversed.AcceleratorByIdentity(id)
		if !ok {
			t.Fatalf("reversed host does not carry device %s", id)
		}
		if a != b {
			t.Errorf("device %s resolves differently across enumeration orders:\n forward  = %+v\n reversed = %+v", id, a, b)
		}
	}

	// The devices must be distinguishable, otherwise an index binding would
	// pass by coincidence.
	if forward.Accelerators[0].MemoryTotal == forward.Accelerators[1].MemoryTotal {
		t.Error("dual fixture devices have identical memory — an index-bound implementation would pass by coincidence")
	}

	// And index binding must actually be wrong here: position 0 is a different
	// device in each ordering.
	if forward.Accelerators[0] == reversed.Accelerators[0] {
		t.Error("position 0 holds the same device in both orderings — the fixture does not exercise the index-binding failure")
	}

	unknown, ok := forward.AcceleratorByIdentity("GPU-does-not-exist")
	if ok {
		t.Errorf("AcceleratorByIdentity resolved an unknown identity to %+v", unknown)
	}
}

// Storage is an axis independent of memory (D2). The low-storage host must be
// unable to fit a footprint on disk that it could hold in memory many times
// over, so a refusal naming memory would be provably wrong.
func TestLowStorageIsolatesStorageAxisFromMemory(t *testing.T) {
	p := fixtures.LowStorage()

	const footprint = 16 * capability.GiB

	if p.StorageAvailable >= footprint {
		t.Fatalf("free storage %d is not below the %d footprint — fixture cannot demonstrate a storage refusal",
			p.StorageAvailable, footprint)
	}
	if p.MemoryAvailable <= footprint {
		t.Fatalf("available memory %d does not exceed the %d footprint — a refusal here would be ambiguous between memory and storage",
			p.MemoryAvailable, footprint)
	}
	if p.MemoryAvailable <= p.StorageAvailable {
		t.Errorf("available memory %d does not exceed free storage %d", p.MemoryAvailable, p.StorageAvailable)
	}
	if !p.AcceleratorStateKnown() {
		t.Error("low-storage host must be fully measured, so storage is the only thing missing")
	}
	if err := p.ValidateForSelection(); err != nil {
		t.Errorf("ValidateForSelection() = %v, want nil — low storage is a fit question, not a measurement failure", err)
	}

	// Contrast: the well-provisioned host fits the same footprint on disk.
	if fixtures.NoAccelerator().StorageAvailable <= footprint {
		t.Error("no-accelerator fixture also lacks the storage, so a storage refusal would not be attributable")
	}
}

// Fixtures must hand out independent values: a test that mutates what it
// received must not affect any other test.
func TestFixturesReturnIndependentCopies(t *testing.T) {
	a := fixtures.DualAccelerator()
	a.Accelerators[0].Identity = "mutated"
	a.CPU.Features[0] = "mutated"
	a.MemoryAvailable = 1

	b := fixtures.DualAccelerator()
	if b.Accelerators[0].Identity == "mutated" {
		t.Error("accelerator slice is shared between fixture calls")
	}
	if b.CPU.Features[0] == "mutated" {
		t.Error("CPU feature slice is shared between fixture calls")
	}
	if b.MemoryAvailable == 1 {
		t.Error("scalar field leaked between fixture calls")
	}
}

func TestStaledOnlyMovesTheReadingTime(t *testing.T) {
	fresh := fixtures.NoAccelerator()
	stale := fixtures.Staled(fresh, 48*time.Hour)

	if !stale.MeasuredAt.Equal(fresh.MeasuredAt.Add(-48 * time.Hour)) {
		t.Errorf("MeasuredAt = %v, want %v", stale.MeasuredAt, fresh.MeasuredAt.Add(-48*time.Hour))
	}
	if got := stale.Age(fresh.MeasuredAt); got != 48*time.Hour {
		t.Errorf("Age() = %v, want 48h", got)
	}

	// Everything else is untouched, so a freshness test cannot accidentally be
	// exercising a different difference.
	stale.MeasuredAt = fresh.MeasuredAt
	if stale.HostIdentity != fresh.HostIdentity ||
		stale.MemoryAvailable != fresh.MemoryAvailable ||
		stale.StorageAvailable != fresh.StorageAvailable ||
		stale.AcceleratorState != fresh.AcceleratorState ||
		stale.MeasurementComplete != fresh.MeasurementComplete {
		t.Error("Staled changed a field other than MeasuredAt")
	}
}

func TestCPUFeatureLookup(t *testing.T) {
	cpu := fixtures.NoAccelerator().CPU
	if !cpu.HasFeature(capability.FeatureAVX2) {
		t.Error("HasFeature(avx2) = false on a fixture that declares it")
	}
	if cpu.HasFeature(capability.FeatureNEON) {
		t.Error("HasFeature(neon) = true on an amd64 fixture")
	}
}
