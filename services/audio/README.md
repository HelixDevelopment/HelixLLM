# Audio-classification service (T081)

Tags sound events with YAMNet through LiteRT on the processor, behind
`POST /v1/audio/classifications`.

This service answers *what sound is this* — a doorbell, breaking glass, an
engine, speech versus music. It does not transcribe words (that is
speech-to-text) and it does not generate audio.

Every returned score vector comes from a real forward pass and is checked
against the class map loaded from the model's own published artefact before it
can be returned. A hand-written stub will not happen to be 521 long, and a real
classifier over real audio does not return a uniform or all-zero vector — so
those are refused rather than served.

## Classification is not generation, and the split is deliberate

Audio classification and audio generation are separate capability families here,
and this service serves only the first. That is structural, not a scope cut.

Classification is mature, cheap and runs on **every** host class, down to a
machine with no accelerator and 8 GB of RAM — the model is about 4 MB. Generation
has no processor-viable option at any RAM size: the cheapest candidate surveyed
still needs roughly 4 GB of accelerator memory, and adding system memory does not
substitute for an accelerator.

Merging them would force a choice between offering a processor-only host
something it cannot run, and hiding generation behind a family that looks
available. Keeping them apart lets such a host be told the reason instead —
`/health` carries that statement, and `audio_engine.resolve` refuses a
generation entry naming the accelerator requirement rather than reporting a bare
family mismatch.

## Why YAMNet

It is the only audio-classification model in this repository's research whose
licence was confirmed: Apache-2.0, via the TensorFlow Model Garden
(`tensorflow/models`, `research/audioset/yamnet`), verified 2026-09-02. PANNs and
CLAP are both recorded as "licence file not read" — a belief about a licence is
not a licence, so neither may be offered commercially and neither is catalogued.

## Layout

| Path | What it is |
|---|---|
| `models.yaml` | This service's catalogue, in the repository wire schema |
| `audio_engine.py` | Selection, real inference, class map — stdlib + PyYAML except the forward pass |
| `audio_server.py` | The FastAPI shim |
| `test_audio_service.py` | Tests; run with `python3 -m unittest test_audio_service` |
| `../../container/helix_model_gate.py` | The shared schema, used identically by all three audio services |

## Running

Build from the **repository root**, not this directory — the shared schema lives
under `container/` and must be copied in:

```
podman build -f services/audio/Containerfile -t helixllm-audio .
```

Boot it through the containers submodule's orchestrator rather than by hand.
Mount the repository catalogue at `/app/catalogue` and the artefacts at
`/models/audio`; obtain them per `.gitignore-meta/yamnet_litert.yaml`.

Audio must arrive as mono 16 kHz float samples. A different rate is refused with
the required rate named rather than silently resampled — resampling here would
change what the model hears and therefore what it reports, and a wrong-rate
classification that looks right is worse than a refusal.

## Honest gaps

- **The memory figure is not a measurement.** It is a conservative 512 MiB
  admission ceiling, and `models.yaml` says so in the entry itself. Its error
  runs one way only: it can cause the service to withhold the model from a host
  that could have run it, never to offer it to a host that cannot. The research
  sourced no figure at all — only "tiny — mobile-targeted". The honest
  replacement is a measured resident-set size of the loaded interpreter, one
  profiling run away. Until then the number must not be quoted as a requirement.
- **The class count is 521, not 527.** The repository catalogue's deferred
  `yamnet` record says 527. Verified 2026-09-02 by fetching
  `yamnet_class_map.csv`: 522 lines, one header plus 521 classes. 527 is the size
  of the AudioSet ontology, which is not YAMNet's output dimension. The
  distinction is load-bearing — the anti-simulation guard checks a returned score
  vector against the loaded map's length, so a wrong count would either reject
  every genuine result or admit a fabricated one.
- **PANNs and CLAP are deferred, not dismissed.** CLAP's zero-shot
  audio-to-text-concept matching is a genuinely more capable shape than YAMNet's
  fixed class list and is worth closing; it needs its variant's licence read and
  its figures measured.
- **No weight digest.** None was gathered. The Kaggle original and the mirror
  should be digest-compared at first download.
- **Real-inference tests skip without the runtime or weights.** They do not
  fabricate scores to go green; they say what is missing and skip.

## Sources verified

- `https://api.github.com/repos/tensorflow/models/contents/research/audioset/yamnet` — 2026-09-02 — artefact list
- `https://raw.githubusercontent.com/tensorflow/models/master/research/audioset/yamnet/yamnet_class_map.csv` — 2026-09-02 — 522 lines, 14,096 B
- `https://pypi.org/pypi/ai-edge-litert/json` — 2026-09-02 — version 2.2.0, Apache-2.0, Python 3.8–3.11
- HTTP HEAD on the YAMNet TFLite artefact — 2026-09-02 — 4,126,810 B
