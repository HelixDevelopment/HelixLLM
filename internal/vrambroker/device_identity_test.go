package vrambroker

import (
	"context"
	"os/exec"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/stretchr/testify/require"
)

// CRITICAL-6. The budget a reservation is admitted against must belong to a
// device named by a STABLE identity, never by its enumeration position
// (§11.4.111).
//
// nvidia-smi emits one row per GPU and the row order is assigned at discovery
// time. Reading "the first row" therefore binds the budget to whichever card
// happened to enumerate first — which changes when a second card is added,
// when the driver reloads, or when the boot order differs. The card the budget
// was checked against then need not be the card the work lands on: admission
// passes on 24 GB of free memory while the model loads onto a 4 GB device.

// twoGPUs is the nvidia-smi output of a host with a small card enumerated
// FIRST and a large one second — the arrangement under which position-binding
// and identity-binding disagree.
const twoGPUs = "GPU-aaaa-1111, 00000000:09:00.0, 8192, 7000, 1192\n" +
	"GPU-bbbb-2222, 00000000:41:00.0, 32607, 19432, 12689\n"

const (
	smallCard = capability.DeviceIdentity("GPU-aaaa-1111")
	largeCard = capability.DeviceIdentity("GPU-bbbb-2222")
)

// TestBudgetFollowsDeviceIdentityNotPosition is the reproduction: asking for
// the large card must yield the large card's numbers whichever row it occupies.
func TestBudgetFollowsDeviceIdentityNotPosition(t *testing.T) {
	devices, err := parseSMICSV(twoGPUs)
	require.NoError(t, err)
	require.Len(t, devices, 2, "both GPU rows must be parsed, not just the first")

	large, err := selectDevice(devices, largeCard)
	require.NoError(t, err)
	require.Equal(t, int64(32607)*MiB, large.Total,
		"the budget was taken from a position rather than from the named device")
	require.Equal(t, int64(12689)*MiB, large.Free)

	small, err := selectDevice(devices, smallCard)
	require.NoError(t, err)
	require.Equal(t, int64(8192)*MiB, small.Total)
	require.Equal(t, int64(1192)*MiB, small.Free)
}

// TestBudgetIdentityIsOrderInvariant. The same two cards reported in the
// opposite order must answer identically. This is the assertion a positional
// read cannot satisfy.
func TestBudgetIdentityIsOrderInvariant(t *testing.T) {
	reversed := "GPU-bbbb-2222, 00000000:41:00.0, 32607, 19432, 12689\n" +
		"GPU-aaaa-1111, 00000000:09:00.0, 8192, 7000, 1192\n"

	forward, err := parseSMICSV(twoGPUs)
	require.NoError(t, err)
	back, err := parseSMICSV(reversed)
	require.NoError(t, err)

	for _, id := range []capability.DeviceIdentity{smallCard, largeCard} {
		a, err := selectDevice(forward, id)
		require.NoError(t, err)
		b, err := selectDevice(back, id)
		require.NoError(t, err)
		require.Equal(t, a, b, "enumeration order changed which device %q resolved to", id)
	}
}

// TestPCIAddressAlsoIdentifiesTheDevice. A PCI bus address is as stable an
// identity as a UUID, and is what an operator reads off lspci.
func TestPCIAddressAlsoIdentifiesTheDevice(t *testing.T) {
	devices, err := parseSMICSV(twoGPUs)
	require.NoError(t, err)

	d, err := selectDevice(devices, capability.DeviceIdentity("00000000:41:00.0"))
	require.NoError(t, err)
	require.Equal(t, int64(32607)*MiB, d.Total)
}

// TestUnnamedDeviceOnMultiGPUHostFailsClosed. With several cards present and
// none named, there is no honest answer — picking one is a guess about where
// the work will land. It must refuse rather than silently return row zero.
func TestUnnamedDeviceOnMultiGPUHostFailsClosed(t *testing.T) {
	devices, err := parseSMICSV(twoGPUs)
	require.NoError(t, err)

	_, err = selectDevice(devices, "")
	require.ErrorIs(t, err, ErrDeviceAmbiguous,
		"a multi-GPU host with no device named must refuse, never pick a position")
}

// TestUnnamedDeviceOnSingleGPUHostIsUnambiguous. One row is not a position —
// it is the only device — so an unconfigured single-GPU host still works.
func TestUnnamedDeviceOnSingleGPUHostIsUnambiguous(t *testing.T) {
	devices, err := parseSMICSV("GPU-only-0000, 00000000:09:00.0, 12288, 130, 11781\n")
	require.NoError(t, err)

	d, err := selectDevice(devices, "")
	require.NoError(t, err)
	require.Equal(t, int64(12288)*MiB, d.Total)
	require.Equal(t, int64(11781)*MiB, d.Free)
}

// TestNamedDeviceAbsentFailsClosed. A configured card that is not present is a
// misconfiguration or a card that has gone; either way admitting against some
// other device is exactly the substitution this rule forbids.
func TestNamedDeviceAbsentFailsClosed(t *testing.T) {
	devices, err := parseSMICSV(twoGPUs)
	require.NoError(t, err)

	_, err = selectDevice(devices, capability.DeviceIdentity("GPU-cccc-3333"))
	require.ErrorIs(t, err, ErrDeviceNotFound)
}

// TestQueryAsksForTheIdentifyingFields. A row cannot be resolved by identity if
// the query never asked nvidia-smi for one.
func TestQueryAsksForTheIdentifyingFields(t *testing.T) {
	joined := ""
	for _, a := range nvidiaSMIArgs {
		joined += a + " "
	}
	require.Contains(t, joined, "uuid", "the query must ask for the device UUID")
	require.Contains(t, joined, "pci.bus_id", "the query must ask for the PCI address")
}

// TestRealNvidiaSMIResolvesByIdentity exercises the CHANGED query against the
// real tool on a host that has one. It is the layer the fixtures above cannot
// reach: that nvidia-smi actually accepts the added `uuid,pci.bus_id` columns
// and emits them in the position the parser expects.
//
// Where no accelerator is present the property genuinely cannot be read, and
// the test SKIPs saying so rather than passing on nothing measured.
func TestRealNvidiaSMIResolvesByIdentity(t *testing.T) {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		t.Skip("SKIP-OK: nvidia-smi not on PATH; the real-query layer is not exercisable on this host")
	}

	devices, err := readNvidiaSMI(context.Background())
	if err != nil {
		t.Skipf("SKIP-OK: nvidia-smi present but reported no usable GPU (%v); nothing measured to resolve", err)
	}
	require.NotEmpty(t, devices)

	for _, d := range devices {
		require.NotEmpty(t, string(d.UUID)+string(d.PCIBus), "a measured device carried no identity")
		require.Positive(t, d.Total, "device %s reported a non-positive total", d.UUID)
		require.LessOrEqual(t, d.Free, d.Total)

		byUUID, err := selectDevice(devices, d.UUID)
		require.NoError(t, err)
		byPCI, err := selectDevice(devices, d.PCIBus)
		require.NoError(t, err)
		require.Equal(t, byUUID, byPCI, "UUID and PCI address resolved to different devices")

		t.Logf("measured device: uuid=%s pci=%s total=%d MiB used=%d MiB free=%d MiB",
			d.UUID, d.PCIBus, d.Total/MiB, d.Used/MiB, d.Free/MiB)
	}
}
