package models

import "testing"

// Standing regression guard for the CPU-inference catalog entries added in
// 08de5c5 ("feat(cpu): CPU-only inference — DeepSeek V4 models, ...").
//
// That commit added two CPU-only models (deepseek-v4-flash ~120 GB RAM,
// deepseek-v4-pro ~680 GB RAM) which carry RAMRequired but deliberately
// leave VRAMRequired at zero. In the SAME commit FilterByVRAM gained a
// `m.VRAMRequired > 0 &&` guard. That guard is load-bearing, not cosmetic:
// without it every zero-VRAM model compares `0 <= maxVRAM` and is selected
// for GPU inference at ANY free-VRAM level, including a nearly-full GPU.
// The gateway would then try to auto-download a 120 GB and a 680 GB file.
//
// TestFilterByVRAM_ExcludesCPUOnlyModels pins the correct behaviour;
// TestFilterByVRAM_GuardIsLoadBearing proves the guard is what produces it,
// so weakening the predicate back to `m.VRAMRequired <= maxVRAM` fails here
// rather than silently re-arming a multi-hundred-GB download.

// liveVRAMFreeBytes is the free VRAM measured on the deployment host at
// 2026-08-08T14:12:47+05:00 (RTX 5090, 1871 MiB free of 32607 MiB — the
// bulk held by the helixllm-coder Qwen3-Coder-30B process). The gateway
// logged "Models selected for inference" count=2 at that instant.
const liveVRAMFreeBytes = 1871 * 1024 * 1024

func TestFilterByVRAM_ExcludesCPUOnlyModels(t *testing.T) {
	c := DefaultCatalog()

	for _, maxVRAM := range []int64{0, liveVRAMFreeBytes, 32607 * 1024 * 1024} {
		for _, m := range c.FilterByVRAM(maxVRAM) {
			if m.VRAMRequired == 0 {
				t.Errorf("FilterByVRAM(%d) returned CPU-only model %q (VRAMRequired=0); "+
					"zero-VRAM models must never be selected for GPU inference",
					maxVRAM, m.ID)
			}
		}
	}
}

func TestFilterByVRAM_MatchesLiveGatewaySelection(t *testing.T) {
	c := DefaultCatalog()
	got := c.FilterByVRAM(liveVRAMFreeBytes)

	// Reproduces the live gateway's logged count=2 at the measured free VRAM.
	if len(got) != 2 {
		ids := make([]string, 0, len(got))
		for _, m := range got {
			ids = append(ids, m.ID)
		}
		t.Fatalf("FilterByVRAM(%d) = %d models %v; live gateway logged count=2",
			int64(liveVRAMFreeBytes), len(got), ids)
	}
}

func TestFilterByVRAM_GuardIsLoadBearing(t *testing.T) {
	c := DefaultCatalog()

	// The pre-08de5c5 predicate, applied to the post-08de5c5 catalog.
	var withoutGuard []ModelDefinition
	for _, m := range c.Models {
		if m.VRAMRequired <= liveVRAMFreeBytes {
			withoutGuard = append(withoutGuard, m)
		}
	}

	withGuard := c.FilterByVRAM(liveVRAMFreeBytes)
	if len(withoutGuard) <= len(withGuard) {
		t.Fatalf("counterfactual is not discriminating: withoutGuard=%d withGuard=%d; "+
			"expected the unguarded predicate to admit strictly more models",
			len(withoutGuard), len(withGuard))
	}

	var leaked []string
	for _, m := range withoutGuard {
		if m.VRAMRequired == 0 {
			leaked = append(leaked, m.ID)
		}
	}
	if len(leaked) == 0 {
		t.Fatal("expected the unguarded predicate to admit zero-VRAM CPU-only models; " +
			"if the catalog no longer has any, this guard test needs rewriting")
	}
	t.Logf("guard suppresses %d CPU-only model(s) that the old predicate admitted: %v",
		len(leaked), leaked)
}
