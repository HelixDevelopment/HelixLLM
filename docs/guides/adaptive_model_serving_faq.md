# Adaptive model serving — FAQ

Companion to the [user guide](./adaptive_model_serving.md). Every answer here describes behaviour
that is in the code today.

---

## Why was this model not offered to me?

Run the plan and read the `WITHHELD` line for that model. It carries exactly one reason, and the
three reasons have **different remedies** — which is the whole point of keeping them apart:

```bash
go run ./cmd/visiongen-boot plan
```

```
WITHHELD <model>: <reason> — <detail>; remedy=<remedy>
```

Then find your reason below. **The sample lines are illustrative — the model names and the axis
figures come from the shipped catalogue and a real reading, so yours will differ. What is fixed is
the shape: reason, its own detail, its own remedy.**

### `insufficient_resources` — the host is short of memory or storage

```
WITHHELD glm-5.2: insufficient_resources — storage short by 190000MiB
  (needs 380928MiB, 190928MiB available after 10048MiB held back to keep the host responsive);
  remedy=change-host-or-pick-smaller
```

**Read the resource name first.** Memory and storage are checked as separate axes, and the line
tells you which one was short. A model can fit memory ten times over and still be refused on disk —
`glm-5.2` in the shipped catalogue needs ~16 GiB of memory and ~372 GiB of disk. If you fix the wrong
axis, nothing changes.

The line also separates *too big for this machine* from *too big once the machine keeps working*:
`available` is what was left after the reserve (15% of nameplate memory, or 5% of free storage), and
`reserved` is what was held back. If required is just above available but below available+reserved,
the model would fit only by making the host unresponsive.

**Remedies:** free the named resource, move to a host that has it, or pick a smaller option — the
plan output lists everything that *was* offered.

### `unsupported_configuration` — nothing here can run it

```
WITHHELD wan2.2-t2v-a14b: unsupported_configuration — this host provides no accelerator (measured);
  more memory does not help; remedy=different-approach
```

This is not a shortage. There is no amount of free memory that resolves it. Three things produce it:

| Requirement | What happened |
|---|---|
| `accelerator` | The entry mandates an acceleration device and this host was **measured** to have none. |
| `streaming-roster` | The entry is served only by the disk-streaming runtime, and that runtime does not list its family. Eligibility is roster membership and nothing else — never architecture. The detail carries the family name that was looked up. |
| `catalogue-entry` | Nothing was recorded that could serve this at all — either the whole family, or a `--pin` naming a model that does not exist. |

**Remedy:** a different approach. A different model, a host of a different *kind* (one with an
accelerator, not merely a bigger one), or — for a roster miss — waiting until the streaming runtime
declares support for that family, which arrives as a catalogue data change.

### `excluded_by_usage_terms` — the licence, not the machine

```
WITHHELD <model>: excluded_by_usage_terms — licence <id> does not permit "commercial"
  (term=non-commercial, granted=false, ref=licence §2(d)); the host could serve it;
  remedy=different-model-or-declared-usage
```

**This host could serve it.** That is why the remedy is never hardware. Two distinct situations both
produce this reason, and `granted` tells you which:

- `granted=false` with **no term** — the licence simply never permitted your declared purpose. There
  is no clause to comply with, only an absence of permission.
- a named **term** — a specific restriction excludes your purpose, and `ref` cites the clause. The
  term reported is the one that actually excluded you, not merely the first restriction the licence
  lists: attribution and share-alike constrain how output is used without withholding anything, and
  naming one of those would point you at a clause you could have complied with.

**Remedies:** pick a different model, or declare a different usage *if that is genuinely what you
are doing*:

```bash
HELIXLLM_DECLARED_USAGE=research go run ./cmd/visiongen-boot plan
```

If you declared nothing, the default is `commercial` — the narrowest purpose — and it is printed on
every run. Widening it deliberately may make models reappear. Do not widen it to get a model.

---

## Why does the family-level refusal name a different reason than I expected?

When several candidates were withheld and nothing could be offered, the family reports the reason
**closest to being satisfiable**: usage terms first, then insufficient resources, then unsupported
configuration.

An entry withheld only by its licence would have run on this machine. Reporting the family as a
hardware problem would send you to spend money that will not help. The per-candidate `WITHHELD`
lines above the refusal still carry each candidate's own reason.

---

## A model that was there yesterday has disappeared

Most likely it was unloaded to give its memory back to the host. That is announced, never silent —
the announcement carries the model, the reason, who decided, how long it had been idle, and how many
bytes were reclaimed.

| Reason key | What happened |
|---|---|
| `idle_timeout` | It served no request for the configured idle period, so its memory went back. |
| `memory_pressure` | It was evicted to make room for a selection the host could not otherwise fit. |
| `user_requested` | You asked. These are not announced — you already know. |

A model **currently serving a request is never taken**, no matter how long it has been idle by the
clock. If room was needed and every loaded model was busy, the answer is "no idle model to free"
rather than a stolen model.

If a model vanished with *no* announcement, that is not lifecycle doing its job — it is a fault, and
worth reporting as one. Distinguishing the two is exactly why the announcement is mandatory.

The idle period is configuration (`lifecycle.Config.IdleTimeout`) and must be greater than zero;
there is no compiled-in default period.

---

## Why was my pin refused?

A pin is a **constraint on the choice, not a bypass**. It narrows the candidate set to the entry it
names; that entry then goes through the identical configuration, fit and terms checks. Nothing about
a pin makes a model fit.

So a refused pin has one of the ordinary reasons, and you read it the same way:

- **`insufficient_resources`** — the host cannot run what you pinned. The named axis is real. Pick
  something smaller or run it somewhere else.
- **`excluded_by_usage_terms`** — the host could run it; your declared usage is not permitted.
- **`unsupported_configuration` / `catalogue-entry`** — the pin names a model the catalogue does not
  record. There is no entry to measure against, so there is no resource to be short of. Check the
  spelling, and check the variant: `--pin id` matches an entry with no variant *and* matches by id
  alone; `--pin id:variant` requires that exact variant.

A `--pin` with no value at all is a usage error (exit 22), not a refusal.

---

## Why does it refuse instead of guessing when it cannot measure my host?

Because a guess here tells you something false about your own machine, and nothing downstream can
tell an invented figure from a measured one.

Two host-level refusals exist, and neither is a statement about any model:

- **`host_not_measured`** (exit 20) — measurement did not complete. The refusal carries a cause key:
  `measurement-incomplete`, `no-cpu-cores`, `no-memory-total`, `accelerator-state-unknown`,
  `no-host-identity`, `no-measurement-time`, or `profile-malformed`.
- **`measurement_stale`** (exit 21) — the reading is older than this decision allows. The refusal
  shows the age and the limit it was compared against, so you can see the comparison it made.
  The default limit is 5 seconds, because available memory and free storage move continuously.

The commonest cause is `accelerator-state-unknown`, and it deserves its own explanation.

### "Accelerator state unknown" when I am sure I have no accelerator

The measurement distinguishes **"this host has no accelerator"** from **"we could not determine this
host's accelerator state"**. The first is a positive finding and a perfectly serviceable host — it
gets offers, including for every processor-viable family. The second is an absence of information.

You get `unknown` when:

- the platform's accelerator-presence scan failed, or the platform has none;
- a vendor's **hardware is present but its probe is not installed** — an NVIDIA card with no
  `nvidia-smi`, say. Reporting zero devices here would invent a CPU-only host out of a machine that
  has a card we cannot read;
- a probe was installed and errored;
- devices came back that cannot be bound by a stable identity.

In every one of those, partial knowledge is withheld entirely rather than being handed on as half a
reading. **Fix:** install the vendor's tooling so the probe can answer, or investigate the probe
error printed as part of `MEASURE-INCOMPLETE`. A genuinely accelerator-free host reports
`accelerators=0 (measured)` and proceeds normally.

Note that a *measured* absence produces a different, later refusal: an entry that requires an
accelerator on a host measured to have none is `unsupported_configuration`, not a measurement gap.

---

## Why can I see a model in the catalogue that I cannot fetch?

Because being **describable** and being **fetchable** are different states with different remedies,
and the catalogue is deliberately allowed to record the first without the second.

An entry needs two different things for the two purposes:

- **To be offered:** a model id, a known family and runtime, non-zero memory *and* storage figures,
  a licence with at least one permitted purpose, and roster membership if it declares streaming.
- **To be fetched:** all of the above, **plus** a `source` (checked against the allowlist) **plus** a
  complete integrity expectation — algorithm *and* digest *and* size.

An entry that passes the first and fails the second is well-researched and simply not located yet.
This is not an oversight in the loader: if a digest were required to load, nothing could load, so no
download could be triggered, so no digest could ever be captured.

**Today, none of the 30 shipped catalogue entries carries a digest.** They record
`algorithm: sha256` and a size, but the digest field is absent, so every one of them fails
`ValidateForAcquisition`. They are fully usable as records — comparable, selectable, offered against
a measured host — and fetching one requires capturing its digest first.

You can also hit this from the other side: `visiongen-boot` exits **24** when the host was measured,
options *were* offered, and none of their weight files is present in `VISIONGEN_MODEL_DIR`. It lists
every option it looked for. It will not boot some other file that happens to be in that directory —
that would be a model nobody chose.

---

## I set an environment variable to pick the model and it was ignored

It was ignored on purpose, and it said so:

```
IGNORED-CONFIG: VISIONGEN_MODEL_GGUF="…" named a model that no measurement chose;
  overwritten with the measured choice "…" (FR-056).
```

`VISIONGEN_MODEL_GGUF`, `VISIONGEN_MMPROJ`, `IMAGEGEN_MODEL` and `IMAGEGEN_PRECISION` are **outputs**
of the decision, written for compose to interpolate. `VISIONGEN_NEED_BYTES` is not honoured at all —
it implied a model ("~9 GiB means the 7B"), and the admitted VRAM figure now comes from the chosen
option's recorded requirement.

What you *can* still set: `VISIONGEN_MODEL_DIR` (where files live), `HELIXLLM_CATALOGUE_DIR` (where
candidates are described), `HELIXLLM_DECLARED_USAGE` (how output will be used), and the forbid-lists.
And `--pin`, which is a constraint, stated at invocation so it is unmistakably deliberate.

---

## Why did a model I forbade still show up in the reasoning?

Forbidding removes an option the measurement offered; it never introduces one. Each removal is
printed:

```
FORBIDDEN-BY-CONFIG: helixllm/gpu-01/<model> removed by VISIONGEN_FORBID_MODELS (operator choice, not a measurement)
```

If the forbid-list empties the offer set, the refusal says so explicitly — it distinguishes
"the host can serve N options, you forbade them all" from "the host can serve nothing".

---

## Why is it serving from disk and running slowly?

The streaming runtime was chosen as a **fallback**, and only when all three of these held: the model
did not fit in available memory, its family is on the streaming runtime's declared roster, and the
host met that runtime's own floors (a resident working set — 25% of the in-memory requirement by
default — plus the full on-disk footprint).

A model that fits in memory is always served from memory, even if it is also on the roster.
Streaming buys feasibility with throughput, by orders of magnitude, so choosing it for a model that
fits would be a large slowdown accepted for nothing.

The choice carries a `Tradeoff` recording that the cost is throughput and the cause is weights read
from disk during inference.

---

## Why does it not simply fall back to a default model when something goes wrong?

There is no default model anywhere in this path — not in configuration, not compiled in.

A model that was not chosen from a measurement may not fit the machine it is started on. Starting
one would replace an honest refusal you can act on with a load-time failure you cannot. So every
failure path here refuses, names what is missing, and exits non-zero.
