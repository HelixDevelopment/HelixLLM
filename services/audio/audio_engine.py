"""Audio-classification engine: catalogue selection, real inference, class map.

Separated from the HTTP shim for the same reason as the text-to-speech engine:
everything except the forward pass imports with nothing but the standard
library plus PyYAML, so the family-separation rules and the anti-simulation
guard are testable on any host rather than only inside a built image.

The engine is YAMNet through LiteRT (the TensorFlow Lite successor) on the
processor. Which model runs is not hardcoded — it is what `plan_for_host`
selects from the catalogue for the declared purpose on the measured host, and
YAMNet wins because it is the only audio-classification model in this
repository whose licence is confirmed permissive.

Two properties of this family shape the whole module:

  * It must run on EVERY host class, including one with no accelerator and 8 GB
    of RAM. So there is no CUDA anywhere in this path, and the selection is not
    gated behind an accelerator.

  * It is NOT audio generation. `resolve()` refuses a generation entry by name.
    That refusal is not defensive coding — merging the two families would mean
    either offering a processor-only host a model that cannot run there, or
    hiding generation behind a family that looks available.

NO SIMULATED CLASSIFICATION. Every score vector comes from a real forward pass
and is checked against the length of the class map actually loaded from the
model's own published artefact before it can be returned.
"""

from __future__ import annotations

import csv
import dataclasses
import os
import sys
import threading
from typing import Iterable, Sequence

sys.path.insert(
    0,
    os.environ.get(
        "HELIXLLM_GATE_DIR",
        os.path.join(
            os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))),
            "container",
        ),
    ),
)

import helix_model_gate as gate  # noqa: E402


class EngineUnsupported(RuntimeError):
    """A catalogued entry is real, but this service cannot run it."""


class ClassMapError(RuntimeError):
    """The class map is absent or the wrong shape, so no score can be named."""


# Properties of the published YAMNet artefact, not tunables. The class count is
# 521 — verified 2026-09-02 against yamnet_class_map.csv in tensorflow/models
# (522 lines: one header and 521 classes). 527 is the size of the AudioSet
# ontology and is a different number; using it here would make the guard reject
# every genuine result.
YAMNET_SAMPLE_RATE = 16000
YAMNET_CLASS_COUNT = 521


@dataclasses.dataclass(frozen=True)
class ClassificationPlan:
    catalogue_key: str
    engine: str
    sample_rate: int
    expected_class_count: int
    license_id: str
    purpose: str = ""
    selection_basis: str = ""
    withheld: tuple[dict, ...] = ()

    def as_dict(self) -> dict:
        d = dataclasses.asdict(self)
        d["withheld"] = list(self.withheld)
        return d


def resolve(entry: gate.CatalogueEntry) -> ClassificationPlan:
    """Map one already-selected catalogue entry onto a runnable engine."""
    if entry.family == gate.FAMILY_AUDIO_GENERATION:
        raise EngineUnsupported(
            f"{entry.key} belongs to the {gate.FAMILY_AUDIO_GENERATION} family, which "
            "this service deliberately does not serve. Generation and classification "
            "are kept apart because every generation model surveyed requires an "
            "accelerator, while this service exists to run on hosts that have none. "
            "A host with no accelerator is owed that reason, not a substitute model."
        )
    if entry.family != gate.FAMILY_AUDIO_CLASSIFICATION:
        raise EngineUnsupported(
            f"{entry.key} is a {entry.family} model; this service serves "
            f"{gate.FAMILY_AUDIO_CLASSIFICATION} only"
        )
    if entry.model_id.lower().startswith("yamnet"):
        return ClassificationPlan(
            catalogue_key=entry.key,
            engine="litert",
            sample_rate=YAMNET_SAMPLE_RATE,
            expected_class_count=YAMNET_CLASS_COUNT,
            license_id=entry.usage_terms.license_id,
            selection_basis="resolved from a supplied catalogue entry",
        )
    raise EngineUnsupported(
        f"{entry.key} cannot be served by this service: it carries a LiteRT YAMNet "
        f"runtime, and '{entry.model_id}' needs an engine this container does not "
        "have. Refusing rather than substituting a different model."
    )


def plan_for_host(
    entries: Iterable[gate.CatalogueEntry],
    host: gate.HostMeasurement,
    purpose: str = gate.USAGE_COMMERCIAL,
) -> ClassificationPlan:
    """Measured host + declared purpose -> a runnable classification plan."""
    candidates = list(entries)
    tried: list[str] = []
    while True:
        selection = gate.select(
            candidates, gate.FAMILY_AUDIO_CLASSIFICATION, purpose, host
        )
        try:
            plan = resolve(selection.entry)
        except EngineUnsupported as exc:
            tried.append(f"{selection.entry.key}: {exc}")
            candidates = [e for e in candidates if e.key != selection.entry.key]
            if not any(
                e.family == gate.FAMILY_AUDIO_CLASSIFICATION for e in candidates
            ):
                raise gate.CannotChoose(
                    gate.REASON_UNSUPPORTED_CONFIGURATION,
                    (
                        "every audio-classification model this host could run needs an "
                        "engine this service does not carry: " + " | ".join(tried)
                    ),
                    selection.withheld,
                ) from exc
            continue
        return dataclasses.replace(
            plan,
            purpose=purpose,
            selection_basis=(
                "measured host + declared usage purpose over the model catalogue "
                "(no configured model name)"
            ),
            withheld=tuple(dataclasses.asdict(w) for w in selection.withheld),
        )


# --------------------------------------------------------------------------
# Artefacts
# --------------------------------------------------------------------------


def _root() -> str:
    return os.environ.get("HELIXLLM_AUDIO_WEIGHTS_DIR", "/models/audio")


def model_path() -> str:
    """Where the model file lives. Configuration says WHERE, never WHICH."""
    return os.path.join(
        _root(),
        os.environ.get(
            "HELIXLLM_AUDIO_MODEL_FILE", "lite-model_yamnet_classification_tflite_1.tflite"
        ),
    )


def class_map_path() -> str:
    return os.path.join(
        _root(), os.environ.get("HELIXLLM_AUDIO_CLASS_MAP", "yamnet_class_map.csv")
    )


def load_class_map(path: str) -> list[str]:
    """Read the display names from the model's own published class map.

    The length is load-bearing: the anti-simulation guard checks a returned
    score vector against it, so a truncated or substituted map would either
    reject every real result or admit a fabricated one. A map of the wrong
    length is therefore an error, not something to work around.
    """
    if not os.path.exists(path):
        raise ClassMapError(f"class map not found at {path}")
    names: list[str] = []
    with open(path, "r", encoding="utf-8", newline="") as fh:
        reader = csv.reader(fh)
        header = next(reader, None)
        if header is None:
            raise ClassMapError(f"class map at {path} is empty")
        for row in reader:
            if not row:
                continue
            names.append(row[-1].strip())
    if len(names) != YAMNET_CLASS_COUNT:
        raise ClassMapError(
            f"class map at {path} has {len(names)} classes; this model emits "
            f"{YAMNET_CLASS_COUNT}. Scores could not be attributed to classes, and "
            "guessing the alignment would mislabel every result."
        )
    return names


_engine = None
_engine_error: str | None = None
_lock = threading.Lock()


def load_engine():
    """Load the REAL LiteRT interpreter. A failure is recorded, never faked."""
    global _engine, _engine_error
    if _engine is not None:
        return _engine
    with _lock:
        if _engine is not None:
            return _engine
        path = model_path()
        if not os.path.exists(path):
            _engine_error = (
                f"model weights are not present at {path}. Obtain them via the "
                "service's .gitignore-meta regeneration manifest."
            )
            raise RuntimeError(_engine_error)
        try:
            from ai_edge_litert.interpreter import Interpreter
        except Exception:
            try:
                from tflite_runtime.interpreter import Interpreter  # type: ignore
            except Exception as exc:
                _engine_error = f"no LiteRT/TFLite runtime is installed: {exc}"
                raise RuntimeError(_engine_error) from exc
        interp = Interpreter(model_path=path)
        interp.allocate_tensors()
        _engine = interp
        _engine_error = None
        return _engine


def engine_error() -> str | None:
    return _engine_error


def classify(interpreter, samples: Sequence[float]) -> list[float]:
    """Run a REAL forward pass over mono 16 kHz float samples.

    Returns the mean score per class across the model's internal frames. The
    caller passes the result to `gate.assert_real_scores` before returning it;
    the guard is not applied here so that a caller can inspect a rejected
    vector while diagnosing, rather than only seeing the exception.
    """
    import numpy as np

    waveform = np.asarray(samples, dtype=np.float32).reshape(-1)
    if waveform.size == 0:
        raise ValueError("no audio to classify")

    detail = interpreter.get_input_details()[0]
    interpreter.resize_tensor_input(detail["index"], [waveform.size], strict=False)
    interpreter.allocate_tensors()
    interpreter.set_tensor(interpreter.get_input_details()[0]["index"], waveform)
    interpreter.invoke()
    scores = interpreter.get_tensor(interpreter.get_output_details()[0]["index"])
    scores = np.asarray(scores, dtype=np.float32)
    if scores.ndim > 1:
        # YAMNet emits one score row per ~0.48 s frame; the clip-level answer
        # is their mean, which is what the model card prescribes.
        scores = scores.mean(axis=0)
    return [float(v) for v in scores.reshape(-1)]


def top_classes(scores: Sequence[float], names: Sequence[str], k: int = 5) -> list[dict]:
    order = sorted(range(len(scores)), key=lambda i: scores[i], reverse=True)[: max(1, k)]
    return [
        {"index": i, "label": names[i] if i < len(names) else "", "score": float(scores[i])}
        for i in order
    ]
