package selection_test

// OPEN-24: the agent lane's configured candidates were absent from the
// catalogue, so an operator holding working weights was REFUSED rather than
// served. That is a narrowing of what the product can do, not a tidy-up, and
// the refusal it produced said so in the worst possible way: "this host
// provides no catalogue-entry … more memory does not help;
// remedy=different-approach". The operator's host was fine. The catalogue was
// empty.
//
// mistral-nemo-12b is now a live entry, carrying a footprint that was MEASURED
// by running the weights rather than read off a vendor page — see
// specs/002-adaptive-local-model-serving/research/06-agent-lane-footprint-measurements.md.
// This is the guard that it stays offerable.
//
// POLARITY SWITCH (§11.4.115) — one source, two roles:
//
//	RED_MODE unset / RED_MODE=0 (DEFAULT, post-change) — standing regression
//	                 guard: the entry is OFFERED to an accelerator-bound agent
//	                 lane on a host that can run it.
//	RED_MODE=1     — reproduction of the pre-change catalogue, by removing the
//	                 entry from the loaded set: the model is absent, and the
//	                 lane can only refuse it for the reason OPEN-24 recorded.
//
// Honest boundary (§11.4.6). RED_MODE reproduces at the DATA seam — it drops
// the entry from a loaded catalogue rather than checking out the pre-change
// file — because the defect WAS the absence of that data. It needs no external
// fixture and no path outside the repository, so it keeps working wherever the
// suite runs.
//
// The host is a fixture, and only the host is. The figures the selection rules
// are fed are the measured ones, read from the shipped catalogue.

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

// open24Model is the entry this guard is about.
const open24Model = "mistral-nemo-12b"

// These are the MEASURED figures. They are asserted, not merely read, because
// the whole point of OPEN-24 is that the number an operator is admitted on has
// to be one somebody produced by running the thing. A later edit that quietly
// restores the researched 7623566950 B — a published VRAM figure, 2.3 GiB
// BELOW the measured CPU footprint — would offer this model to hosts that then
// cannot run it, and would fail here.
const (
	open24MeasuredMemoryBytes  = uint64(9956106240)
	open24MeasuredStorageBytes = uint64(7477208192)
)

func open24RedMode() bool { return os.Getenv("RED_MODE") == "1" }

// open24Host has RAM and a card both comfortably above the measured figure.
// The card matters: every *-boot lane is AcceleratorBound, so the memory
// figure is checked against the device as well as against host RAM.
func open24Host() capability.HostCapabilityProfile {
	p := fixtures.SingleAccelerator()
	p.HostIdentity = "fixture-open24-can-run-nemo"
	p.MemoryTotal = 48 * capability.GiB
	p.MemoryAvailable = 40 * capability.GiB
	p.StorageAvailable = 500 * capability.GiB
	p.Accelerators = []capability.Accelerator{{
		Identity:        capability.DeviceIdentity("GPU-fixture-24gib-0000"),
		Model:           "fixture 24 GiB accelerator",
		API:             capability.APICUDA,
		MemoryTotal:     24 * capability.GiB,
		MemoryAvailable: 22 * capability.GiB,
	}}
	return p
}

func TestAgentLaneAdmitsTheMeasuredMistralNemo(t *testing.T) {
	loaded, err := catalogue.Load(shippedCatalogueDir)
	require.NoError(t, err)

	entries := loaded.Entries()
	var entry *catalogue.Entry
	for i := range entries {
		if entries[i].ModelID == open24Model {
			entry = &entries[i]
		}
	}

	if open24RedMode() {
		// The pre-change catalogue exactly: the entry is not in it.
		kept := entries[:0:0]
		for _, e := range entries {
			if e.ModelID != open24Model {
				kept = append(kept, e)
			}
		}
		entries, entry = kept, nil
	} else {
		require.NotNil(t, entry,
			"the measured entry has been removed from the shipped catalogue; "+
				"OPEN-24's narrowing is back and operators holding this GGUF are refused again")
	}

	res, err := selection.Select(selection.Request{
		Profile:          open24Host(),
		Entries:          entries,
		Families:         []catalogue.CapabilityFamily{catalogue.FamilyText},
		DeclaredUsage:    catalogue.UsageCommercial,
		Now:              time.Now().UTC(),
		MaxProfileAge:    time.Minute,
		Reserve:          runtime.SelectionReserve(),
		AcceleratorBound: true,
	})
	require.NoError(t, err)
	fr, ok := res.Family(catalogue.FamilyText)
	require.True(t, ok)

	var offered *selection.Option
	for i := range fr.Offered {
		if fr.Offered[i].ModelID == open24Model {
			offered = &fr.Offered[i]
		}
	}

	if open24RedMode() {
		require.Nil(t, offered,
			"RED: with the entry removed the lane must not be able to offer it")
		t.Logf("RED reproduced: %s is absent from the catalogue; the lane can only "+
			"refuse it as an unknown model, which is OPEN-24. %d other text options offered.",
			open24Model, len(fr.Offered))
		return
	}

	require.NotNil(t, offered,
		"GREEN: a host with 40 GiB free and a 22 GiB card can run a 9496 MiB model; "+
			"if it is not offered, admission has regressed")
	require.Equal(t, "q4_k_m", offered.Variant)
	require.Equal(t, open24MeasuredMemoryBytes, offered.Cost.MemoryRequiredBytes,
		"the figure the lane admits on must be the MEASURED one")
	require.Equal(t, open24MeasuredStorageBytes, offered.Cost.StorageRequiredBytes)

	require.NoError(t, entry.Validate())
	require.NoError(t, entry.ValidateForAcquisition(),
		"this entry's digest was verified against the bytes on disk, so unlike every "+
			"other entry here it must also pass the gate that precedes touching a weight file")

	t.Logf("GREEN: OFFERED %s memory=%dMiB storage=%dMiB source=%s",
		offered.Identity,
		offered.Cost.MemoryRequiredBytes/(1024*1024),
		offered.Cost.StorageRequiredBytes/(1024*1024),
		entry.Source)
}
