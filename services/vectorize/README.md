# HelixLLM vectorization capability (vtracer default + StarVector-8B optional tier)

A real, containerized CPU raster→SVG vectorization service — vtracer
(visioncortex VTracer, Rust) behind a minimal Go HTTP shim. No mocks, no
simulated tracing, no hardcoded/templated SVG: every response comes from a
real `vtracer` invocation over real pixels (§11.4.6), and every re-rasterized
comparison PNG comes from a real `rsvg-convert` invocation over the real
produced SVG.

Booted via the `digital.vasic.containers` submodule compose orchestrator
(§11.4.76), rootless podman (§11.4.161), **CPU-only** — vtracer is a
pure-CPU raster tracer, so this capability never contends for GPU with the
resident `helixllm-coder` (`:18434`), RAG/Qdrant (also CPU-only), or any
future StarVector-8B GPU tier. Host port **18452** (distinct from every
sibling Phase-3/4 capability port: coder `18434`, embeddings `18435`,
translation `18436`, whisper `18437`, OCR `18438`, vision `18439`, RAG/Qdrant
`18440`, a2a `18441`, imagegen `18442`, videogen `18443`, mcp-gateway
`18444`, helixmemory `18450`-`18451`).

## Why vtracer is the default (per the capabilities plan)

`docs/research/07.2026/02_vision_generative/CAPABILITIES_MASTER_PLAN_v2.md`
(P3-T4′) designates vtracer (CPU, 0 GPU) as the **default** raster→SVG engine
and — per the same plan — **the real path for natural-image / illustration /
pixelized-graphic vectorization**. StarVector-8B (a VLM, ~12-16 GB VRAM) is
scoped to a narrower **optional** smart-mode tier. Per StarVector's own
Hugging Face model card
(https://huggingface.co/starvector/starvector-8b-im2svg, accessed
2026-07-08, re-verified 2026-07-11):

> "StarVector models will not work for natural images or illustrations, as
> they have not been trained on those images."

StarVector is scoped to icons, logotypes, technical diagrams, graphs, and
charts only. This service therefore implements the vtracer default path
first (content-agnostic — works on photos, illustrations, logos, icons
alike) with a self-validated fidelity analyzer; StarVector is a documented,
honestly-deferred follow-up (see "StarVector tier status" below), not forced
in when it would not cleanly fit free VRAM or would not be genuinely useful
for the target content class.

## Contract

```
POST /v1/vectorize   body = raw raster image bytes (PNG/JPEG/GIF/BMP/...)
                     query ?preset=bw|poster|photo (optional — omit to use
                     vtracer's own general-purpose defaults)
                     -> { engine, preset, source_format, width, height, svg }
                     via `vtracer -i <in> -o <out.svg> [--preset X]`. `svg`
                     is the verbatim vtracer output (an SVG 1.1 document
                     whose width/height match the source raster's pixel
                     dimensions).

POST /v1/rasterize   body = raw SVG bytes
                     query ?w=&h= (optional explicit pixel size — pass the
                     ORIGINAL raster's own width/height for a pixel-exact
                     round-trip comparison)
                     -> image/png bytes, rasterized INSIDE this container via
                     `rsvg-convert` (so the fidelity harness never depends on
                     the calling host's own SVG-renderer version/behaviour —
                     the same server-side-rendering principle the sibling OCR
                     service's /v1/render documents).

GET  /health         -> real `vtracer --version` + `rsvg-convert --version`
                     (proves both engine binaries are present + callable in
                     THIS image).
```

## Build the binaries (§11.4.77 regeneration mechanism)

The Containerfile expects two pre-built binaries alongside it (both
gitignored build artifacts — the same host-compile-then-COPY pattern the
sibling OCR service's `ocr-shim` uses, for the same §11.4.174-documented
reason: an in-container toolchain build risks host RLIMIT_NPROC exhaustion
under concurrent-track process pressure on this shared host):

```bash
cd submodules/helix_llm/services/vectorize

# Go HTTP shim (static, CGO-free)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o vectorize-shim .

# vtracer CLI (host cargo build; dynamically linked against glibc symbols
# up to GLIBC_2.34 — compatible with Debian bookworm's glibc 2.36, verified
# 2026-07-11 via `objdump -T`/`ldd --version`, see Containerfile comment)
cargo install vtracer --locked --root ./.cargo-install-vtracer
cp ./.cargo-install-vtracer/bin/vtracer ./vtracer
```

`docs/qa/vectorization_liveproof_<run-id>/harness/run_proof.sh` runs both of
these automatically before every boot, so a fresh clone + `./run_proof.sh`
needs no manual step.

## Why rasterize server-side

Mirrors the sibling OCR service's documented rationale for server-side
`/v1/render`: an SVG rasterizer's output can vary with library version, font
substitution, and anti-aliasing defaults. Doing the re-rasterize step INSIDE
the same container image that produced the SVG (rather than on whatever host
happens to run the fidelity harness) means the SAME `rsvg-convert`
version/build renders every fixture on every run, on every host — the
harness's SSIM comparison is never confounded by renderer drift between the
"good" SVG (from `/v1/vectorize`) and its own re-rasterization (from
`/v1/rasterize`).

## Fidelity self-validation (§11.4.107(10) / §11.4.107(13))

See `docs/qa/vectorization_liveproof_<run-id>/` (repo root) for the Go
harness that boots this container through the containers-submodule
orchestrator, vectorizes a real repo image asset, re-rasterizes the produced
SVG through this service's own `/v1/rasterize`, and computes a windowed SSIM
(structural similarity) between the source raster and the re-rasterized
result — a golden-good real conversion MUST clear a project-calibrated SSIM
floor; golden-bad degenerate SVG fixtures (a blank canvas, a flat-color
rect — no traced structure at all) MUST FAIL it. The harness also proves
determinism (identical input -> byte-identical SVG across two calls) and
carries a paired mutation (§1.1): the fidelity check is temporarily neutered
(forced to always pass) to prove the golden-bad fixtures WOULD wrongly pass
without a load-bearing check, then reverted and re-proven correct.

## StarVector tier status

**Deferred as a documented follow-up, not forced in.** At proof time
(2026-07-11) `nvidia-smi` showed 12632 MiB free VRAM on the single resident
GPU, entirely consumed by the resident `helixllm-coder` (`llama-server`,
19440 MiB) — i.e. free VRAM sits at the very bottom of StarVector-8B's own
12-16 GB estimated footprint (per the capabilities plan), leaving no safe
headroom for model load, KV cache, or transient allocation spikes. Loading
StarVector under those conditions would risk OOM-killing or destabilizing
the coder's resident GPU context, which this task's hard constraints require
to stay untouched. Per the plan's own caveat, StarVector would also add no
value for the default vtracer use case this service targets (natural
image/illustration/pixelized-graphic vectorization) — it is scoped to
icon/logo/diagram/chart content only. **Recommendation:** re-attempt the
StarVector-8B optional tier only after a free-VRAM re-check shows a clean
≥16 GB margin with the coder (and any other resident GPU consumer) still
running, or after the coder is intentionally stopped for a dedicated
StarVector session — track as a separate follow-up item, not bundled into
this default-path delivery.

## Provenance

- vtracer 0.6.5 — crates.io (`https://crates.io/crates/vtracer`), installed
  via `cargo install vtracer --locked`, verified 2026-07-11.
- Debian bookworm ships `librsvg2-bin`
  (https://packages.debian.org/bookworm/librsvg2-bin), verified 2026-07-11.
- StarVector-8B model card:
  https://huggingface.co/starvector/starvector-8b-im2svg, accessed
  2026-07-08, re-verified 2026-07-11 (natural-image/illustration
  incompatibility caveat).
