package runtime_test

// CRITICAL-2: the streaming path admitted hosts that cannot run the model.
//
// This file is BOTH the reproduction and the standing regression guard, per
// §11.4.115 / §11.4.146. It drives the REAL shipped catalogue rather than a
// fixture entry deliberately: the defect is a disagreement between what the
// data records and what the code computes from it, and a hand-built entry can
// be made to agree with either side. Only the shipped rows can show which.
//
// §11.4.115 polarity switch (RED_MODE):
//
//	RED_MODE=1  reproduce-and-assert-defect-present — asserts the PRE-FIX
//	            artifact admits below the runtime's stated floor and offers
//	            in-memory for a streaming-only artifact.
//	unset / 0   (DEFAULT, post-fix) the standing GREEN guard — asserts both
//	            defects are ABSENT.
//
// The oracle is not invented here. internal/catalogue/data/text.yaml records a
// per-family "min RAM" for every member of the streaming runtime's roster,
// with the source phrasing preserved:
//
//	glm-5.2  min RAM 17179869184 B  "16 GB min, 24 GB comfortable RAM"
//	                 disk 399431958528 B  "~372 GB disk storage"
//	qwen3.6  min RAM 25769803776 B  "24 GB RAM minimum"
//	olmoe    min RAM  8589934592 B  "8 GB RAM"
//
// Those are the figures the runtime itself states. A host below one of them
// cannot run that family, and offering it there is the FR-027 failure ("offer
// the disk-streaming path only for models that the streaming runtime actually
// supports") arriving as a load-time crash instead of a selection-time answer.

import (
	"os"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/capability/testdata/fixtures"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/runtime"
	"github.com/stretchr/testify/require"
)

// c2RedMode is the polarity switch. Default is the guard: the defect is fixed,
// and RED_MODE=1 is retained so the reproduction stays runnable against a
// pre-fix checkout rather than being described in prose only.
func c2RedMode() bool { return os.Getenv("RED_MODE") == "1" }

const (
	oneGiB = 1 << 30

	// The streaming runtime's own stated minimum RAM per roster family, as
	// recorded in text.yaml. These are the ORACLE for every assertion below.
	glm52StreamingFloorBytes  = uint64(17179869184) // "16 GB min"
	qwen36StreamingFloorBytes = uint64(25769803776) // "24 GB RAM minimum"

	// glm-5.2's full on-disk weight footprint — "~372 GB disk storage". It is
	// over twenty times the memory figure and is why treating the memory
	// figure as an in-memory footprint is not a rounding error.
	glm52StorageBytes = uint64(399431958528)
)

// shippedEntry returns one row of the REAL catalogue by identity.
func shippedEntry(t *testing.T, modelID, variant string) catalogue.Entry {
	t.Helper()
	cat, err := catalogue.Load("../catalogue/data")
	require.NoError(t, err, "the shipped catalogue must load")
	for _, e := range cat.Entries() {
		if e.ModelID == modelID && e.Variant == variant {
			return e
		}
	}
	t.Fatalf("no shipped catalogue entry %s/%s — this test is about the shipped data", modelID, variant)
	return catalogue.Entry{}
}

// hostWithFreeMemory is a measured host with a stated amount of free RAM and
// otherwise-ample resources. Storage is raised well past glm-5.2's 372 GiB so
// the memory axis is the only one under test — the storage axis has its own
// case below.
func hostWithFreeMemory(free capability.Bytes) capability.HostCapabilityProfile {
	h := fixtures.DualAccelerator()
	h.MemoryAvailable = free
	h.StorageAvailable = capability.Bytes(1024) * oneGiB
	return h
}

func chooseShipped(t *testing.T, e catalogue.Entry, free capability.Bytes) (runtime.Choice, error) {
	t.Helper()
	h := hostWithFreeMemory(free)
	return runtime.NewChooser().Choose(h, e, h.MeasuredAt)
}

// --- 1. the defect CRITICAL-2 names: admitted below the runtime's floor -----

// TestStreamingIsNotOfferedBelowTheRuntimesOwnStatedFloor.
//
// glm-5.2's runtime states "16 GB min". A 4 GiB host is a quarter of that. The
// pre-fix code reached that figure by multiplying the recorded 16 GiB floor by
// an unmeasured 0.25 — subtracting from a number that WAS the minimum.
func TestStreamingIsNotOfferedBelowTheRuntimesOwnStatedFloor(t *testing.T) {
	e := shippedEntry(t, "glm-5.2", "colibri-int4-g64")
	require.Equal(t, glm52StreamingFloorBytes, e.MemoryRequiredBytes,
		"the shipped row must still carry the recorded floor this test reasons about")

	ch, err := chooseShipped(t, e, 4*oneGiB)

	if c2RedMode() {
		require.NoError(t, err,
			"RED_MODE=1 expects the pre-fix artifact to ADMIT a 4 GiB host; it refused, "+
				"so the defect is gone — re-run without RED_MODE")
		require.Equal(t, catalogue.RuntimeStreaming, ch.Runtime,
			"RED_MODE=1 expects the admission to be the streaming path")
		return
	}

	r := refusalFrom(t, ch, err)
	require.Equal(t, runtime.ReasonInsufficientResources, r.Reason,
		"a host below the runtime's stated minimum is short of a resource, not misconfigured")
	require.NotNil(t, r.Shortfall, "the refusal must name which resource")
	require.Equal(t, runtime.ResourceMemory, r.Shortfall.Resource,
		"memory is the axis that is short; naming storage would send the user to fix the resource that was fine")
	require.Equal(t, glm52StreamingFloorBytes, r.Shortfall.RequiredBytes,
		"the requirement reported must be the runtime's RECORDED floor, not a fraction of it")
	require.Equal(t, uint64(4*oneGiB), r.Shortfall.AvailableBytes)
}

// TestTheFloorIsTheRecordedFigureAndNotAFractionOfIt.
//
// Stated on the accessor both Admit and the launcher call, so the figure the
// broker is asked to reserve is the same one selection admitted against. A
// discount here understates the reservation by the same factor it widens
// admission by, so the process is admitted AND under-reserved.
func TestTheFloorIsTheRecordedFigureAndNotAFractionOfIt(t *testing.T) {
	e := shippedEntry(t, "glm-5.2", "colibri-int4-g64")
	got := runtime.DefaultStreamingMinimums().ResidentMemoryBytes(e)

	if c2RedMode() {
		require.Less(t, got, glm52StreamingFloorBytes,
			"RED_MODE=1 expects the pre-fix policy to discount the recorded floor")
		return
	}
	require.Equal(t, glm52StreamingFloorBytes, got,
		"the streaming runtime's floor is recorded data; it is not derived by discounting")
}

// --- 2. the deeper half: in-memory offered for a streaming-only artifact ----

// TestAStreamingOnlyArtifactIsNeverOfferedInMemory.
//
// The pre-fix chooser read the SAME field as the in-memory requirement, so on
// any host at or above 16 GiB it reported that in-memory could serve a 744B
// model whose weights are 372 GiB. Streaming was then never reached at all —
// step (a) had already answered.
//
// A Colibri container is a different artifact from a GGUF release (FR-012) and
// the in-memory runtime cannot open it, so nothing faster is being forgone
// here: the fast path for a model is its own separate in-memory row.
func TestAStreamingOnlyArtifactIsNeverOfferedInMemory(t *testing.T) {
	e := shippedEntry(t, "glm-5.2", "colibri-int4-g64")
	require.Equal(t, catalogue.RuntimeStreaming, e.Runtime,
		"this case is about a row whose only artifact shape is the streaming one")
	require.Equal(t, glm52StorageBytes, e.StorageRequiredBytes,
		"the 372 GiB footprint is what makes the in-memory offer false")

	// 24 GiB is glm-5.2's own "comfortable" figure — comfortably above its
	// 16 GiB floor, and nowhere near its 372 GiB weight set.
	ch, err := chooseShipped(t, e, 24*oneGiB)
	require.NoError(t, err, "24 GiB is above the runtime's stated minimum, so a path must exist")

	if c2RedMode() {
		require.Equal(t, catalogue.RuntimeInMemory, ch.Runtime,
			"RED_MODE=1 expects the pre-fix artifact to claim in-memory can serve a 372 GiB weight set")
		require.False(t, ch.Fallback, "RED_MODE=1 expects it offered as the preferred path, unlabelled")
		return
	}

	require.Equal(t, catalogue.RuntimeStreaming, ch.Runtime,
		"a streaming-only artifact is served by streaming or not at all")
	require.True(t, ch.Fallback, "streaming is a fallback and must be reported as one")
	require.NotNil(t, ch.Tradeoff, "the speed trade-off must be labelled (FR-027)")
}

// --- 3. the opposite direction: not one host over-refused ------------------

// TestTheFloorAdmitsExactlyAtTheRuntimesStatedMinimum.
//
// The guard against over-correction. "16 GB min" means 16 GiB is enough, so a
// host with exactly that much must be ADMITTED. A bound that refused here
// would deny the model to hosts the runtime says can run it — the same defect
// pointing the other way, and equally a defect.
func TestTheFloorAdmitsExactlyAtTheRuntimesStatedMinimum(t *testing.T) {
	e := shippedEntry(t, "glm-5.2", "colibri-int4-g64")

	for _, free := range []capability.Bytes{
		capability.Bytes(glm52StreamingFloorBytes),     // exactly "16 GB min"
		capability.Bytes(glm52StreamingFloorBytes) + 1, // one byte over
		24 * oneGiB, // the "comfortable" figure
		96 * oneGiB, // far above
	} {
		ch, err := chooseShipped(t, e, free)
		require.NoError(t, err,
			"%d bytes free is at or above the runtime's stated 16 GiB minimum and must be admitted", free)
		if c2RedMode() {
			// Pre-fix these hosts were admitted too, but on the wrong path.
			require.Equal(t, catalogue.RuntimeInMemory, ch.Runtime,
				"RED_MODE=1 expects the pre-fix artifact to answer in-memory here")
			continue
		}
		require.Equal(t, catalogue.RuntimeStreaming, ch.Runtime,
			"%d bytes free must be admitted on the streaming path", free)
	}
}

// TestOneByteBelowTheFloorIsRefusedAndTheFloorItselfIsNot.
//
// The boundary itself, both sides of it, in one assertion pair so an off-by-one
// in either direction cannot hide.
func TestOneByteBelowTheFloorIsRefusedAndTheFloorItselfIsNot(t *testing.T) {
	if c2RedMode() {
		t.Skip("SKIP-OK: pre-fix there is no boundary at the recorded floor to test — " +
			"the discounted bound sits at a quarter of it and step (a) answers above it")
	}
	e := shippedEntry(t, "glm-5.2", "colibri-int4-g64")

	_, err := chooseShipped(t, e, capability.Bytes(glm52StreamingFloorBytes)-1)
	require.Error(t, err, "one byte below the stated minimum must be refused")

	ch, err := chooseShipped(t, e, capability.Bytes(glm52StreamingFloorBytes))
	require.NoError(t, err, "the stated minimum itself must be admitted")
	require.Equal(t, catalogue.RuntimeStreaming, ch.Runtime)
}

// TestAModelThatFitsInMemoryIsStillServedFromMemory.
//
// The fix must not turn every roster-eligible row into a streaming offer. This
// row is roster-eligible AND has a genuine in-memory footprint; on a host that
// holds it, in-memory remains the answer (FR-026, US4 acceptance scenario 2).
// Asserted in BOTH polarities: it was correct before the fix and must stay
// correct after it.
func TestAModelThatFitsInMemoryIsStillServedFromMemory(t *testing.T) {
	e := shippedEntry(t, "qwen3.6-35b-a3b", "q4_k_m")
	require.Equal(t, catalogue.RuntimeInMemory, e.Runtime)
	require.True(t, e.StreamingEligible(), "the case is only meaningful for a roster-eligible row")

	ch, err := chooseShipped(t, e, 64*oneGiB)
	require.NoError(t, err)
	require.Equal(t, catalogue.RuntimeInMemory, ch.Runtime,
		"a model that fits in memory is served from memory even when it is on the roster")
	require.False(t, ch.Fallback)
}

// TestTheSecondRosterEligibleRowIsAlsoHeldToItsRecordedFloor.
//
// CRITICAL-2 named one row, but the admission surface is every roster-eligible
// row, and both of the shipped ones were admitted at a quarter of their stated
// minimum. qwen3.6's runtime states "24 GB RAM minimum"; the pre-fix bound
// admitted a 6 GiB host.
func TestTheSecondRosterEligibleRowIsAlsoHeldToItsRecordedFloor(t *testing.T) {
	e := shippedEntry(t, "qwen3.6-35b-a3b", "q4_k_m")
	require.Equal(t, qwen36StreamingFloorBytes, e.MemoryRequiredBytes,
		"the shipped row must still carry the figure this test reasons about")

	ch, err := chooseShipped(t, e, 6*oneGiB)

	if c2RedMode() {
		require.NoError(t, err, "RED_MODE=1 expects a 6 GiB host to be admitted pre-fix")
		require.Equal(t, catalogue.RuntimeStreaming, ch.Runtime)
		return
	}

	r := refusalFrom(t, ch, err)
	require.Equal(t, runtime.ReasonInsufficientResources, r.Reason)
	require.NotNil(t, r.Shortfall)
	require.Equal(t, runtime.ResourceMemory, r.Shortfall.Resource)
	require.Equal(t, qwen36StreamingFloorBytes, r.Shortfall.RequiredBytes,
		"the requirement reported must be the runtime's recorded 24 GiB minimum")
}

// --- 4. the storage axis is untouched by any of this -----------------------

// TestTheStorageAxisStillRefusesOnItsOwnTerms.
//
// Invariant 2: storage is a genuinely independent axis. A host with abundant
// memory and no room for 372 GiB of weights is refused NAMING STORAGE, before
// and after the fix alike, so this carries no polarity switch.
func TestTheStorageAxisStillRefusesOnItsOwnTerms(t *testing.T) {
	e := shippedEntry(t, "glm-5.2", "colibri-int4-g64")
	h := hostWithFreeMemory(96 * oneGiB)
	h.StorageAvailable = 100 * oneGiB // far short of the 372 GiB footprint

	ch, err := runtime.NewChooser().Choose(h, e, h.MeasuredAt)
	r := refusalFrom(t, ch, err)
	require.Equal(t, runtime.ReasonInsufficientResources, r.Reason)
	require.NotNil(t, r.Shortfall)
	require.Equal(t, runtime.ResourceStorage, r.Shortfall.Resource,
		"storage is what is short here and memory is ample; naming memory would be the wrong remedy")
	require.Equal(t, glm52StorageBytes, r.Shortfall.RequiredBytes)
}

// TestARowWithNoRosterStandingIsUnaffected.
//
// An unrostered row that does not fit memory is an unsupported configuration,
// not a resource shortage, and none of this changes that. Both polarities.
func TestARowWithNoRosterStandingIsUnaffected(t *testing.T) {
	e := shippedEntry(t, "qwen3-235b-a22b", "q4_k_m")
	require.False(t, e.StreamingEligible(), "the case needs an unrostered row")

	ch, err := chooseShipped(t, e, 8*oneGiB)
	r := refusalFrom(t, ch, err)
	require.Equal(t, runtime.ReasonUnsupportedConfiguration, r.Reason,
		"no runtime here serves this model; more memory would not create one")
}
