package laneboot

// The lane's request must declare BOTH halves of its agreement with the
// admission gate.
//
// Every *-boot binary selects here and then admits through vrambroker with the
// chosen option's MemoryRequiredBytes. Two fields make selection agree with
// that gate, and until this change only one of them was stated:
//
//   - Reserve  — the gate's device-memory margin. It was stated, and it is the
//     basis of agentgen-boot's claim that the lane "cannot offer a model it
//     will then refuse to start".
//   - AcceleratorBound — that the figure is SPENT on the device. It was not
//     stated, so the margin applied on an axis that entries with
//     requires_accelerator: false never reached, and the claim was false for
//     five of the six live entries in the shipped text catalogue.
//
// This is a unit test of the request shape rather than of Decide, because
// Decide measures the host and reads the environment; the two fields under test
// are neither measured nor configured.

import (
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/runtime"
	"github.com/HelixDevelopment/HelixLLM/internal/selection"
	"github.com/stretchr/testify/require"
)

func TestLaneRequestDeclaresTheAcceleratorBinding(t *testing.T) {
	loaded, err := catalogue.Load("../catalogue/data")
	require.NoError(t, err)

	req := selectionRequest(
		capability.HostCapabilityProfile{HostIdentity: "unit-fixture"},
		loaded,
		catalogue.FamilyText,
		catalogue.UsageCommercial,
		nil,
		time.Minute,
	)

	require.True(t, req.AcceleratorBound,
		"every lane admits the chosen option's memory figure against the card "+
			"(vrambroker.Acquire); selection must be told so, or it checks that figure "+
			"against host RAM and the lane spends it on the device")

	require.Equal(t, runtime.SelectionReserve(), req.Reserve,
		"selection must hold back the same device-memory margin the admission gate does")
	require.NotZero(t, req.Reserve.AcceleratorHeadroomBytes,
		"a zero margin would make the declaration above check the raw card, not the "+
			"card the gate will actually admit against")
}

func TestLaneRequestCarriesTheCandidatesAndConstraints(t *testing.T) {
	loaded, err := catalogue.Load("../catalogue/data")
	require.NoError(t, err)

	pin := &selection.Pin{ModelID: "qwen3-0.6b", Variant: "q4_0"}
	req := selectionRequest(
		capability.HostCapabilityProfile{HostIdentity: "unit-fixture"},
		loaded,
		catalogue.FamilyText,
		catalogue.UsageResearch,
		pin,
		2*time.Minute,
	)

	require.Equal(t, []catalogue.CapabilityFamily{catalogue.FamilyText}, req.Families)
	require.Equal(t, catalogue.UsageResearch, req.DeclaredUsage)
	require.Same(t, pin, req.Pin, "the pin is a constraint the caller stated; it must reach selection")
	require.Equal(t, 2*time.Minute, req.MaxProfileAge)
	require.NotEmpty(t, req.Entries, "the catalogue's entries must reach selection")
	require.False(t, req.Now.IsZero(), "the decision instant must be stated, not left for selection to invent")
}
