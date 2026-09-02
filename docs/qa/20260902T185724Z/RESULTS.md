# T092 — mandated test-type sweep, per-target result

Run id: `20260902T185724Z` · host `anton` (16 cores, 32 GiB RAM, RTX 3060 12 GiB, cuda)
Git HEAD throughout: `4f7deae845f88c2c14ba4da3a02928f3cd66ef56` · go1.26.1 linux/amd64
Window: 2026-09-02 18:57Z – 19:07Z

**Read the drift caveat in `README.md` before quoting any number here.** The working
tree was being edited by another writer during the sweep; 13 source files changed
under it. Every row is a real run, but rows taken before 19:02Z were taken against a
slightly different tree than rows taken after.

## The table

| Target | Ran? | Passed? | Duration | Reason / what actually happened |
|---|---|---|---|---|
| `test-unit` | yes | **PASS** | 18s (1st) / 9s (2nd) | 38 pkgs ok, 0 pkg fail. 1st reading 1645 PASS / 2 SKIP / 0 FAIL; confirming re-run 1650 PASS / 2 SKIP / 0 FAIL. Genuine. |
| `test-integration` | yes | **PASS** | 10s (1st) / 3s (2nd) | 35 PASS / 4 SKIP / 0 FAIL. All 4 skips are marked opt-in live tests (qdrant/tei/verifier), i.e. honest skips. |
| `test-e2e` | yes | **PASS** | 3s | 35 PASS / 4 SKIP — **identical to `test-integration`**. The target runs `-tags=e2e ./tests/integration/...`. `tests/e2e/` (8 files, 6 carrying `//go:build e2e`) is referenced by **no** Makefile target and did not run. |
| `test-race` | yes | **PASS** | 123s | 45 packages ok across `./internal/... ./pkg/... ./tests/...`, `-p 1`. No race reported. Genuine. |
| `test-stress` | 1st: **no** · after build: yes | **exit 0 — vacuous** | 0s / 0s | 1st: exit 2, `./bin/helixllm` absent (err 127) — `bin/` is gitignored and no target builds it. After `make build`: "3 passed, 0 failed, 0 skipped" in **0 s against a server that is not running**. See bluff finding B1. |
| `test-chaos` | same | **exit 0 — vacuous** | 0s | Same as above; 3 "passed" = the 3 entries of `chaos/{provider_failure,redis_failure}.yaml`. No request was issued. |
| `test-security` | same | **exit 0 — vacuous** | 0s | Same; 3 "passed" = the 3 entries of `security/scanning.yaml`. `security/owasp.yaml` (10 steps) contributes **0**. |
| `test-benchmark` | same | **exit 0 — vacuous** | 0s | Same bank as `test-stress` (`benchmarks/`); 3 "passed", no measurement taken. |
| `test-usecases` | 1st: **no** · after build: yes | **exit 0 on an empty set** | 0s | "0 passed, 0 failed, 0 skipped". All 4 `workflows/*.yaml` use the `steps:` schema the loader cannot read; 29 declared entries loaded as zero. Exit 0. |
| `test-challenges` | 1st: **no** · after build: yes | **exit 0 on an empty set** | 0s | "0 passed, 0 failed, 0 skipped". `--banks-dir=challenges/banks/` is the tree *root*; the loader is non-recursive, so it finds no YAML at all and exits 0. |
| `test-monitoring` | yes | **FAIL** | 0s | 3/3 fail: `dial tcp 127.0.0.1:8443: connect: connection refused`. **Infrastructure absent: no HelixLLM server running.** This is the one HTTP target that fails honestly. |
| `test-performance` | yes | **FAIL** | 6s | 1 of 3 fails (`HealthThroughput`: `0 req/s, want >= 100`). The other 2 **"PASS" on zero samples** — `p50=0s p95=0s p99=0s (n=0)`. Same missing server. See bluff finding B2. |
| `test-automation` | yes | **exit 0 — but see reason** | 15s | Pipeline = `build` + `test-unit` + `test-integration` + `test-challenges`. Green overall, yet its final stage printed "0 passed, 0 failed, 0 skipped". The "full automation pipeline" validates nothing at the challenge layer. |
| `test-all` | yes | **PASS** | 12s | `test-unit` + `test-integration` only. Does not include e2e, race, security, chaos, stress, performance, monitoring or challenges despite the name. |

### Targets outside the brief, run because the sweep reached them

| Target | Ran? | Passed? | Duration | Reason |
|---|---|---|---|---|
| `build` | yes | PASS | 7s | Needed: six targets above hard-depend on `./bin/helixllm` and nothing builds it. |
| `test-stress-go` | yes | **FAIL** | 6s | 3 PASS / 1 FAIL. `TestStress_HealthEndpointUnderLoad`: `Success=0, Failure=618585 over 5s` — missing server. |
| `test-benchmark-go` | yes | PASS | 102s | `go test -bench=. -benchmem -count=3 ./internal/...`. Real in-process benchmarks; the only benchmark target that measures anything. |
| `coverage` | yes | **FAIL** | 11s | `Total coverage: 83.4% (threshold: 91%)` → exit 2. |

## Tally

- 13 brief targets: **4 genuinely pass** (unit, integration, race, all) · **1 passes but runs the wrong package** (e2e) · **2 fail honestly** (monitoring, performance) · **6 report success without exercising anything** (stress, chaos, security, benchmark, usecases, challenges) · and `test-automation` is green while containing one of those six.
- Nothing was skipped by me. Every named target was executed.

## Infrastructure genuinely absent on this host

- **No HelixLLM server on `https://localhost:8443`.** Verified independently: `ss -ltnp` shows nothing listening on :8443, `curl` refuses. This is the direct cause of the `test-monitoring`, `test-performance` and `test-stress-go` failures, and the reason the challenge targets *should* have failed but did not.
- **No VLM weights** at `/home/milosvasic/models/vlm_cache` — `visiongen-boot plan` refuses because of it (a correct refusal, captured as evidence).
- Not attempted, so not claimed either way: podman/qdrant/tei containers for the 4 opt-in integration skips, and a live LLMsVerifier for the scorer-bridge skip.

## Bluff findings (detail in `README.md` and the evidence files)

- **B1 — the challenge runner reports PASS without executing anything.** `internal/testing/runner.go` dispatches a step on `step.Action` (`"GET /path"`); every bank YAML instead writes `method:`/`path:`/`assertions:`, so `Action` is empty, every step falls to the `default` branch and is marked `skipped`, and `runChallenge` then treats a challenge with no *failed* step as `passed`. Proven at runtime: the same "3 passed" is printed with `--base-url=https://192.0.2.1:9/` (unroutable TEST-NET-1).
- **B1b — 19 of 28 bank files never load.** They use a top-level `steps:` key; the loader's `Bank` struct only has `challenges:`. ~115 declared step entries load as zero, silently, exit 0.
- **B2 — latency assertions pass on an empty sample.** `TestPerformance_{Health,Models}Latency` report `p50=0s p95=0s p99=0s (n=0)` and PASS. Zero measurements is treated as success.
- **B3 — a bank set that loads nothing is a green result.** Empty directory → "0 passed, 0 failed, 0 skipped", exit 0. Only a *missing* directory is an error.

## Test code that no target runs

`tests/e2e/` (8 files), `tests/benchmark/` (1), `tests/security/` (1) are referenced by
no Makefile target. `grep -n 'tests/e2e\|tests/benchmark\|tests/security' Makefile`
returns nothing. `test-security`/`test-benchmark` run challenge banks instead.
