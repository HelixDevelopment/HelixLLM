package selection

import (
	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
)

// Reserve is what a selection holds back so the host stays usable while the
// selected model serves requests.
//
// It is a stated policy carried in the request rather than a constant buried in
// the check, because the two fractions come from different places and a reader
// is entitled to see which:
//
//   - MemoryFraction is SC-003's threshold: while a selected model serves a
//     typical request, the host retains at least this share of its memory free.
//   - StorageFraction is the guard for FR-007 — refuse a selection that would
//     exhaust host resources. Filling the disk to its last byte exhausts it, so
//     a share of free storage is held back too. The value is a project policy
//     choice, not a measured threshold, and it is exposed here so a caller can
//     state a different one rather than discover this one by its effects.
type Reserve struct {
	MemoryFraction  float64
	StorageFraction float64
}

// Reserve fractions. MemoryFraction is SC-003's 15%. StorageFraction is the
// project's stated FR-007 exhaustion guard.
const (
	defaultMemoryReserveFraction  = 0.15
	defaultStorageReserveFraction = 0.05
)

// DefaultReserve is the reserve applied when a request states none.
func DefaultReserve() Reserve {
	return Reserve{
		MemoryFraction:  defaultMemoryReserveFraction,
		StorageFraction: defaultStorageReserveFraction,
	}
}

// Zero reports whether r holds nothing back on either axis.
func (r Reserve) Zero() bool { return r.MemoryFraction == 0 && r.StorageFraction == 0 }

// MemoryReserve is the share of the host's nameplate memory total held back.
// SC-003 is expressed against the total, not against what happens to be free,
// so a host already under pressure does not get a smaller reserve.
func (r Reserve) MemoryReserve(total capability.Bytes) capability.Bytes {
	return fraction(total, r.MemoryFraction)
}

// StorageReserve is the share of free storage held back. Storage is reserved
// against what is free because a full disk's total says nothing about what a
// download may consume.
func (r Reserve) StorageReserve(available capability.Bytes) capability.Bytes {
	return fraction(available, r.StorageFraction)
}

func fraction(of capability.Bytes, f float64) capability.Bytes {
	if f <= 0 || of == 0 {
		return 0
	}
	if f >= 1 {
		return of
	}
	return capability.Bytes(float64(of) * f)
}

// axis is one measured dimension an option is checked against. Memory and
// storage are evaluated through the same shape but never through the same
// value: a single combined figure cannot report which of the two was short,
// and that report is the whole of the remedy (D2).
type axis struct {
	resource  Resource
	required  uint64
	available uint64
	reserved  uint64
}

// short reports whether the requirement exceeds what is available after the
// reserve, and by how much.
func (a axis) short() (Shortfall, bool) {
	usable := uint64(0)
	if a.available > a.reserved {
		usable = a.available - a.reserved
	}
	if a.required <= usable {
		return Shortfall{}, false
	}
	return Shortfall{
		Resource:       a.resource,
		RequiredBytes:  a.required,
		AvailableBytes: usable,
		ReservedBytes:  a.reserved,
	}, true
}

// remaining is what the axis has left once the option is served, before the
// reserve is added back — the honest "what is left on the machine" figure.
func (a axis) remaining() uint64 {
	if a.available <= a.required {
		return 0
	}
	return a.available - a.required
}

// memoryAxis and storageAxis derive the two axes from the measurement that was
// passed to this call. They read the profile handed in and nothing cached: a
// selection reflects the reading it was given, so a caller that re-measures
// gets a re-evaluated answer (FR-006, FR-033).
func memoryAxis(p capability.HostCapabilityProfile, e catalogue.Entry, r Reserve) axis {
	return axis{
		resource:  ResourceMemory,
		required:  e.MemoryRequiredBytes,
		available: uint64(p.MemoryAvailable),
		reserved:  uint64(r.MemoryReserve(p.MemoryTotal)),
	}
}

func storageAxis(p capability.HostCapabilityProfile, e catalogue.Entry, r Reserve) axis {
	return axis{
		resource:  ResourceStorage,
		required:  e.StorageRequiredBytes,
		available: uint64(p.StorageAvailable),
		reserved:  uint64(r.StorageReserve(p.StorageAvailable)),
	}
}

// fits evaluates both axes SEPARATELY and returns the first shortfall found,
// memory before storage.
//
// The separation is the point. The low-storage fixture host fits a model's
// memory more than twenty times over and does not fit its disk at all: a check
// that combines the axes, or that returns early on memory alone, reports the
// wrong resource and sends the user to fix something that is not broken.
func fits(p capability.HostCapabilityProfile, e catalogue.Entry, r Reserve) (Headroom, *Shortfall) {
	mem := memoryAxis(p, e, r)
	store := storageAxis(p, e, r)

	if s, isShort := mem.short(); isShort {
		return Headroom{}, &s
	}
	if s, isShort := store.short(); isShort {
		return Headroom{}, &s
	}

	memRemaining := mem.remaining()
	headroom := Headroom{
		MemoryRemainingBytes:  memRemaining,
		StorageRemainingBytes: store.remaining(),
	}
	if p.MemoryTotal > 0 {
		headroom.MemoryRemainingFraction = float64(memRemaining) / float64(p.MemoryTotal)
	}
	return headroom, nil
}

// supports reports the configuration requirement this host does not meet, or
// nil when it meets them all.
//
// This is a different question from fit and produces a different reason: a
// missing accelerator or a missing runtime path is not a quantity the host is
// short of, and no amount of free memory resolves it.
func supports(p capability.HostCapabilityProfile, e catalogue.Entry) *Unsupported {
	if e.RequiresAccelerator && len(p.Accelerators) == 0 {
		return &Unsupported{
			Requirement: RequirementAccelerator,
			Detail:      p.AcceleratorState.String(),
		}
	}
	// Streaming eligibility is roster membership, and only roster membership.
	// Architecture is deliberately not consulted: mixture-of-experts models
	// exist that the streaming runtime does not support, and inferring
	// eligibility from architecture offers them anyway — an offer that fails at
	// load time instead of here (D1).
	if e.Runtime == catalogue.RuntimeStreaming && !e.StreamingEligible() {
		return &Unsupported{
			Requirement: RequirementStreamingRoster,
			Detail:      e.StreamingRoster.FamilyName,
		}
	}
	return nil
}
