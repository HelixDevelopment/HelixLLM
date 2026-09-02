"""Tests for the shared audio-capability model gate (helix_model_gate).

These are stdlib-only (`unittest`) on purpose: the gate itself imports nothing
outside the standard library plus PyYAML, so this suite runs on any host that
can run the repo's own tooling — no service container, no model weights, no
accelerator. That matters, because the gate is the load-bearing invariant for
FR-054 (usage terms gate what is offered) and FR-055 (the three distinct
reasons an offer is unavailable): a test for it that could only run inside a
built GPU image would in practice never run.

What is NOT tested here is inference — the gate performs none. Inference
proof lives in each service's own suite and SKIPS with a stated reason when
the weights are absent (§11.4.3), never fabricates a waveform or a score.
"""

import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import helix_model_gate as gate  # noqa: E402


def _entry(
    model_id="m",
    family=gate.FAMILY_TEXT_TO_SPEECH,
    permitted=(gate.USAGE_COMMERCIAL, gate.USAGE_PERSONAL),
    restrictions=(),
    requires_accelerator=False,
    memory=1024,
    storage=512,
    variant="",
):
    return gate.CatalogueEntry(
        model_id=model_id,
        variant=variant,
        family=family,
        usage_terms=gate.UsageTerms(
            license_id="TEST",
            permitted=tuple(permitted),
            restrictions=tuple(restrictions),
        ),
        requires_accelerator=requires_accelerator,
        memory_required_bytes=memory,
        storage_required_bytes=storage,
    )


def _host(accel=False, accel_free=0, sys_free=1 << 30, disk_free=1 << 30, measured=True):
    return gate.HostMeasurement(
        measured=measured,
        has_accelerator=accel,
        accelerator_free_bytes=accel_free,
        system_free_bytes=sys_free,
        free_disk_bytes=disk_free,
    )


class UsageTermsTest(unittest.TestCase):
    """Mirrors internal/catalogue/entry.go UsageTerms.Permits / RestrictionFor."""

    def test_grant_alone_permits(self):
        t = gate.UsageTerms("MIT", (gate.USAGE_COMMERCIAL,), ())
        self.assertTrue(t.permits(gate.USAGE_COMMERCIAL))

    def test_absent_grant_does_not_permit(self):
        t = gate.UsageTerms("MIT", (gate.USAGE_RESEARCH,), ())
        self.assertFalse(t.permits(gate.USAGE_COMMERCIAL))

    def test_exclusionary_restriction_beats_the_grant(self):
        # A licence that both grants and excludes the same purpose must be read
        # as excluding it. Reading it the other way round is how a
        # commercially-restricted model reaches a commercial user.
        t = gate.UsageTerms(
            "CPML",
            (gate.USAGE_COMMERCIAL, gate.USAGE_RESEARCH),
            (gate.Restriction("non-commercial", (gate.USAGE_COMMERCIAL,), "CPML"),),
        )
        self.assertFalse(t.permits(gate.USAGE_COMMERCIAL))
        self.assertTrue(t.permits(gate.USAGE_RESEARCH))

    def test_non_exclusionary_restriction_never_withholds(self):
        # CC-BY attribution constrains how output is used; it excludes no
        # purpose and must NEVER be reported as a reason an entry was withheld.
        t = gate.UsageTerms(
            "CC-BY-4.0",
            (gate.USAGE_COMMERCIAL,),
            (gate.Restriction("attribution-required", (), "CC-BY-4.0"),),
        )
        self.assertTrue(t.permits(gate.USAGE_COMMERCIAL))
        self.assertIsNone(t.restriction_for(gate.USAGE_COMMERCIAL))


class GateTest(unittest.TestCase):
    def test_commercially_restricted_entry_is_withheld_with_the_term_named(self):
        e = _entry(
            "xtts-v2",
            permitted=(gate.USAGE_RESEARCH,),
            restrictions=(gate.Restriction("non-commercial", (gate.USAGE_COMMERCIAL,), "CPML"),),
        )
        d = gate.evaluate(e, gate.USAGE_COMMERCIAL, _host())
        self.assertFalse(d.allowed)
        self.assertEqual(d.kind, gate.REASON_USAGE_TERMS)
        self.assertEqual(d.term, "non-commercial")
        self.assertEqual(d.reference, "CPML")

    def test_accelerator_only_entry_on_a_processor_host_is_unsupported_not_poor(self):
        # FR-055: "no available option supports this host's configuration at
        # all" is a DIFFERENT reason from "the host lacks resources" — adding
        # RAM does not conjure an accelerator.
        e = _entry("needs-gpu", requires_accelerator=True)
        d = gate.evaluate(e, gate.USAGE_COMMERCIAL, _host(accel=False))
        self.assertFalse(d.allowed)
        self.assertEqual(d.kind, gate.REASON_UNSUPPORTED_CONFIGURATION)

    def test_memory_and_disk_are_independent_axes(self):
        big_mem = _entry("big-mem", memory=1 << 40, storage=1)
        big_disk = _entry("big-disk", memory=1, storage=1 << 40)
        for e in (big_mem, big_disk):
            d = gate.evaluate(e, gate.USAGE_COMMERCIAL, _host())
            self.assertFalse(d.allowed, e.model_id)
            self.assertEqual(d.kind, gate.REASON_INSUFFICIENT_RESOURCES, e.model_id)

    def test_a_fitting_permitted_entry_is_allowed(self):
        d = gate.evaluate(_entry(), gate.USAGE_COMMERCIAL, _host())
        self.assertTrue(d.allowed)


class SelectionTest(unittest.TestCase):
    def test_unmeasured_host_refuses_to_choose_rather_than_pick_a_default(self):
        # FR-056: no fixed default model when measurement is unavailable.
        with self.assertRaises(gate.CannotChoose) as ctx:
            gate.select([_entry()], gate.FAMILY_TEXT_TO_SPEECH, gate.USAGE_COMMERCIAL, _host(measured=False))
        self.assertEqual(ctx.exception.kind, gate.REASON_HOST_UNMEASURED)

    def test_selection_prefers_the_cheapest_entry_that_actually_runs(self):
        cheap = _entry("cheap", memory=100)
        dear = _entry("dear", memory=900)
        sel = gate.select([dear, cheap], gate.FAMILY_TEXT_TO_SPEECH, gate.USAGE_COMMERCIAL, _host(sys_free=1000, disk_free=1000))
        self.assertEqual(sel.entry.model_id, "cheap")

    def test_commercial_use_never_selects_a_non_commercial_model(self):
        restricted = _entry(
            "restricted",
            memory=1,  # cheapest by far — it must still lose
            permitted=(gate.USAGE_RESEARCH,),
            restrictions=(gate.Restriction("non-commercial", (gate.USAGE_COMMERCIAL,), "CPML"),),
        )
        safe = _entry("safe", memory=999)
        sel = gate.select([restricted, safe], gate.FAMILY_TEXT_TO_SPEECH, gate.USAGE_COMMERCIAL, _host(sys_free=10000, disk_free=10000))
        self.assertEqual(sel.entry.model_id, "safe")
        self.assertIn("restricted", [w.entry_key for w in sel.withheld])

    def test_all_excluded_by_terms_reports_that_reason_specifically(self):
        restricted = _entry(
            "restricted",
            permitted=(gate.USAGE_RESEARCH,),
            restrictions=(gate.Restriction("non-commercial", (gate.USAGE_COMMERCIAL,), "CPML"),),
        )
        with self.assertRaises(gate.CannotChoose) as ctx:
            gate.select([restricted], gate.FAMILY_TEXT_TO_SPEECH, gate.USAGE_COMMERCIAL, _host())
        self.assertEqual(ctx.exception.kind, gate.REASON_USAGE_TERMS)
        self.assertIn("non-commercial", ctx.exception.detail)

    def test_family_isolation_classification_is_not_generation(self):
        # audio-classification and audio-generation are deliberately separate
        # families. Asking for one must never be answered from the other.
        clf = _entry("clf", family=gate.FAMILY_AUDIO_CLASSIFICATION)
        with self.assertRaises(gate.CannotChoose) as ctx:
            gate.select([clf], gate.FAMILY_AUDIO_GENERATION, gate.USAGE_COMMERCIAL, _host())
        self.assertEqual(ctx.exception.kind, gate.REASON_UNSUPPORTED_CONFIGURATION)


class LoaderTest(unittest.TestCase):
    def test_loads_the_repo_speech_audio_catalogue_and_finds_the_stt_entries(self):
        path = os.path.join(
            os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
            "internal", "catalogue", "data", "speech_audio.yaml",
        )
        if not os.path.exists(path):
            self.skipTest(f"SKIP-REASON: repo catalogue not present at {path}")
        entries = gate.load_catalogue(path)
        stt = [e for e in entries if e.family == gate.FAMILY_SPEECH_TO_TEXT]
        self.assertGreaterEqual(len(stt), 1, "the catalogue must carry speech-to-text entries")
        for e in stt:
            self.assertGreater(e.memory_required_bytes, 0)
            self.assertGreater(e.storage_required_bytes, 0)
            self.assertTrue(e.usage_terms.permitted)

    def test_a_zero_resource_figure_is_refused_not_treated_as_small(self):
        # An ABSENT measurement is not a small number (the catalogue's own
        # rule 3). A record that reaches the loader with a zero figure is a
        # defect, and must refuse rather than admit an entry that will be
        # compared against free memory as though it needed none.
        with self.assertRaises(gate.CatalogueError):
            gate.entry_from_mapping(
                {
                    "model_id": "bad",
                    "family": gate.FAMILY_TEXT_TO_SPEECH,
                    "memory_required_bytes": 0,
                    "storage_required_bytes": 10,
                    "usage_terms": {"license_id": "MIT", "permitted": ["commercial"]},
                },
                source_path="<test>",
            )

    def test_unknown_family_is_refused(self):
        with self.assertRaises(gate.CatalogueError):
            gate.entry_from_mapping(
                {
                    "model_id": "bad",
                    "family": "audio-telepathy",
                    "memory_required_bytes": 1,
                    "storage_required_bytes": 1,
                    "usage_terms": {"license_id": "MIT", "permitted": ["commercial"]},
                },
                source_path="<test>",
            )


class WaveformGuardTest(unittest.TestCase):
    """The anti-simulation guard shared by the two synthesising services.

    A fabricated 'proof' of speech synthesis is overwhelmingly likely to be a
    constant buffer — silence, or a DC value. A real utterance is neither. The
    guard is therefore not decoration: it is the single check that a returned
    waveform came from an engine rather than from `bytes(n)`.
    """

    def test_empty_waveform_is_rejected(self):
        with self.assertRaises(gate.SimulationSuspected):
            gate.assert_real_waveform([], sample_rate=24000)

    def test_all_zero_waveform_is_rejected(self):
        with self.assertRaises(gate.SimulationSuspected):
            gate.assert_real_waveform([0.0] * 24000, sample_rate=24000)

    def test_constant_non_zero_waveform_is_rejected(self):
        with self.assertRaises(gate.SimulationSuspected):
            gate.assert_real_waveform([0.5] * 24000, sample_rate=24000)

    def test_a_varying_waveform_of_real_duration_passes(self):
        import math

        sr = 24000
        wave = [0.4 * math.sin(2 * math.pi * 220 * t / sr) for t in range(sr)]
        stats = gate.assert_real_waveform(wave, sample_rate=sr)
        self.assertGreater(stats["rms"], 0.0)
        self.assertGreater(stats["distinct_ratio"], 0.0)
        self.assertAlmostEqual(stats["duration_seconds"], 1.0, places=3)

    def test_a_waveform_too_short_to_be_speech_is_rejected(self):
        with self.assertRaises(gate.SimulationSuspected):
            gate.assert_real_waveform([0.1, -0.1, 0.2], sample_rate=24000)


class ScoreGuardTest(unittest.TestCase):
    """The anti-simulation guard for the classifying service."""

    def test_score_vector_must_match_the_loaded_class_map(self):
        with self.assertRaises(gate.SimulationSuspected):
            gate.assert_real_scores([0.1, 0.9], class_count=521)

    def test_uniform_scores_are_rejected(self):
        n = 521
        with self.assertRaises(gate.SimulationSuspected):
            gate.assert_real_scores([1.0 / n] * n, class_count=n)

    def test_all_zero_scores_are_rejected(self):
        with self.assertRaises(gate.SimulationSuspected):
            gate.assert_real_scores([0.0] * 521, class_count=521)

    def test_a_real_looking_score_vector_passes(self):
        import random

        rng = random.Random(7)
        scores = [rng.random() for _ in range(521)]
        stats = gate.assert_real_scores(scores, class_count=521)
        self.assertEqual(stats["class_count"], 521)
        self.assertGreater(stats["spread"], 0.0)


if __name__ == "__main__":
    unittest.main(verbosity=2)
