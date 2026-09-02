package telemetry

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
)

// Per-model and per-host resource use while serving (FR-030).
//
// This file answers two questions continuously, for as long as models are
// loaded: what is each running model consuming, and what does the host have
// left. They are separate readings on purpose — a model's own footprint does
// not tell you whether the machine around it is still comfortable.
//
// The vocabulary is capability's, not a second one. Bytes, DeviceIdentity and
// AccelerationAPI already mean something precise in this codebase and a model's
// consumption is measured in the same units its host was measured in; inventing
// a parallel set here would let the two drift apart (§11.4.74).
//
// Two honesty invariants carry over from measurement, because the failure they
// prevent is the same:
//
//   - A device is named by its stable identity, never by an enumeration index
//     (§11.4.111). An index re-points the moment a second card appears.
//   - "Undetermined" is a distinct value from "determined to be zero".
//     capability draws that line for accelerator enumeration; SwapState draws
//     it for swapping, because a host whose swap state could not be read must
//     not be reported as a host that is not swapping.
//
// Every reading is dated. That is the serving-side half of FR-033: the
// measurement side decides whether a host profile is current enough to justify
// a refusal (capability.FreshnessPolicy owns that policy and this package does
// not restate it); this side simply guarantees that no figure about a running
// model is ever undated, so its age is always knowable to whoever reads it.

// Observation errors. Callers switch on these with errors.Is.
var (
	// ErrNoModelID is returned for an observation that names no model. An
	// unattributed reading cannot be shown to a user or compared over time.
	ErrNoModelID = errors.New("telemetry: observation names no model")

	// ErrNoHostIdentity is returned for a host observation that names no host.
	ErrNoHostIdentity = errors.New("telemetry: observation names no host")

	// ErrNoObservationTime is returned for an undated reading. An undated
	// figure can never be shown to be current (FR-033).
	ErrNoObservationTime = errors.New("telemetry: observation carries no time, so its age is unknowable")

	// ErrDeviceNoIdentity is returned when accelerator use is attributed to a
	// device with no stable identity (§11.4.111).
	ErrDeviceNoIdentity = errors.New("telemetry: accelerator usage carries no stable device identity")

	// ErrDeviceRepeated is returned when one observation reports the same
	// device twice, which would double-count that device's memory.
	ErrDeviceRepeated = errors.New("telemetry: one observation reports the same device twice")

	// ErrDeviceUnknownAPI is returned when accelerator use names no
	// acceleration interface.
	ErrDeviceUnknownAPI = errors.New("telemetry: accelerator usage reports no acceleration API")

	// ErrDeviceMemoryExceeds is returned when a device reports more memory
	// available than it has.
	ErrDeviceMemoryExceeds = errors.New("telemetry: device available memory exceeds its total")

	// ErrMemoryAvailableExceeds is returned when a host reports more memory
	// available than it has.
	ErrMemoryAvailableExceeds = errors.New("telemetry: host available memory exceeds total memory")

	// ErrSwapUnknownHasFigure is returned when a host's swap state is
	// undetermined yet a swap figure is supplied. The figure implies the
	// reading succeeded, so one of the two is wrong; guessing which would turn
	// an undetermined state into a fabricated one.
	ErrSwapUnknownHasFigure = errors.New("telemetry: swap state is undetermined yet a swap figure is reported")
)

// SwapState is whether the serving host is paging to disk (FR-030).
//
// It is three-valued for the same reason capability.AcceleratorMeasurement is:
// a host whose swap state could not be read is not a host that is not swapping.
// The zero value is the undetermined one, so a struct built without setting it
// cannot accidentally assert the good news.
type SwapState uint8

const (
	// SwapUnknown means the swap state was not determined. It MUST NOT be read
	// as "not swapping".
	SwapUnknown SwapState = iota
	// SwapQuiet means the host was read and is not paging.
	SwapQuiet
	// SwapActive means the host was read and is paging. A model that fits
	// "on paper" while the host swaps is the case FR-030 exists to surface.
	SwapActive
)

// Known reports whether the swap state was actually determined.
func (s SwapState) Known() bool { return s == SwapQuiet || s == SwapActive }

// String renders the state as a stable machine token, not a sentence. The
// user-facing wording is resolved from a message key by the presentation layer
// (CONST-046).
func (s SwapState) String() string {
	switch s {
	case SwapQuiet:
		return "quiet"
	case SwapActive:
		return "active"
	case SwapUnknown:
		return "unknown"
	default:
		return fmt.Sprintf("SwapState(%d)", uint8(s))
	}
}

// AcceleratorUsage is what one running model consumes on one device.
type AcceleratorUsage struct {
	// Device is the stable device identity, never an enumeration index
	// (§11.4.111).
	Device capability.DeviceIdentity
	// API is the acceleration interface the model is served through.
	API capability.AccelerationAPI
	// MemoryUsed is device memory this model holds.
	MemoryUsed capability.Bytes
}

// ModelUsage is one running model's consumption at one instant.
type ModelUsage struct {
	// ModelID is the model this reading is about.
	ModelID string
	// HostMemoryUsed is system memory the model holds, separate from any
	// device memory — a partially offloaded model holds both.
	HostMemoryUsed capability.Bytes
	// Accelerators is the model's per-device consumption, one entry per
	// device it occupies. Empty is valid and first-class: a CPU-served model
	// consumes no device memory.
	Accelerators []AcceleratorUsage
	// ObservedAt is when this reading was taken.
	ObservedAt time.Time
}

// AcceleratorMemoryUsed is the model's total device memory across every device
// it occupies.
func (u ModelUsage) AcceleratorMemoryUsed() capability.Bytes {
	var total capability.Bytes
	for _, a := range u.Accelerators {
		total += a.MemoryUsed
	}
	return total
}

// Age is how old this reading is at now. A negative age means the reading is
// stamped ahead of now and its true age is unknowable.
func (u ModelUsage) Age(now time.Time) time.Duration { return now.Sub(u.ObservedAt) }

// Validate reports whether the reading can be trusted as an observation.
func (u ModelUsage) Validate() error {
	if u.ModelID == "" {
		return ErrNoModelID
	}
	if u.ObservedAt.IsZero() {
		return fmt.Errorf("%w: model=%s", ErrNoObservationTime, u.ModelID)
	}
	seen := make(map[capability.DeviceIdentity]struct{}, len(u.Accelerators))
	for _, a := range u.Accelerators {
		if a.Device == "" {
			return fmt.Errorf("%w: model=%s", ErrDeviceNoIdentity, u.ModelID)
		}
		if _, dup := seen[a.Device]; dup {
			return fmt.Errorf("%w: model=%s device=%s", ErrDeviceRepeated, u.ModelID, a.Device)
		}
		seen[a.Device] = struct{}{}
		if !a.API.Known() {
			return fmt.Errorf("%w: model=%s device=%s", ErrDeviceUnknownAPI, u.ModelID, a.Device)
		}
	}
	return nil
}

// clone returns a copy that shares no memory with u, so a reading cannot be
// rewritten through a slice one side kept a reference to.
func (u ModelUsage) clone() ModelUsage {
	out := u
	if u.Accelerators != nil {
		out.Accelerators = append([]AcceleratorUsage(nil), u.Accelerators...)
	}
	return out
}

// DeviceHeadroom is what one accelerator on the serving host has left.
type DeviceHeadroom struct {
	Device          capability.DeviceIdentity
	API             capability.AccelerationAPI
	MemoryTotal     capability.Bytes
	MemoryAvailable capability.Bytes
}

// HostUsage is the serving host's remaining headroom and swap state (FR-030).
type HostUsage struct {
	// HostIdentity is the machine this reading is about.
	HostIdentity string
	// MemoryTotal is nameplate system memory; MemoryAvailable is what is left
	// right now — the headroom FR-030 asks for.
	MemoryTotal     capability.Bytes
	MemoryAvailable capability.Bytes
	// Devices is per-accelerator headroom.
	Devices []DeviceHeadroom
	// Swap says whether the host is paging, or that this could not be read.
	Swap SwapState
	// SwapUsed is occupied swap. It is meaningful only when Swap is known.
	SwapUsed capability.Bytes
	// ObservedAt is when this reading was taken.
	ObservedAt time.Time
}

// Age is how old this reading is at now. A negative age means the reading is
// stamped ahead of now and its true age is unknowable.
func (h HostUsage) Age(now time.Time) time.Duration { return now.Sub(h.ObservedAt) }

// Validate reports whether the reading can be trusted as an observation.
func (h HostUsage) Validate() error {
	if h.HostIdentity == "" {
		return ErrNoHostIdentity
	}
	if h.ObservedAt.IsZero() {
		return fmt.Errorf("%w: host=%s", ErrNoObservationTime, h.HostIdentity)
	}
	if h.MemoryAvailable > h.MemoryTotal {
		return fmt.Errorf("%w: %d > %d", ErrMemoryAvailableExceeds, h.MemoryAvailable, h.MemoryTotal)
	}
	if !h.Swap.Known() && h.SwapUsed > 0 {
		return fmt.Errorf("%w: host=%s swap_used=%d", ErrSwapUnknownHasFigure, h.HostIdentity, h.SwapUsed)
	}
	seen := make(map[capability.DeviceIdentity]struct{}, len(h.Devices))
	for _, d := range h.Devices {
		if d.Device == "" {
			return fmt.Errorf("%w: host=%s", ErrDeviceNoIdentity, h.HostIdentity)
		}
		if _, dup := seen[d.Device]; dup {
			return fmt.Errorf("%w: host=%s device=%s", ErrDeviceRepeated, h.HostIdentity, d.Device)
		}
		seen[d.Device] = struct{}{}
		if !d.API.Known() {
			return fmt.Errorf("%w: host=%s device=%s", ErrDeviceUnknownAPI, h.HostIdentity, d.Device)
		}
		if d.MemoryAvailable > d.MemoryTotal {
			return fmt.Errorf("%w: host=%s device=%s: %d > %d",
				ErrDeviceMemoryExceeds, h.HostIdentity, d.Device, d.MemoryAvailable, d.MemoryTotal)
		}
	}
	return nil
}

func (h HostUsage) clone() HostUsage {
	out := h
	if h.Devices != nil {
		out.Devices = append([]DeviceHeadroom(nil), h.Devices...)
	}
	return out
}

// UsageSnapshot is every usage reading a Registry holds at one instant.
type UsageSnapshot struct {
	// HostKnown distinguishes "no host reading yet" from a host reading of
	// zero. Read it before reading Host.
	HostKnown bool
	Host      HostUsage
	// Models is one reading per tracked model, ordered by model id so a
	// rendering never depends on map iteration order.
	Models []ModelUsage
}

// Registry holds the latest usage reading for each running model and for the
// serving host.
//
// Sampling and rendering happen on different goroutines from each other and
// from serving, so all state is guarded by one mutex and every read hands back
// a copy — the same single-lock discipline the lifecycle manager uses, so the
// two packages do not present a reader with two concurrency styles.
//
// Registry is a store, not a sampler: it does not decide when a reading is
// taken. Whoever polls the host owns the cadence; this type owns keeping the
// latest reading coherent and dated.
type Registry struct {
	mu        sync.Mutex
	models    map[string]ModelUsage
	host      HostUsage
	hostKnown bool
}

// NewRegistry returns an empty Registry. Its zero state is an honest one: no
// models tracked and no host reading, rather than a host of zero bytes.
func NewRegistry() *Registry {
	return &Registry{models: make(map[string]ModelUsage)}
}

// ObserveModel records the latest reading for one running model, replacing any
// previous one. Readings are whole: a model's memory and its device use are
// updated together, so no reader ever sees half of one sample and half of the
// next.
func (r *Registry) ObserveModel(u ModelUsage) error {
	if err := u.Validate(); err != nil {
		return err
	}
	stored := u.clone()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.models[u.ModelID] = stored
	return nil
}

// ObserveHost records the latest reading for the serving host.
func (r *Registry) ObserveHost(h HostUsage) error {
	if err := h.Validate(); err != nil {
		return err
	}
	stored := h.clone()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.host = stored
	r.hostKnown = true
	return nil
}

// ModelUsage returns the latest reading for modelID. The second result is
// false when no reading has been recorded, which is an absence of information
// rather than a reading of zero.
func (r *Registry) ModelUsage(modelID string) (ModelUsage, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.models[modelID]
	if !ok {
		return ModelUsage{}, false
	}
	return u.clone(), true
}

// HostUsage returns the latest host reading. The second result is false when
// no reading has been recorded.
func (r *Registry) HostUsage() (HostUsage, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.hostKnown {
		return HostUsage{}, false
	}
	return r.host.clone(), true
}

// Forget drops a model's readings. It is called when a model stops running —
// a model that has been unloaded must not keep appearing with the last figures
// it had while it was alive.
func (r *Registry) Forget(modelID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.models, modelID)
}

// Snapshot returns every reading currently held, ordered by model id.
func (r *Registry) Snapshot() UsageSnapshot {
	r.mu.Lock()
	models := make([]ModelUsage, 0, len(r.models))
	for _, u := range r.models {
		models = append(models, u.clone())
	}
	snap := UsageSnapshot{HostKnown: r.hostKnown}
	if r.hostKnown {
		snap.Host = r.host.clone()
	}
	r.mu.Unlock()

	sort.Slice(models, func(i, j int) bool { return models[i].ModelID < models[j].ModelID })
	snap.Models = models
	return snap
}
