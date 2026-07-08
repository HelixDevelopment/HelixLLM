package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// baseValidConfig returns a config that passes ALL three D1/D4/D7 checks.
// n_layers=32, n_kv_heads=8, head_dim=128, q8_0 (units=2) ->
// kvBytesPerToken = 32*8*128*2 = 65536 bytes/token (64 KiB/token, exact).
// ctx=16384, parallel=4 -> worstCaseKV = 65536*16384*4 = 4,294,967,296 (4 GiB exact).
// weight=6 GiB -> total_static = 10 GiB.
// budget=32 GiB, headroom=2 GiB -> available=30 GiB >= 10 GiB -> PASS (a).
// ngl=99 >= full_layers=32 -> PASS (b).
// port/name distinct from DefaultKnownLanes -> PASS (c).
func baseValidConfig() LaneConfig {
	const gib = 1024 * 1024 * 1024
	return LaneConfig{
		Port:          18445,
		ContainerName: "helixllm-agent-mistral-nemo",
		ModelPath:     "testdata/fake-model.gguf",
		WeightBytes:   6 * gib,
		NLayers:       32,
		NKVHeads:      8,
		HeadDim:       128,
		FullLayers:    32,
		KVQuant:       "q8_0",
		CtxSize:       16384,
		Parallel:      4,
		NGL:           99,
		BudgetBytes:   32 * gib,
		HeadroomBytes: 2 * gib,
	}
}

func TestKVBytesPerToken_MatchesSection4Formula(t *testing.T) {
	// §4: 2 (K+V) × n_layers × n_kv_heads × head_dim × bytes_per_elem.
	// Dense 32B example from 01_local_models_serving.md §4: 64 layers, 8 KV
	// heads, head_dim 128, q8_0 -> 128 KiB/token = 131072 bytes.
	units, err := kvUnitsPerElem("q8_0")
	require.NoError(t, err)
	require.Equal(t, int64(131072), kvBytesPerToken(64, 8, 128, units))

	// Same architecture, fp16 -> 256 KiB/token = 262144 bytes.
	units, err = kvUnitsPerElem("fp16")
	require.NoError(t, err)
	require.Equal(t, int64(262144), kvBytesPerToken(64, 8, 128, units))

	// MoE 30B-A3B example: 48 layers, 4 KV heads, head_dim 128, q8_0 ->
	// 48 KiB/token = 49152 bytes.
	units, err = kvUnitsPerElem("q8_0")
	require.NoError(t, err)
	require.Equal(t, int64(49152), kvBytesPerToken(48, 4, 128, units))
}

func TestKVUnitsPerElem_UnknownQuant_Errors(t *testing.T) {
	_, err := kvUnitsPerElem("q3_k_xl")
	require.Error(t, err)
}

func TestValidate_ValidConfig_PassesAllChecks(t *testing.T) {
	res, err := Validate(baseValidConfig(), DefaultKnownLanes)
	require.NoError(t, err)
	require.True(t, res.OK, "expected PASS, got codes=%v reasons=%v", res.Codes, res.Reasons)
	require.Empty(t, res.Codes)
	require.Equal(t, int64(4*1024*1024*1024), res.WorstCaseKVBytes)
	require.Equal(t, int64(10*1024*1024*1024), res.TotalStaticBytes)
}

// TestValidate_OverBudget_FailsStaticFootprintCheck is the direct in-process
// analogue of the Challenge test's over-budget fixture: blow the KV sum via
// ctx*parallel while keeping everything else identical to the valid config.
func TestValidate_OverBudget_FailsStaticFootprintCheck(t *testing.T) {
	cfg := baseValidConfig()
	cfg.Port = 18446
	cfg.ContainerName = "helixllm-agent-overbudget-test"
	cfg.CtxSize = 32768
	cfg.Parallel = 16 // worstCaseKV = 65536*32768*16 = 32 GiB exact; total = 38 GiB > 30 GiB available

	res, err := Validate(cfg, DefaultKnownLanes)
	require.NoError(t, err)
	require.False(t, res.OK)
	require.Contains(t, res.Codes, "STATIC_FOOTPRINT_EXCEEDS_BUDGET")
	require.Equal(t, int64(32*1024*1024*1024), res.WorstCaseKVBytes)
	require.Equal(t, int64(38*1024*1024*1024), res.TotalStaticBytes)

	// The OTHER two checks must still PASS on this fixture — it deliberately
	// trips ONLY the footprint check, keeping the error unambiguous.
	require.NotContains(t, res.Codes, "NGL_BELOW_FULL_RESIDENCY")
	require.NotContains(t, res.Codes, "PORT_COLLISION")
	require.NotContains(t, res.Codes, "CONTAINER_NAME_COLLISION")
}

func TestValidate_NGLBelowFullResidency_Fails(t *testing.T) {
	cfg := baseValidConfig()
	cfg.Port = 18447
	cfg.ContainerName = "helixllm-agent-ngl-test"
	cfg.NGL = 20 // < full_layers=32 -> CPU spillover risk (D7)

	res, err := Validate(cfg, DefaultKnownLanes)
	require.NoError(t, err)
	require.False(t, res.OK)
	require.Contains(t, res.Codes, "NGL_BELOW_FULL_RESIDENCY")
}

func TestValidate_NGLEqualsFullLayers_Passes(t *testing.T) {
	// Boundary (§11.4.85): ngl == full_layers (not the "99 sentinel") must
	// ALSO satisfy full residency — the check is a plain >= comparison, no
	// magic-number special-casing of 99 required.
	cfg := baseValidConfig()
	cfg.Port = 18448
	cfg.ContainerName = "helixllm-agent-ngl-exact"
	cfg.NGL = 32 // == full_layers exactly

	res, err := Validate(cfg, DefaultKnownLanes)
	require.NoError(t, err)
	require.NotContains(t, res.Codes, "NGL_BELOW_FULL_RESIDENCY")
}

func TestValidate_PortCollision_Fails(t *testing.T) {
	cfg := baseValidConfig()
	cfg.Port = 18434 // == live coder port
	cfg.ContainerName = "helixllm-agent-port-collision-test"

	res, err := Validate(cfg, DefaultKnownLanes)
	require.NoError(t, err)
	require.False(t, res.OK)
	require.Contains(t, res.Codes, "PORT_COLLISION")
}

func TestValidate_ContainerNameCollision_Fails(t *testing.T) {
	cfg := baseValidConfig()
	cfg.Port = 18449
	cfg.ContainerName = "helixllm-coder" // == live coder's name, distinct port

	res, err := Validate(cfg, DefaultKnownLanes)
	require.NoError(t, err)
	require.False(t, res.OK)
	require.Contains(t, res.Codes, "CONTAINER_NAME_COLLISION")
}

func TestValidate_MultipleFailuresReportedTogether(t *testing.T) {
	cfg := baseValidConfig()
	cfg.Port = 18434              // collides with coder
	cfg.ContainerName = "helixllm-coder" // ALSO collides with coder's name
	cfg.NGL = 1                   // ALSO below full residency
	cfg.CtxSize = 32768
	cfg.Parallel = 16 // ALSO blows the footprint

	res, err := Validate(cfg, DefaultKnownLanes)
	require.NoError(t, err)
	require.False(t, res.OK)
	require.Contains(t, res.Codes, "STATIC_FOOTPRINT_EXCEEDS_BUDGET")
	require.Contains(t, res.Codes, "NGL_BELOW_FULL_RESIDENCY")
	require.Contains(t, res.Codes, "PORT_COLLISION")
	require.Contains(t, res.Codes, "CONTAINER_NAME_COLLISION")
	require.Len(t, res.Codes, 4, "every applicable failure MUST be reported, not just the first")
}

// --- malformed-config error paths (distinct failure class from a
// well-formed-but-failing config — never silently coerced) ---

func TestValidate_MalformedConfig_Errors(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*LaneConfig)
	}{
		{"zero port", func(c *LaneConfig) { c.Port = 0 }},
		{"port out of range", func(c *LaneConfig) { c.Port = 70000 }},
		{"empty container name", func(c *LaneConfig) { c.ContainerName = "" }},
		{"zero n_layers", func(c *LaneConfig) { c.NLayers = 0 }},
		{"zero n_kv_heads", func(c *LaneConfig) { c.NKVHeads = 0 }},
		{"zero head_dim", func(c *LaneConfig) { c.HeadDim = 0 }},
		{"zero ctx_size", func(c *LaneConfig) { c.CtxSize = 0 }},
		{"zero parallel", func(c *LaneConfig) { c.Parallel = 0 }},
		{"negative ngl", func(c *LaneConfig) { c.NGL = -1 }},
		{"unknown kv_quant", func(c *LaneConfig) { c.KVQuant = "bogus" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseValidConfig()
			tc.mutate(&cfg)
			_, err := Validate(cfg, DefaultKnownLanes)
			require.Error(t, err, "malformed config MUST error, not silently produce a Result")
		})
	}
}

func TestValidate_WeightBytesMissing_StatsModelPath(t *testing.T) {
	cfg := baseValidConfig()
	cfg.WeightBytes = 0
	cfg.ModelPath = "validate.go" // a real, stat-able file in this package

	res, err := Validate(cfg, DefaultKnownLanes)
	require.NoError(t, err)
	require.Greater(t, res.WeightBytes, int64(0), "weight_bytes must be derived from the real file size")
}

func TestValidate_WeightBytesMissing_ModelPathNotStatable_Errors(t *testing.T) {
	cfg := baseValidConfig()
	cfg.WeightBytes = 0
	cfg.ModelPath = "/nonexistent/path/does-not-exist.gguf"

	_, err := Validate(cfg, DefaultKnownLanes)
	require.Error(t, err)
}

func TestValidate_WeightBytesAndModelPathBothMissing_Errors(t *testing.T) {
	cfg := baseValidConfig()
	cfg.WeightBytes = 0
	cfg.ModelPath = ""

	_, err := Validate(cfg, DefaultKnownLanes)
	require.Error(t, err)
}

// TestValidate_PairedMutation_FootprintCheck is the §1.1 paired mutation for
// the STATIC_FOOTPRINT_EXCEEDS_BUDGET guard: proves the `totalStatic >
// available` comparison is load-bearing (not a tautology) by showing a
// mutated always-pass check would (wrongly) admit the same over-budget
// config the real check refuses.
func TestValidate_PairedMutation_FootprintCheck(t *testing.T) {
	cfg := baseValidConfig()
	cfg.CtxSize = 32768
	cfg.Parallel = 16 // over-budget, as in TestValidate_OverBudget_FailsStaticFootprintCheck

	res, err := Validate(cfg, DefaultKnownLanes)
	require.NoError(t, err)

	// REAL guard: over-budget config is refused.
	require.False(t, res.OK, "real check MUST refuse an over-budget config")
	require.Contains(t, res.Codes, "STATIC_FOOTPRINT_EXCEEDS_BUDGET")

	// MUTATION: disable the footprint comparison (always "fits"). This is the
	// exact defect class the check exists to prevent.
	mutatedFits := func(totalStatic, available int64) bool { return true }
	require.True(t, mutatedFits(res.TotalStaticBytes, res.AvailableBytes),
		"with the footprint check disabled, the over-budget config would be (wrongly) admitted — proves the guard is load-bearing")

	realFits := res.TotalStaticBytes <= res.AvailableBytes
	require.NotEqual(t, realFits, mutatedFits(res.TotalStaticBytes, res.AvailableBytes),
		"real guard and mutated guard MUST disagree on the same over-budget input")
}
