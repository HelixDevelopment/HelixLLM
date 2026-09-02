# Run `20260902T185724Z` — T092 test-type sweep + T093 runtime evidence

Host `anton`, 2026-09-02 18:57Z–19:07Z. Git HEAD `4f7deae` throughout.
This was a **measurement** exercise. Nothing was fixed. Nothing was committed.

`RESULTS.md` holds the per-target table for T092. This file explains each captured
artefact: what it proves, and — the part that matters — what it does not.

---

## Read this first: the tree moved under the sweep

Another writer was editing this same checkout while the sweep ran. Between 20:57 and
21:06 local, **13 source files changed**, including `internal/catalogue/entry.go`,
`internal/selection/family.go`, `internal/brain/brain.go`, two new catalogue data files
and three new test files. `evidence/tree-drift-during-sweep.txt` lists them with mtimes.

The visible symptom: `TestShippedCatalogueLoads` printed *31 entries across 6 families*
at 18:58:52Z and *34 entries across 8 families* at 19:05:28Z — same command, same HEAD,
no action of mine in between. The cause is `internal/catalogue/data/{embedding,vector}.yaml`
appearing at 21:02 and 21:03 local.

**Consequence for every number in this run:** results dated before ~19:02Z were measured
against a slightly different tree than results after. To bound this, a confirming re-run
of `test-unit` + `test-integration` was taken at 19:06Z with a tree fingerprint recorded
before and after (`evidence/confirming-rerun.txt`); the fingerprint was **identical**
across that re-run, so that reading at least is internally consistent. The first
`test-unit` reading had 1645 passing tests, the second 1650 — the drift added five, all
passing.

This does not invalidate the sweep. It does mean nobody should quote it as "the state of
HEAD" — it is the state of a working tree that was in motion.

---

## T093 evidence artefacts

Everything below is a captured run on this machine. Each file records the exact command,
the host, a UTC timestamp, and the process exit code. **No configuration file is
presented as evidence anywhere in this directory** — where a config value matters it is
shown as the output of a program that read it, not as the file itself.

### `evidence/measured-host-profile.txt`
**Proves:** the capability layer measures this machine and produces the real numbers.
`host="anton" arch=amd64 cores=8/16 mem=16384622592/32478781440 storage=1248688152576 accel=1`,
and the accelerator resolves to `identity="GPU-18c33a3a-…" model="NVIDIA GeForce RTX 3060"
api=cuda total=12884901888 available=12320768000`, state `measured`, no gaps. An
independent `nvidia-smi` reading is captured in the same file (12288 MiB total,
11750 MiB free) and agrees — 12884901888 B is exactly 12288 MiB.
**Does not prove:** that anything was *served* on that GPU. No model was loaded, no
inference ran, no VRAM was allocated. It proves measurement, not use. It also says
nothing about the accuracy of the *storage* figure beyond it being non-zero.

### `evidence/videogen-boot-plan.txt`
**Proves:** `go run ./cmd/videogen-boot plan` works end to end. It prints the measured
host, withholds `ltx-video-13b:gguf-q4` naming the restricting licence term
(`revenue-cap`, ref `LTX-Video-Open-Weights-License-0.X §2`), and chooses
`wan2.2-ti2v-5b:fp8-480p` with the arithmetic shown (requires 10240 MiB, leaves 5216 MiB
= 16.8%). Exit 0 in 1 s.
**Does not prove:** that the chosen model runs. The command says so itself —
`PLAN-OK: … (nothing was booted)`. No weights were fetched, no container started, no
frame generated. The 3.06 s clip shape is the shape selection was performed *at*, not a
clip that exists.

### `evidence/imagegen-boot-plan.txt`
**Proves:** the same for the image family, and it is the richest refusal sample —
**four** licence refusals (SDXL-Turbo, SD 3.5 Medium, SD 3.5 Large, FLUX.1-dev) plus
**two resource refusals** with real arithmetic (`flux.2-dev` short by 6682 MiB;
`qwen-image` short by 3821 MiB, both against 11437 MiB available after a 4646 MiB
headroom hold-back), then chooses `flux.1-schnell:nvfp4`. Exit 0.
**Does not prove:** anything was rendered. Again `nothing was booted`.

### `evidence/visiongen-boot-plan.txt`
**Proves:** a *correct refusal*, which is the point of capturing it. The host is measured,
6 vision models are found servable, one is withheld on licence
(`minicpm-model-license`, research-only), and then the command refuses to choose at all
with exit status 24 because none of the weights are present in
`/home/milosvasic/models/vlm_cache`. It names the remedy and states the reason it will
not fall back: *"booting some other file that happens to be in that directory would be a
model nobody chose."*
**Does not prove:** that it would succeed if the weights were there. That path is
untested on this host.

### The three refusal kinds — all three reached

| Kind | Where captured | Sample |
|---|---|---|
| `excluded_by_usage_terms` (licence) | all three plan files | `flux.1-dev:nvfp4` — "flatly non-commercial, no revenue threshold" |
| `insufficient_resources` (host too small) | `imagegen-boot-plan.txt` | `flux.2-dev:gguf-q4_k` — "memory short by 6682MiB" |
| weights absent → refuse to choose (exit 24) | `visiongen-boot-plan.txt` | all 6 vision options, `no such file or directory` |

**What this does not show:** these are the three kinds *reachable on this host*. It is not
a proof that the selector has exactly three refusal kinds, nor that every kind is
correctly implemented — only that these three fired, with a stated cause each.

### `evidence/shipped-catalogue-loads.txt` and `evidence/catalogue-family-coverage.txt`
**Proves:** the shipped catalogue parses at runtime and the per-family commercial
coverage map is produced from it — text, vision, image-generation, video-generation,
speech-to-text, text-to-speech, embedding, vector all carry at least one
commercially-usable option, enumerated by name.
**Does not prove:** that any listed weight exists, downloads, or runs. This is a
parse-and-classify result over declared metadata. `visiongen-boot` refusing for want of
weights is the counter-example: catalogue coverage and on-disk reality are different
things. Also note the two readings differ (31/6 → 34/8) because of the tree drift above.

### `evidence/catalogue-count-determinism-probe.txt`
**Proves:** the 31→34 change was *not* test-order or filter dependent — four runs under
three different `-run` filters all report 34/8 once the new data files existed. It was
the tree changing, not a flaky test.

### `evidence/unit-coverage-total.txt`
**Proves:** unit statement coverage is 83.4–83.5% and the repo's own gate demands 91%, so
`make coverage` exits 2.
**Does not prove:** anything about coverage *quality*. 83% of statements executed is not
83% of behaviour verified — see the bluff findings, where executed-and-asserted-nothing
is exactly the failure mode.

---

## The challenge-runner probes (this run's main finding)

### `evidence/challenge-runner-probe.txt`
**Proves, by differential runs:**
- nothing is listening on `:8443` (`ss` + `curl`, captured in the same file);
- `--banks-dir=challenges/banks/security/` → "3 passed, 0 failed, 0 skipped", exit 0, 0 s;
- the **same "3 passed"** with `--base-url=https://192.0.2.1:9/` — unroutable TEST-NET-1.
  A pass that is unchanged by pointing the system under test at a black hole is not
  testing the system under test;
- an empty directory → "0 passed, 0 failed, 0 skipped", exit 0 (a green result);
- a missing directory → error, exit 1 (so the loader *is* reading the path).

### `evidence/challenge-schema-probe.txt`
**Proves:** a census of all 28 bank YAML files — 9 use the top-level `challenges:` key,
**19 use `steps:`**. Isolating one file of each schema: `scanning.yaml` (`challenges:`)
yields 3 "passed"; `owasp.yaml` (`steps:`, 10 declared entries) yields 0, silently, exit 0.

### Root cause, read from the source
`internal/testing/runner.go`:
- `Bank` has `Challenges []Challenge \`yaml:"challenges"\`` — no `steps` field, so the 19
  `steps:`-schema files parse to an empty bank with no diagnostic;
- `ChallengeStep` has `Action string \`yaml:"action"\`` and `Expected` — the banks write
  `method:`/`path:`/`assertions:`, so `Action` is always empty;
- `runStep` switches on `Action` having a `GET `/`POST ` prefix and otherwise returns
  `Status: "skipped"`;
- `runChallenge` returns early only on a `"failed"` step, then: `if result.Status == "" {
  result.Status = "passed" }` — **a challenge whose every step was skipped is reported
  passed**;
- `cmd/helixllm/challenges.go` counts challenge-level status only (step-level skips are
  invisible in the summary) and returns 0 unless `failed > 0`.

So `make test-security`, `test-stress`, `test-chaos` and `test-benchmark` each print a
green line, in zero seconds, having issued zero HTTP requests, on a host with no server.
**What this does not show:** whether the banks' assertions are themselves any good. They
have never executed, so their quality is unknown — this finding is about the harness, and
it means the bank content is entirely unverified, not that it is wrong.

---

## Deviations from a pure read-only run, disclosed

1. **`make build` was run** after the six challenge targets had already failed with
   exit 127. Without it the only finding available would have been "the binary is
   missing", which hides everything above. It writes `bin/helixllm`; `bin/` is gitignored.
   Both readings — before and after the build — are reported separately in `RESULTS.md`.
2. **`coverage-unit.out` was overwritten** by `test-unit`/`coverage` (it is an untracked,
   gitignored artefact). It was checksummed beforehand and **restored** afterwards;
   md5 `88055478710db6f225e08964cb47c7e8` before and after.
3. Nothing else outside this directory was written. No code, test or Makefile was
   modified. No commit, no push.

---

## What remains unproven on this host

- **That any model serves a request.** No weights, no container, no inference, in any
  family. Every boot command was run in `plan` mode and said so.
- **Everything the challenge banks claim to cover** — OWASP rejection behaviour, chaos
  resilience, latency/throughput SLOs, RAG ingestion and retrieval, API compatibility,
  the four developer workflows. 19 of 28 banks have never been parsed, and the 9 that
  parse have never issued a request.
- **The HTTP surface as a whole:** health, metrics, `/v1/chat/completions`, auth
  rejection. Every target that touches it either failed on connection-refused or passed
  without connecting.
- **`tests/e2e/`, `tests/benchmark/`, `tests/security/`** — no Makefile target compiles
  them; they did not run in any form during this sweep.
- **The four opt-in integration tests** (qdrant ingest, rerank pipeline, verifier scorer
  bridge, fallback chain) — they skip with a stated reason and an env var to enable. The
  skips are honest; the behaviour behind them is untested here.
- **macOS/Darwin measurement paths** — `measure_darwin*.go` cannot execute on this Linux
  host, and was one of the files being edited during the sweep.
