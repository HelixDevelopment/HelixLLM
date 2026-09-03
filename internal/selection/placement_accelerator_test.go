package selection_test

// FR-042 on the DEVICE axis: what a host has left after a placement includes
// what its ACCELERATOR has left.
//
// Fleet.Available drew committed memory off host RAM and stopped there. A model
// that mandates a card consumes that card, and the card kept reading as fully
// free: two placements were each told the same 24 GiB device was empty, and
// 32 GiB was committed to it. Every individual decision was correct against the
// reading it was given — which is precisely why this needs a ledger and not a
// better check (see the Placement doc comment).
//
// Host RAM cannot stand in for it. These fixtures give the host 200 GiB free
// against a 24 GiB card, so anything refused below was refused BY THE DEVICE.

import (
	"sync"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/capability/testdata/fixtures"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/discovery"
	"github.com/HelixDevelopment/HelixLLM/internal/selection"
	"github.com/stretchr/testify/require"
)

// acceleratedHost is a machine with abundant RAM and storage whose accelerators
// are exactly those given, so the device axis is the only one that can bind.
func acceleratedHost(identity string, cards ...capability.Accelerator) selection.Host {
	p := fixtures.SingleAccelerator()
	p.HostIdentity = identity
	p.MemoryTotal = 256 * capability.GiB
	p.MemoryAvailable = 200 * capability.GiB
	p.StorageAvailable = 4096 * capability.GiB
	p.Accelerators = cards
	return selection.Host{
		Profile: p,
		Instance: discovery.Instance{
			Endpoint:     "http://" + identity + ":11434",
			Reachability: discovery.LocalNetwork,
			Trusted:      true,
			Health:       discovery.Health{Reachable: true, LastSeen: time.Now().UTC()},
		},
	}
}

func card(identity string, total, available capability.Bytes) capability.Accelerator {
	return capability.Accelerator{
		Identity:        capability.DeviceIdentity(identity),
		Model:           "fixture card " + identity,
		API:             capability.APICUDA,
		MemoryTotal:     total,
		MemoryAvailable: available,
	}
}

// gpuEntry mandates a device and needs deviceMemory of it. For an
// accelerator-required entry the catalogue's memory figure IS the device-memory
// requirement, so that one number is what the card is measured against.
func gpuEntry(id string, deviceMemory capability.Bytes) catalogue.Entry {
	return catalogue.Entry{
		ModelID:              id,
		Family:               catalogue.FamilyVideoGeneration,
		Architecture:         catalogue.ArchitectureDiffusion,
		MemoryRequiredBytes:  uint64(deviceMemory),
		StorageRequiredBytes: 8 * uint64(capability.GiB),
		RequiresAccelerator:  true,
		Runtime:              catalogue.RuntimeInMemory,
		UsageTerms: catalogue.UsageTerms{
			LicenseID: "Apache-2.0",
			Permitted: []catalogue.UsagePurpose{catalogue.UsagePersonal, catalogue.UsageCommercial},
		},
		Descriptor:         catalogue.Descriptor{ParameterCount: 4_000_000_000},
		ExpectedCapability: catalogue.ExpectedCapability{Modalities: []string{"text"}},
	}
}

func gpuFleet(t *testing.T, hosts ...selection.Host) *selection.Fleet {
	t.Helper()
	f, err := selection.NewFleet(selection.FleetOptions{
		Hosts:         hosts,
		Reserve:       selection.DefaultReserve(),
		HealthTTL:     discovery.DefaultHealthTTL,
		MaxProfileAge: time.Minute,
	})
	require.NoError(t, err)
	return f
}

// deviceCommitment returns the ledger line for one card, or the zero value when
// nothing stands on it.
func deviceCommitment(c selection.Commitment, id capability.DeviceIdentity) selection.DeviceCommitment {
	for _, d := range c.Devices {
		if d.Device == id {
			return d
		}
	}
	return selection.DeviceCommitment{}
}

// TestSecondPlacementCannotHaveTheCardTheFirstTook is the reproduction, as a
// standing guard. Two 16 GiB models cannot both stand on a 24 GiB card.
func TestSecondPlacementCannotHaveTheCardTheFirstTook(t *testing.T) {
	const deviceID = capability.DeviceIdentity("GPU-fleet-single-0000")
	fleet := gpuFleet(t, acceleratedHost("gpu-host",
		card(string(deviceID), 24*capability.GiB, 24*capability.GiB)))

	first, err := fleet.Place(placeRequest(gpuEntry("first-16gib", 16*capability.GiB)))
	require.NoError(t, err, "the first 16 GiB model fits a 24 GiB card")
	require.NotNil(t, first.Chosen)

	// The premise: host RAM is nowhere near exhausted, so only the device can
	// refuse the second.
	available, known := fleet.Available("gpu-host")
	require.True(t, known)
	require.Greater(t, available.MemoryAvailable, 100*capability.GiB,
		"fixture premise: host RAM must still be abundant after the first placement")
	require.Equal(t, 8*capability.GiB, available.Accelerators[0].MemoryAvailable,
		"the card must read as drawn down by exactly what stands on it")

	second, err := fleet.Place(placeRequest(gpuEntry("second-16gib", 16*capability.GiB)))
	require.Error(t, err, "32 GiB of device memory was committed to a 24 GiB card")
	require.ErrorIs(t, err, selection.ErrNoPlacement)
	require.Nil(t, second.Chosen)

	excluded := considerationFor(t, second, "gpu-host").Excluded
	require.NotNil(t, excluded)
	require.Equal(t, selection.ExcludedInsufficientResources, excluded.Reason)
	require.NotNil(t, excluded.Shortfall, "the refusal must carry the quantity that was short")
	require.Equal(t, selection.ResourceAccelerator, excluded.Shortfall.Resource,
		"the refusal must name the accelerator, not send the user to buy system RAM")
}

// TestASmallerModelStillFitsWhatIsLeft is the positive control. Without it,
// "refuse every second device-bound placement" would pass the test above.
func TestASmallerModelStillFitsWhatIsLeft(t *testing.T) {
	const deviceID = capability.DeviceIdentity("GPU-fleet-single-0000")
	fleet := gpuFleet(t, acceleratedHost("gpu-host",
		card(string(deviceID), 24*capability.GiB, 24*capability.GiB)))

	_, err := fleet.Place(placeRequest(gpuEntry("first-16gib", 16*capability.GiB)))
	require.NoError(t, err)

	res, err := fleet.Place(placeRequest(gpuEntry("small-6gib", 6*capability.GiB)))
	require.NoError(t, err, "6 GiB must still fit the 8 GiB the card has left")
	require.NotNil(t, res.Chosen)

	c := fleet.Commitment("gpu-host")
	require.Equal(t, 22*capability.GiB, deviceCommitment(c, deviceID).MemoryBytes)
	require.Equal(t, 2, deviceCommitment(c, deviceID).Placements)
}

// TestCommitmentNamesTheDeviceItTook. A commitment that recorded only an amount
// could not be applied to the right card on a multi-device host, and could not
// be released back to it.
func TestCommitmentNamesTheDeviceItTook(t *testing.T) {
	const big = capability.DeviceIdentity("GPU-big-0001")
	const small = capability.DeviceIdentity("GPU-small-0002")
	fleet := gpuFleet(t, acceleratedHost("two-card-host",
		card(string(small), 12*capability.GiB, 12*capability.GiB),
		card(string(big), 24*capability.GiB, 24*capability.GiB)))

	res, err := fleet.Place(placeRequest(gpuEntry("needs-16gib", 16*capability.GiB)))
	require.NoError(t, err)
	require.Equal(t, big, res.Chosen.Option.Headroom.AcceleratorDevice,
		"the offer must record the card it was weighed against")

	c := fleet.Commitment("two-card-host")
	require.Len(t, c.Devices, 1, "only the card that was taken owes anything")
	require.Equal(t, big, c.Devices[0].Device)
	require.Equal(t, 16*capability.GiB, c.Devices[0].MemoryBytes)
	require.Zero(t, deviceCommitment(c, small).MemoryBytes,
		"the untouched card must owe nothing")

	// And the untouched card is still fully available.
	available, known := fleet.Available("two-card-host")
	require.True(t, known)
	for _, a := range available.Accelerators {
		switch a.Identity {
		case big:
			require.Equal(t, 8*capability.GiB, a.MemoryAvailable)
		case small:
			require.Equal(t, 12*capability.GiB, a.MemoryAvailable,
				"a card nothing stands on must not be drawn down")
		}
	}
}

// TestTheSecondPlacementLandsOnTheOtherCard. Per-device accounting is what lets
// a two-card host take two models one card could not hold. A host-wide device
// figure would refuse the second.
func TestTheSecondPlacementLandsOnTheOtherCard(t *testing.T) {
	const big = capability.DeviceIdentity("GPU-big-0001")
	const small = capability.DeviceIdentity("GPU-small-0002")
	fleet := gpuFleet(t, acceleratedHost("two-card-host",
		card(string(big), 24*capability.GiB, 24*capability.GiB),
		card(string(small), 12*capability.GiB, 12*capability.GiB)))

	first, err := fleet.Place(placeRequest(gpuEntry("a-16gib", 16*capability.GiB)))
	require.NoError(t, err)
	require.Equal(t, big, first.Chosen.Option.Headroom.AcceleratorDevice)

	// 10 GiB no longer fits the big card's 8 GiB remainder, but the small card
	// is untouched and holds it.
	second, err := fleet.Place(placeRequest(gpuEntry("b-10gib", 10*capability.GiB)))
	require.NoError(t, err, "the other card is free and holds this model")
	require.Equal(t, small, second.Chosen.Option.Headroom.AcceleratorDevice,
		"the second placement must land on the card that still has room")

	c := fleet.Commitment("two-card-host")
	require.Equal(t, 16*capability.GiB, deviceCommitment(c, big).MemoryBytes)
	require.Equal(t, 10*capability.GiB, deviceCommitment(c, small).MemoryBytes)
}

// TestDeviceAccountingFollowsIdentityNotEnumerationOrder. The ledger outlives
// reboots and driver reloads, both of which reorder the enumeration. A debit
// bound to a position would after either be charged to a different physical
// card — the defect 39701cd removed from the broker, reached through the
// ledger instead (§11.4.111).
func TestDeviceAccountingFollowsIdentityNotEnumerationOrder(t *testing.T) {
	const big = capability.DeviceIdentity("GPU-big-0001")
	const small = capability.DeviceIdentity("GPU-small-0002")
	bigCard := card(string(big), 24*capability.GiB, 24*capability.GiB)
	smallCard := card(string(small), 12*capability.GiB, 12*capability.GiB)

	place := func(cards ...capability.Accelerator) selection.Commitment {
		fleet := gpuFleet(t, acceleratedHost("h", cards...))
		_, err := fleet.Place(placeRequest(gpuEntry("needs-16gib", 16*capability.GiB)))
		require.NoError(t, err)
		return fleet.Commitment("h")
	}

	forward := place(bigCard, smallCard)
	reversed := place(smallCard, bigCard)

	require.Equal(t, forward.Devices, reversed.Devices,
		"enumeration order changed which device was charged; the debit followed a position, not a device")
	require.Equal(t, big, forward.Devices[0].Device)
}

// TestReleaseGivesTheCardBack. A ledger that only debits leaks the card away
// one placement at a time, and crediting the WRONG card is worse than not
// crediting at all — it would invent capacity on a device that never lent any.
func TestReleaseGivesTheCardBack(t *testing.T) {
	const big = capability.DeviceIdentity("GPU-big-0001")
	const small = capability.DeviceIdentity("GPU-small-0002")
	fleet := gpuFleet(t, acceleratedHost("two-card-host",
		card(string(big), 24*capability.GiB, 24*capability.GiB),
		card(string(small), 12*capability.GiB, 12*capability.GiB)))

	first, err := fleet.Place(placeRequest(gpuEntry("a-16gib", 16*capability.GiB)))
	require.NoError(t, err)
	second, err := fleet.Place(placeRequest(gpuEntry("b-10gib", 10*capability.GiB)))
	require.NoError(t, err)
	require.Equal(t, small, second.Chosen.Option.Headroom.AcceleratorDevice)

	require.True(t, fleet.Release(first.Chosen.ID))

	c := fleet.Commitment("two-card-host")
	require.Zero(t, deviceCommitment(c, big).MemoryBytes,
		"the released card must owe nothing again")
	require.Equal(t, 10*capability.GiB, deviceCommitment(c, small).MemoryBytes,
		"releasing one placement must not credit the card a DIFFERENT placement stands on")

	// The freed card takes a 16 GiB model again.
	again, err := fleet.Place(placeRequest(gpuEntry("c-16gib", 16*capability.GiB)))
	require.NoError(t, err, "the card was released and must be usable again")
	require.Equal(t, big, again.Chosen.Option.Headroom.AcceleratorDevice)
}

// TestReleasingAnUnknownPlacementCreditsNoDevice. Crediting capacity nothing
// debited over-states the card exactly as failing to debit under-states it.
func TestReleasingAnUnknownPlacementCreditsNoDevice(t *testing.T) {
	const deviceID = capability.DeviceIdentity("GPU-fleet-single-0000")
	fleet := gpuFleet(t, acceleratedHost("gpu-host",
		card(string(deviceID), 24*capability.GiB, 24*capability.GiB)))

	_, err := fleet.Place(placeRequest(gpuEntry("first-16gib", 16*capability.GiB)))
	require.NoError(t, err)
	before := fleet.Commitment("gpu-host")

	require.False(t, fleet.Release("no-such-placement#99"))
	require.Equal(t, before, fleet.Commitment("gpu-host"),
		"releasing an unknown id changed the device ledger")
}

// TestACPUModelCommitsNoDevice. An entry that runs on the processor must not
// draw down a card it never touches.
func TestACPUModelCommitsNoDevice(t *testing.T) {
	const deviceID = capability.DeviceIdentity("GPU-fleet-single-0000")
	fleet := gpuFleet(t, acceleratedHost("gpu-host",
		card(string(deviceID), 24*capability.GiB, 24*capability.GiB)))

	res, err := fleet.Place(placeRequest(
		placementEntry("cpu-model", 20*capability.GiB, 40*capability.GiB)))
	require.NoError(t, err)
	require.Empty(t, res.Chosen.Option.Headroom.AcceleratorDevice,
		"an option that needs no device must name none")

	c := fleet.Commitment("gpu-host")
	require.Empty(t, c.Devices, "a CPU-served model must not commit device memory")
	require.Equal(t, 20*capability.GiB, c.MemoryBytes, "host memory is still committed")

	available, known := fleet.Available("gpu-host")
	require.True(t, known)
	require.Equal(t, 24*capability.GiB, available.Accelerators[0].MemoryAvailable,
		"the card must be untouched by a model that does not use it")
}

// TestConcurrentDevicePlacementsDoNotOverCommitTheCard. Deciding and committing
// separately is the concurrent form of the over-commitment defect: two callers
// both read a card with room, both conclude it fits, and both commit. The
// device axis has to hold under contention exactly as the memory axis does.
func TestConcurrentDevicePlacementsDoNotOverCommitTheCard(t *testing.T) {
	const deviceID = capability.DeviceIdentity("GPU-fleet-single-0000")
	build := func() *selection.Fleet {
		return gpuFleet(t, acceleratedHost("gpu-host",
			card(string(deviceID), 24*capability.GiB, 24*capability.GiB)))
	}
	model := gpuEntry("m-6gib", 6*capability.GiB)
	const attempts = 16

	sequential := build()
	expected := 0
	for range attempts {
		if _, err := sequential.Place(placeRequest(model)); err == nil {
			expected++
		}
	}
	require.Positive(t, expected, "the card must hold at least one copy for this to mean anything")
	require.Less(t, expected, attempts, "the card must run out, or nothing is being contended for")

	concurrent := build()
	var wg sync.WaitGroup
	granted := make([]bool, attempts)
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := concurrent.Place(placeRequest(model))
			granted[i] = err == nil
		}()
	}
	wg.Wait()

	count := 0
	for _, ok := range granted {
		if ok {
			count++
		}
	}
	require.Equal(t, expected, count,
		"deciding device placements concurrently changed how many the card granted")

	c := concurrent.Commitment("gpu-host")
	require.LessOrEqual(t, deviceCommitment(c, deviceID).MemoryBytes, 24*capability.GiB,
		"the card was committed beyond its measured free memory")
	require.Equal(t, sequential.Commitment("gpu-host"), c,
		"the card ended in a different state under concurrency")

	remaining, known := concurrent.Available("gpu-host")
	require.True(t, known)
	require.LessOrEqual(t, remaining.Accelerators[0].MemoryAvailable,
		remaining.Accelerators[0].MemoryTotal,
		"an over-committed card would underflow its own reading")
}
