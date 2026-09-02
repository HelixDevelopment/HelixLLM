# Adaptive local model serving

**What this does:** every boot measures the machine it is on, joins that measurement against
the recorded model catalogue under the usage you have declared, and serves a model the host was
proven able to run. If nothing qualifies, it refuses and names what is missing.

**What it does not do:** take the model's name from configuration. There is no default model and
no environment variable that selects one. See [Configuration cannot name the model](#configuration-cannot-name-the-model).

Related: [FAQ](./adaptive_model_serving_faq.md) · [consumer setup](./consumer_setup.md).

Diagrams: [measurement → selection → runtime](../diagrams/adaptive-model-serving-flow.mmd) ·
[the refusal decision tree](../diagrams/adaptive-model-serving-refusals.mmd) ·
[which runtime serves it](../diagrams/adaptive-model-serving-runtime-path.mmd).

---

## See what this host would serve

The `plan` subcommand measures, decides and reports. It boots nothing and touches no accelerator.

```bash
# from the repository root
go run ./cmd/visiongen-boot plan
go run ./cmd/imagegen-boot plan
```

Output has four parts, in this order (**the figures below are illustrative; the line shapes are
what the code emits**):

```
DECLARED-USAGE: commercial (default — the narrowest purpose; set HELIXLLM_DECLARED_USAGE to declare another)
MEASURED host=gpu-01 cpu=32 memory_available=51200MiB storage_available=880000MiB accelerators=1 (measured)
WITHHELD <model>: <one of three reasons, with its own detail and its own remedy>
CHOSEN helixllm/gpu-01/<model> — decided from the measured host "gpu-01", not from configuration.
```

A `--pin` is reported on its own line, between the declared usage and the measurement. A failed
measurement prints `MEASURE-INCOMPLETE: <what was missing>` instead of `MEASURED`.

`WITHHELD` lines are the interesting ones. Each names exactly one reason, and the three reasons
have different remedies — that is why they are kept apart. See
[Refusals](#refusals-and-what-each-one-asks-of-you).

To boot after planning:

```bash
go run ./cmd/visiongen-boot boot <compose-file> <project>
go run ./cmd/visiongen-boot status <compose-file> <project>
go run ./cmd/visiongen-boot down   <compose-file> <project>
```

`visiongen-boot` also has `admit-check`, which runs the measured decision and then tests the VRAM
admission gate without booting.

---

## What is measured

`internal/capability` reads five things and stamps the time it read them:

| Axis | What it means |
|---|---|
| CPU | architecture, physical and logical cores, instruction-set features |
| Memory | nameplate total, and what is *actually free right now* (`MemAvailable` on Linux) |
| Storage | free bytes on the filesystem that will hold the weights — a separate axis from memory |
| Accelerators | zero or more devices, each bound by a **stable device identity** (UUID / PCI address), never by enumeration index |
| Accelerator *state* | whether enumeration completed at all — see below |

Two properties are worth knowing because they change what you see:

**Memory and storage are checked separately.** A model can fit memory ten times over and still be
refused on disk. The refusal names which axis was short. Nothing derives a disk figure from a
memory figure.

**"No accelerator" and "unknown" are different values.** `AcceleratorStateMeasured` with zero
devices is a positive finding — a CPU-only host, fully serviceable, and it gets offers.
`AcceleratorStateUnknown` means enumeration did not complete: a vendor's hardware is present but
its probe is not installed, a probe errored, or the presence scan itself failed. An unknown state
refuses; it never reads as "there are none".

If any axis fails, the profile is returned with `MeasurementComplete = false` and the missing
figure stays missing. It is never replaced by a plausible default, because once a default reaches
selection nothing downstream can tell it apart from a measurement.

**Readings expire.** `capability.DefaultMaxMeasurementAge` is 5 seconds. Available memory and free
storage move continuously while other work runs, so a decision is made from a reading taken now,
not one carried forward from start-up.

---

## How an option is chosen

`selection.Select` is a pure function of `(measured profile, catalogue entries, declared usage,
optional pin)`. It reads the measurement and never writes back into it — which is why the whole
surface, refusals included, is exercised from fixture hosts with no hardware present.

Each candidate is checked in a fixed order, and the first failure is the reason:

1. **Configuration** — does this host provide what the entry *requires* at all? A mandatory
   accelerator it does not have; a streaming-only entry whose family the streaming runtime does not
   list. → `unsupported_configuration`
2. **Fit** — memory first, then storage, each against what is free *after* the reserve.
   → `insufficient_resources`
3. **Usage terms** — do the licence terms permit the usage you declared?
   → `excluded_by_usage_terms`

The order is deliberate. An entry withheld for its licence is one that genuinely *would* have run
here, which is what makes "a different model, or a different declared usage" the honest remedy
rather than a guess.

### The reserve

Selection holds resources back so the machine stays usable while it serves:

- **15%** of nameplate memory total (against the *total*, so a host already under pressure does not
  get a smaller reserve)
- **5%** of free storage

Both are stated policy carried in the request (`selection.Reserve`), not constants buried in a
comparison, and a caller can supply different fractions. The withheld line reports what was held
back, so you can tell "too big for this machine" from "too big once the machine keeps working".

### Every family answers

A capability family is never silently empty. It either offers something, or it carries a refusal
naming what its candidates lacked. A family the catalogue records nothing for is also an answer:
`unsupported_configuration`, requirement `catalogue-entry`.

When several candidates were withheld, the family reports the reason **closest to being
satisfiable** — terms, then resources, then unsupported. Reporting a hardware obstacle when the
real one is a licence sends you to spend money that will not help.

### The identity you get back

Every offer carries `helixllm/<host>/<model>[:<variant>]`. That string is a *value* — a label to
read. It is not the identifier your tools use; see [consumer setup](./consumer_setup.md).

---

## Refusals, and what each one asks of you

A refusal is the product working. The system says no and names what is missing, rather than
starting something that will not fit and failing later at load time.

### The three withheld reasons

| Reason | What it means | Remedy key |
|---|---|---|
| `insufficient_resources` | The host lacks the memory or storage this option needs. The line names **which** axis, what was required, what was available after the reserve, and how much was reserved. | `change-host-or-pick-smaller` |
| `unsupported_configuration` | Nothing about this host can run it. A mandatory accelerator it does not have, or a runtime path that does not exist. **More memory does not help.** | `different-approach` |
| `excluded_by_usage_terms` | The host could serve it; the licence forbids the usage you declared. Names the licence, the restricting term, whether the purpose was granted at all, and the clause reference. | `different-model-or-declared-usage` |

The set is closed. A generic "unavailable" invented downstream is not a reason.

### Two host-level refusals

These are *not* withheld reasons — no option was reached, so nothing can be said about one:

| Kind | Meaning | Exit code |
|---|---|---|
| `host_not_measured` | Measurement did not complete. There is no basis for a choice, and no default stands in for one. | 20 |
| `measurement_stale` | The reading is older than this decision allows. The refusal shows the age and the limit it was compared against. | 21 |

### Exit codes

From `cmd/visiongen-boot` and `cmd/imagegen-boot`:

| Code | Meaning |
|---|---|
| 20 | host not measured |
| 21 | measurement stale |
| 22 | no option offered (includes an unusable `HELIXLLM_DECLARED_USAGE`, and a bad `--pin` argument) |
| 23 | catalogue could not be read |
| 24 | an option was offered but cannot be served here — `visiongen`: its weights are not present in the model directory; `imagegen`: the runtime cannot serve that build |

`visiongen-boot` and `imagegen-boot` also exit 10–14 for VRAM admission verdicts (budget exceeded,
burst in use, budget unreadable, thermally unsafe, other), 4 when the service never becomes
healthy, and 2 on a usage error.

---

## Configuration cannot name the model

If you set an environment variable expecting it to choose the model, it will not. The model name is
now an **output** of the decision, not an input.

Concretely, in the vision lane:

- `VISIONGEN_MODEL_GGUF` and `VISIONGEN_MMPROJ` are written *by* the decision for compose to
  interpolate. A value already in the environment is reported and overwritten:
  `IGNORED-CONFIG: … named a model that no measurement chose`.
- `VISIONGEN_NEED_BYTES` is no longer honoured at all — it implied a model ("~9 GiB means the 7B").
  The admitted VRAM figure comes from the chosen option's recorded requirement.
- `IMAGEGEN_MODEL` and `IMAGEGEN_PRECISION` are outputs the same way.

What configuration still legitimately says:

| Variable | Role |
|---|---|
| `VISIONGEN_MODEL_DIR` | **Where** weight files live on this host. Defaults to `~/models/vlm_cache`. Never which of them runs. |
| `HELIXLLM_CATALOGUE_DIR` | Where the candidate catalogue is read from. Defaults to `internal/catalogue/data` (relative to the working directory). |
| `HELIXLLM_DECLARED_USAGE` | How output will be used: `commercial`, `personal`, `research`, `evaluation`. |
| `VISIONGEN_FORBID_MODELS` / `IMAGEGEN_FORBID_MODELS` | Comma-separated `id` or `id:variant` to exclude. Forbidding can only *remove* an option the measurement offered — never introduce one it did not. Every removal is printed as `FORBIDDEN-BY-CONFIG`. |

Once selection has produced offers, they arrive **already ordered, cheapest-admissible-first** —
by memory required, then storage required, then the catalogue identity — and the boot lanes take the
first one this runtime can actually serve, then look for that model's files. They never scan the
directory and run whatever they find: that would be the directory naming the model.

Cheapest rather than largest, because a host does not serve one model. A coder model runs beside a
vision or video one on the same accelerator (`internal/vrambroker` is what accounts for that), so
memory taken by the biggest option that fits is memory the next model cannot have. Largest-first
optimises one model in isolation; cheapest-that-works optimises the machine.

The ordering is decided once, in `internal/selection`, and not in each lane — a rule copied into
three lanes is a rule that drifts. It is the same rule `container/helix_model_gate.py` applies, so
the Go path and the Python path choose the same build on the same host. Ordering only ranks options
that already passed configuration, fit on **both** the memory and the storage axis, and the declared
usage terms; it never promotes something that was withheld.

---

## Declared usage

Selection *requires* a declared usage. Terms cannot be applied against an undeclared one, and
assuming a permissive one would offer models you may not be permitted to use.

If `HELIXLLM_DECLARED_USAGE` is unset, the boot lanes default to **`commercial`** — the narrowest
purpose — and say so:

```
DECLARED-USAGE: commercial (default — the narrowest purpose; set HELIXLLM_DECLARED_USAGE to declare another)
```

Defaulting narrow can only ever withhold an option you were in fact entitled to; it can never offer
one you are not. Widen it deliberately:

```bash
HELIXLLM_DECLARED_USAGE=research go run ./cmd/visiongen-boot plan
```

An unrecognised value is refused, not guessed at (exit 22).

Every offered option carries its terms, so you see what you may do with the output at the moment of
choosing rather than afterwards. A withheld-on-terms line names the licence, the term that actually
excluded your purpose (not merely the first term the licence lists), any threshold it carries, and
the clause reference.

---

## Pinning a model

A pin is a **constraint on the choice, never a bypass**.

```bash
go run ./cmd/visiongen-boot plan --pin qwen2.5-vl-7b
go run ./cmd/visiongen-boot plan --pin=qwen2.5-vl-7b:q4_k_m
```

The pin narrows the candidate set to the entry it names. That entry then goes through exactly the
same configuration, fit and terms checks as any other — and is refused, with the insufficient
resource named, when this host cannot run it. Nothing about a pin makes a model fit.

An empty variant matches an entry that declares none, and also matches by model id alone.

A pin naming a model the catalogue does not record is refused as `unsupported_configuration`,
requirement `catalogue-entry` — there is no entry to measure against, so there is no resource to be
short of. The remedy is a different pin, not a bigger machine.

---

## Which runtime serves it

Separate decision, made by `internal/runtime`. The general **in-memory** path is tried first,
always. The **disk-streaming** path is reached only when all three hold:

1. the model does **not** fit in the host's available memory, **and**
2. the model's family is on the streaming runtime's declared roster, **and**
3. the host meets that runtime's own floors — a resident working set (25% of the in-memory
   requirement by default) plus the **full** on-disk footprint.

Streaming buys feasibility with throughput, by orders of magnitude. It is a fallback and never a
preference: a model that fits in memory is served from memory even when it is also on the roster.
When streaming is chosen, the result carries a `Tradeoff` recording that weights are read from disk
during inference and what throughput the catalogue records for that path.

**Roster membership is the only eligibility test.** Architecture is recorded but never consulted —
mixture-of-experts models exist that the streaming runtime does not support, and inferring
eligibility from architecture would offer them anyway, turning a selection bug into a load-time
failure. The roster itself is catalogue data (`streaming_roster:` in the YAML), so a runtime release
that adds a supported family is a data change, not a code change.

The catalogue entry's own declared `runtime:` field is deliberately *not* read when choosing the
path. It records what the catalogue expects; honouring it would serve from disk on a host with
memory to spare.

---

## Models leaving on their own

`internal/lifecycle` returns idle models' memory to the host after a configurable
`IdleTimeout` (must be greater than zero; there is no compiled-in period).

Two guarantees:

- **A model serving a request is never taken.** Eviction is claimed atomically with the check that
  permits it, so a request cannot begin on a model already on its way out, and a sweep cannot claim
  a model twice.
- **Every system-initiated unload is announced** with the model, the reason
  (`idle_timeout` / `memory_pressure` / `user_requested`), the initiator, how long it was idle, and
  how many bytes went back. A model must never leave the available set unexplained — a silent
  disappearance is indistinguishable from a fault.

Unloads you asked for are not re-announced; you already know.

If room is needed and every loaded model is serving, the answer is `ErrNoIdleModel` — there is no
honest offer to make.

---

## The catalogue

Model records live in `internal/catalogue/data/*.yaml`. The loader is strict on purpose:

- **Unknown keys are an error, not a silence.** A misspelled `storage_requried_bytes` would
  otherwise decode as zero. Free-form material goes under `annotations`, which is carried and shown
  but never read to make a decision.
- **Loading is all-or-nothing.** One defective entry yields no entries at all — a partially loaded
  catalogue silently omits models, and a model omitted without explanation is indistinguishable
  from one that was never researched.
- Rosters from every file are unioned before any entry is resolved, so file order cannot change the
  answer.
- Two entries sharing one identity is an error naming both files.

### Visible is not the same as acquirable

An entry has two validation gates:

- `Validate()` — enough to **offer**. Requires a model id, a known family and runtime, non-zero
  memory *and* storage figures, a licence with at least one permitted purpose and no
  self-contradiction, and roster membership if it declares the streaming runtime.
- `ValidateForAcquisition()` — enough to **fetch**. Adds two things: a `source`, and a complete
  integrity expectation (algorithm **and** digest **and** size).

An entry can pass the first and fail the second. That is the ordinary state of a well-researched
model whose weights have not been located and hashed yet, and it is why the loader does not demand a
digest: if it did, nothing could load, so no download could be triggered, so no digest could ever be
captured.

At the time of writing, **all 30 shipped entries record `algorithm: sha256` but no `digest`**, so
none of them passes `ValidateForAcquisition` today. They are visible, comparable and selectable as
catalogue records; fetching one requires its digest to be captured first.

### Where weights may come from

`catalogue.SourceAllowlist` fails closed: an allowlist that was never populated permits nothing, so
a missing configuration can never become "obtain from anywhere". Matching is on whole path segments
(`…/DeepSeek-R1` does not admit `…/DeepSeek-R1-evil`), and sources with embedded credentials or `..`
segments are rejected outright.

`catalogue.WeightGate` is the only way to get a readable handle on a weight file: it checks the
source against the allowlist, verifies the file's length (cheaply, first) and then its digest
against the recorded expectation, and returns the *same open handle* it verified — so the file
cannot be swapped between verification and use. An algorithm this build cannot compute is an error,
never a skipped check.
