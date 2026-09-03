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
	"os"
	"path/filepath"
	"strings"
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

// TestEveryBootRootSelectsUnderTheGatesMargin closes the gap the test above
// leaves open.
//
// TestBootRootsSelectUnderTheGatesMargin asserts that [runtime.SelectionReserve]
// carries the gate's margin. It does NOT assert that any binary actually passes
// it, and that is the half that drifts: the reserve is stated in one place and
// consumed in several, so a new lane — or a lane migrated to measured selection
// later than the others — reintroduces the offered-then-refused band simply by
// copying a decide() that predates the seam. That is exactly how the agent lane
// arrived: it was the fourth boot root and the last to select at all.
//
// So this walks the boot roots as they exist on disk and requires each one that
// SELECTS to also state the gate's margin.
//
// RECONCILED when the decision was extracted to internal/laneboot (§11.4.120).
// The four copies of decide() became one, which moved the only selection.Select
// call out of cmd/ entirely. The original form of this guard keyed on "a cmd
// file that calls selection.Select" and would now match nothing — its floor of
// four correctly refused to pass vacuously rather than going quiet, which is
// what sent this here. Keying on the shared entry point instead restores the
// property AND strengthens it, because a single call site can be checked in
// ways four copies could not:
//
//   - Every lane still has to be counted (the floor survives, so a lane that
//     stops routing through laneboot is still caught).
//   - No lane may call selection.Select DIRECTLY any more. Previously
//     unstatable: when every lane had its own call, "has a Select call" was
//     the normal case. Now it is the bypass — the one way a lane could
//     reintroduce a reserve-less decision — and it is banned outright.
//   - The shared decision must hold EXACTLY ONE Select call, and that call must
//     state the margin. The old honest boundary (a file with two Select calls
//     where only one carried the reserve would pass) is closed by counting.
//
// Honest boundary (§11.4.6): this remains a source-level check. It proves every
// lane routes through the one decision and that decision states the margin; the
// arithmetic agreement itself is what
// TestSelectionOffersExactlyWhatTheBrokerAdmits proves.
func TestEveryBootRootSelectsUnderTheGatesMargin(t *testing.T) {
	sources, err := filepath.Glob(filepath.Join("..", "..", "cmd", "*", "*.go"))
	require.NoError(t, err)

	var routing, bypassing []string
	for _, path := range sources {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		b, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		src := string(b)
		if strings.Contains(src, "selection.Select(") {
			bypassing = append(bypassing, path)
		}
		if strings.Contains(src, "laneboot.Decide(") {
			routing = append(routing, path)
		}
	}

	// A glob that found nothing would pass vacuously and prove the opposite of
	// what it claims. Four roots decide today; a fifth is welcome, a drop to
	// three is a deliberate decision someone should have to record here.
	require.GreaterOrEqual(t, len(routing), 4,
		"expected at least the four boot roots that decide via laneboot, found %v — if a lane "+
			"was removed or stopped routing through the shared decision, update this floor "+
			"deliberately rather than letting the guard go quiet", routing)

	require.Empty(t, bypassing,
		"these boot roots call selection.Select directly instead of going through "+
			"laneboot.Decide, so each can ask what fits without telling selection the admission "+
			"gate's margin — the band TestSelectionOffersExactlyWhatTheBrokerAdmits measures: %v",
		bypassing)

	// The one shared decision is where the margin is now stated, so it is worth
	// checking there is exactly one place to state it.
	//
	// Read the whole PACKAGE, not just laneboot.go: a second Select added in a
	// sibling file is exactly the case this is meant to catch, and reading one
	// file by name would miss it while the failure message still claimed the
	// package was covered.
	shared, err := filepath.Glob(filepath.Join("..", "laneboot", "*.go"))
	require.NoError(t, err)
	var src string
	for _, path := range shared {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		b, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		src += stripLineComments(string(b))
	}
	require.NotEmpty(t, src, "the shared decision every boot root routes through must exist")

	require.Equal(t, 1, strings.Count(src, "selection.Select("),
		"internal/laneboot must hold exactly one call to selection.Select — the single place "+
			"every lane's offer is decided; a second call is a second chance to omit the margin")

	// Matched as the Reserve FIELD rather than a bare mention, and tolerant of
	// gofmt's alignment. Two false-negatives are deliberately closed here:
	//
	//   - The predecessor looked for the padded literal
	//     "Reserve:       runtime.SelectionReserve()," first and fell back to a
	//     bare "runtime.SelectionReserve()". gofmt writes a single space, so the
	//     precise pattern matched nothing and only the loose fallback worked —
	//     and a mention anywhere in the file satisfied it even with the field
	//     dropped from the request.
	//   - Comments are stripped before matching, so commenting the field OUT
	//     fails this. Left in, the line would still read as a match while the
	//     request no longer carried the margin.
	require.Regexp(t, `Reserve:\s*runtime\.SelectionReserve\(\),`, src,
		"the shared decision asks selection what fits without telling it the admission gate's "+
			"margin, so every lane at once can offer a model the broker will then refuse to start")

	t.Logf("all %d boot roots decide via the single shared laneboot.Decide, which states the gate's margin: %v",
		len(routing), routing)
}

// stripLineComments removes whole-line and trailing // comments so a source
// check cannot be satisfied by commented-out code.
//
// Deliberately naive: it does not understand /* */ blocks, and a "//" inside a
// string literal would be treated as a comment. Both are acceptable here —
// the cost of the first is a missed detection in code this guard also requires
// to be one call long, and the cost of the second is a FALSE ALARM, which is
// the safe direction for a guard to fail in.
func stripLineComments(src string) string {
	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
