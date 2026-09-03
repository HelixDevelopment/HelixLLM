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
        if has_source and refused_precision:
            # CRITICAL-4. The symmetric half of the branch above, and the one that
            # was MISSING: the check bit on `backend` and never on `precision`, so
            # an entry recording a source for a quantisation this loader cannot
            # read passed silently. That is not a lesser case of the same defect —
            # it is the SAME defect with the worse blast radius, because the
            # refused precision is `gguf-q4` and the only sources that exist for
            # those entries are UNQUANTISED repositories. Recording one asserts
            # "these are the weights for the build above" about weights that are
            # a different build entirely, at a different memory and disk cost.
            problems.append(
                f"{model_id} records a source but its recorded quantisation {quant!r} is one this "
                "loader does not implement: the source therefore cannot be the weights for the build "
                "this entry describes — it is a source for a DIFFERENT build, recorded in the field "
                "the acquisition gate validates and the runtime fetches from. Either establish a "
                "source that genuinely serves the recorded quantisation (and lift the marker in the "
                "same change), or withdraw the source so `repositoryFor` refuses the entry at boot "
                "as it does for the ltx entry"
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


def check_sources_serve_the_recorded_build(manifest: dict, entries: dict) -> list[str]:
    """CRITICAL-4: the manifest and the catalogue must agree about what a repo IS.

    The manifest already grades every repository it names with
    `serves_recorded_build`. Where that grade is `false` the manifest states, in
    its own words, that the repository is retained "as the PROVENANCE of the
    measured encoder size ... NOT as a weight source to fetch".

    `source:` is the one field the runtime DOES fetch from (`repositoryFor` reads
    it and nothing else) and the one field the FR-012/SC-011 allowlist validates.
    So a repo the manifest grades `serves_recorded_build: false` appearing as its
    entry's `source:` is not a nuance — it is the two documents flatly
    contradicting each other about the same URL, with the runtime believing the
    one that is wrong.

    Returns a list of contradictions; empty means the two documents agree.
    """
    problems: list[str] = []
    graded = 0
    for source in manifest.get("sources") or []:
        name = source.get("name", "<unnamed>")
        if "serves_recorded_build" not in source:
            problems.append(
                f"manifest source {name} states no `serves_recorded_build`: whether a repository "
                "serves the build its entry records is the fact this check exists to compare, and "
                "an ungraded repository is one nothing can compare"
            )
            continue
        graded += 1
        if source["serves_recorded_build"]:
            continue

        entry_id = source.get("catalogue_entry")
        entry = entries.get(entry_id)
        if entry is None:
            problems.append(
                f"manifest source {name} names catalogue_entry {entry_id!r}, which is not in the "
                "catalogue — the grade cannot be checked against anything"
            )
            continue

        recorded = str(entry.get("source") or "").strip()
        if not recorded:
            continue
        repo = source.get("repo", "")
        if repo and repo in recorded:
            problems.append(
                f"{entry_id} records source {recorded!r}, but the manifest grades that same "
                f"repository (source {name}) `serves_recorded_build: false` — the two documents "
                "contradict each other about the same URL. `source:` is what the runtime fetches "
                "from and what the acquisition gate validates, so recording it here asserts the "
                "opposite of what the manifest measured. Withdraw the source (keeping the URL in "
                "`annotations` as provenance, as the ltx entry does) or re-grade the manifest with "
                "the measurement that justifies it"
            )
    if graded == 0:
        problems.append("no manifest source carried a `serves_recorded_build` grade — refusing to pass vacuously")
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

# CRITICAL-4's pre-fix catalogue: the `wan2.2-t2v-a14b` entry as it shipped —
# quantisation `gguf-q4` (which this loader cannot read at all) recorded
# alongside a `source:` pointing at the UNQUANTISED Diffusers repository. Both
# documents already MEASURED that repo as containing zero .gguf files; only the
# field the runtime actually fetches from still said otherwise.
PRE_FIX_SOURCED_GGUF_ENTRIES = {
    "wan2.2-t2v-a14b": {
        "model_id": "wan2.2-t2v-a14b",
        "descriptor": {"quantisation": "gguf-q4"},
        "source": "https://huggingface.co/Wan-AI/Wan2.2-T2V-A14B-Diffusers",
    },
}

PRE_FIX_GRADED_MANIFEST = {
    "sources": [
        {
            "name": "wan22_14b_gguf",
            "repo": "Wan-AI/Wan2.2-T2V-A14B-Diffusers",
            "catalogue_entry": "wan2.2-t2v-a14b",
            "serves_recorded_build": False,
        },
    ],
}


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


class SourcesServeTheRecordedBuild(unittest.TestCase):
    """CRITICAL-4 — the root cause of all three ltx-class defects.

    All three came from ONE fact about this service: it has NO GGUF load path.
    It builds pipelines through `from_pretrained` alone, which resolves a
    diffusers-format repository via `model_index.json` and cannot read a
    single-file `.gguf` checkpoint. Everything else followed from three records
    written as though it could:

      EX-2         the `ltx` backend, pointed at a repo with no .gguf
      EX-12        the ltx entry, correctly left with no source
      CRITICAL-4   `wan2.2-t2v-a14b`, recording `gguf-q4` AND a source — the
                   one that was still LIVE, because nothing compared the
                   quantisation against the recorded source

    The refusals now cover the SERVING path twice (the loader refuses `gguf-q4`
    at configuration time; the boot's `servingPrecisions` no longer lists it, so
    selection refuses first with exit 24). This class covers the DATA, which is
    the layer those refusals do not reach: both of them are conditions a future
    change is explicitly invited to lift — the loader's own remedy text and
    `modelchoice.go` both say "add the precision back HERE in the same change".
    Lifting them with the wrong source still recorded re-arms the defect
    immediately, so the source must be wrong in NO document, not merely
    unreachable through two of them.
    """

    def test_no_entry_records_a_source_for_a_precision_the_loader_cannot_read(self):
        entries = load_catalogue_entries()
        backend_of = runtime_backend_map()
        if red_mode():
            # The pre-fix catalogue, through the SAME checker, with today's real
            # refusal tables — which is the honest pre-fix pairing: the loader
            # refused gguf-q4 and the entry recorded a source for it anyway.
            unimplemented, unsupported = server_markers()
            problems = check_backends_resolve_or_are_unsupported(
                PRE_FIX_SOURCED_GGUF_ENTRIES,
                {"wan2.2-t2v-a14b": "wan"},
                unsupported,
                unimplemented,
            )
            self.assertTrue(
                problems,
                "RED_MODE=1: the pre-fix entry recorded a source for a quantisation the loader "
                "cannot read, so the checker MUST report it. Silence here means the guard cannot "
                "catch the defect it exists for.",
            )
            self.assertTrue(
                any("wan2.2-t2v-a14b" in p and "quantisation" in p for p in problems),
                f"RED_MODE=1: expected the sourced gguf-q4 entry to be named; got {problems}",
            )
            return
        unimplemented, unsupported = server_markers()
        self.assertEqual(
            check_backends_resolve_or_are_unsupported(entries, backend_of, unsupported, unimplemented), []
        )

    def test_manifest_and_catalogue_agree_on_what_each_repo_serves(self):
        if red_mode():
            problems = check_sources_serve_the_recorded_build(
                PRE_FIX_GRADED_MANIFEST, PRE_FIX_SOURCED_GGUF_ENTRIES
            )
            self.assertTrue(
                problems,
                "RED_MODE=1: the manifest graded that repo `serves_recorded_build: false` while the "
                "entry recorded it as `source:`. The checker MUST report the contradiction.",
            )
            self.assertTrue(
                any("contradict" in p for p in problems),
                f"RED_MODE=1: expected the contradiction to be named; got {problems}",
            )
            return
        self.assertEqual(
            check_sources_serve_the_recorded_build(load_manifest(), load_catalogue_entries()), []
        )

    def test_the_gguf_entries_are_unacquirable_at_boot(self):
        """The remedy, asserted where a caller meets it.

        `repositoryFor` reads `Entry.Source` and nothing else, so an entry with
        no source cannot reach a download regardless of what any precision table
        later says. That is the property that survives someone adding a GGUF
        load path: this is a DATA refusal, not a runtime one.
        """
        if red_mode():
            self.skipTest(
                "SKIP-OK: no pre-fix counterpart — the pre-fix a14b entry recorded a source, which "
                "is the defect this asserts the absence of"
            )
        entries = load_catalogue_entries()
        unimplemented, _ = server_markers()
        checked = 0
        for model_id, entry in sorted(entries.items()):
            quant = str((entry.get("descriptor") or {}).get("quantisation") or "").strip().lower()
            if quant not in unimplemented:
                continue
            checked += 1
            self.assertEqual(
                str(entry.get("source") or ""), "",
                f"{model_id} records quantisation {quant!r}, which this loader cannot read, yet it "
                "records a weight source: boot would resolve a repository for a build that cannot "
                "be served from it",
            )
        self.assertGreater(
            checked, 0,
            "no entry carried an unimplemented quantisation; this guard asserted nothing. Either the "
            "loader's refusal table was emptied (in which case a GGUF load path must now exist and "
            "these entries need re-measured sources) or the entries were removed",
        )

    def test_video_generation_still_has_an_acquirable_option(self):
        """Withdrawing a source must not withdraw the capability (§11.4.122).

        The a14b source was withdrawn because it was wrong for that entry, not
        because video generation should stop being offered. The default fp8 path
        keeps a recorded, servable source; if this ever fails, the lane has no
        bootable option left and that is a capability loss, not a data fix.
        """
        if red_mode():
            self.skipTest("SKIP-OK: no pre-fix counterpart — the capability was never absent")
        entries = load_catalogue_entries()
        unimplemented, unsupported = server_markers()
        backend_of = runtime_backend_map()
        servable = [
            model_id
            for model_id, entry in entries.items()
            if str(entry.get("source") or "").strip()
            and str((entry.get("descriptor") or {}).get("quantisation") or "").strip().lower()
            not in unimplemented
            and backend_of.get(model_id) not in unsupported
        ]
        self.assertTrue(
            servable,
            "no video entry is both sourced AND servable by this loader: the lane can measure the "
            "host and then boot nothing. Video generation would be silently unavailable",
        )


if __name__ == "__main__":
    print(f"RED_MODE={os.environ.get('RED_MODE', '0')}")
    unittest.main(verbosity=2)
