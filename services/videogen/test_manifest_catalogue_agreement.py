#!/usr/bin/env python3
"""Guards that the video service's THREE records of the same facts agree.

The same facts are written down in three places, and each pair of them has
already drifted at least once:

    services/videogen/.gitignore-meta/wan_ltx_gguf.yaml   the weight manifest
    internal/catalogue/data/video.yaml                    the model catalogue
    services/videogen/videogen_server.py                  what the loader serves

WHAT THIS PROVES

  EX-1  Every disk figure in the manifest equals the `storage_required_bytes`
        of the catalogue entry it names. The shipped defect was a manifest
        declaring "~18 GiB" for a path the catalogue MEASURED at 31.85 GiB — a
        ~14 GiB understatement, which spends itself as a host running out of
        disk PART WAY THROUGH a long download. Nothing compared the two numbers,
        so nothing caught it.

  EX-2  Every backend the runtime implements either resolves a RECORDED weight
        source or is EXPLICITLY marked unsupported. The shipped defect was a
        backend that did neither: it resolved a repository whose
        `from_pretrained` SUCCEEDS on a ~2B transformer while the catalogue
        records 13B. Serving a smaller model than documented is the worst
        outcome available — the caller attributes the output to the 13B model —
        so "resolves something" is not the bar; "resolves what is recorded, or
        refuses" is.

POLARITY (§11.4.115) — one source, two roles, switched by RED_MODE:

    RED_MODE=1  feed the checkers the PRE-FIX inputs and assert each one REPORTS
                the defect. This is what makes the guard falsifiable: a checker
                that cannot fail on the broken input is decoration, not a gate.
    RED_MODE=0  the standing guard (default): run the checkers against the real
                files on disk and require silence.

    The checkers are the SAME functions in both modes. Only their input differs.

HONEST BOUNDARY (§11.4.6)
    This compares RECORDS against each other. It proves the three documents
    cannot drift apart again; it does NOT prove any figure is correct against
    the upstream repositories, and it downloads nothing. The manifest carries
    the re-measurement command and the revision each figure was read at; that
    re-measurement is the separate, network-bearing check.

Run:  python3 test_manifest_catalogue_agreement.py
      RED_MODE=1 python3 test_manifest_catalogue_agreement.py
"""

import ast
import os
import re
import unittest
from pathlib import Path

import yaml

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
MANIFEST = HERE / ".gitignore-meta" / "wan_ltx_gguf.yaml"
CATALOGUE = REPO / "internal" / "catalogue" / "data" / "video.yaml"
SERVER = HERE / "videogen_server.py"
BOOT_MODELCHOICE = REPO / "cmd" / "videogen-boot" / "modelchoice.go"


def red_mode() -> bool:
    return os.environ.get("RED_MODE") == "1"


# --------------------------------------------------------------------------
# readers
# --------------------------------------------------------------------------

def load_manifest() -> dict:
    return yaml.safe_load(MANIFEST.read_text(encoding="utf-8"))


def load_catalogue_entries() -> dict:
    doc = yaml.safe_load(CATALOGUE.read_text(encoding="utf-8"))
    return {e["model_id"]: e for e in doc["entries"]}


def server_markers() -> tuple[dict, dict]:
    """Read the loader's refusal tables WITHOUT importing the module.

    Importing would drag in the service image's web framework. The tables are
    module-level dict literals, so the AST is both sufficient and stricter: it
    reads what the source declares, not what some import-time branch produced.
    """
    tree = ast.parse(SERVER.read_text(encoding="utf-8"))
    found: dict[str, dict] = {}
    for node in ast.walk(tree):
        if not isinstance(node, ast.Assign):
            continue
        for target in node.targets:
            if isinstance(target, ast.Name) and target.id in (
                "_UNIMPLEMENTED_PRECISIONS",
                "_UNSUPPORTED_BACKENDS",
            ):
                found[target.id] = ast.literal_eval(node.value)
    missing = {"_UNIMPLEMENTED_PRECISIONS", "_UNSUPPORTED_BACKENDS"} - set(found)
    if missing:
        raise AssertionError(
            f"{SERVER.name} declares no {sorted(missing)} table. The refusal markers are "
            "load-bearing: without them an unservable configuration is served silently."
        )
    return found["_UNIMPLEMENTED_PRECISIONS"], found["_UNSUPPORTED_BACKENDS"]


def runtime_backend_map() -> dict[str, str]:
    """model_id -> backend, mirrored from the boot's `servingBackends`.

    Read from the Go source rather than restated here, so this guard cannot pass
    against a mapping the runtime no longer has. A parse failure is an error,
    never a skip: a guard that quietly stops checking is the failure mode.
    """
    src = BOOT_MODELCHOICE.read_text(encoding="utf-8")
    block = re.search(r"var\s+servingBackends\s*=\s*map\[string\]string\{(.*?)\}", src, re.S)
    if block is None:
        raise AssertionError(
            f"could not locate `servingBackends` in {BOOT_MODELCHOICE}; this guard's "
            "mirror of the runtime backend map is stale and must be re-pointed."
        )
    pairs = re.findall(r'"([^"]+)"\s*:\s*"([^"]+)"', block.group(1))
    if not pairs:
        raise AssertionError("`servingBackends` parsed to zero entries — refusing to pass vacuously.")
    return dict(pairs)


# --------------------------------------------------------------------------
# the checkers — shared by both polarities
# --------------------------------------------------------------------------

def check_disk_agreement(manifest: dict, entries: dict) -> list[str]:
    """EX-1: every manifest disk figure equals its catalogue entry's.

    Returns a list of disagreements; empty means agreement.
    """
    problems: list[str] = []
    sources = manifest.get("sources") or []

    for src in sources:
        name = src.get("name", "<unnamed>")
        entry_id = src.get("catalogue_entry")
        if entry_id is None:
            problems.append(
                f"manifest source {name!r} names no `catalogue_entry`, so its disk figure "
                "is comparable to nothing and can drift silently"
            )
            continue
        entry = entries.get(entry_id)
        if entry is None:
            problems.append(f"manifest source {name!r} names catalogue entry {entry_id!r}, which does not exist")
            continue
        declared = src.get("expected_disk_usage_bytes")
        if declared is None:
            problems.append(
                f"manifest source {name!r} declares no `expected_disk_usage_bytes` — prose alone "
                "('~18 GiB') is exactly what drifted from the catalogue unnoticed"
            )
            continue
        recorded = entry.get("storage_required_bytes")
        if declared != recorded:
            problems.append(
                f"disk disagreement for {entry_id}: manifest says {declared} B, catalogue "
                f"`storage_required_bytes` says {recorded} B (delta {declared - recorded:+d} B)"
            )

    # The headline figure operators provision from must be the default path's,
    # and must not be prose that disagrees with its own number.
    top = manifest.get("expected_disk_usage_bytes")
    default = next((s for s in sources if s.get("name") == "wan22_5b_default"), None)
    if top is None:
        problems.append("manifest declares no top-level `expected_disk_usage_bytes`")
    elif default is not None and top != default.get("expected_disk_usage_bytes"):
        problems.append(
            f"top-level expected_disk_usage_bytes ({top}) is not the DEFAULT path's "
            f"({default.get('expected_disk_usage_bytes')}); operators provision from the headline figure"
        )
    prose = str(manifest.get("expected_disk_usage", ""))
    if top is not None and str(top) not in prose:
        problems.append(
            f"the prose `expected_disk_usage` does not contain its own byte figure ({top}); "
            "prose and number must not be able to drift apart"
        )

    total = manifest.get("expected_disk_usage_all_sources_bytes")
    if total is not None:
        summed = sum(s.get("expected_disk_usage_bytes") or 0 for s in sources)
        if total != summed:
            problems.append(f"expected_disk_usage_all_sources_bytes {total} != sum of sources {summed}")
    return problems


def check_backends_resolve_or_are_unsupported(
    entries: dict, backend_of: dict, unsupported_backends: dict, unimplemented_precisions: dict
) -> list[str]:
    """EX-2: no backend may resolve to something other than what is recorded.

    For each model the runtime claims a pipeline for, exactly one of these must
    hold, and the two records must AGREE about which:
      * the entry records a `source` and the configuration is servable, or
      * the configuration is explicitly refused (unsupported backend, or a
        precision this loader does not implement).
    """
    problems: list[str] = []
    for model_id, backend in sorted(backend_of.items()):
        entry = entries.get(model_id)
        if entry is None:
            problems.append(f"the runtime claims backend {backend!r} for {model_id!r}, which is not in the catalogue")
            continue
        has_source = bool(str(entry.get("source") or "").strip())
        quant = str((entry.get("descriptor") or {}).get("quantisation") or "").strip().lower()
        refused_backend = backend in unsupported_backends
        refused_precision = quant in unimplemented_precisions
        refused = refused_backend or refused_precision

        if not has_source and not refused:
            problems.append(
                f"{model_id} (backend {backend!r}) records NO source and is NOT marked unsupported: "
                "the runtime would resolve whatever it is pointed at, with nothing asserting that is "
                "the recorded build — the silent-wrong-model defect"
            )
        if has_source and refused_backend:
            problems.append(
                f"{model_id} records a source but backend {backend!r} is still marked unsupported: "
                "if a source that serves the recorded build has been established, lift the marker in "
                "the same change; if it has not, the source must not be recorded"
            )
        if refused:
            why = unsupported_backends.get(backend) or unimplemented_precisions.get(quant) or ""
            if "establish support" not in why.lower():
                problems.append(
                    f"the refusal for {model_id} states no way out — an unsupported marker must say "
                    "what would establish support, or it is a dead end nobody can clear"
                )
    return problems


def check_server_and_manifest_markers_agree(manifest: dict, unsupported_backends: dict,
                                            unimplemented_precisions: dict) -> list[str]:
    """The loader's refusals and the manifest's record of them must match."""
    problems: list[str] = []
    m_backends = {b["backend"] for b in (manifest.get("unsupported_backends") or [])}
    m_precisions = {p["precision"] for p in (manifest.get("unimplemented_precisions") or [])}
    if m_backends != set(unsupported_backends):
        problems.append(
            f"unsupported backends disagree: manifest {sorted(m_backends)} vs loader "
            f"{sorted(unsupported_backends)}"
        )
    if m_precisions != set(unimplemented_precisions):
        problems.append(
            f"unimplemented precisions disagree: manifest {sorted(m_precisions)} vs loader "
            f"{sorted(unimplemented_precisions)}"
        )
    return problems


# --------------------------------------------------------------------------
# the PRE-FIX inputs, replayed through the same checkers under RED_MODE=1
# --------------------------------------------------------------------------

PRE_FIX_MANIFEST = {
    # verbatim shape of the manifest before this fix: prose only, no byte
    # figure, no per-source figures, nothing a comparison could bite on.
    "expected_disk_usage": "~18 GiB (WAN 2.2 TI2V-5B FP8 default; +GGUF-Q4 14B / LTX-13B variants on demand)",
    "sources": [
        {"name": "wan22_5b_default", "repo": "Wan-AI/Wan2.2-TI2V-5B-Diffusers"},
        {"name": "wan22_14b_gguf", "repo": "Wan-AI/Wan2.2-T2V-A14B-Diffusers"},
        {"name": "ltx_13b", "repo": "Lightricks/LTX-Video"},
    ],
}

# the pre-fix loader: an `ltx` backend with no refusal marker at all
PRE_FIX_UNSUPPORTED_BACKENDS: dict[str, str] = {}
PRE_FIX_UNIMPLEMENTED_PRECISIONS: dict[str, str] = {}


class DiskFiguresAgree(unittest.TestCase):
    """EX-1."""

    def test_manifest_and_catalogue_agree_on_disk(self):
        entries = load_catalogue_entries()
        if red_mode():
            problems = check_disk_agreement(PRE_FIX_MANIFEST, entries)
            self.assertTrue(
                problems,
                "RED_MODE=1: the pre-fix manifest declared no comparable byte figure, so the "
                "checker MUST report it. Silence here means the guard cannot catch the defect "
                "it exists for.",
            )
            self.assertTrue(
                any("expected_disk_usage_bytes" in p for p in problems),
                f"RED_MODE=1: expected the missing byte figure to be named; got {problems}",
            )
            return
        self.assertEqual(check_disk_agreement(load_manifest(), entries), [])

    def test_default_path_is_the_measured_figure(self):
        """The specific number the shipped defect got wrong."""
        if red_mode():
            self.assertNotIn(
                "34203021834", str(PRE_FIX_MANIFEST["expected_disk_usage"]),
                "RED_MODE=1: the pre-fix figure must NOT be the measured one",
            )
            return
        manifest = load_manifest()
        entry = load_catalogue_entries()["wan2.2-ti2v-5b"]
        self.assertEqual(manifest["expected_disk_usage_bytes"], 34203021834)
        self.assertEqual(manifest["expected_disk_usage_bytes"], entry["storage_required_bytes"])

    def test_measurement_method_is_recorded(self):
        """A corrected number with no method drifts again at the next estimate."""
        if red_mode():
            self.skipTest("SKIP-OK: no pre-fix counterpart — the pre-fix manifest recorded no method")
        text = MANIFEST.read_text(encoding="utf-8")
        self.assertIn("blobs=true", text, "the re-measurement command is not recorded")
        for source in load_manifest()["sources"]:
            self.assertIn("disk_basis", source, f"source {source['name']} does not say whether its figure is measured")
            if source["disk_basis"] == "measured":
                self.assertIn("resolved_revision", source, f"{source['name']}: measured but no revision pinned")


class BackendsResolveWhatIsRecorded(unittest.TestCase):
    """EX-2."""

    def test_every_backend_resolves_a_source_or_is_refused(self):
        entries = load_catalogue_entries()
        backend_of = runtime_backend_map()
        if red_mode():
            problems = check_backends_resolve_or_are_unsupported(
                entries, backend_of, PRE_FIX_UNSUPPORTED_BACKENDS, PRE_FIX_UNIMPLEMENTED_PRECISIONS
            )
            self.assertTrue(
                problems,
                "RED_MODE=1: with no refusal markers the ltx backend resolves a repository nothing "
                "asserts is the recorded build — the checker MUST report it.",
            )
            self.assertTrue(
                any("ltx-video-13b" in p for p in problems),
                f"RED_MODE=1: expected the ltx entry to be named; got {problems}",
            )
            return
        unimplemented, unsupported = server_markers()
        self.assertEqual(
            check_backends_resolve_or_are_unsupported(entries, backend_of, unsupported, unimplemented), []
        )

    def test_loader_and_manifest_agree_on_what_is_refused(self):
        if red_mode():
            self.skipTest("SKIP-OK: no pre-fix counterpart — neither document recorded a refusal")
        unimplemented, unsupported = server_markers()
        self.assertEqual(check_server_and_manifest_markers_agree(load_manifest(), unsupported, unimplemented), [])

    def test_ltx_backend_is_refused_at_configuration_time(self):
        """The chosen remedy, asserted at the surface a caller meets."""
        if red_mode():
            self.skipTest("SKIP-OK: no pre-fix counterpart — the pre-fix loader had no refusal to assert")
        _, unsupported = server_markers()
        self.assertIn("ltx", unsupported, "the ltx backend is no longer refused")
        self.assertIn("establish support", unsupported["ltx"].lower())
        # and the entry it would serve still records no source
        self.assertEqual(str(load_catalogue_entries()["ltx-video-13b"].get("source") or ""), "")

    def test_gguf_is_refused_because_no_loader_implements_it(self):
        """The root cause: `from_pretrained` cannot read GGUF, so both gguf-q4
        entries are unservable — not just the ltx one."""
        if red_mode():
            self.skipTest("SKIP-OK: no pre-fix counterpart — the pre-fix loader claimed gguf-q4 servable")
        unimplemented, _ = server_markers()
        self.assertIn("gguf-q4", unimplemented)

        # The oracle is the AST, not the file text — the same distinction the
        # FR-056 guard makes. `from_single_file` NAMED in a refusal's remedy text
        # is prose telling a maintainer what to build; `from_single_file` CALLED
        # is a GGUF path that exists. Only identifiers reach these node types, so
        # a string literal can never trip this.
        tree = ast.parse(SERVER.read_text(encoding="utf-8"))
        identifiers = {
            node.attr if isinstance(node, ast.Attribute) else node.id
            for node in ast.walk(tree)
            if isinstance(node, (ast.Attribute, ast.Name))
        }
        for absent in ("from_single_file", "GGUFQuantizationConfig"):
            self.assertNotIn(
                absent, identifiers,
                f"{absent} is now CALLED in the loader — if a real GGUF path was added, the "
                "gguf-q4 refusal must be lifted and the affected entries re-measured in the "
                "same change, or this guard is asserting a gap that no longer exists",
            )


if __name__ == "__main__":
    print(f"RED_MODE={os.environ.get('RED_MODE', '0')}")
    unittest.main(verbosity=2)
