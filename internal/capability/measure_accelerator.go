package capability

import (
	"context"
	"fmt"
	"slices"
)

// Accelerator measurement, and the one rule that governs all of it: a device is
// reached by its STABLE IDENTITY, never by where it happened to land in an
// enumeration (§11.4.111).
//
// Enumeration order is not a property of the machine. It changes when a second
// card is added, when boot order shifts, when a driver is upgraded, when one
// device is reset. Code that bound to "the first device" then silently points
// at a different piece of hardware — and it keeps working, on the wrong device,
// which is why this defect is so quiet. So this file canonicalises: whatever
// order a probe returns, the set is keyed and ordered by identity, and even
// Devices()[0] means the same physical card across every enumeration.

// AcceleratorVendor names an acceleration stack. It exists so that "this vendor
// has hardware here" and "this vendor's tooling can be interrogated" are two
// separate questions — the gap between them is exactly where a measurement must
// answer "unknown" rather than "none".
type AcceleratorVendor string

// The vendors measurement knows how to reason about.
const (
	VendorNVIDIA AcceleratorVendor = "nvidia"
	VendorAMD    AcceleratorVendor = "amd"
	VendorApple  AcceleratorVendor = "apple"
)

// AcceleratorProbe interrogates one vendor's acceleration stack.
//
// Available and Probe are separate calls because a missing vendor tool is not
// an error — it is an absence of information, and the difference decides
// whether the measurement is a finding or a gap.
type AcceleratorProbe interface {
	// Vendor is the stack this probe reads.
	Vendor() AcceleratorVendor
	// Available reports whether this probe's tooling is present and usable.
	Available(ctx context.Context) bool
	// Probe enumerates the vendor's devices. Each returned device must carry
	// its stable identity; a probe that cannot read an identity must return an
	// error rather than a device that cannot be bound.
	Probe(ctx context.Context) ([]Accelerator, error)
}

// VendorPresence reports which accelerator vendors have hardware attached,
// independent of whether that vendor's tooling is installed.
//
// This is the half that makes an honest "none" possible. Without it, a host
// with a card and no driver tooling is indistinguishable from a host with no
// card at all, and measurement would have to guess.
type VendorPresence interface {
	AcceleratorVendorsPresent(ctx context.Context) (map[AcceleratorVendor]bool, error)
}

// AcceleratorMeasurementResult is one accelerator reading.
//
// State is the load-bearing field: Devices is only meaningful once State says
// the enumeration completed. Gaps names what could not be determined, so an
// unknown state is never silent.
type AcceleratorMeasurementResult struct {
	State   AcceleratorMeasurement
	Devices []Accelerator
	Gaps    []string
}

// AcceleratorSet is a set of measured devices keyed by stable identity.
//
// It has no index-based accessor by design. Devices and Identities return
// identity-sorted views, so a caller that does reach for the first element
// still gets the same physical device on every enumeration order.
type AcceleratorSet struct {
	byIdentity map[DeviceIdentity]Accelerator
	order      []DeviceIdentity
}

// NewAcceleratorSet canonicalises an enumeration into an identity-keyed set.
//
// The input order is discarded on purpose. A device without an identity, or
// two devices sharing one, are rejected rather than accepted and disambiguated
// by position — position is precisely what must not become load-bearing.
func NewAcceleratorSet(devices []Accelerator) (AcceleratorSet, error) {
	set := AcceleratorSet{byIdentity: make(map[DeviceIdentity]Accelerator, len(devices))}
	for _, d := range devices {
		if d.Identity == "" {
			return AcceleratorSet{}, fmt.Errorf("%w: model %q", ErrAcceleratorNoIdentity, d.Model)
		}
		if _, dup := set.byIdentity[d.Identity]; dup {
			return AcceleratorSet{}, fmt.Errorf("%w: %s", ErrAcceleratorDuplicateID, d.Identity)
		}
		set.byIdentity[d.Identity] = d
		set.order = append(set.order, d.Identity)
	}
	slices.Sort(set.order)
	return set, nil
}

// ByIdentity resolves a device by its stable identity. This is the only way to
// reach a specific device, and it answers the same on every enumeration order.
func (s AcceleratorSet) ByIdentity(id DeviceIdentity) (Accelerator, bool) {
	d, ok := s.byIdentity[id]
	return d, ok
}

// Devices returns every measured device in canonical identity order.
//
// The slice is freshly allocated: a caller that mutates what it receives cannot
// re-point a binding inside the set.
func (s AcceleratorSet) Devices() []Accelerator {
	out := make([]Accelerator, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.byIdentity[id])
	}
	return out
}

// Identities returns the device identities in canonical order.
func (s AcceleratorSet) Identities() []DeviceIdentity {
	return slices.Clone(s.order)
}

// Len reports how many devices the set holds.
func (s AcceleratorSet) Len() int { return len(s.order) }

// MeasureAccelerators enumerates this host's acceleration devices using the
// platform's own presence scan and probes.
func MeasureAccelerators(ctx context.Context) (AcceleratorMeasurementResult, error) {
	presence, probes, err := platformAcceleratorSources()
	if err != nil {
		return AcceleratorMeasurementResult{
			State: AcceleratorStateUnknown,
			Gaps:  []string{err.Error()},
		}, err
	}
	return MeasureAcceleratorsWith(ctx, presence, probes), nil
}

// MeasureAcceleratorsWith performs the measurement against the given presence
// scan and probes.
//
// The decision it encodes is the whole point of the accelerator axis:
//
//   - presence scan failed              -> unknown (we know nothing)
//   - a present vendor has no live probe -> unknown (a device exists that we
//     cannot read; reporting zero here would invent a CPU-only host)
//   - a probe errored                   -> unknown (partial truth is not truth)
//   - everything covered, zero devices  -> MEASURED, and that zero is a finding
//
// The last line is the one that has to be deliberate: a host with no
// accelerator is a first-class, fully serviceable host, and it must be
// distinguishable from a host we simply failed to interrogate (FR-002/FR-056).
func MeasureAcceleratorsWith(ctx context.Context, presence VendorPresence, probes []AcceleratorProbe) AcceleratorMeasurementResult {
	var gaps []string

	if presence == nil {
		return AcceleratorMeasurementResult{
			State: AcceleratorStateUnknown,
			Gaps:  []string{"no accelerator-presence scan available on this platform"},
		}
	}
	present, err := presence.AcceleratorVendorsPresent(ctx)
	if err != nil {
		return AcceleratorMeasurementResult{
			State: AcceleratorStateUnknown,
			Gaps:  []string{fmt.Sprintf("accelerator-presence scan failed: %v", err)},
		}
	}

	// Index the live probes by vendor so coverage can be checked against what
	// the presence scan actually found.
	live := map[AcceleratorVendor][]AcceleratorProbe{}
	for _, p := range probes {
		if p != nil && p.Available(ctx) {
			live[p.Vendor()] = append(live[p.Vendor()], p)
		}
	}

	var found []Accelerator
	for vendor, isPresent := range present {
		if !isPresent {
			continue
		}
		vendorProbes := live[vendor]
		if len(vendorProbes) == 0 {
			gaps = append(gaps, fmt.Sprintf("%s hardware is present but no usable %s probe is installed", vendor, vendor))
			continue
		}
		for _, p := range vendorProbes {
			devices, err := p.Probe(ctx)
			if err != nil {
				gaps = append(gaps, fmt.Sprintf("%s probe failed: %v", vendor, err))
				continue
			}
			found = append(found, devices...)
		}
	}

	// Canonicalise. Two probes reporting the same physical device collapse to
	// one because they agree on its identity — positional accumulation would
	// have double-counted its memory.
	set, err := newAcceleratorSetMerging(found)
	if err != nil {
		gaps = append(gaps, fmt.Sprintf("device enumeration is not bindable: %v", err))
	}

	if len(gaps) > 0 {
		// Partial knowledge is not knowledge. Devices are withheld so no
		// caller can act on half a reading (FR-056).
		slices.Sort(gaps)
		return AcceleratorMeasurementResult{State: AcceleratorStateUnknown, Gaps: gaps}
	}
	return AcceleratorMeasurementResult{
		State:   AcceleratorStateMeasured,
		Devices: set.Devices(),
	}
}

// newAcceleratorSetMerging builds a set from an enumeration in which the same
// device may legitimately appear more than once — two probes reading one
// machine. Identical repeats merge; a genuine conflict on one identity is an
// error, because then the identity no longer identifies anything.
func newAcceleratorSetMerging(devices []Accelerator) (AcceleratorSet, error) {
	set := AcceleratorSet{byIdentity: make(map[DeviceIdentity]Accelerator, len(devices))}
	for _, d := range devices {
		if d.Identity == "" {
			return AcceleratorSet{}, fmt.Errorf("%w: model %q", ErrAcceleratorNoIdentity, d.Model)
		}
		if existing, seen := set.byIdentity[d.Identity]; seen {
			if existing != d {
				return AcceleratorSet{}, fmt.Errorf("%w: %s reported with conflicting details", ErrAcceleratorDuplicateID, d.Identity)
			}
			continue
		}
		set.byIdentity[d.Identity] = d
		set.order = append(set.order, d.Identity)
	}
	slices.Sort(set.order)
	return set, nil
}
