package capability_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/capability/testdata/fixtures"
)

// T008 RED — §11.4.111. Accelerator binding resolves by STABLE DEVICE IDENTITY,
// never by enumeration position.
//
// The two dual-accelerator fixtures are the same physical host reported in
// opposite enumeration order — exactly what a different boot order, a hotplug,
// or a driver upgrade produces. Every assertion below is one an index-based
// binding fails and an identity-based binding passes.

func TestAcceleratorSet_SameDeviceResolvesUnderEitherEnumerationOrder(t *testing.T) {
	forward, err := capability.NewAcceleratorSet(fixtures.DualAccelerator().Accelerators)
	if err != nil {
		t.Fatalf("NewAcceleratorSet(forward): %v", err)
	}
	reversed, err := capability.NewAcceleratorSet(fixtures.DualAcceleratorReversed().Accelerators)
	if err != nil {
		t.Fatalf("NewAcceleratorSet(reversed): %v", err)
	}

	for _, id := range []capability.DeviceIdentity{
		fixtures.IdentityWorkstationPrimary,
		fixtures.IdentityWorkstationSecondary,
	} {
		fromForward, okF := forward.ByIdentity(id)
		fromReversed, okR := reversed.ByIdentity(id)
		if !okF || !okR {
			t.Fatalf("identity %s resolved forward=%v reversed=%v; it must resolve in both", id, okF, okR)
		}
		if !reflect.DeepEqual(fromForward, fromReversed) {
			t.Errorf("identity %s resolved to different devices depending on enumeration order:\n forward  = %+v\n reversed = %+v",
				id, fromForward, fromReversed)
		}
	}
}

func TestAcceleratorSet_DeviceOrderIsCanonicalNotEnumerationOrder(t *testing.T) {
	// This is the assertion that actually protects downstream code. Even a
	// caller that reaches for Devices()[0] must get the same physical device
	// on both hosts — otherwise the enumeration order has leaked out of this
	// package and re-pointed a binding, which is the §11.4.111 defect.
	forward, err := capability.NewAcceleratorSet(fixtures.DualAccelerator().Accelerators)
	if err != nil {
		t.Fatalf("NewAcceleratorSet(forward): %v", err)
	}
	reversed, err := capability.NewAcceleratorSet(fixtures.DualAcceleratorReversed().Accelerators)
	if err != nil {
		t.Fatalf("NewAcceleratorSet(reversed): %v", err)
	}

	gotForward, gotReversed := forward.Devices(), reversed.Devices()
	if !reflect.DeepEqual(gotForward, gotReversed) {
		t.Fatalf("Devices() differs by enumeration order — position is load-bearing, which it must not be:\n forward  = %+v\n reversed = %+v",
			gotForward, gotReversed)
	}
	if len(gotForward) != 2 {
		t.Fatalf("Devices() returned %d devices, want 2", len(gotForward))
	}
	// And the same for the identity list, which is what a caller iterates.
	if !reflect.DeepEqual(forward.Identities(), reversed.Identities()) {
		t.Errorf("Identities() differs by enumeration order: %v vs %v", forward.Identities(), reversed.Identities())
	}
}

func TestAcceleratorSet_MutatingTheResultCannotCorruptTheSet(t *testing.T) {
	set, err := capability.NewAcceleratorSet(fixtures.DualAccelerator().Accelerators)
	if err != nil {
		t.Fatalf("NewAcceleratorSet: %v", err)
	}
	devices := set.Devices()
	devices[0].Identity = "clobbered"

	if _, ok := set.ByIdentity(fixtures.IdentityWorkstationPrimary); !ok {
		t.Error("the set lost a device after a caller mutated the slice it handed out")
	}
	if got := set.Devices()[0].Identity; got == "clobbered" {
		t.Error("Devices() hands out the set's own backing array; a caller can re-point a binding")
	}
}

func TestAcceleratorSet_RejectsDevicesWithoutAStableIdentity(t *testing.T) {
	_, err := capability.NewAcceleratorSet([]capability.Accelerator{{
		Model: "a card whose identity the probe could not read",
		API:   capability.APICUDA,
	}})
	if !errors.Is(err, capability.ErrAcceleratorNoIdentity) {
		t.Errorf("err = %v, want ErrAcceleratorNoIdentity — a device with no stable identity cannot be bound at all", err)
	}
}

func TestAcceleratorSet_RejectsTwoDevicesSharingOneIdentity(t *testing.T) {
	dup := fixtures.DualAccelerator().Accelerators
	dup[1].Identity = dup[0].Identity
	_, err := capability.NewAcceleratorSet(dup)
	if !errors.Is(err, capability.ErrAcceleratorDuplicateID) {
		t.Errorf("err = %v, want ErrAcceleratorDuplicateID — a shared identity makes resolution ambiguous", err)
	}
}

// T009 RED. Zero accelerators is a FIRST-CLASS measured state, and it is a
// different value from "we could not find out".

func TestMeasureAccelerators_ZeroDevicesIsAPositiveFinding(t *testing.T) {
	res := capability.MeasureAcceleratorsWith(context.Background(),
		presenceStub{present: map[capability.AcceleratorVendor]bool{}},
		nil)

	if res.State != capability.AcceleratorStateMeasured {
		t.Fatalf("State = %v, want measured: a host with no accelerator vendor present is a finding, not a gap", res.State)
	}
	if len(res.Devices) != 0 {
		t.Errorf("Devices = %+v, want none", res.Devices)
	}
	if len(res.Gaps) != 0 {
		t.Errorf("Gaps = %v, want none on a complete measurement", res.Gaps)
	}

	// And the profile built from it must report the positive finding.
	p := profileFromResult(res)
	if !p.HasNoAccelerator() {
		t.Error("HasNoAccelerator() = false on a measured host with zero devices")
	}
	if err := p.ValidateForSelection(); err != nil {
		t.Errorf("ValidateForSelection() = %v; a CPU-only host is a perfectly valid basis for selection", err)
	}
}

func TestMeasureAccelerators_UncoveredVendorIsUnknownNotZero(t *testing.T) {
	// Hardware is present but nothing can interrogate it. Reporting zero
	// devices here would tell selection "CPU-only host" — a fabricated finding
	// that hides a whole accelerator.
	res := capability.MeasureAcceleratorsWith(context.Background(),
		presenceStub{present: map[capability.AcceleratorVendor]bool{capability.VendorNVIDIA: true}},
		nil)

	if res.State != capability.AcceleratorStateUnknown {
		t.Fatalf("State = %v, want unknown: a vendor is present with no probe to read it", res.State)
	}
	if len(res.Gaps) == 0 {
		t.Error("Gaps is empty; an unknown state must say what could not be determined")
	}
	p := profileFromResult(res)
	if p.HasNoAccelerator() {
		t.Error("HasNoAccelerator() = true while the state is unknown — a failed measurement is masquerading as a CPU-only host")
	}
	if !errors.Is(p.ValidateForSelection(), capability.ErrNotMeasured) {
		t.Error("an unknown accelerator state must refuse selection")
	}
}

func TestMeasureAccelerators_ProbeFailureIsUnknownNotZero(t *testing.T) {
	res := capability.MeasureAcceleratorsWith(context.Background(),
		presenceStub{present: map[capability.AcceleratorVendor]bool{capability.VendorNVIDIA: true}},
		[]capability.AcceleratorProbe{probeStub{
			vendor:    capability.VendorNVIDIA,
			available: true,
			err:       errors.New("driver query failed"),
		}})

	if res.State != capability.AcceleratorStateUnknown {
		t.Fatalf("State = %v, want unknown after a probe error", res.State)
	}
	if len(res.Devices) != 0 {
		t.Errorf("Devices = %+v; a failed enumeration must carry no devices", res.Devices)
	}
}

func TestMeasureAccelerators_PresenceFailureIsUnknown(t *testing.T) {
	res := capability.MeasureAcceleratorsWith(context.Background(),
		presenceStub{err: errors.New("cannot enumerate the bus")},
		nil)
	if res.State != capability.AcceleratorStateUnknown {
		t.Fatalf("State = %v, want unknown when the vendor scan itself failed", res.State)
	}
}

func TestMeasureAccelerators_ProbeResultsAreIdentityKeyed(t *testing.T) {
	// Two probes report the same physical device (a machine with both the
	// vendor tool and a generic runtime installed). Identity keying collapses
	// them into one; positional accumulation would double-count the memory.
	card := fixtures.SingleAccelerator().Accelerators[0]
	res := capability.MeasureAcceleratorsWith(context.Background(),
		presenceStub{present: map[capability.AcceleratorVendor]bool{capability.VendorNVIDIA: true}},
		[]capability.AcceleratorProbe{
			probeStub{vendor: capability.VendorNVIDIA, available: true, devices: []capability.Accelerator{card}},
			probeStub{vendor: capability.VendorNVIDIA, available: true, devices: []capability.Accelerator{card}},
		})

	if res.State != capability.AcceleratorStateMeasured {
		t.Fatalf("State = %v, want measured", res.State)
	}
	if len(res.Devices) != 1 {
		t.Fatalf("Devices = %d, want 1: the same identity reported twice is one device", len(res.Devices))
	}
}

// The live-hardware path. This machine either has an accelerator or does not;
// either way measurement must reach a MEASURED state, because both answers are
// findings. Only a genuine inability to determine the state is Unknown, and
// that must come with a stated gap rather than silence.
func TestMeasureAccelerators_OnThisMachine(t *testing.T) {
	res, err := capability.MeasureAccelerators(context.Background())
	if err != nil && !errors.Is(err, capability.ErrPlatformUnsupported) {
		t.Fatalf("MeasureAccelerators(): %v", err)
	}
	t.Logf("state=%s devices=%d gaps=%v", res.State, len(res.Devices), res.Gaps)
	for _, d := range res.Devices {
		t.Logf("  identity=%q model=%q api=%s total=%d available=%d", d.Identity, d.Model, d.API, d.MemoryTotal, d.MemoryAvailable)
		if d.Identity == "" {
			t.Error("a measured device carries no stable identity; it cannot be bound")
		}
		if !d.API.Known() {
			t.Errorf("device %s reports no acceleration API", d.Identity)
		}
		if d.MemoryTotal == 0 {
			t.Errorf("device %s reports zero total memory", d.Identity)
		}
		if d.MemoryAvailable > d.MemoryTotal {
			t.Errorf("device %s: available %d exceeds total %d", d.Identity, d.MemoryAvailable, d.MemoryTotal)
		}
	}
	if res.State == capability.AcceleratorStateUnknown && len(res.Gaps) == 0 {
		t.Error("state is unknown but no gap was named; an unknown must say what it could not determine")
	}
	// Whatever was measured must survive the profile's own validation.
	if res.State == capability.AcceleratorStateMeasured {
		if err := profileFromResult(res).ValidateForSelection(); err != nil {
			t.Errorf("a measured accelerator set failed profile validation: %v", err)
		}
	}
}

// --- test doubles (unit-test scope only) ---

type presenceStub struct {
	present map[capability.AcceleratorVendor]bool
	err     error
}

func (p presenceStub) AcceleratorVendorsPresent(context.Context) (map[capability.AcceleratorVendor]bool, error) {
	return p.present, p.err
}

type probeStub struct {
	vendor    capability.AcceleratorVendor
	available bool
	devices   []capability.Accelerator
	err       error
}

func (p probeStub) Vendor() capability.AcceleratorVendor { return p.vendor }
func (p probeStub) Available(context.Context) bool       { return p.available }
func (p probeStub) Probe(context.Context) ([]capability.Accelerator, error) {
	return p.devices, p.err
}

// profileFromResult builds the minimum well-formed profile around an
// accelerator measurement so the result can be checked through the same
// accessors selection will use.
func profileFromResult(res capability.AcceleratorMeasurementResult) capability.HostCapabilityProfile {
	p := fixtures.NoAccelerator()
	p.AcceleratorState = res.State
	p.Accelerators = res.Devices
	p.MeasurementComplete = res.State == capability.AcceleratorStateMeasured
	return p
}
