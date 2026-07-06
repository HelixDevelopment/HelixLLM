# VRAM Broker CORE — Runtime-Signature Evidence (§11.4.108)

**Run-id:** phase3_vrambroker_20260706 · **Date:** 2026-07-06 · Track `(T1/main)`
**Package:** `internal/vrambroker` · **Design:** `docs/VRAM_BROKER.md` (DZ-04 / P1-T4)

The broker CORE: `Budget()` via real `nvidia-smi` (design Q3 — shelled, rootless
in-container) + single-owner burst (§11.4.119) + fail-closed over-budget refusal
(never OOMs the single 32 GB card). The live `helixllm-coder` at `:18434`
(~19 GB resident) was used as the RESIDENT anchor and was never stopped, and no
large model was loaded.

## Artefacts

| File | What it proves |
|------|----------------|
| `nvidia_smi_snapshot.txt` | Live card: `32607, 19432, 12689` MiB (total, used, free) — the ground truth `Budget()` must match. |
| `coder_models.txt` | The live coder advertises `Qwen3-Coder-30B-A3B-Instruct-Q4_K_M` — the RESIDENT anchor. |
| `unit_mutation_test_run.log` | Unit + §1.1 paired-mutation tests (faked nvidia-smi, CONST-050(A) unit-only) — all PASS. |
| `integration_test_run.log` | REAL nvidia-smi + REAL live coder — all PASS. |

## Runtime signature (captured, §11.4.108)

1. **`Budget()` real numbers** — `Budget() live: total=32607 MiB used=19432 MiB
   free=12689 MiB`, exactly matching the `nvidia-smi` snapshot; `used>0` (coder
   resident), `total≈32 GB`, `free≈total-used`.
2. **Coder stays live** — a REAL `/v1/chat/completions` returned `"4"` BEFORE and
   AFTER an over-budget burst was refused; the broker never touched the coder.
3. **Over-budget REFUSED, no OOM** — `ErrBudgetExceeded: need=56255053824
   free=13305380864 headroom=2147483648`; 25 consecutive over-budget refusals,
   coder still answering afterwards.
4. **Single-owner burst (§11.4.119)** — a second concurrent burst lease is
   refused with `ErrBurstInUse`; a race of 32 goroutines yields exactly one
   winner.
5. **§1.1 paired mutation** — `TestAdmit_PairedMutation`: the real budget guard
   refuses the over-commit (`admit == false`) while the mutated always-grant
   guard would (wrongly) admit it (`== true`); the flip proves the guard is
   load-bearing, at the LOGIC path — no real OOM was ever risked.

## Reproduce

```bash
# unit + §1.1 mutation (no hardware needed)
go test -count=1 -v ./internal/vrambroker/...
# integration (real nvidia-smi + live coder at :18434; SKIPs honestly if absent)
go test -tags=integration -count=1 -v -run Integration ./internal/vrambroker/...
```
