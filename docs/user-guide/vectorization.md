# Vectorization Capability: vtracer (raster → SVG), port 18452

Companion to the source README (`submodules/helix_llm/services/vectorize/README.md`),
which is already comprehensive and accurate; this document is a
user-guide-style cross-reference against upstream sources, verified against
`submodules/helix_llm` HEAD `e2ce163`, 2026-07-11.

## What this is

A real, containerized CPU service that converts raster images (PNG / JPEG /
GIF / BMP) to SVG using **vtracer** (visioncortex VTracer, Rust) via a
minimal Go HTTP shim, plus a rasterize-back endpoint (via `rsvg-convert`)
used for fidelity self-checks. No mocked tracing, no templated/hardcoded
SVG output — every response is the verbatim output of a real `vtracer`
subprocess invocation (`services/vectorize/main.go: handleVectorize`).

- Host port: **18452** (CPU-only capability; distinct from every sibling
  Phase-3/4 port: coder `18434`, embeddings `18435` — see note below, the
  `18435` port is also used by the Lane-B agent instance documented in
  [`lane-b-agent.md`](lane-b-agent.md); the two are never booted for the
  same purpose — translation `18436`, whisper `18437`, OCR `18438`,
  vision `18439`, RAG/Qdrant `18440`, a2a `18441`, imagegen `18442`,
  videogen `18443`, mcp-gateway `18444`, helixmemory `18450`-`18451`).
- Internal container port: `8080` (`services/vectorize/main.go:
  addr := ":8080"`).
- Booted via the `digital.vasic.containers` submodule compose orchestrator
  (§11.4.76), rootless podman (§11.4.161), no GPU device requested.

## API contract (verified against `services/vectorize/main.go`)

### `POST /v1/vectorize`

Body: raw raster image bytes. Optional query param `?preset=bw|poster|photo`
(omit to use vtracer's own general-purpose defaults; any other value returns
HTTP 400 `"unknown preset"`).

```bash
curl -s -X POST "http://localhost:18452/v1/vectorize?preset=photo" \
  --data-binary @input.png \
  -H "Content-Type: image/png"
```

Response `200 application/json`:

```json
{
  "engine": "vtracer-0.6.5",
  "preset": "photo",
  "source_format": "png",
  "width": 512,
  "height": 512,
  "svg": "<svg xmlns=... width=\"512\" height=\"512\">...</svg>"
}
```

- `svg` is the **verbatim** file content vtracer wrote to `out.svg` — no
  post-processing, no templating.
- `width`/`height` are read from the source raster via Go's
  `image.DecodeConfig`, independent of vtracer.
- On decode failure (unsupported/corrupt raster): `400`. On empty body:
  `400`. On vtracer subprocess failure: `500` with `stdout`/`stderr`
  included in the error body (useful for debugging preset/flag issues).

### `POST /v1/rasterize`

Body: raw SVG bytes. Optional query params `?w=&h=` (explicit output pixel
size — pass the *original* raster's width/height for a pixel-exact
round-trip comparison). Rasterization happens **inside** the container via
`rsvg-convert`, so results never depend on the calling host's own
SVG-renderer version (mirrors the sibling OCR service's `/v1/render`
rationale).

```bash
curl -s -X POST "http://localhost:18452/v1/rasterize?w=512&h=512" \
  --data-binary @traced.svg \
  -o roundtrip.png
```

Response: `200 image/png` bytes, or `400`/`500` with `rsvg-convert` stderr
on failure.

### `GET /health`

```bash
curl -s http://localhost:18452/health
```

```json
{"status": "ok", "engine": "vtracer", "vtracer_version": "visioncortex VTracer 0.6.5", "rsvg_convert_version": "..."}
```

Proves both engine binaries (`vtracer`, `rsvg-convert`) are present and
callable inside the running container (a real §11.4.108 artifact-layer
signature — not a static "ok").

## Setup

### Build the two pre-built binaries (§11.4.77 regeneration mechanism)

The container image does **not** compile anything — `vectorize-shim` and
`vtracer` are both built on the host and `COPY`'d in (the same pattern the
sibling OCR service's `ocr-shim` uses, deliberately, to avoid host
`RLIMIT_NPROC` exhaustion from an in-container Rust toolchain build under
concurrent-track process pressure — see `services/vectorize/Containerfile`
comment header and §11.4.174).

```bash
cd submodules/helix_llm/services/vectorize

# Go HTTP shim (static, CGO-free)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o vectorize-shim .

# vtracer CLI (host cargo build)
cargo install vtracer --locked --root ./.cargo-install-vtracer
cp ./.cargo-install-vtracer/bin/vtracer ./vtracer
```

Both `vectorize-shim` and `vtracer` are gitignored build artifacts
(`services/vectorize/.gitignore`) — a fresh clone must run the two commands
above (or the harness's `run_proof.sh`, if present, which the README states
runs both automatically) before building the container image.

### Build + boot the container

```bash
cd submodules/helix_llm/services/vectorize
podman build -t localhost/helixllm/vectorize:latest -f Containerfile .
# Boot THROUGH the containers submodule orchestrator (§11.4.76) — never a
# raw `podman run` as a standing workflow. See the sibling boot harnesses
# (cmd/agentgen-boot, cmd/visiongen-boot) for the Go orchestrator pattern;
# no vectorize-boot Go harness exists in this codebase as of this pass — if
# one does not yet exist when you read this, wire boot/health-poll/down
# subcommands following that same pattern (compose.Orchestrator.Up/Down +
# pkg/health.CheckHTTP against GET /health on :18452) rather than a manual
# `podman-compose up`.
```

## `--preset` values (cross-checked against upstream vtracer docs)

`bw`, `poster`, `photo` — confirmed against the upstream visioncortex/vtracer
CLI documentation (see Sources footer). The service's `handleVectorize`
validates the query param against exactly this closed set (plus empty =
vtracer defaults) before invoking the binary, so an unsupported preset
value fails fast with a `400` rather than being silently ignored.

## StarVector-8B (optional GPU tier) — not implemented, honestly deferred

The service README documents this clearly and it is worth restating in
user-facing docs so nobody files a bug expecting it: StarVector-8B (a
~12-16 GB VRAM VLM alternative, scoped to icon/logo/diagram/chart content
only per its own Hugging Face model card) is **not wired into this
service**. At the README's proof time (2026-07-11) the resident coder
consumed all free VRAM, leaving no safe headroom to load it without risking
destabilizing the coder. Track re-attempting this as a separate follow-up
item once a clean ≥16 GB VRAM margin is confirmed.

## Known gaps as of this pass

- No dedicated `vectorize-boot` Go harness (mirroring `cmd/agentgen-boot`,
  `cmd/visiongen-boot`) exists under `cmd/` for this service — boot today
  appears to rely on the fidelity-proof harness referenced by the README at
  `docs/qa/vectorization_liveproof_<run-id>/`, which does **not exist in
  this checkout** as of 2026-07-11 (searched `docs/qa/` — no
  `vectorization_liveproof_*` directory present, despite the README citing
  it as already-run "at proof time (2026-07-11)"). This may be pending work
  in another stream not yet committed/synced to this checkout — flagged
  here rather than fabricated.

## Sources verified 2026-07-11:
- https://github.com/visioncortex/vtracer (CLI flags, `--preset` values bw/poster/photo, confirms crates.io as canonical version source)
- (in-repo, cross-referenced, not re-fetched externally in this pass) `services/vectorize/README.md`'s own citations: https://crates.io/crates/vtracer, https://huggingface.co/starvector/starvector-8b-im2svg, https://packages.debian.org/bookworm/librsvg2-bin, https://packages.debian.org/bookworm/libc6 — all cited there as accessed/verified 2026-07-08/2026-07-11
