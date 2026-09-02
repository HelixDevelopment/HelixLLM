"""Speech-to-text: the path from a catalogue entry to the running engine (T079).

This is WIRING, not a new service. The engine already exists and works:
`whisper_stt_server.py` runs faster-whisper (CTranslate2) on the processor
with a Silero VAD filter and a no_speech_prob threshold guarding against the
silence-hallucination failure mode, behind an OpenAI-compatible
`POST /v1/audio/transcriptions`. None of that is re-implemented here.

What was missing is the connection to the model catalogue. The server chose
its model from a `WHISPER_MODEL` environment variable defaulting to `base` —
a configuration value naming the model to run, which FR-056 forbids as the
selection MECHANISM. This module replaces that with: measured host + declared
usage purpose -> catalogue selection -> engine plan. Configuration still says
WHERE weight files live and may pin a device, which FR-056 explicitly allows;
it no longer says WHICH model.

Honest boundary. This container runs faster-whisper, which serves Whisper
weights. The catalogue's speech-to-text family also carries Parakeet, which is
a NeMo/sherpa-onnx model CTranslate2 does not load. A Parakeet entry is
therefore REFUSED here with that exact reason rather than silently mapped to
a Whisper size — a wrong model quietly serving a request is worse than a
refusal that names the gap. Serving Parakeet needs a second engine, which is
not this task.
"""

from __future__ import annotations

import dataclasses
import os
from typing import Iterable

import helix_model_gate as gate


class EngineUnsupported(RuntimeError):
    """A catalogued entry is real, but this container's engine cannot run it."""


@dataclasses.dataclass(frozen=True)
class EnginePlan:
    """Everything faster-whisper needs, derived — never configured by name."""

    catalogue_key: str
    family: str
    model_name: str
    device: str
    compute_type: str
    license_id: str
    purpose: str
    selection_basis: str
    withheld: tuple[dict, ...] = ()

    def as_dict(self) -> dict:
        d = dataclasses.asdict(self)
        d["withheld"] = list(self.withheld)
        return d


# The faster-whisper size names, keyed by the catalogue model_id prefix. This
# is a NAME TRANSLATION between two vocabularies for the same weights, not a
# model choice: which entry arrives here was already decided by measurement.
_WHISPER_SIZES = (
    "large-v3-turbo",
    "large-v3",
    "large-v2",
    "large",
    "distil-large-v3",
    "medium",
    "small",
    "base",
    "tiny",
)


def _whisper_size_for(model_id: str) -> str | None:
    """Translate a catalogue model_id into a faster-whisper size name.

    Longest match first so `whisper-large-v3` does not resolve to `large`.
    """
    lowered = model_id.lower()
    if "whisper" not in lowered:
        return None
    for size in _WHISPER_SIZES:
        if lowered.endswith(size) or f"-{size}-" in lowered or lowered == f"whisper-{size}":
            return size
    return None


def _compute_type_for(host: gate.HostMeasurement) -> str:
    """Pick the numeric precision from what the host actually has.

    An accelerator with real free memory gets float16; a processor gets int8,
    which is the quantised CPU path this engine was chosen for. Either may be
    pinned by the operator — pinning a PRECISION is not naming a model.
    """
    pinned = os.environ.get("WHISPER_COMPUTE_TYPE", "").strip()
    if pinned:
        return pinned
    if host.has_accelerator and host.accelerator_free_bytes > 0:
        return "float16"
    return "int8"


def _device_for(host: gate.HostMeasurement) -> str:
    pinned = os.environ.get("WHISPER_DEVICE", "").strip()
    if pinned:
        return pinned
    return "cuda" if (host.has_accelerator and host.accelerator_free_bytes > 0) else "cpu"


def resolve(entry: gate.CatalogueEntry, host: gate.HostMeasurement) -> EnginePlan:
    """Turn one already-selected catalogue entry into an engine plan.

    Raises EngineUnsupported when the entry is a real catalogued model that
    this container's engine cannot load.
    """
    if entry.family != gate.FAMILY_SPEECH_TO_TEXT:
        raise EngineUnsupported(
            f"{entry.key} is a {entry.family} model; this engine serves "
            f"{gate.FAMILY_SPEECH_TO_TEXT} only"
        )

    size = _whisper_size_for(entry.model_id)
    if size is None:
        raise EngineUnsupported(
            f"{entry.key} cannot be served by this container: its engine is "
            "faster-whisper (CTranslate2), which loads Whisper weights. "
            f"'{entry.model_id}' is not a Whisper model and would need a second "
            "engine (sherpa-onnx / NeMo) that this container does not carry. "
            "Refusing rather than substituting a different model."
        )

    return EnginePlan(
        catalogue_key=entry.key,
        family=entry.family,
        model_name=size,
        device=_device_for(host),
        compute_type=_compute_type_for(host),
        license_id=entry.usage_terms.license_id,
        purpose="",
        selection_basis="resolved from a supplied catalogue entry",
    )


def plan_for_host(
    entries: Iterable[gate.CatalogueEntry],
    host: gate.HostMeasurement,
    purpose: str = gate.USAGE_COMMERCIAL,
) -> EnginePlan:
    """The whole catalogue path: measured host -> selection -> engine plan.

    Entries the gate admits but this engine cannot load are dropped from the
    running and the selection is retried, so a Parakeet entry does not block a
    Whisper one. If nothing is left, the reason is reported specifically —
    never as a fixed default model.
    """
    candidates = list(entries)
    tried: list[str] = []
    while True:
        selection = gate.select(candidates, gate.FAMILY_SPEECH_TO_TEXT, purpose, host)
        try:
            plan = resolve(selection.entry, host)
        except EngineUnsupported as exc:
            tried.append(f"{selection.entry.key}: {exc}")
            candidates = [e for e in candidates if e.key != selection.entry.key]
            if not any(e.family == gate.FAMILY_SPEECH_TO_TEXT for e in candidates):
                raise gate.CannotChoose(
                    gate.REASON_UNSUPPORTED_CONFIGURATION,
                    (
                        "every speech-to-text model this host could run needs an engine "
                        "this container does not carry: " + " | ".join(tried)
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


def measure_host() -> gate.HostMeasurement:
    """Measure THIS host: free system memory, free disk, accelerator presence.

    Every figure is read from the running system. When a figure cannot be
    read, `measured` is False and the caller refuses to choose (FR-056) rather
    than proceeding on a guess.
    """
    try:
        page_size = os.sysconf("SC_PAGE_SIZE")
        avail_pages = os.sysconf("SC_AVPHYS_PAGES")
        system_free = int(page_size) * int(avail_pages)
    except (ValueError, OSError, AttributeError):
        return gate.HostMeasurement(measured=False)

    try:
        st = os.statvfs(os.environ.get("HELIXLLM_WEIGHTS_DIR", "/"))
        free_disk = int(st.f_bavail) * int(st.f_frsize)
    except OSError:
        return gate.HostMeasurement(measured=False)

    has_accel, accel_free = _measure_accelerator()
    return gate.HostMeasurement(
        measured=True,
        has_accelerator=has_accel,
        accelerator_free_bytes=accel_free,
        system_free_bytes=system_free,
        free_disk_bytes=free_disk,
    )


def _measure_accelerator() -> tuple[bool, int]:
    """Accelerator presence and USABLE free memory, not nominal capacity.

    Absence of an accelerator is a fully supported configuration, not a
    failure — a processor-only host still receives real options here.
    """
    try:
        import torch  # noqa: WPS433 — optional; absent on a processor-only image
    except Exception:
        return False, 0
    try:
        if not torch.cuda.is_available():
            return False, 0
        free, _total = torch.cuda.mem_get_info(0)
        return True, int(free)
    except Exception:
        return False, 0
