package telemetry

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
)

func testTime() time.Time { return time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC) }

func validModelUsage() ModelUsage {
	return ModelUsage{
		ModelID:        "qwen2.5-coder-7b",
		HostMemoryUsed: 6 * capability.GiB,
		Accelerators: []AcceleratorUsage{{
			Device:     capability.DeviceIdentity("GPU-8f3c"),
			API:        capability.APICUDA,
			MemoryUsed: 5 * capability.GiB,
		}},
		ObservedAt: testTime(),
	}
}

func validHostUsage() HostUsage {
	return HostUsage{
		HostIdentity:    "builder-01",
		MemoryTotal:     64 * capability.GiB,
		MemoryAvailable: 20 * capability.GiB,
		Devices: []DeviceHeadroom{{
			Device:          capability.DeviceIdentity("GPU-8f3c"),
			API:             capability.APICUDA,
			MemoryTotal:     24 * capability.GiB,
			MemoryAvailable: 19 * capability.GiB,
		}},
		Swap:       SwapQuiet,
		ObservedAt: testTime(),
	}
}

// FR-030: a model's memory and accelerator consumption is tracked while it
// serves, and can be read back exactly as observed.
func TestObserveModel_TracksMemoryAndAcceleratorUse(t *testing.T) {
	r := NewRegistry()
	want := validModelUsage()
	if err := r.ObserveModel(want); err != nil {
		t.Fatalf("ObserveModel: %v", err)
	}

	got, ok := r.ModelUsage("qwen2.5-coder-7b")
	if !ok {
		t.Fatal("model was observed but is not tracked")
	}
	if got.HostMemoryUsed != want.HostMemoryUsed {
		t.Errorf("host memory = %d, want %d", got.HostMemoryUsed, want.HostMemoryUsed)
	}
	if len(got.Accelerators) != 1 {
		t.Fatalf("accelerators = %d, want 1", len(got.Accelerators))
	}
	if got.Accelerators[0].Device != capability.DeviceIdentity("GPU-8f3c") {
		t.Errorf("device = %q, want GPU-8f3c", got.Accelerators[0].Device)
	}
	if got.Accelerators[0].MemoryUsed != 5*capability.GiB {
		t.Errorf("device memory = %d, want %d", got.Accelerators[0].MemoryUsed, 5*capability.GiB)
	}
	if got.AcceleratorMemoryUsed() != 5*capability.GiB {
		t.Errorf("total accelerator memory = %d, want %d", got.AcceleratorMemoryUsed(), 5*capability.GiB)
	}
}

// A reading the caller can mutate after handing it over, or after reading it
// back, would let one goroutine rewrite another's observation.
func TestObserveModel_DoesNotShareTheAcceleratorSliceWithCallers(t *testing.T) {
	r := NewRegistry()
	in := validModelUsage()
	if err := r.ObserveModel(in); err != nil {
		t.Fatalf("ObserveModel: %v", err)
	}

	in.Accelerators[0].MemoryUsed = 999
	got, _ := r.ModelUsage("qwen2.5-coder-7b")
	if got.Accelerators[0].MemoryUsed != 5*capability.GiB {
		t.Fatal("mutating the submitted slice changed the stored observation")
	}

	got.Accelerators[0].MemoryUsed = 111
	again, _ := r.ModelUsage("qwen2.5-coder-7b")
	if again.Accelerators[0].MemoryUsed != 5*capability.GiB {
		t.Fatal("mutating a read-back slice changed the stored observation")
	}
}

func TestObserveModel_RejectsUnusableObservations(t *testing.T) {
	dupDevice := validModelUsage()
	dupDevice.Accelerators = append(dupDevice.Accelerators, dupDevice.Accelerators[0])

	noIdentity := validModelUsage()
	noIdentity.Accelerators[0].Device = ""

	unknownAPI := validModelUsage()
	unknownAPI.Accelerators[0].API = capability.APIUnknown

	noTime := validModelUsage()
	noTime.ObservedAt = time.Time{}

	noID := validModelUsage()
	noID.ModelID = ""

	cases := []struct {
		name string
		in   ModelUsage
		want error
	}{
		{"no model id", noID, ErrNoModelID},
		{"undated", noTime, ErrNoObservationTime},
		{"device without identity", noIdentity, ErrDeviceNoIdentity},
		{"same device twice", dupDevice, ErrDeviceRepeated},
		{"device without an API", unknownAPI, ErrDeviceUnknownAPI},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := NewRegistry().ObserveModel(tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// FR-030: the serving host's remaining headroom and whether it is swapping.
func TestObserveHost_TracksHeadroomAndSwapping(t *testing.T) {
	r := NewRegistry()
	if err := r.ObserveHost(validHostUsage()); err != nil {
		t.Fatalf("ObserveHost: %v", err)
	}
	got, ok := r.HostUsage()
	if !ok {
		t.Fatal("host was observed but is not tracked")
	}
	if got.MemoryAvailable != 20*capability.GiB {
		t.Errorf("headroom = %d, want %d", got.MemoryAvailable, 20*capability.GiB)
	}
	if got.Swap != SwapQuiet {
		t.Errorf("swap = %v, want quiet", got.Swap)
	}
	if len(got.Devices) != 1 || got.Devices[0].MemoryAvailable != 19*capability.GiB {
		t.Errorf("device headroom = %+v", got.Devices)
	}
}

// "We could not tell" must never read as "it is not swapping" — the same
// distinction capability.AcceleratorMeasurement draws for device enumeration.
func TestSwapUnknown_IsNotTheSameAsNotSwapping(t *testing.T) {
	if SwapUnknown.Known() {
		t.Error("SwapUnknown reports itself as a determined state")
	}
	if !SwapQuiet.Known() || !SwapActive.Known() {
		t.Error("a determined swap state reports itself as unknown")
	}
	if SwapUnknown == SwapQuiet {
		t.Error("unknown and quiet are the same value")
	}
	if SwapUnknown.String() == SwapQuiet.String() {
		t.Error("unknown and quiet render identically")
	}
}

func TestObserveHost_RejectsUnusableObservations(t *testing.T) {
	noID := validHostUsage()
	noID.HostIdentity = ""

	noTime := validHostUsage()
	noTime.ObservedAt = time.Time{}

	overAvail := validHostUsage()
	overAvail.MemoryAvailable = overAvail.MemoryTotal + 1

	dupDevice := validHostUsage()
	dupDevice.Devices = append(dupDevice.Devices, dupDevice.Devices[0])

	// An undetermined swap state that nonetheless carries a figure is a
	// contradiction: the figure implies the reading succeeded.
	unknownWithFigure := validHostUsage()
	unknownWithFigure.Swap = SwapUnknown
	unknownWithFigure.SwapUsed = 1 * capability.MiB

	cases := []struct {
		name string
		in   HostUsage
		want error
	}{
		{"no host id", noID, ErrNoHostIdentity},
		{"undated", noTime, ErrNoObservationTime},
		{"headroom above total", overAvail, ErrMemoryAvailableExceeds},
		{"same device twice", dupDevice, ErrDeviceRepeated},
		{"unknown swap carrying a figure", unknownWithFigure, ErrSwapUnknownHasFigure},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := NewRegistry().ObserveHost(tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// FR-033, serving side: every reading is dated, so its age is knowable and a
// stale figure can be seen to be stale rather than silently believed.
func TestObservations_CarryAKnowableAge(t *testing.T) {
	u := validModelUsage()
	if got := u.Age(testTime().Add(90 * time.Second)); got != 90*time.Second {
		t.Errorf("model observation age = %s, want 90s", got)
	}
	h := validHostUsage()
	if got := h.Age(testTime().Add(2 * time.Minute)); got != 2*time.Minute {
		t.Errorf("host observation age = %s, want 2m", got)
	}
}

func TestForget_StopsTrackingAnUnloadedModel(t *testing.T) {
	r := NewRegistry()
	if err := r.ObserveModel(validModelUsage()); err != nil {
		t.Fatalf("ObserveModel: %v", err)
	}
	r.Forget("qwen2.5-coder-7b")
	if _, ok := r.ModelUsage("qwen2.5-coder-7b"); ok {
		t.Fatal("a forgotten model is still tracked")
	}
}

func TestSnapshot_IsOrderedAndComplete(t *testing.T) {
	r := NewRegistry()
	for _, id := range []string{"zeta", "alpha", "mid"} {
		u := validModelUsage()
		u.ModelID = id
		if err := r.ObserveModel(u); err != nil {
			t.Fatalf("ObserveModel(%s): %v", id, err)
		}
	}
	if err := r.ObserveHost(validHostUsage()); err != nil {
		t.Fatalf("ObserveHost: %v", err)
	}

	snap := r.Snapshot()
	if !snap.HostKnown {
		t.Error("snapshot does not know the host")
	}
	want := []string{"alpha", "mid", "zeta"}
	if len(snap.Models) != len(want) {
		t.Fatalf("models = %d, want %d", len(snap.Models), len(want))
	}
	for i, id := range want {
		if snap.Models[i].ModelID != id {
			t.Fatalf("models[%d] = %s, want %s", i, snap.Models[i].ModelID, id)
		}
	}
}

// A registry with no host observation must say so rather than report an
// all-zero host, which would read as "no memory left, not swapping".
func TestSnapshot_AbsentHostIsAnAbsenceNotAZeroReading(t *testing.T) {
	snap := NewRegistry().Snapshot()
	if snap.HostKnown {
		t.Fatal("an unobserved host is reported as known")
	}
	if len(snap.Models) != 0 {
		t.Fatalf("models = %d, want 0", len(snap.Models))
	}
}

// Serving is concurrent by nature: samplers write while readers render.
func TestRegistry_ConcurrentObserveAndRead(t *testing.T) {
	r := NewRegistry()
	const goroutines, iterations = 16, 200

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			u := validModelUsage()
			u.ModelID = string(rune('a'+g%4)) + "-model"
			for i := 0; i < iterations; i++ {
				u.HostMemoryUsed = capability.Bytes(i) * capability.MiB
				u.ObservedAt = testTime().Add(time.Duration(i) * time.Second)
				if err := r.ObserveModel(u); err != nil {
					t.Errorf("ObserveModel: %v", err)
					return
				}
				h := validHostUsage()
				h.ObservedAt = u.ObservedAt
				if err := r.ObserveHost(h); err != nil {
					t.Errorf("ObserveHost: %v", err)
					return
				}
			}
		}(g)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				snap := r.Snapshot()
				for _, m := range snap.Models {
					_ = m.AcceleratorMemoryUsed()
				}
				_, _ = r.HostUsage()
			}
		}()
	}
	wg.Wait()

	if got := len(r.Snapshot().Models); got != 4 {
		t.Fatalf("models = %d, want 4", got)
	}
}
