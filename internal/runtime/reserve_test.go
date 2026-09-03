package runtime_test

// The offer and the admission must answer the same question.
//
// Selection decides what the user is SHOWN. The VRAM broker decides what may
// actually LOAD. When those two carry different margins on device memory there
// is a band of model sizes that selection offers and the broker then refuses:
// the user picks one and it does not start. The reproduction that opened this
// was a 10 GiB model on an 11.5 GiB card — offered, then refused, because
// selection held nothing back and the broker holds 2 GiB.
//
// WHAT THIS GUARDS, AND WHY IT IS NOT A COPY OF THE CONSTANT
//
// Asserting that selection's reserve equals 2 GiB would restate the broker's
// policy a third time and prove only that two literals were typed the same. It
// would keep passing if the broker's arithmetic changed shape — a strict
// comparison instead of an inclusive one, a second margin, a floor — because
// the number would still match while the DECISIONS diverged.
//
// So this drives BOTH REAL DECIDERS across the same boundary and requires them
// to agree at every point:
//
//   - the broker's own admission arithmetic, via [vrambroker.Admits]
//   - selection's actual offer, via [selection.Select] under the reserve the
//     boot binaries pass, [runtime.SelectionReserve]
//
// Every sampled point is derived from [vrambroker.HeadroomBytes], so raising or
// lowering that constant moves the fixtures with it and the property still
// holds — while leaving selection uninformed of the change makes the two
// answers differ somewhere in the band, and this fails.

import (
	"fmt"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/capability/testdata/fixtures"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/runtime"
	"github.com/HelixDevelopment/HelixLLM/internal/selection"
	"github.com/HelixDevelopment/HelixLLM/internal/vrambroker"
	"github.com/stretchr/testify/require"
)

// gpuNeedBytes is the model footprint the sweep is run at: a real video-model
// figure, large enough that the band under test is not an artefact of a tiny
// requirement.
const gpuNeedBytes int64 = 10 * 1024 * vrambroker.MiB

// cardWithFree is a measured host whose sole accelerator has exactly free bytes
// available. Host RAM and storage are abundant, so nothing but the device can
// decide the outcome — a refusal here names the card or the fixture is wrong.
func cardWithFree(free int64) capability.HostCapabilityProfile {
	p := fixtures.SingleAccelerator()
	p.HostIdentity = "fixture-agreement-host"
	p.MemoryTotal = 256 * capability.GiB
	p.MemoryAvailable = 200 * capability.GiB
	p.StorageAvailable = 4096 * capability.GiB
	p.Accelerators = []capability.Accelerator{{
		Identity:        capability.DeviceIdentity("GPU-agreement-0000"),
		Model:           "agreement fixture card",
		API:             capability.APICUDA,
		MemoryTotal:     capability.Bytes(free + 4*1024*vrambroker.MiB),
		MemoryAvailable: capability.Bytes(free),
	}}
	return p
}

// gpuEntry is a candidate that mandates a device and needs needBytes of it. For
// an accelerator-required entry the catalogue records its memory figure AS the
// device-memory requirement — the same number handed to the broker.
func gpuEntry(needBytes int64) catalogue.Entry {
	return catalogue.Entry{
		ModelID:              "agreement-candidate",
		Family:               catalogue.FamilyVideoGeneration,
		Architecture:         catalogue.ArchitectureDiffusion,
		MemoryRequiredBytes:  uint64(needBytes),
		StorageRequiredBytes: 8 * uint64(capability.GiB),
		RequiresAccelerator:  true,
		Runtime:              catalogue.RuntimeInMemory,
		UsageTerms: catalogue.UsageTerms{
			LicenseID: "Apache-2.0",
			Permitted: []catalogue.UsagePurpose{catalogue.UsageCommercial},
		},
		Descriptor:         catalogue.Descriptor{ParameterCount: 4_000_000_000},
		ExpectedCapability: catalogue.ExpectedCapability{Modalities: []string{"text"}},
	}
}

// selectionOffers reports whether selection, under the reserve the boot
// binaries pass, would show this model to a user on a card with free bytes.
func selectionOffers(t *testing.T, free, need int64) bool {
	t.Helper()
	entry := gpuEntry(need)
	res, err := selection.Select(selection.Request{
		Profile:       cardWithFree(free),
		Entries:       []catalogue.Entry{entry},
		DeclaredUsage: catalogue.UsageCommercial,
		Now:           time.Now().UTC(),
		MaxProfileAge: time.Minute,
		Reserve:       runtime.SelectionReserve(),
	})
	require.NoError(t, err)
	require.Len(t, res.Families, 1)
	for _, o := range res.Families[0].Offered {
		if o.ModelID == entry.ModelID {
			return true
		}
	}
	return false
}

// sweepPoints are the free-memory readings the two deciders are compared at.
// They are derived from the broker's headroom rather than written as absolute
// sizes, so the sweep still straddles the boundary if that constant moves.
func sweepPoints(need int64) []int64 {
	h := vrambroker.HeadroomBytes
	points := []int64{
		need - 1,     // does not fit at all
		need,         // fits exactly, as measured
		need + h - 1, // fits the card, NOT the card plus the gate's margin
		need + h,     // the first reading the gate admits
		need + h + 1,
		need + 2*h,
	}
	// Plus a spread across the band, so a divergence that is not exactly at a
	// boundary is caught too.
	if step := h / 8; step > 0 {
		for i := int64(0); i <= 16; i++ {
			points = append(points, need+i*step)
		}
	}
	return points
}

// TestSelectionOffersExactlyWhatTheBrokerAdmits is the guard. One disagreement
// anywhere in the band is a model the user can be shown and cannot start.
func TestSelectionOffersExactlyWhatTheBrokerAdmits(t *testing.T) {
	need := gpuNeedBytes

	for _, free := range sweepPoints(need) {
		if free < 0 {
			continue
		}
		t.Run(fmt.Sprintf("free=%dMiB", free/vrambroker.MiB), func(t *testing.T) {
			admitted := vrambroker.Admits(free, need)
			offered := selectionOffers(t, free, need)

			require.Equal(t, admitted, offered,
				"selection and the admission gate disagree on a card with %d MiB free for a "+
					"%d MiB model: the gate admits=%v, selection offers=%v. A model offered "+
					"but not admitted fails after the user has chosen it; one admitted but "+
					"not offered is capacity withheld for no reason.",
				free/vrambroker.MiB, need/vrambroker.MiB, admitted, offered)
		})
	}
}

// TestTheBandIsRealAtThisHeadroom is the premise check. If the broker's headroom
// were ever zero the sweep above would pass with selection holding nothing back,
// and would prove nothing. This states the band exists, so the guard above is
// known to be exercising it rather than agreeing vacuously.
func TestTheBandIsRealAtThisHeadroom(t *testing.T) {
	require.Positive(t, vrambroker.HeadroomBytes,
		"the gate keeps no margin, so there is no band for selection to disagree across")

	justInside := gpuNeedBytes + vrambroker.HeadroomBytes - 1
	require.False(t, vrambroker.Admits(justInside, gpuNeedBytes),
		"fixture premise: a card holding the model but not the model plus the margin must be refused")
	require.True(t, vrambroker.Admits(gpuNeedBytes+vrambroker.HeadroomBytes, gpuNeedBytes),
		"fixture premise: a card holding the model plus the margin must be admitted")
}

// TestBootRootsSelectUnderTheGatesMargin is the wiring check. The agreement
// above is a property of SelectionReserve; this is what makes it a property of
// what the binaries actually run, by asserting the reserve they pass carries
// the gate's own margin rather than a number of its own.
func TestBootRootsSelectUnderTheGatesMargin(t *testing.T) {
	r := runtime.SelectionReserve()

	require.Equal(t, uint64(vrambroker.HeadroomBytes), r.AcceleratorHeadroomBytes,
		"the reserve handed to selection must carry the admission gate's margin, read from it")

	// The other two axes are unchanged: stating the device margin must not
	// quietly alter what is held back on host memory or storage.
	require.Equal(t, selection.DefaultReserve().MemoryFraction, r.MemoryFraction)
	require.Equal(t, selection.DefaultReserve().StorageFraction, r.StorageFraction)
}
