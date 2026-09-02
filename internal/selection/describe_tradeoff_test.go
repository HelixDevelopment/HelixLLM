package selection_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/runtime"
	"github.com/HelixDevelopment/HelixLLM/internal/selection"
	"github.com/stretchr/testify/require"
)

// T074 — the speed trade-off is labelled on every streaming option.
//
// This file lives in the EXTERNAL test package deliberately. internal/runtime
// imports internal/selection, so selection itself cannot import runtime; an
// external test package can import both, which is what makes the drift guard
// below possible at all.

// fieldsByKey indexes a composed description so a test can assert on one datum
// without depending on the order the fields happen to be emitted in.
func fieldsByKey(fields []selection.Field) map[selection.FieldKey]string {
	byKey := make(map[selection.FieldKey]string, len(fields))
	for _, f := range fields {
		byKey[f.Key] = f.Value
	}
	return byKey
}

// streamingOption is an offer served by the streaming runtime, carrying the
// throughput the rostered fixture records for that path.
func streamingOption() selection.Option {
	return selection.Option{
		ModelID:      "deepseek-r1-671b",
		Variant:      "q4_k_m",
		Identity:     "helixllm/host-a/deepseek-r1-671b:q4_k_m",
		HostIdentity: "host-a",
		Family:       catalogue.FamilyText,
		Runtime:      catalogue.RuntimeStreaming,
		Cost:         selection.ResourceCost{MemoryRequiredBytes: 20 << 30, StorageRequiredBytes: 372 << 30},
		Expected: catalogue.ExpectedCapability{
			ContextTokens:             65536,
			ThroughputTokensPerSecond: 3.4,
		},
		Terms: catalogue.UsageTerms{LicenseID: "MIT"},
	}
}

// inMemoryOption is the same shape served by the preferred path.
func inMemoryOption() selection.Option {
	o := streamingOption()
	o.Runtime = catalogue.RuntimeInMemory
	o.ModelID = "qwen2.5-coder-7b-instruct"
	o.Expected.ThroughputTokensPerSecond = 22.5
	return o
}

// TestEveryStreamingOptionIsLabelledWithItsSpeedTradeoff.
//
// The streaming runtime buys feasibility with throughput, and the price is
// orders of magnitude — the same model runs at roughly 9 tokens/second with its
// working set resident and 0.05–0.1 streaming cold from disk. A user choosing
// this option is choosing slowness for feasibility, and can only choose it
// knowingly if the offer says so.
func TestEveryStreamingOptionIsLabelledWithItsSpeedTradeoff(t *testing.T) {
	got := fieldsByKey(selection.DescribeOption(streamingOption()))

	require.Equal(t, "true", got[selection.FieldFallback],
		"a streaming option is a fallback path, and the offer must say so")
	require.Equal(t, "throughput", got[selection.FieldTradeoffCost],
		"what it costs")
	require.Equal(t, "weights-streamed-from-disk", got[selection.FieldTradeoffCause],
		"why it costs that — a reader seeing a low figure with no cause cannot tell a slow model from a slow path")
}

// TestAnInMemoryOptionCarriesNoTradeoffLabel.
//
// The preferred path trades nothing. Emitting a cost field on it would teach a
// reader that the field carries no information, and the streaming label would
// then mean nothing either.
func TestAnInMemoryOptionCarriesNoTradeoffLabel(t *testing.T) {
	got := fieldsByKey(selection.DescribeOption(inMemoryOption()))

	require.NotContains(t, got, selection.FieldFallback)
	require.NotContains(t, got, selection.FieldTradeoffCost)
	require.NotContains(t, got, selection.FieldTradeoffCause)
}

// TestTheTwoOptionsAreDistinguishableOnTheirDescriptionsAlone.
//
// This is the requirement stated as the user meets it: two offers, described
// side by side, must not look alike when one is a hundred times slower. A
// presentation layer given only these fields must be able to tell them apart.
func TestTheTwoOptionsAreDistinguishableOnTheirDescriptionsAlone(t *testing.T) {
	streaming := fieldsByKey(selection.DescribeOption(streamingOption()))
	inMemory := fieldsByKey(selection.DescribeOption(inMemoryOption()))

	require.NotEqual(t, streaming[selection.FieldFallback], inMemory[selection.FieldFallback],
		"the two offers must differ on the fact that decides whether the user is accepting slowness")
	require.NotEmpty(t, streaming[selection.FieldTradeoffCause])
	require.Empty(t, inMemory[selection.FieldTradeoffCause])
}

// TestTheTradeoffLabelIsEmittedForEveryStreamingOptionWithoutException.
//
// "Every streaming option" is the requirement, so it is asserted across varying
// options rather than on one. An implementation that labelled only options with
// a recorded throughput figure, or only those above some size, would pass a
// single-case test and leave real offers unlabelled.
func TestTheTradeoffLabelIsEmittedForEveryStreamingOptionWithoutException(t *testing.T) {
	base := streamingOption()

	noThroughput := base
	noThroughput.Expected.ThroughputTokensPerSecond = 0

	noVariant := base
	noVariant.Variant = ""

	tiny := base
	tiny.Cost = selection.ResourceCost{MemoryRequiredBytes: 1 << 20, StorageRequiredBytes: 1 << 20}

	for name, o := range map[string]selection.Option{
		"rostered":         base,
		"no-throughput":    noThroughput,
		"no-variant":       noVariant,
		"tiny-requirement": tiny,
	} {
		got := fieldsByKey(selection.DescribeOption(o))
		require.Equal(t, "true", got[selection.FieldFallback], "option %q must be labelled", name)
		require.Equal(t, "throughput", got[selection.FieldTradeoffCost], "option %q must state its cost", name)
		require.Equal(t, "weights-streamed-from-disk", got[selection.FieldTradeoffCause],
			"option %q must state its cause", name)
	}
}

// TestTheTradeoffKeysAreTheRuntimePackagesOwnSpelling.
//
// The trade-off's machine keys are restated in selection because
// internal/runtime imports this package and importing it back would be a cycle.
// A restatement can drift, and a drifted key reaches the presentation layer as
// a value it has no wording for — the CONST-046 failure this whole
// machine-key discipline exists to prevent.
//
// This test is the drift guard, and it is only possible from the external test
// package, which may import both.
func TestTheTradeoffKeysAreTheRuntimePackagesOwnSpelling(t *testing.T) {
	got := fieldsByKey(selection.DescribeOption(streamingOption()))

	require.Equal(t, string(runtime.TradeoffThroughput), got[selection.FieldTradeoffCost],
		"selection's restated cost key must equal runtime.TradeoffThroughput exactly")
	require.Equal(t, string(runtime.CauseWeightsStreamedFromDisk), got[selection.FieldTradeoffCause],
		"selection's restated cause key must equal runtime.CauseWeightsStreamedFromDisk exactly")
}

// TestTheNewFieldKeysAreInTheClosedSet.
//
// A presentation layer maps the closed set exhaustively. A key emitted but not
// recorded is one the layer cannot be told about, so it renders as nothing —
// the label would be present in the data and absent from the screen.
func TestTheNewFieldKeysAreInTheClosedSet(t *testing.T) {
	for _, k := range []selection.FieldKey{
		selection.FieldFallback,
		selection.FieldTradeoffCost,
		selection.FieldTradeoffCause,
	} {
		require.True(t, k.Known(), "field key %q must be in the recorded set", k)
	}
}

// TestDescribedOptionStillCarriesEverythingItCarriedBefore.
//
// The trade-off is ADDED to the description, never in place of anything. A
// reader comparing two offers still needs the runtime, the throughput figure,
// the context window and the licence.
func TestDescribedOptionStillCarriesEverythingItCarriedBefore(t *testing.T) {
	got := fieldsByKey(selection.DescribeOption(streamingOption()))

	require.Equal(t, string(catalogue.RuntimeStreaming), got[selection.FieldRuntime])
	require.Equal(t, "3.4", got[selection.FieldThroughput],
		"the figure stays; the trade-off fields say what the figure means")
	require.Equal(t, "65536", got[selection.FieldContextTokens])
	require.Equal(t, "MIT", got[selection.FieldLicense])
	require.Equal(t, "helixllm/host-a/deepseek-r1-671b:q4_k_m", got[selection.FieldIdentity])
}
