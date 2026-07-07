# HelixLLM Phase-3 CPU OCR capability (Tesseract)

A real, containerized CPU OCR service — Tesseract 5 (OEM 1 / LSTM, the
current-default engine) behind a minimal Go HTTP shim. No mocks, no
simulated text, no hardcoded recognition results: every response comes from
a real `tesseract` invocation over real pixels (§11.4.6).

Booted via the `digital.vasic.containers` submodule compose orchestrator
(§11.4.76), rootless podman (§11.4.161), CPU-only. Host port **18438**
(coder `:18434`, embeddings `:18435`, translation `:18436`, whisper `:18437`
are sibling Phase-3 capabilities on distinct ports — never reused here).

## Contract

```
POST /v1/render   { "text": "...", "mode": "label|blank|noise", "pointsize": 48 }
                  -> image/png (rendered INSIDE this container via ImageMagick,
                     so the harness never depends on host fonts/versions)

POST /v1/ocr      body = raw image bytes
                  -> { engine, config, words:[{text,conf,left,top,width,height,
                       line,block}], full_text, mean_conf }
                     via `tesseract <img> - --oem 1 --psm 6 tsv`, parsing the
                     real per-word TSV confidence column (tessdoc
                     Command-Line-Usage; conf==-1 on non-word rows, 0-100 on
                     word-level rows — https://tesseract-ocr.github.io/tessdoc/Command-Line-Usage.html).

GET  /health      -> real `tesseract --version` + config (proves the engine
                     binary is present + callable in THIS image).
```

## Build the shim binary (§11.4.77 regeneration mechanism)

The Containerfile expects a pre-built static `ocr-shim` binary alongside it
(gitignored build artifact — a two-stage in-container Go build was tried
first and reproducibly failed on this host under concurrent-track process
pressure, see the Containerfile's honest-substitution comment):

```bash
cd submodules/helix_llm/services/ocr
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ocr-shim .
```

`docs/qa/phase3_tesseract_ocr_20260707/harness/run_proof.sh` runs this
automatically before every boot, so a fresh clone + `./run_proof.sh` needs no
manual step.

## Why render server-side

An earlier local proof (`docs/qa/p3_tesseract_ocr/`) discovered that
rendering the golden fixtures on the HOST and feeding them to a separately
built container FAILED intermittently because the host's font paths /
ImageMagick version differ from the container's — a host re-render can
silently produce a broken, unreadably-tiny-font image. This service closes
that gap structurally: `/v1/render` runs INSIDE the same image that serves
`/v1/ocr`, so the SAME font + ImageMagick + Tesseract versions render and
then recognize the fixture on every run, on every host.

## End-to-end proof

See `docs/qa/phase3_tesseract_ocr_20260707/` (repo root) for the Go harness
that boots this container through the containers-submodule orchestrator,
renders known-text fixtures, asserts the §11.4.108 runtime signature, and
runs the §11.4.107(10) self-validated golden-good/golden-bad analyzer.

## Provenance

Debian bookworm ships `tesseract-ocr` 5.3.0-2 — Tesseract 5.x line, OEM 1
(LSTM) is the compiled-in default (verified 2026-07-07,
https://packages.debian.org/bookworm/tesseract-ocr). The upstream-latest
5.5.x patch line is not in bookworm's main or backports archive at this
writing; this affects only the patch version, not the OEM/LSTM behaviour or
the TSV/`image_to_data`-equivalent contract this service relies on.
