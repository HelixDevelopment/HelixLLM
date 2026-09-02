# Text-to-speech service (T080)

Synthesises speech on the processor with Kokoro-82M through onnxruntime, behind
an OpenAI-audio-style `POST /v1/audio/speech`.

Every returned WAV comes from a real forward pass over real weights. There is no
simulated synthesis and no placeholder audio: a waveform is checked by
`assert_real_waveform` *before* it can be encoded, so silence, a DC constant and
a too-short stub — the shapes a fabricated response takes — cannot become a WAV
file at all, let alone reach a caller.

## Why Kokoro is the default for a commercial caller

Not because it is hardcoded. The default is whatever `plan_for_host` selects
from the catalogue for the declared usage purpose on the measured host; for a
commercial caller on a machine with no accelerator, that resolves to Kokoro-82M.

It matters that it does. The only text-to-speech entry the repository catalogue
currently loads is `xtts-v2`, under the Coqui Public Model License, which
forbids commercial use — and cannot be licensed for it, because Coqui Inc shut
down in January 2024. A service defaulting to it would be one a paying caller
may not lawfully use. Kokoro-82M is Apache-2.0: verified 2026-09-02 against the
Hugging Face model API, which reports `license: apache-2.0`, `gated: false`,
`private: false` for `hexgrad/Kokoro-82M` and the same for the ONNX mirror. No
token, no click-through, no non-commercial clause.

The trade is real and worth stating plainly: Kokoro does not clone voices. Every
cloning-capable model surveyed is either non-commercial or accelerator-only, so
a commercial processor-only caller gets fast, natural, non-cloning speech — and
this paragraph — rather than a cloning offer that cannot be run or lawfully
used. `xtts-v2` remains catalogued and IS offered to a research, personal or
evaluation caller who has an accelerator; the gate withholds by purpose, not by
blanket removal.

## Layout

| Path | What it is |
|---|---|
| `models.yaml` | This service's catalogue, in the repository wire schema |
| `tts_engine.py` | Selection, real synthesis, WAV encoding — stdlib + PyYAML except the forward pass |
| `tts_server.py` | The FastAPI shim |
| `test_tts_service.py` | Tests; run with `python3 -m unittest test_tts_service` |
| `../../container/helix_model_gate.py` | The shared schema, used identically by all three audio services |

## Running

Build from the **repository root**, not this directory — the shared schema lives
under `container/` and must be copied in:

```
podman build -f services/tts/Containerfile -t helixllm-tts .
```

Boot it through the containers submodule's orchestrator rather than by hand.
Mount the repository catalogue at `/app/catalogue` and the weights at
`/models/tts`; obtain the weights per `.gitignore-meta/kokoro_onnx.yaml`.

Set `HELIXLLM_USAGE_PURPOSE=research` to make the non-commercial cloning entries
selectable. It gates which licences may be offered; it names no model.

## What `/health` tells you

`engine_ready` reports whether a model could be *selected*, without forcing a
weight load, so an idle container stays cheap. When nothing could be selected it
carries which of the three reasons applies — the host lacks resources, no option
supports this configuration, or every option is excluded by its usage terms —
because those have different remedies. `withheld` lists the models that were not
offered and names the restricting term for each, so a caller can see that an
option exists and why it is not theirs, rather than believing none exists.

## Honest gaps

- **Piper is not catalogued.** It is the research's recommended pick for the
  smallest tier and it is absent, because no runtime memory figure was ever
  measured for it — the record reads only "minimal — runs on a Raspberry Pi 5".
  A qualitative claim is not a figure. It needs a measured resident-set size and
  a per-voice weight size; both are cheap to obtain.
- **A correction to the research record.** The repository catalogue states
  Piper's licence as GPL-3.0. Verified 2026-09-02 against the GitHub licence API
  for `rhasspy/piper`: the repository reports SPDX id **MIT**. The family
  statement offers Piper as the option for those who care more about a small
  footprint than about avoiding copyleft — on the verified licence there is no
  copyleft to avoid. Piper's per-*voice* models carry their own separate
  licences, which must be read per voice at acquisition.
- **No weight digest.** None was gathered, and fabricating one would defeat the
  check it exists to perform. Captured at first download.
- **Real-inference tests skip without weights.** They do not synthesise a
  waveform to go green; they say what is missing and skip.

## Sources verified

- `https://huggingface.co/api/models/hexgrad/Kokoro-82M` — 2026-09-02 — licence, gating
- `https://huggingface.co/api/models/onnx-community/Kokoro-82M-v1.0-ONNX` — 2026-09-02 — licence, ONNX asset list
- `https://pypi.org/pypi/kokoro-onnx/json` — 2026-09-02 — version 0.6.1, MIT, dependencies
- `https://api.github.com/repos/rhasspy/piper/license` — 2026-09-02 — SPDX MIT
- HTTP HEAD on the two Kokoro release assets — 2026-09-02 — the measured weight sizes
