package selection_test

import (
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/capability/testdata/fixtures"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/selection"
	"github.com/stretchr/testify/require"
)

// CRITICAL-5. An accelerator-required option must be checked against the
// accelerator's MEASURED memory, not merely against the presence of a card.
//
// The catalogue's own data says why this matters. For every entry that sets
// requires_accelerator, memory_required_bytes IS the device-memory figure:
// internal/catalogue/data/video.yaml records it as "the value actually passed
// to vrambroker.Acquire(ClassVideo)" and repeats it verbatim as
// annotations.accelerator_requirement.minimum_free_vram_bytes. Comparing that
// figure against host RAM answers a question nobody asked — a 4 GiB card in a
// 64 GiB machine passes a 19 GB requirement with room to spare, and the model
// is offered to a user whose card cannot hold a third of it.

// smallAcceleratorHost is a well-provisioned machine whose accelerator is
// small: abundant system RAM, a 4 GiB card. It is the shape the defect hides
// in — every memory figure below clears host RAM easily, so any refusal here
// can only have come from reading the device.
func smallAcceleratorHost() capability.HostCapabilityProfile {
	p := fixtures.SingleAccelerator()
	p.HostIdentity = "fixture-small-gpu"
	p.Accelerators = []capability.Accelerator{{
		Identity:        capability.DeviceIdentity("GPU-fixture-small-0000"),
		Model:           "fixture small accelerator",
		API:             capability.APICUDA,
		MemoryTotal:     4 * capability.GiB,
		MemoryAvailable: 3*capability.GiB + 800*capability.MiB,
	}}
	return p
}

// acceleratorEntry is a candidate that mandates a device and needs needBytes of
// it. Everything not under test is set to something this host clears.
func acceleratorEntry(modelID string, needBytes uint64) catalogue.Entry {
	return catalogue.Entry{
		ModelID:              modelID,
		Family:               catalogue.FamilyImageGeneration,
		Architecture:         catalogue.ArchitectureDiffusion,
		MemoryRequiredBytes:  needBytes,
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

func acceleratorRequest(p capability.HostCapabilityProfile, entries []catalogue.Entry) selection.Request {
	return selection.Request{
		Profile:       p,
		Entries:       entries,
		DeclaredUsage: catalogue.UsageCommercial,
		Now:           time.Now().UTC(),
		MaxProfileAge: time.Minute,
		Reserve:       selection.DefaultReserve(),
	}
}

// TestModelTooLargeForTheCardIsNotOffered is the reproduction. The requirement
// (19 GB, the flux.2-dev figure the shipped catalogue records) is five times
// the card and comfortably inside host RAM. An offer here is an offer the host
// cannot load.
func TestModelTooLargeForTheCardIsNotOffered(t *testing.T) {
	host := smallAcceleratorHost()
	tooBig := acceleratorEntry("needs-19gb-of-vram", 19_000_000_000)

	// The premise the defect rests on: host RAM is not the constraint here.
	require.Greater(t, uint64(host.MemoryAvailable), tooBig.MemoryRequiredBytes,
		"fixture premise: host RAM must clear the requirement, so only the device can refuse it")

	res, err := selection.Select(acceleratorRequest(host, []catalogue.Entry{tooBig}))
	require.NoError(t, err)
	require.Len(t, res.Families, 1)
	fr := res.Families[0]

	require.Empty(t, offeredIDs(fr),
		"a model needing 19 GB of device memory was offered to a host measured with a %d-byte card",
		host.Accelerators[0].MemoryTotal)

	w := withheldFor(t, fr, tooBig.ModelID)
	require.Equal(t, selection.ReasonInsufficientResources, w.Reason)
	require.NotNil(t, w.Shortfall, "the refusal must carry the quantity that was short")
	require.Equal(t, selection.ResourceAccelerator, w.Shortfall.Resource,
		"the refusal must name the accelerator, not send the user to buy system RAM")
	require.Equal(t, tooBig.MemoryRequiredBytes, w.Shortfall.RequiredBytes)
	require.Equal(t, uint64(host.Accelerators[0].MemoryAvailable), w.Shortfall.AvailableBytes,
		"the figure compared against must be the device's measured available memory")
}

// TestModelThatFitsTheCardIsStillOffered is the positive control. Without it,
// "refuse everything with requires_accelerator" would pass the test above.
func TestModelThatFitsTheCardIsStillOffered(t *testing.T) {
	host := smallAcceleratorHost()
	fits := acceleratorEntry("fits-the-card", 2_300_000_000) // the shipped sd-1.5 figure

	res, err := selection.Select(acceleratorRequest(host, []catalogue.Entry{fits}))
	require.NoError(t, err)
	require.Len(t, res.Families, 1)
	require.Contains(t, offeredIDs(res.Families[0]), fits.ModelID,
		"a model that fits the measured card must still be offered")
}

// TestLargestCardDecidesTheFit. A model loads onto ONE device, so the question
// is whether ANY single measured device can hold it — and the answer must not
// depend on which card the host happened to enumerate first (§11.4.111).
func TestLargestCardDecidesTheFit(t *testing.T) {
	// The dual fixtures report the same two physical cards in opposite orders.
	// 12 GiB fits the secondary; 20 GiB fits only the primary's 24 GiB.
	fitsLargerOnly := acceleratorEntry("fits-larger-card-only", 20*uint64(capability.GiB))

	forward, err := selection.Select(acceleratorRequest(fixtures.DualAccelerator(), []catalogue.Entry{fitsLargerOnly}))
	require.NoError(t, err)
	reversed, err := selection.Select(acceleratorRequest(fixtures.DualAcceleratorReversed(), []catalogue.Entry{fitsLargerOnly}))
	require.NoError(t, err)

	require.Contains(t, offeredIDs(forward.Families[0]), fitsLargerOnly.ModelID,
		"a model that fits one of the measured cards must be offered")
	require.Equal(t, offeredIDs(forward.Families[0]), offeredIDs(reversed.Families[0]),
		"enumeration order changed the answer; the fit was bound to a position, not a device")
}

// TestNoAcceleratorStillRefusesForConfiguration. Adding the device-memory axis
// must not change the answer for a host with no card at all: "there is no
// device" stays a configuration refusal, not a quantity the host is short of.
func TestNoAcceleratorStillRefusesForConfiguration(t *testing.T) {
	e := acceleratorEntry("needs-a-card", 2_000_000_000)

	res, err := selection.Select(acceleratorRequest(fixtures.NoAccelerator(), []catalogue.Entry{e}))
	require.NoError(t, err)

	w := withheldFor(t, res.Families[0], e.ModelID)
	require.Equal(t, selection.ReasonUnsupportedConfiguration, w.Reason)
	require.NotNil(t, w.Unsupported)
	require.Equal(t, selection.RequirementAccelerator, w.Unsupported.Requirement)
}
