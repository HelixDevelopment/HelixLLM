"""Tests for the text-to-speech service (T080).

Text-to-speech IS greenfield here — unlike speech-to-text, no engine existed.
So these tests carry two burdens the STT suite does not:

  1. That the licence gate genuinely governs what is offered. The catalogue
     records that several of the strongest TTS models cannot be used
     commercially at all, and `xtts-v2` — the one text-to-speech entry that
     loads in the repo catalogue today — is one of them. A service whose only
     default were a model a commercial user may not use would be broken for
     its principal caller. These tests pin that it is not.

  2. That the audio returned is REAL. The proof-of-inference test runs the
     actual ONNX engine over real weights, and SKIPS with a stated reason when
     those weights are not on this host. It never synthesises a waveform to go
     green, and the anti-simulation guard it asserts through would reject one
     if it did.
"""

import os
import struct
import sys
import unittest
import wave

HERE = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.dirname(os.path.dirname(HERE))
sys.path.insert(0, HERE)
sys.path.insert(0, os.path.join(REPO_ROOT, "container"))

import helix_model_gate as gate  # noqa: E402
import tts_engine  # noqa: E402

SERVICE_CATALOGUE = os.path.join(HERE, "models.yaml")
REPO_CATALOGUE = os.path.join(
    REPO_ROOT, "internal", "catalogue", "data", "speech_audio.yaml"
)


def _host(**kw):
    base = dict(
        measured=True,
        has_accelerator=False,
        accelerator_free_bytes=0,
        system_free_bytes=16 << 30,
        free_disk_bytes=200 << 30,
    )
    base.update(kw)
    return gate.HostMeasurement(**base)


class ServiceCatalogueTest(unittest.TestCase):
    def setUp(self):
        self.entries = gate.load_catalogue(SERVICE_CATALOGUE)

    def test_the_service_catalogue_loads_and_is_all_text_to_speech(self):
        self.assertTrue(self.entries)
        for e in self.entries:
            self.assertEqual(e.family, gate.FAMILY_TEXT_TO_SPEECH, e.key)

    def test_every_entry_carries_a_licence_identifier(self):
        for e in self.entries:
            self.assertTrue(e.usage_terms.license_id, e.key)

    def test_at_least_one_entry_is_commercially_usable_on_a_processor(self):
        # The point of the whole exercise: a commercial caller on a
        # no-accelerator host must receive a real option, never an empty set.
        usable = [
            e
            for e in self.entries
            if e.usage_terms.permits(gate.USAGE_COMMERCIAL) and not e.requires_accelerator
        ]
        self.assertTrue(usable, "no commercially-usable processor option is catalogued")


class CommercialDefaultTest(unittest.TestCase):
    """The default a commercial caller gets must be commercially usable."""

    def setUp(self):
        self.entries = gate.load_catalogue(SERVICE_CATALOGUE)

    def test_commercial_selection_is_permissively_licensed(self):
        sel = gate.select(
            self.entries, gate.FAMILY_TEXT_TO_SPEECH, gate.USAGE_COMMERCIAL, _host()
        )
        self.assertTrue(sel.entry.usage_terms.permits(gate.USAGE_COMMERCIAL))
        self.assertIsNone(sel.entry.usage_terms.restriction_for(gate.USAGE_COMMERCIAL))

    def test_a_non_commercial_model_is_never_the_commercial_default(self):
        sel = gate.select(
            self.entries, gate.FAMILY_TEXT_TO_SPEECH, gate.USAGE_COMMERCIAL, _host()
        )
        restricted = [
            e.key
            for e in self.entries
            if e.usage_terms.restriction_for(gate.USAGE_COMMERCIAL) is not None
        ]
        self.assertNotIn(sel.entry.key, restricted)

    def test_the_restricted_entry_is_withheld_with_its_term_named(self):
        # FR-054: shown as unavailable with the restricting term named — never
        # silently offered and left for the user to discover the restriction.
        sel = gate.select(
            self.entries, gate.FAMILY_TEXT_TO_SPEECH, gate.USAGE_COMMERCIAL, _host()
        )
        restricted = [
            e for e in self.entries
            if e.usage_terms.restriction_for(gate.USAGE_COMMERCIAL) is not None
        ]
        if not restricted:
            self.skipTest("SKIP-REASON: no commercially-restricted entry is catalogued")
        for e in restricted:
            w = next((w for w in sel.withheld if w.entry_key == e.key), None)
            self.assertIsNotNone(w, f"{e.key} was neither offered nor explained")
            self.assertEqual(w.kind, gate.REASON_USAGE_TERMS)
            self.assertTrue(w.term)

    def test_a_grant_that_is_contradicted_by_a_restriction_never_wins(self):
        # This is the test that makes the whole suite depend on the licence
        # gate, and it was added because a paired mutation proved the earlier
        # tests did not. Disabling the gate entirely left them all green: the
        # catalogued non-commercial entry is ALSO accelerator-only and ALSO
        # absent from the commercial grant list, so two unrelated checks were
        # quietly doing the gate's job.
        #
        # The trap below can be withheld by the exclusionary-restriction check
        # and by nothing else. It runs on a processor, it is by far the
        # cheapest entry so ranking would pick it, and its licence LISTS
        # commercial use as permitted while also carrying a term excluding it —
        # the real CPML shape, which contemplates a commercial licence that in
        # practice cannot be obtained. If the gate stops reading the
        # restriction, this entry wins the selection and a commercial caller is
        # served a model they may not use.
        trap = gate.CatalogueEntry(
            model_id="grant-contradicted-by-restriction",
            family=gate.FAMILY_TEXT_TO_SPEECH,
            usage_terms=gate.UsageTerms(
                license_id="CPML-shaped",
                permitted=(gate.USAGE_COMMERCIAL, gate.USAGE_RESEARCH),
                restrictions=(
                    gate.Restriction("non-commercial", (gate.USAGE_COMMERCIAL,), "CPML"),
                ),
            ),
            requires_accelerator=False,
            memory_required_bytes=1,
            storage_required_bytes=1,
        )
        sel = gate.select(
            self.entries + [trap],
            gate.FAMILY_TEXT_TO_SPEECH,
            gate.USAGE_COMMERCIAL,
            _host(),
        )
        self.assertNotEqual(sel.entry.model_id, trap.model_id)
        withheld = next((w for w in sel.withheld if w.entry_key == trap.key), None)
        self.assertIsNotNone(withheld, "the trap was neither offered nor explained")
        self.assertEqual(withheld.kind, gate.REASON_USAGE_TERMS)
        self.assertEqual(withheld.term, "non-commercial")

    def test_a_research_caller_may_receive_what_a_commercial_caller_may_not(self):
        # The gate withholds by PURPOSE, not by blanket removal — a research
        # user is entitled to the models a commercial user is not.
        commercial = {
            e.key for e in self.entries if e.usage_terms.permits(gate.USAGE_COMMERCIAL)
        }
        research = {
            e.key for e in self.entries if e.usage_terms.permits(gate.USAGE_RESEARCH)
        }
        self.assertTrue(research >= commercial or research - commercial)


class RepoCataloguePairingTest(unittest.TestCase):
    """The repo catalogue alone cannot serve a commercial TTS caller today.

    This is a finding, not a defect of this service: the only text-to-speech
    entry that currently LOADS in `internal/catalogue/data/speech_audio.yaml`
    is `xtts-v2`, whose CPML licence excludes commercial use. The service
    catalogue is what supplies a commercially-usable option. Pinning it here
    means the day the repo catalogue gains a permissive TTS entry, this test
    tells us rather than the gap closing silently.
    """

    def test_repo_catalogue_alone_yields_no_commercial_tts_offer(self):
        if not os.path.exists(REPO_CATALOGUE):
            self.skipTest(f"SKIP-REASON: repo catalogue absent at {REPO_CATALOGUE}")
        entries = gate.load_catalogue(REPO_CATALOGUE)
        tts = [e for e in entries if e.family == gate.FAMILY_TEXT_TO_SPEECH]
        if not tts:
            self.skipTest("SKIP-REASON: repo catalogue loads no text-to-speech entry")
        commercial = [e for e in tts if e.usage_terms.permits(gate.USAGE_COMMERCIAL)]
        if commercial:
            self.skipTest(
                "SKIP-REASON: the repo catalogue now carries a commercially-usable "
                f"text-to-speech entry ({[e.key for e in commercial]}); this test "
                "existed to record that it did not, and the finding is now stale."
            )
        with self.assertRaises(gate.CannotChoose) as ctx:
            gate.select(entries, gate.FAMILY_TEXT_TO_SPEECH, gate.USAGE_COMMERCIAL, _host())
        self.assertEqual(ctx.exception.kind, gate.REASON_USAGE_TERMS)

    def test_union_of_both_catalogues_serves_a_commercial_caller(self):
        if not os.path.exists(REPO_CATALOGUE):
            self.skipTest(f"SKIP-REASON: repo catalogue absent at {REPO_CATALOGUE}")
        entries = gate.load_catalogue(SERVICE_CATALOGUE, REPO_CATALOGUE)
        sel = gate.select(
            entries, gate.FAMILY_TEXT_TO_SPEECH, gate.USAGE_COMMERCIAL, _host()
        )
        self.assertTrue(sel.entry.usage_terms.permits(gate.USAGE_COMMERCIAL))


class EngineResolutionTest(unittest.TestCase):
    def setUp(self):
        self.entries = gate.load_catalogue(SERVICE_CATALOGUE)

    def test_every_catalogued_entry_resolves_or_is_refused_by_name(self):
        for e in self.entries:
            with self.subTest(entry=e.key):
                try:
                    plan = tts_engine.resolve(e)
                except tts_engine.EngineUnsupported as exc:
                    self.assertIn(e.model_id, str(exc))
                else:
                    self.assertTrue(plan.engine)
                    self.assertGreater(plan.sample_rate, 0)

    def test_the_commercial_default_is_one_this_service_can_actually_run(self):
        plan = tts_engine.plan_for_host(self.entries, _host(), gate.USAGE_COMMERCIAL)
        self.assertTrue(plan.engine)
        self.assertEqual(plan.purpose, gate.USAGE_COMMERCIAL)

    def test_unmeasured_host_refuses_rather_than_defaulting(self):
        with self.assertRaises(gate.CannotChoose) as ctx:
            tts_engine.plan_for_host(
                self.entries, _host(measured=False), gate.USAGE_COMMERCIAL
            )
        self.assertEqual(ctx.exception.kind, gate.REASON_HOST_UNMEASURED)


class WavEncodingTest(unittest.TestCase):
    def test_encoded_wav_round_trips_to_the_same_duration_and_rate(self):
        import math

        sr = 24000
        samples = [0.3 * math.sin(2 * math.pi * 200 * t / sr) for t in range(sr // 2)]
        blob = tts_engine.encode_wav(samples, sr)
        with wave.open(__import__("io").BytesIO(blob)) as w:
            self.assertEqual(w.getframerate(), sr)
            self.assertEqual(w.getnchannels(), 1)
            self.assertEqual(w.getsampwidth(), 2)
            frames = w.readframes(w.getnframes())
        self.assertEqual(len(frames) // 2, len(samples))
        decoded = struct.unpack(f"<{len(samples)}h", frames)
        self.assertGreater(max(abs(v) for v in decoded), 0)

    def test_encoding_refuses_a_simulated_waveform(self):
        # The guard sits BEFORE encoding, so a fabricated buffer cannot even
        # become a WAV file, let alone reach a caller.
        with self.assertRaises(gate.SimulationSuspected):
            tts_engine.encode_wav([0.0] * 24000, 24000)


class RealInferenceTest(unittest.TestCase):
    """Runs the ACTUAL engine over REAL weights, or skips saying why.

    There is no third option here. This test does not construct audio, does
    not stub the engine, and does not assert on anything it did not obtain
    from a real forward pass.
    """

    def setUp(self):
        try:
            import kokoro_onnx  # noqa: F401
        except Exception as exc:
            self.skipTest(
                f"SKIP-REASON: the kokoro-onnx runtime is not installed on this host ({exc}). "
                "It ships in the service container; install the service requirements or run "
                "this suite inside the built image to exercise real inference."
            )
        self.weights = tts_engine.weight_paths()
        missing = [p for p in self.weights.values() if not os.path.exists(p)]
        if missing:
            self.skipTest(
                "SKIP-REASON: model weights are not present on this host: "
                f"{missing}. Obtain them via the .gitignore-meta regeneration "
                "manifest; this test will not fabricate audio in their absence."
            )

    def test_synthesis_produces_real_varying_audio(self):
        engine = tts_engine.load_engine()
        samples, sr = tts_engine.synthesise(engine, "The quick brown fox.", voice=None)
        stats = gate.assert_real_waveform(samples, sr)
        self.assertGreater(stats["duration_seconds"], 0.2)
        self.assertGreater(stats["rms"], 0.0)

    def test_different_text_produces_different_audio(self):
        # A hardcoded response would return the same bytes for any input. This
        # is the cheapest check that the text actually reached the model.
        engine = tts_engine.load_engine()
        a, sr_a = tts_engine.synthesise(engine, "One.", voice=None)
        b, sr_b = tts_engine.synthesise(engine, "A far longer sentence than the first.", voice=None)
        self.assertEqual(sr_a, sr_b)
        self.assertNotEqual(len(a), len(b))


if __name__ == "__main__":
    unittest.main(verbosity=2)
