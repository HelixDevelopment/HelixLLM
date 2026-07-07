# Phase-4 GPU video-generation harness — WAN 2.2 / LTX-Video (SCAFFOLD)

**Status:** SCAFFOLD — ready-to-run harness, NOT yet a captured runtime proof.
**Revision:** 1
**Last modified:** 2026-07-07
**Host target:** RTX 5090 (Blackwell, sm_120 / cc 10.x), 32 GB VRAM, CUDA 12.8, rootless podman.

This directory is the ready-to-run harness for HelixLLM's GPU video-generation
capability (WAN 2.2 / LTX-Video served through diffusers, the Blackwell
co-resident path). It is the sibling of the accepted image-gen scaffold. It is
authored so the eventual runtime proof is a single authorized operator step — it
does **not** itself run a GPU workload, build the heavy image, or pause the live
coder.

> **What is proven NOW (no GPU):** the anti-bluff **video analyzer** self-validates
> for real — see [Analyzer self-validation](#analyzer-self-validation-real-now).
> **What is PENDING:** the end-to-end WAN/LTX generation + first-run VRAM calibration.
> `PENDING: runtime proof requires operator-authorized coder-pause (§11.4.122) +
> first-run VRAM calibration (§11.4.6/§11.4.108).`

---

## Layout

```
docs/qa/phase4_videogen_20260707/
├── README.md                     # this file
└── harness/
    ├── run_proof.sh              # the ONE operator driver (admit-check | boot | generate | selfcheck | down)
    └── vidanalyzer/              # ffmpeg-backed anti-bluff video liveness oracle (§11.4.107(10)/§11.4.115)
        ├── main.go
        ├── go.mod
        └── .gitignore

services/videogen/                # (sibling) the container the harness boots
├── Containerfile                # CUDA 12.8 + torch cu128 + diffusers WAN/LTX + FastAPI shim
├── requirements.txt             # pinned
├── videogen_server.py           # real WanPipeline / LTXPipeline + export_to_video (MP4)
├── .gitignore                   # models/ + *.safetensors + *.gguf + *.mp4 (gitignored)
└── .gitignore-meta/wan_ltx_gguf.yaml   # §11.4.77 regen manifest (HF download)

cmd/videogen-boot/                # (sibling) on-demand boot + health + teardown (Go)
├── main.go                      # admit-via-vrambroker (ClassVideo) -> compose up -> /health -> down
└── compose.videogen.yml         # containers-submodule compose spec (:18443, CDI GPU)
```

The boot harness lives in the **main module** (`cmd/videogen-boot`) because it
imports the internal `vrambroker` for admission (`ClassVideo`); the analyzer is a
**separate stdlib+ffmpeg module** so it compiles + self-validates today with no
heavy deps.

---

## How to RUN it (once a coder-pause window is authorized)

Everything is config-injected — no model/precision/host/port literal is hardcoded
(§CONST-045/046). Set `HF_TOKEN` in the environment / `.env` (§11.4.10, never
committed) only if a gated WAN/LTX variant is selected before the boot/generate
phases; the default repos are open.

```bash
cd docs/qa/phase4_videogen_20260707/harness

# 0. SAFE AT ANY TIME — analyzer self-validation, no GPU:
./run_proof.sh selfcheck

# 1. SAFE AT ANY TIME — read-only nvidia-smi admission verdict, no boot, coder untouched:
./run_proof.sh admit-check
#    -> "ADMIT-OK ... fast path"                  co-resident fits now → generation is unblocked
#    -> "BLOCKED: ErrBudgetExceeded ..."          the selected no-pause config does not fit alongside
#                                                 the live coder RIGHT NOW; a coder-pause is required
#                                                 (operator-gated §11.4.122 — the harness NEVER
#                                                 pauses the coder itself).

# 2. AUTHORIZED WINDOW ONLY — boot the service on :18443 through the containers submodule:
./run_proof.sh boot
#    admit (ClassVideo) → compose up (rootless podman, CDI GPU) → poll /health → single-owner teardown.

# 3. AUTHORIZED WINDOW ONLY — one real generation + the analyzer's GREEN-guard verdict:
./run_proof.sh generate "a red fox running through a snowy forest, cinematic"
#    POSTs /v1/videos/generations → writes an MP4 → RED_MODE=0 analyzer PASS iff it is a REAL
#    LIVE generated video (§11.4.115). This MP4 + verdict is the captured runtime proof; commit it
#    under docs/qa/<run-id>/ per §11.4.83.

# 4. teardown (also runs automatically at the end of `boot`):
./run_proof.sh down
```

`admit-check` is genuinely safe to run this minute — it only reads `nvidia-smi`
through the broker (`ClassVideo`) and releases the lease immediately (§11.4.119
single-owner). It never touches the coder on `:18434` or the image-gen sibling on
`:18442`.

---

## VRAM math (the admission the broker enforces BEFORE boot)

The RTX 5090 has **32 GB**. During normal operation the coder is resident and the
broker admits the video burst only if the selected footprint fits under the
free-VRAM ceiling minus a safety headroom. Image + video are BOTH `IsBurst`
classes, so the broker guarantees only ONE generative job holds VRAM at a time.

**No-coder-pause (co-resident, peak ≤ ~10.4 GiB) paths** — §11.4.150 research:

| Config | Cited peak | Note |
|---|---|---|
| **WAN 2.2 TI2V-5B — FP8 @480p** (native offload) | **~8–10 GB** | **DEFAULT**; near the ceiling → calibrate margin |
| WAN 2.2 14B — GGUF-Q4 + T5-CPU @480p | ~6–8 GB | T5-XXL MUST be CPU-offloaded |
| LTX-Video 13B — GGUF-Q4 (short clip) | ~6–10 GB | needs **32 GB+ system RAM** |

**MUST-pause paths (peak > ~10.4 GiB — NOT the default, operator-gated §11.4.122):**
WAN-14B FP8+T5-CPU (~14–16 GB @720p), WAN-14B FP16 (~28 GB), LTX-13B FP8
(15.7 GB) / full (28.6 GB).

Broker admission (illustrative capture from the design session — **re-measured at
boot time**, never assumed):

```
total = 32 GiB
coder resident        ≈ 19432 MiB
free (nvidia-smi)     ≈ 12689 MiB
− headroom            = 2048 MiB   (broker HeadroomBytes)
──────────────────────────────────
co-resident ceiling   ≈ 10.4 GiB   ≥ ~10 GiB need  →  ADMIT-OK (coder stays live)
```

The default `VIDEOGEN_NEED_BYTES` is **10 GiB** — the rounded WAN-5B-FP8 @480p
peak margin. Because that sits *close* to the co-resident ceiling, the calibration
below is load-bearing: a longer clip / 720p pushes over the ceiling.

If `free` drops below `need + headroom`, the broker returns `ErrBudgetExceeded`
and the harness reports BLOCKED — the coder-pause path is required, and the
operator (never the harness) makes that call (§11.4.122 / §11.4.101).

> **PENDING calibration.** The `10 GiB` need is a research-derived **placeholder**.
> The FIRST authorized generation MUST capture the real peak on THIS card
> (`torch.cuda.max_memory_allocated()` / the `nvidia-smi` free-delta across the
> run) and replace `VIDEOGEN_NEED_BYTES` with the measured value
> (§11.4.6 no-guessing / §11.4.108 runtime-signature). Until then, treat the
> admission verdict as conservative, not exact.

---

## Analyzer self-validation (real, NOW)

The anti-bluff oracle in `harness/vidanalyzer/` decides whether a generated MP4 is
a **LIVE** generated video or a **DEGENERATE** clip (a frozen single-repeated-frame /
solid / static-loop clip — what a broken or un-implemented service, or the
§11.4.107 stale/frozen-frame trap, would emit). It decodes MP4 fixtures with
`ffmpeg`/`ffprobe` (**no GPU**), so its own §11.4.107(10) self-validation is real
evidence captured today. It uses a **multi-signal AND** oracle (a single metric is
never proof, §11.4.107):

- **frame count** (ffprobe) ≥ floor,
- **PTS strictly increasing** — an *independent* frame-advance counter (a flat
  counter ⇒ stuck muxer ⇒ FAIL even if pixels seem to move),
- **mean adjacent-frame perceptual distance** > floor — the *freeze-detection*
  oracle (NOT byte-identical; frozen/looped ~ 0 ⇒ FAIL),
- **distinct-adjacent-frame fraction** ≥ floor — a static-loop clip is ~0 ⇒ FAIL,
- **mean per-frame luma entropy** > floor — a solid/blank clip is ~0 ⇒ FAIL.

The golden-good is a **moving `testsrc2` pattern**; the golden-bads are a **single
repeated frame** (frozen) and a **solid colour**.

Captured output (`2026-07-07`, this host, no GPU):

```
$ cd harness/vidanalyzer && go build -o vidanalyzer . && ./vidanalyzer selfvalidate
[GOLDEN-GOOD good (expect LIVE)] -> LIVE  frames=20 ptsMono=true adjDist=4.19 distinctFrac=1.000 entropy=4.92
    reason: all liveness signals satisfied (real live generated video)
[GOLDEN-BAD frozen (expect DEGENERATE)] -> DEGENERATE  frames=20 ptsMono=true adjDist=0.00 distinctFrac=0.000 entropy=4.72
    reason: mean adjacent-frame diff 0.000 < floor 1.5 (frozen / single-repeated-frame / static loop)
    reason: distinct adjacent-frame fraction 0.000 < floor 0.50 (too many identical frames = static loop)
[GOLDEN-BAD solid (expect DEGENERATE)] -> DEGENERATE  frames=20 ptsMono=true adjDist=0.00 distinctFrac=0.000 entropy=0.00
    reason: mean adjacent-frame diff 0.000 < floor 1.5 (frozen / single-repeated-frame / static loop)
    reason: distinct adjacent-frame fraction 0.000 < floor 0.50 (too many identical frames = static loop)
    reason: mean frame entropy 0.000 < floor 3.0 (solid/blank frames)
[SELF-VALIDATION] PASS: oracle classifies golden-good LIVE and all golden-bad DEGENERATE
$ echo $?
0
```

Note the load-bearing separation: the **frozen** clip has healthy per-frame
entropy (4.72 — it *is* a structured picture) yet is correctly rejected because
its adjacent-frame distance is 0.00 — this is exactly the §11.4.107 "a picture
showed, but it was a frozen/stale frame" trap, and the freeze oracle catches it
while a naive single-frame check would have PASSed it.

Determinism: `selfvalidate` PASSed at **3 consecutive runs** (exit 0 each,
§11.4.98). The `RED_MODE` polarity switch (§11.4.115) is verified by exit code —
`RED_MODE=1 analyze <frozen>` exits 0 (defect reproduced), `RED_MODE=1 analyze
<good>` exits 1; `RED_MODE=0 analyze <good>` exits 0 (green guard), `RED_MODE=0
analyze <frozen>` exits 1 (all four confirmed this host, 2026-07-07).

> **Honest boundary (§11.4.6).** The golden-good fixture is a *moving reference
> pattern*, not yet a real WAN/LTX output — the oracle **contract** (live vs
> frozen/solid/static-loop) is identical, so proving discrimination here proves it
> for the real clip, but the thresholds are calibrated on this harness's own
> fixtures and MUST be re-calibrated against a captured WAN/LTX MP4 at
> runtime-proof time. The analyzer proves a clip is *structurally a real live
> generated video*; a prompt-adherence (CLIP-per-frame) + not-stale-from-previous
> cross-check are documented follow-ups (§11.4.107(4)) that need a model / a prior
> gen respectively.

---

## Honest boundary of the whole harness (§11.4.6 / §11.4.108)

* **Proven now (no GPU):** the analyzer discriminates live vs frozen/solid clips
  and self-validates; the boot harness compiles and routes admission through the
  `vrambroker` (`ClassVideo`) + boot through the containers submodule; the
  container spec + shim are authored to make real inference calls (no simulated
  responses).
* **NOT proven (PENDING):** that WAN/LTX actually loads and generates on this card,
  the measured VRAM peak, and the end-to-end `generate → LIVE-video` verdict.
  Those require an operator-authorized coder-pause window + the weights present +
  the heavy image build. Marked:
  `PENDING: runtime proof requires operator-authorized coder-pause (§11.4.122) +
  first-run VRAM calibration (§11.4.6/§11.4.108).`
* The shim returns **HTTP 503 with the exact reason** if the pipeline fails to
  load — it NEVER returns a fake clip (that would be a §11.4 PASS-bluff).

---

## Sources verified

Verified 2026-07-07 (design-session research, to be re-verified per §11.4.99
before the runtime-proof commit — vendor/AI-stack docs carry a 90-day staleness
bound):

- WAN 2.2 TI2V-5B (default no-pause 480p FP8) — https://huggingface.co/Wan-AI/Wan2.2-TI2V-5B
- WAN 2.2 T2V-A14B (14B GGUF-Q4 + T5-CPU) — https://huggingface.co/Wan-AI/Wan2.2-T2V-A14B
- WAN 2.2 VRAM guide — https://willitrunai.com/blog/wan-2-2-vram-requirements
- LTX-Video 13B (GGUF-Q4 co-resident path) — https://huggingface.co/Lightricks/LTX-Video
- LTX-Video system requirements — https://docs.ltx.video/open-source-model/getting-started/system-requirements
- diffusers WAN/LTX pipelines — https://huggingface.co/docs/diffusers
- PyTorch cu128 (sm_120 / Blackwell) wheels — https://pytorch.org/get-started/locally/

> The exact WAN FP8 / GGUF-Q4 asset ids + the LTX GGUF variant filenames move
> between releases — resolve LATEST and pin the resolved revision sha into
> `services/videogen/.gitignore-meta/wan_ltx_gguf.yaml` at download time
> (§11.4.99 / §11.4.6).
