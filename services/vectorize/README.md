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

## Measured model selection (vector + embedding) — `model_choice.py`

`vtracer` is **not** a model, so it is not under measured selection: it has no
weights, no licence gating a usage purpose, and no per-host memory figure to
admit against. It is served unconditionally by the Go shim above. That is a
deliberate boundary, not an omission — putting an algorithm in a *model*
catalogue would mean inventing every field the catalogue exists to check.

What IS under measured selection is the model side of these two capability
families, decided by `model_choice.py`:

```bash
python3 model_choice.py --family vector      # StarVector-8B tier
python3 model_choice.py --family embedding   # local text embeddings
```

It uses `container/helix_model_gate.py` — the shared catalogue/usage-terms
vocabulary the audio family built — rather than adding another schema. (The Go
lanes already carry a DUPLICATION NOTICE recording that `videogen-boot` is the
third near-copy of one decision; a Go copy here would have been a fourth, and
could not have been shared anyway since this service is its own Go module.)

**Configuration says WHERE, never WHICH** (FR-056). `HELIXLLM_CATALOGUE_PATHS`
points at catalogue files, `HELIXLLM_USAGE_PURPOSE` declares how output will be
used (defaults to the *narrowest* purpose, `commercial`, and always reports that
it defaulted), and `HELIXLLM_{VECTOR,EMBEDDING}_FORBID_MODELS` removes
candidates. None of them can introduce a model. There is no fixed default: an
unmeasurable host is told why and the process exits non-zero.

The three withheld reasons stay distinct all the way to the shell, each with its
own exit code, because each implies a different remedy:

| Exit | Meaning | Remedy |
|-----:|---------|--------|
| 0  | a model was decided | — |
| 20 | host could not be measured | investigate the measurement |
| 21 | insufficient resources | more memory / disk |
| 22 | unsupported configuration | an accelerator; more memory will not help |
| 23 | excluded by usage terms | obtain a licence, or widen the declared purpose |
| 24 | catalogue unreadable | fix the catalogue path |

Real behaviour on the development host (RTX 3060, 11752 MiB free):

```
$ python3 model_choice.py --family vector
DECLARED-USAGE: commercial (default - the narrowest purpose; set HELIXLLM_USAGE_PURPOSE to declare another)
MEASURED memory_available=3238MiB storage_available=1190610MiB accelerator=yes accelerator_free=11752MiB
CANNOT-CHOOSE (vector): this host lacks the resources every vector model needs
  WITHHELD starvector-8b-im2svg:bf16: insufficient_resources - starvector-8b-im2svg:bf16 needs 15014294040 bytes of memory; 12322865152 bytes are free
  No model is started: there is deliberately no default, because a model that was
  not chosen from a measurement may not fit this host (FR-056).
$ echo $?
21
```

That refusal is the correct answer for this host, and it is the same conclusion
the "StarVector tier status" section below reached by hand in July — now reached
mechanically, from a measurement, with the shortfall named in bytes.

Tests: `python3 -m unittest test_model_choice -v`. The anti-simulation guard
(`assert_real_embedding`) is self-validated against golden-good and golden-bad
fixtures so it is proven to have teeth even on a host with no weights; the
real-inference test runs against a real `llama-server` when
`HELIXLLM_EMBED_GGUF` points at a real `.gguf`, and otherwise SKIPS with a
stated reason rather than asserting on a fabricated vector.

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
