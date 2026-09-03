# Lane B: Second Coder/Agent Instance (`agentgen-boot`), port 18435

New capability — no prior user-facing doc existed under `docs/user-guide/`
or `docs/courses/` before this document. Verified directly against
`cmd/agentgen-boot/main.go`, `cmd/agentgen-boot/compose.agent.yml`, and
`internal/vrambroker/broker.go`, `submodules/helix_llm` HEAD `e2ce163`,
2026-07-11; the model-selection section re-verified against the measured-
selection migration, 2026-09-03.

## What this is

Lane B is a **second, independent** llama.cpp coder/agent instance, served
on its own port (`:18435`), booted **co-resident** with the already-running
resident coder (`:18434`, never touched/restarted by this capability —
§11.4.122). It exists to let a second concurrent coding/agent workload run
on the same GPU as the resident coder, when there is enough free VRAM to
admit it safely.

This is not a load-balancer or a replacement for the resident coder — it is
an additive, independently-addressable llama-server instance with its own
model — decided per host by measurement, see below — its own context window,
and its own admission gate.

## VRAM admission: the vrambroker gate (this is the load-bearing safety mechanism)

Before Lane B's container is ever started, `cmd/agentgen-boot` calls
`vrambroker.Broker.Acquire(ctx, vrambroker.ClassAgent, needBytesFor(chosen))`
(`internal/vrambroker/broker.go`). This is a **real, measured** admission
check against `nvidia-smi`-reported free VRAM — never a raw
"just start the container and hope":

```go
// internal/vrambroker/broker.go
func admit(free, needBytes, headroom int64) bool {
    if needBytes < 0 { return false }
    return free >= needBytes+headroom
}
```

`headroom` is a fixed **2 GiB** safety margin (`vrambroker.HeadroomBytes`)
kept free above every admitted reservation. `ClassAgent` is a **warm tier**
(`IsResident() == false`, `IsBurst() == false`) — semantically identical to
the existing `ClassVLM` warm tier but deliberately kept as its own distinct
`Class` value so a future genuine vision-serving workload can never collide
with Lane B for VRAM accounting purposes (`broker.go` comment, "danger zone
D5").

Possible outcomes of the admission check:

| Outcome | Meaning | Exit code (`agentgen-boot boot`/`admit-check`) |
|---|---|---|
| `ADMIT-OK` | Granted; co-resident boot proceeds, coder untouched | `0` |
| `ErrBudgetExceeded` | Lane B does not fit alongside the live coder right now. The coder-pause path is operator-gated (§11.4.122/§11.4.101) — **this harness never pauses the resident coder autonomously.** | `10` |
| `ErrBurstInUse` | An image/video generation burst currently owns the GPU (single-owner §11.4.119) — queue and retry later | `11` |
| `ErrBudgetUnavailable` | `nvidia-smi` unreadable — refuses fail-closed (§11.4.6), never guesses | `12` |
| `ErrThermalUnsafe` | Card outside safe thermal/power envelope (§11.4.133) — no boot | `13` |

```bash
cd submodules/helix_llm/cmd/agentgen-boot

# Measure this host and report which model it would serve, and why the other
# candidates were not offered. Boots nothing, touches the card not at all:
go run . plan

# Check admission WITHOUT booting anything (lease is acquired then
# immediately released — pure gate test):
go run . admit-check

# Actually boot (admits, then boots on :18435, then health-polls, then
# LEAVES THE SERVICE RUNNING — this is a warm tier, not a one-shot job):
go run . boot compose.agent.yml <project-name>

# Check compose service status:
go run . status compose.agent.yml <project-name>

# Single-owner teardown (the ONLY way this harness stops Lane B — `boot`
# never auto-tears-down):
go run . down compose.agent.yml <project-name>
```

## Model selection and the VRAM footprint you are actually claiming

**Which model runs is measured, not configured.** This harness has no default
model and cannot be told which one to run. Every `plan` / `admit-check` / `boot`
measures this host, joins the measurement against the recorded catalogue
(`internal/catalogue/data/text.yaml`) under your declared usage, and serves an
option the host was proven able to run — or refuses, naming what was short.

Use `plan` to see the answer without touching the card:

```bash
go run . plan
```

### What changed, and why

This lane used to take the model from `AGENTGEN_MODEL_GGUF` and its VRAM claim
from `AGENTGEN_NEED_BYTES`, and those two had to be kept in agreement **by
hand**. Forgetting was silent. Measured on a 12288 MiB card with 11781 MiB free:

```
AGENTGEN_MODEL_GGUF=Qwen3.6-27B-Q4_K_M.gguf   # a 19.5 GiB model
# AGENTGEN_NEED_BYTES left at its 9 GiB default
  -> need=9216MiB   ADMIT-OK          exit=0     # agreed to run it, unchecked
# the same binary, same card, told that model's real figure
  -> need=19968MiB  ErrBudgetExceeded exit=10
```

How much memory a model needs was recorded in two places and only one was true.
It is now recorded once — in the catalogue entry — and read from the entry the
decision actually chose. On the same card the same input now refuses, saying the
27B is short by 7289 MiB, and starts nothing.

### Environment variables

```bash
# INPUT — where artefacts live (gitignored, outside the repo, §11.4.30).
# A LOCATION, never a model: it says where to look, not what to run.
export AGENTGEN_MODEL_DIR=$HOME/models    # auto-defaults to ~/models if unset

# INPUT — how the output will be used, so licence terms can be applied.
# Defaults to the narrowest purpose (commercial) and always reports the default.
export HELIXLLM_DECLARED_USAGE=commercial

# INPUT — options you forbid. This can only ever REMOVE a candidate the
# measurement offered, never introduce one it did not.
export AGENTGEN_FORBID_MODELS=some-model,another-model:variant

# OUTPUT — written by the harness from the decision, for compose to
# interpolate. A value you set here is reported and OVERWRITTEN.
# AGENTGEN_MODEL_GGUF

# NO LONGER HONOURED — a static VRAM figure implied a model. If set, it is
# reported and ignored; the admitted figure comes from the chosen entry.
# AGENTGEN_NEED_BYTES

# Serving/host-safety knobs — these say HOW and WHERE the service runs and
# name no model, so they keep their defaults (§CONST-045/§11.4.28):
export AGENTGEN_CTX_SIZE=16384       # llama-server -c
export AGENTGEN_PARALLEL=4           # llama-server --parallel
export AGENTGEN_MEM_LIMIT=16g        # container mem_limit (§12.3/§12.6 host-safety cap)
export AGENTGEN_SHM_SIZE=2g
```

### Naming a model deliberately

`--pin` is the one legitimate way to name a model, and it is a CONSTRAINT on the
choice rather than a bypass: the host is still measured first, and the pin is
refused — with the insufficient resource named — when this host cannot run it.

```bash
go run . plan --pin qwen3-0.6b:q4_0
```

A pin naming something the catalogue does not record is refused, not started.

### When nothing is offered

The three withheld reasons stay distinct all the way to your terminal, because
each implies a different remedy: `insufficient_resources` (change the host or
pick smaller), `unsupported_configuration` (more memory will not help), and
`excluded_by_usage_terms` (the host could serve it; the licence forbids your
declared usage). Selection is told the admission gate's own 2 GiB margin
(`runtime.SelectionReserve`), so this lane cannot offer a model the broker will
then refuse to start.

Exit codes for the selection stage, distinct from the admission codes above:
`20` host not measured, `21` measurement stale, `22` no option offered, `23`
catalogue unreadable, `24` no offered model's weights are present on this host.

## Container + orchestration details (verified against `compose.agent.yml`)

- Image: `localhost/helixllm/llamacpp-router:cuda12.8-sm120` — **reused**
  from the resident coder and the VLM lane, no `build:` directive, no
  rebuild (§11.4.74 reuse-don't-reimplement).
- `command:` is llama-server ARGV (the image's `ENTRYPOINT` is already
  `["llama-server"]` — do not prepend another `llama-server` token if you
  edit this file):
  `-m /models/<the decided gguf> --host 0.0.0.0 --port 18435 -ngl 99 -c <ctx> --parallel <n> --cont-batching --cache-type-k q8_0 --cache-type-v q8_0 -fa on --jinja`
  — all layers on GPU (`-ngl 99`), q8_0 KV cache both K and V, flash
  attention on, Jinja chat templates enabled (needed for correct
  tool-calling/structured-output sampling on many models).
- GPU access via the NVIDIA CDI spec (`devices: ["nvidia.com/gpu=all"]`) —
  rootless podman (§11.4.161); the NVIDIA container toolkit must have
  already generated the CDI spec on the host.
- Booted through the containers submodule's `compose.Orchestrator`
  (§11.4.76) — `cmd/agentgen-boot` never shells out to a raw
  `podman-compose`/`docker compose` command directly; it calls
  `compose.NewDefaultOrchestrator(".", nil)` then `.Up(...)`/`.Down(...)`.
- Health check: real HTTP poll of `http://localhost:18435/health` via the
  containers submodule's `pkg/health.CheckHTTP` primitive
  (`cmd/agentgen-boot/main.go: pollHealth`), up to a 5-minute budget,
  polling every 3 seconds — not a fixed sleep-and-hope.

## Using Lane B once it is up

Lane B exposes the same llama-server OpenAI-compatible HTTP API as the
resident coder, just on its own port:

The model name to send is the one the boot reported as `CHOSEN`, since which
model is serving is decided per host — `/v1/models` asks the running server
rather than assuming:

```bash
curl -s http://localhost:18435/v1/models

curl -s http://localhost:18435/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "<the CHOSEN model>", "messages": [{"role": "user", "content": "Write a Go hello-world."}]}'

curl -s http://localhost:18435/health
```

This is the raw llama-server instance, not routed through HelixLLM's
`Brain`/gateway facade documented in [`openai-anthropic-facade.md`](openai-anthropic-facade.md) —
Lane B is a second independent inference backend an operator (or a future
routing-policy change) can point traffic at, not automatically
load-balanced with the resident coder by anything in this codebase today.
Whether/how the HelixLLM `Brain` routing layer is meant to discover and use
Lane B as a second provider target was **not found** as of this pass — no
reference to port `18435` or "agentgen" inside `internal/brain/`.

## Sources verified 2026-07-11:
No external upstream source applies — Lane B is original HelixLLM
infrastructure (a config-driven second llama-server instance behind the
project's own VRAM broker and containers-submodule orchestrator), not a
wrapper around a third-party service with its own external docs to
cross-check. The llama-server CLI flags used (`-ngl`, `-c`, `--parallel`,
`--cont-batching`, `--cache-type-k/v`, `-fa`, `--jinja`) are documented
upstream by `dependencies/LLama_CPP` (llama.cpp's own `--help` /
`docs/server`); not independently re-fetched in this pass since this
document's purpose is Lane B's own admission/boot/config contract, not a
llama-server flag reference.
