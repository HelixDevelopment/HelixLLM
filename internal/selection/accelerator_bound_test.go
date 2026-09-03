package selection_test

// The lane that spends the memory figure on the card must have selection check
// it against the card.
//
// THE DEFECT. Five of the six live entries in the shipped text catalogue carry
// requires_accelerator: false, and their memory figures are sourced as HOST RAM
// figures — internal/catalogue/data/text.yaml records qwen3-0.6b's as
// '"~2 GB, usable" (CPU-only)' and glm-5.2's accelerator_note as "GPU is
// explicitly optional for Colibri — it accelerates but does not gate
// functionality". Those labels are right, and this change does not touch them.
//
// What was wrong is what the LANES then did with the figure. Every *-boot
// binary computes its admission need as the chosen option's
// MemoryRequiredBytes (cmd/agentgen-boot/main.go needBytesFor) and hands it to
// vrambroker.Acquire — device memory. Selection had checked the same number
// against host RAM, and applied the device axis only when the ENTRY set
// RequiresAccelerator. So for exactly these five entries the number was checked
// on one resource and spent on another: the CRITICAL-5 shape (see
// accelerator_memory_test.go), reached by a second route.
//
// Measured on the shipped catalogue before the fix, on a host with 44 GiB free
// RAM and a 4 GiB card, family text:
//
//	OFFERED qwen3-0.6b:q4_0        requires_accelerator=false   2048MiB
//	OFFERED glm-5.2:colibri-int4   requires_accelerator=false  16384MiB  <- beyond the card
//	OFFERED qwen3.6-27b:q4_k_m     requires_accelerator=false  19968MiB  <- beyond the card
//	OFFERED qwen3-30b-a3b:q4_k_m   requires_accelerator=false  20480MiB  <- beyond the card
//	OFFERED qwen3.6-35b-a3b:q4_k_m requires_accelerator=false  24576MiB  <- beyond the card
//
// agentgen-boot's own doc comment claims it "cannot offer a model it will then
// refuse to start", on the strength of passing the admission gate's margin to
// selection through Reserve. The margin was real; the axis it applies to was
// never reached by these entries, so the claim was not true for most of the
// text catalogue. And 19968MiB is the very figure that comment cites as the
// forensic case it was written to close.
//
// POLARITY SWITCH (§11.4.115) — one source, two roles:
//
//	RED_MODE unset / RED_MODE=0 (DEFAULT, post-fix) — standing regression
//	                 guard: the defect is ABSENT.
//	RED_MODE=1     — reproduction: the defect IS PRESENT. It fails on fixed
//	                 code, which is what proves the guard is not blind.
//
// Honest boundary on what RED_MODE=1 replays (§11.4.6). It reproduces at the
// SEAM, not against the pre-fix artifact: it flips Request.AcceleratorBound to
// false, which is precisely the pre-fix behaviour, but the field does not exist
// before this change so this file does not compile against that commit. The
// artifact-level capture was taken separately, with a throwaway test using only
// pre-existing API, and is the table above.

import (
	"os"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/capability/testdata/fixtures"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/runtime"
	"github.com/HelixDevelopment/HelixLLM/internal/selection"
	"github.com/stretchr/testify/require"
)

// redMode reports whether the reproduction polarity is selected.
func redMode() bool { return os.Getenv("RED_MODE") == "1" }

// shippedCatalogueDir is the recorded catalogue this repository ships. These
// tests read it rather than a fixture because the claim under test is about the
// entries a user is actually offered.
const shippedCatalogueDir = "../catalogue/data"

// generousRAMSmallCardHost is the shape the defect hides in: abundant system
// RAM and storage, and a card that is small next to the catalogue's larger
// entries but not so small that nothing fits it. Both halves matter — a host
// where nothing fits could not tell a correct narrowing apart from a blanket
// refusal.
//
// 7 GiB free on the card, less the admission gate's own 2 GiB margin, leaves
// 5 GiB: room for the 2 GiB floor-tier option and for none of the four above
// it.
func generousRAMSmallCardHost() capability.HostCapabilityProfile {
	p := fixtures.SingleAccelerator()
	p.HostIdentity = "fixture-generous-ram-small-card"
	p.Accelerators = []capability.Accelerator{{
		Identity:        capability.DeviceIdentity("GPU-fixture-8gib-0000"),
		Model:           "fixture 8 GiB accelerator",
		API:             capability.APICUDA,
		MemoryTotal:     8 * capability.GiB,
		MemoryAvailable: 7 * capability.GiB,
	}}
	return p
}

// laneRequest is the request an accelerator-binding lane makes: the admission
// gate's own margin, and the declaration that the memory figure will be spent
// on the device. It is the shape internal/laneboot builds for every *-boot
// binary.
func laneRequest(
	p capability.HostCapabilityProfile,
	entries []catalogue.Entry,
	family catalogue.CapabilityFamily,
) selection.Request {
	return selection.Request{
		Profile:          p,
		Entries:          entries,
		Families:         []catalogue.CapabilityFamily{family},
		DeclaredUsage:    catalogue.UsageCommercial,
		Now:              time.Now().UTC(),
		MaxProfileAge:    time.Minute,
		Reserve:          runtime.SelectionReserve(),
		AcceleratorBound: true,
	}
}

// TestAcceleratorBoundLaneOffersNothingBeyondTheCard is the guard, run against
// the shipped text catalogue on a host whose RAM is generous and whose card is
// not.
func TestAcceleratorBoundLaneOffersNothingBeyondTheCard(t *testing.T) {
	loaded, err := catalogue.Load(shippedCatalogueDir)
	require.NoError(t, err)

	host := generousRAMSmallCardHost()
	card := host.Accelerators[0]

	req := laneRequest(host, loaded.Entries(), catalogue.FamilyText)
	if redMode() {
		// The pre-fix behaviour exactly: the lane still spends the figure on
		// the card, selection is simply not told so.
		req.AcceleratorBound = false
	}

	res, err := selection.Select(req)
	require.NoError(t, err)
	fr, ok := res.Family(catalogue.FamilyText)
	require.True(t, ok, "the shipped catalogue must serve the text family")

	var beyond []string
	for _, o := range fr.Offered {
		if o.Cost.MemoryRequiredBytes > uint64(card.MemoryAvailable) {
			beyond = append(beyond, o.Identity)
		}
	}

	if redMode() {
		require.NotEmpty(t, beyond,
			"RED_MODE=1 expected the defect: with the lane's binding unstated, at least one "+
				"option must be offered whose memory figure exceeds the %dMiB the card has free. "+
				"If this fails the defect is already fixed — rerun with RED_MODE=0.",
			card.MemoryAvailable/capability.MiB)
		t.Logf("defect reproduced: offered beyond the card: %v", beyond)
		return
	}

	require.Empty(t, beyond,
		"a lane that admits the memory figure against the card (vrambroker.Acquire) was offered "+
			"%v, each needing more than the %dMiB that card has free",
		beyond, card.MemoryAvailable/capability.MiB)

	// Not vacuous: the guard would also pass if nothing at all were offered,
	// which would be a different defect. Something must survive.
	require.NotEmpty(t, fr.Offered,
		"every text option was withheld; the card axis must narrow the offer, not empty it")
}

// TestAcceleratorBoundNamesTheAcceleratorInTheRefusal. A refusal that says
// "memory" sends the operator to buy system RAM they already have enough of.
// The whole value of the axis being separate is that it can name the right one.
func TestAcceleratorBoundNamesTheAcceleratorInTheRefusal(t *testing.T) {
	loaded, err := catalogue.Load(shippedCatalogueDir)
	require.NoError(t, err)

	host := generousRAMSmallCardHost()
	card := host.Accelerators[0]

	res, err := selection.Select(laneRequest(host, loaded.Entries(), catalogue.FamilyText))
	require.NoError(t, err)
	fr, ok := res.Family(catalogue.FamilyText)
	require.True(t, ok)

	// glm-5.2 is the clearest case in the shipped data: requires_accelerator is
	// false and correctly so, its 16 GiB figure is comfortably inside this
	// host's RAM, and it is four times the card.
	w := withheldFor(t, fr, "glm-5.2")
	require.Equal(t, selection.ReasonInsufficientResources, w.Reason)
	require.NotNil(t, w.Shortfall, "the refusal must carry the quantity that was short")
	require.Equal(t, selection.ResourceAccelerator, w.Shortfall.Resource,
		"the refusal named %s; the host has %dMiB of RAM free and only the card is short",
		w.Shortfall.Resource, host.MemoryAvailable/capability.MiB)
	require.Equal(t, uint64(card.MemoryAvailable), w.Shortfall.AvailableBytes+w.Shortfall.ReservedBytes,
		"the figure compared against must be the device's measured available memory, "+
			"less the admission gate's own margin")
}

// TestAcceleratorBoundStillOffersWhatFitsTheCard is the positive control.
// Without it, "refuse everything when bound" would pass the guard above.
func TestAcceleratorBoundStillOffersWhatFitsTheCard(t *testing.T) {
	loaded, err := catalogue.Load(shippedCatalogueDir)
	require.NoError(t, err)

	res, err := selection.Select(laneRequest(generousRAMSmallCardHost(), loaded.Entries(), catalogue.FamilyText))
	require.NoError(t, err)
	fr, ok := res.Family(catalogue.FamilyText)
	require.True(t, ok)

	// 2 GiB against a card with 3872 MiB free, less the gate's margin — the
	// floor-tier option the catalogue describes as running "on a no-GPU host
	// with 8 GiB of system RAM". It fits this card too, and must survive.
	require.Contains(t, offeredIDs(fr), "qwen3-0.6b",
		"the one text option that fits this card was withheld")
}

// TestUnboundCallerIsUnchanged. The field is a statement about the caller, so a
// caller that makes no such statement must get exactly the answer it got
// before: an entry that runs on the processor is not made infeasible by a small
// card when nobody is putting it on the card.
func TestUnboundCallerIsUnchanged(t *testing.T) {
	loaded, err := catalogue.Load(shippedCatalogueDir)
	require.NoError(t, err)

	req := laneRequest(generousRAMSmallCardHost(), loaded.Entries(), catalogue.FamilyText)
	req.AcceleratorBound = false

	res, err := selection.Select(req)
	require.NoError(t, err)
	fr, ok := res.Family(catalogue.FamilyText)
	require.True(t, ok)

	require.Contains(t, offeredIDs(fr), "glm-5.2",
		"a processor-servable model was withheld from a caller that never said it would "+
			"put the model on the card")
}

// TestAcceleratorBoundOnAHostWithNoCardIsUnchanged states the boundary in a
// test rather than only in a comment.
//
// Selection is asked to check a device against a host where no device was
// measured. It has nothing to check against, and inventing a refusal would be
// selection guessing at hardware it cannot see (§11.4.6). The refusal for that
// host belongs to the admission gate, which fails closed on an unreadable
// budget (vrambroker.ErrBudgetUnavailable). So the answer here must be the same
// bound or unbound.
func TestAcceleratorBoundOnAHostWithNoCardIsUnchanged(t *testing.T) {
	loaded, err := catalogue.Load(shippedCatalogueDir)
	require.NoError(t, err)

	host := fixtures.NoAccelerator()

	bound := laneRequest(host, loaded.Entries(), catalogue.FamilyText)
	unbound := bound
	unbound.AcceleratorBound = false

	boundRes, err := selection.Select(bound)
	require.NoError(t, err)
	unboundRes, err := selection.Select(unbound)
	require.NoError(t, err)

	boundFR, ok := boundRes.Family(catalogue.FamilyText)
	require.True(t, ok)
	unboundFR, ok := unboundRes.Family(catalogue.FamilyText)
	require.True(t, ok)

	require.Equal(t, offeredIDs(unboundFR), offeredIDs(boundFR),
		"declaring the binding changed the answer on a host where no device was measured; "+
			"selection has no reading to check and must not invent one")
}
