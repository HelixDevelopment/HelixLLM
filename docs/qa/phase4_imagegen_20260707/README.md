# Phase-4 GPU image-generation harness — FLUX + Nunchaku NVFP4 (SCAFFOLD)

**Status:** SCAFFOLD — ready-to-run harness, NOT yet a captured runtime proof.
**Revision:** 1
**Last modified:** 2026-07-07
**Host target:** RTX 5090 (Blackwell, sm_120 / cc 10.x), 32 GB VRAM, CUDA 12.8, rootless podman.

This directory is the ready-to-run harness for HelixLLM's GPU image-generation
capability (FLUX.1-dev served through the Nunchaku **NVFP4** SVDQuant transformer,
the Blackwell co-resident path). It is authored so the eventual runtime proof is a
single authorized operator step — it does **not** itself run a GPU workload, build
the heavy image, or pause the live coder.

> **What is proven NOW (no GPU):** the anti-bluff **image analyzer** self-validates
> for real — see [Analyzer self-validation](#analyzer-self-validation-real-now).
> **What is PENDING:** the end-to-end FLUX generation + first-run VRAM calibration.
> `PENDING: runtime proof requires operator-authorized coder-pause (§11.4.122) +
> first-run VRAM calibration (§11.4.6/§11.4.108).`

---

## Layout

```
docs/qa/phase4_imagegen_20260707/
├── README.md                     # this file
└── harness/
    ├── run_proof.sh              # the ONE operator driver (admit-check | boot | generate | down)
    └── imganalyzer/              # stdlib-only anti-bluff image oracle (§11.4.107(10)/§11.4.115)
        ├── main.go
        ├── go.mod
        └── .gitignore

services/imagegen/                # (sibling) the container the harness boots
├── Containerfile                # CUDA 12.8 + torch cu128 + Nunchaku NVFP4 + FastAPI shim
├── requirements.txt             # pinned
├── imagegen_server.py           # real FluxPipeline + NunchakuFluxTransformer2dModel
├── .gitignore                   # models/ + *.safetensors (gitignored)
└── .gitignore-meta/flux_nunchaku_nvfp4.yaml   # §11.4.77 regen manifest (HF download)

cmd/imagegen-boot/                # (sibling) on-demand boot + health + teardown (Go)
├── main.go                      # admit-via-vrambroker -> compose up -> /health -> down
└── compose.imagegen.yml         # containers-submodule compose spec (:18442, CDI GPU)
```

The boot harness lives in the **main module** (`cmd/imagegen-boot`) because it
imports the internal `vrambroker` for admission; the analyzer is a **separate
stdlib-only module** so it compiles + self-validates today with no heavy deps.

---

## How to RUN it (once a coder-pause window is authorized)

Everything is config-injected — no model/precision/host/port literal is hardcoded
(§CONST-045/046). Set `HF_TOKEN` + `NUNCHAKU_WHEEL` in the environment / `.env`
(§11.4.10, never committed) before the boot/generate phases.

```bash
cd docs/qa/phase4_imagegen_20260707/harness

# 1. SAFE AT ANY TIME — read-only nvidia-smi admission verdict, no boot, coder untouched:
./run_proof.sh admit-check
#    -> "ADMIT-OK ... fast path"                  co-resident fits now → generation is unblocked
#    -> "BLOCKED: ErrBudgetExceeded ..."          the NVFP4 footprint does not fit alongside the
#                                                 live coder RIGHT NOW; a coder-pause is required
#                                                 (operator-gated §11.4.122 — the harness NEVER
#                                                 pauses the coder itself).

# 2. AUTHORIZED WINDOW ONLY — boot the service on :18442 through the containers submodule:
./run_proof.sh boot
#    admit → compose up (rootless podman, CDI GPU) → poll /health → single-owner teardown.

# 3. AUTHORIZED WINDOW ONLY — one real generation + the analyzer's GREEN-guard verdict:
./run_proof.sh generate "a red fox in a snowy forest, cinematic"
#    POSTs /v1/images/generations → writes a PNG → RED_MODE=0 analyzer PASS iff it is a REAL
#    generated image (§11.4.115). This PNG + verdict is the captured runtime proof; commit it
#    under docs/qa/<run-id>/ per §11.4.83.

# 4. teardown (also runs automatically at the end of `boot`):
./run_proof.sh down
```

`admit-check` is genuinely safe to run this minute — it only reads `nvidia-smi`
through the broker and releases the lease immediately (§11.4.119 single-owner).
It never touches the coder on `:18434`.

---

## VRAM math (the admission the broker enforces BEFORE boot)

The RTX 5090 has **32 GB**. During normal operation the coder is resident and the
broker admits the image burst only if the NVFP4 footprint fits under the free-VRAM
ceiling minus a safety headroom — a **min-mem co-resident** configuration:

| Component | Footprint | Note |
|---|---|---|
| FLUX.1-dev **NVFP4** transformer (Nunchaku SVDQuant) | **~6.1 GiB** | cc10.x → **fp4** (NOT int4) on the 5090 |
| T5-XXL text encoder (quantized) + VAE | folds into peak | `IMAGEGEN_QUANTIZE_T5=1` |
| CPU-offload of idle stages | keeps GPU peak low | `enable_model_cpu_offload()` |
| **Estimated co-resident peak** | **~6 GB** | placeholder `IMAGEGEN_NEED_BYTES` default **7 GiB** (rounded margin) |

Broker admission (illustrative capture from the design session — **re-measured at
boot time**, never assumed):

```
total = 32 GiB
coder resident        ≈ 19432 MiB
free (nvidia-smi)     ≈ 12689 MiB
− headroom            = 2048 MiB   (broker HeadroomBytes)
──────────────────────────────────
co-resident ceiling   ≈ 10.4 GiB   ≥ ~7 GiB need  →  ADMIT-OK (coder stays live)
```

If `free` drops below `need + headroom`, the broker returns `ErrBudgetExceeded`
and the harness reports BLOCKED — the coder-pause path is required, and the
operator (never the harness) makes that call (§11.4.122 / §11.4.101).

> **PENDING calibration.** The `7 GiB` need is a research-derived **placeholder**.
> The FIRST authorized generation MUST capture the real peak on THIS card
> (`torch.cuda.max_memory_allocated()` / the `nvidia-smi` free-delta across the
> run) and replace `IMAGEGEN_NEED_BYTES` with the measured value
> (§11.4.6 no-guessing / §11.4.108 runtime-signature). Until then, treat the
> admission verdict as conservative, not exact.

---

## Analyzer self-validation (real, NOW)

The anti-bluff oracle in `harness/imganalyzer/` decides whether a generated PNG is
a **REAL** generated image or a **DEGENERATE** frame (solid / blank / pure-noise —
what a broken or un-implemented service would emit). It runs with **no GPU** on
generated pixel fixtures, so its own §11.4.107(10) self-validation is real evidence
captured today. It uses a **multi-signal AND** oracle (a single metric is never
proof, §11.4.107): Shannon entropy, unique-colour count, dominant-colour fraction,
adjacent-pixel-diff band, and PNG-compressibility.

Captured output (`2026-07-07`, this host, no GPU):

```
$ cd harness/imganalyzer && go build -o imganalyzer . && ./imganalyzer selfvalidate
[GOLDEN-GOOD real (expect REAL)] -> REAL  entropy=6.21 colors=43692 dominant=0.097 adjDiff=0.73 compress=0.241
    reason: all structure signals satisfied (real generated image)
[GOLDEN-BAD solid (expect DEGENERATE)] -> DEGENERATE  entropy=0.00 colors=1 dominant=1.000 adjDiff=0.00 compress=0.003
[GOLDEN-BAD blank (expect DEGENERATE)] -> DEGENERATE  entropy=0.00 colors=1 dominant=1.000 adjDiff=0.00 compress=0.003
[GOLDEN-BAD noise (expect DEGENERATE)] -> DEGENERATE  entropy=7.45 colors=65393 dominant=0.000 adjDiff=48.66 compress=1.002
    reason: compress ratio 1.002 > ceil 0.85 (near-incompressible = noise)
[SELF-VALIDATION] PASS: oracle classifies golden-good REAL and all golden-bad DEGENERATE
$ echo $?
0
```

Determinism: `selfvalidate` PASSes at 3 consecutive runs (§11.4.98). The
`RED_MODE` polarity switch (§11.4.115) is verified by exit code —
`RED_MODE=1 analyze <solid>` exits 0 (defect reproduced), `RED_MODE=1 analyze
<real>` exits 1; `RED_MODE=0 analyze <real>` exits 0 (green guard), `RED_MODE=0
analyze <solid>` exits 1.

> **Honest boundary (§11.4.6).** The golden-good fixture is a *structured
> reference frame*, not yet a real FLUX output — the oracle **contract** (real vs
> solid/blank/noise) is identical, so proving discrimination here proves it for
> the real image, but the thresholds are calibrated on this harness's own fixtures
> and MUST be re-calibrated against a captured FLUX PNG at runtime-proof time. The
> analyzer proves an image is *structurally a real generated image*; a
> prompt-adherence (CLIP-score) oracle is a documented follow-up that needs a model.

---

## Honest boundary of the whole harness (§11.4.6 / §11.4.108)

* **Proven now (no GPU):** the analyzer discriminates real vs degenerate frames
  and self-validates; the boot harness compiles and routes admission through the
  `vrambroker` + boot through the containers submodule; the container spec + shim
  are authored to make real inference calls (no simulated responses).
* **NOT proven (PENDING):** that FLUX+NVFP4 actually loads and generates on this
  card, the measured VRAM peak, and the end-to-end `generate → REAL-image` verdict.
  Those require an operator-authorized coder-pause window + the weights present +
  the heavy image build. Marked:
  `PENDING: runtime proof requires operator-authorized coder-pause (§11.4.122) +
  first-run VRAM calibration (§11.4.6/§11.4.108).`
* The shim returns **HTTP 503 with the exact reason** if the pipeline fails to
  load — it NEVER returns a fake image (that would be a §11.4 PASS-bluff).

---

## Sources verified

Verified 2026-07-07 (design-session research, to be re-verified per §11.4.99
before the runtime-proof commit — messenger/vendor/AI-stack docs carry a 90-day
staleness bound):

- Nunchaku (SVDQuant, NVFP4 on Blackwell) — https://github.com/nunchaku-tech/nunchaku
- Nunchaku FLUX.1-dev NVFP4 assets — https://huggingface.co/nunchaku-tech/nunchaku-flux.1-dev
- FLUX.1-dev (gated base model) — https://huggingface.co/black-forest-labs/FLUX.1-dev
- diffusers `FluxPipeline` — https://huggingface.co/docs/diffusers/en/api/pipelines/flux
- PyTorch cu128 (sm_120 / Blackwell) wheels — https://pytorch.org/get-started/locally/

> The exact NVFP4 transformer asset id + the Nunchaku Blackwell cu128 wheel URL
> move between releases — resolve LATEST and pin the resolved revision sha into
> `services/imagegen/.gitignore-meta/flux_nunchaku_nvfp4.yaml` at download time
> (§11.4.99 / §11.4.6). `NUNCHAKU_WHEEL` is supplied as a build-arg, never committed.
