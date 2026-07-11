# Lane B: Second Coder/Agent Instance (`agentgen-boot`), port 18435

New capability — no prior user-facing doc existed under `docs/user-guide/`
or `docs/courses/` before this document. Verified directly against
`cmd/agentgen-boot/main.go`, `cmd/agentgen-boot/compose.agent.yml`, and
`internal/vrambroker/broker.go`, `submodules/helix_llm` HEAD `e2ce163`,
2026-07-11.

## What this is

Lane B is a **second, independent** llama.cpp coder/agent instance, served
on its own port (`:18435`), booted **co-resident** with the already-running
resident coder (`:18434`, never touched/restarted by this capability —
§11.4.122). It exists to let a second concurrent coding/agent workload run
on the same GPU as the resident coder, when there is enough free VRAM to
admit it safely.

This is not a load-balancer or a replacement for the resident coder — it is
an additive, independently-addressable llama-server instance with its own
model, its own context window, and its own admission gate.

## VRAM admission: the vrambroker gate (this is the load-bearing safety mechanism)

Before Lane B's container is ever started, `cmd/agentgen-boot` calls
`vrambroker.Broker.Acquire(ctx, vrambroker.ClassAgent, needBytes)`
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

The default model is **Mistral-Nemo-Instruct-2407 Q4_K_M** (bartowski GGUF,
~6.96 GiB weights measured from the GGUF's HTTP `Content-Length`), with a
default `needBytes` reservation of **9 GiB** (weights + a modest 16384-ctx /
4-parallel-slot / q8_0-KV budget + activation headroom —
`cmd/agentgen-boot/main.go: defaultNeedBytes` comment).

```bash
# Model artefact path (gitignored, lives outside the repo — §11.4.30):
export AGENTGEN_MODEL_DIR=$HOME/models         # same cache dir the resident coder uses; auto-defaulted to ~/models if unset
export AGENTGEN_MODEL_GGUF=Mistral-Nemo-Instruct-2407-Q4_K_M.gguf   # default; override to select a different Lane-B candidate

# If you select a DIFFERENT model, you MUST also override the VRAM claim —
# the broker has no way to know a bigger model's real footprint otherwise
# (§11.4.6/§11.4.108 — never assume a bigger model fits the smaller default):
export AGENTGEN_NEED_BYTES=$((10 * 1024 * 1024 * 1024))   # e.g. 10 GiB for a bigger candidate

# Other compose.agent.yml-injected tuning knobs (all env-overridable,
# none hardcoded in the compose file itself — §CONST-045/§11.4.28):
export AGENTGEN_CTX_SIZE=16384       # llama-server -c
export AGENTGEN_PARALLEL=4           # llama-server --parallel
export AGENTGEN_MEM_LIMIT=16g        # container mem_limit (§12.3/§12.6 host-safety cap)
export AGENTGEN_SHM_SIZE=2g
```

Documented alternative Lane-B candidates (per the plan cited in the source
comments, not independently re-verified against the model registry in this
pass): GLM-4.7-Flash (smallest quant, ~9.78 GiB) and DeepSeek-Coder-V2-Lite
Q4_K_M (~9.65 GiB) — both noted as **single-slot-only** given the plan's
headroom math, i.e. do not also raise `AGENTGEN_PARALLEL` when switching to
one of these without re-deriving the KV-cache budget.

`AGENTGEN_MODEL_GGUF` and `AGENTGEN_MODEL_DIR` are two of the two env vars
that MUST be changed **together** when switching models
(`AGENTGEN_NEED_BYTES` is the third — see above); changing only the model
file without updating the VRAM claim risks either a false admission refusal
(claim too high) or, worse, an under-claimed admission that lets a bigger
model destabilize the resident coder's VRAM (claim too low) — the broker
has no independent way to measure a not-yet-loaded model's real footprint.

## Container + orchestration details (verified against `compose.agent.yml`)

- Image: `localhost/helixllm/llamacpp-router:cuda12.8-sm120` — **reused**
  from the resident coder and the VLM lane, no `build:` directive, no
  rebuild (§11.4.74 reuse-don't-reimplement).
- `command:` is llama-server ARGV (the image's `ENTRYPOINT` is already
  `["llama-server"]` — do not prepend another `llama-server` token if you
  edit this file):
  `-m /models/<gguf> --host 0.0.0.0 --port 18435 -ngl 99 -c <ctx> --parallel <n> --cont-batching --cache-type-k q8_0 --cache-type-v q8_0 -fa on --jinja`
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

```bash
curl -s http://localhost:18435/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "Mistral-Nemo-Instruct-2407-Q4_K_M", "messages": [{"role": "user", "content": "Write a Go hello-world."}]}'

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
