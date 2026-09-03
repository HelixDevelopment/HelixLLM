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

---

## The model identifier I saved has stopped working

Identifiers for locally served models changed once, and only once.

They used to be built from the address the gateway saw itself on, which for the
embedded server is `127.0.0.1`. So the published identifier looked like
`helixllm-127-0-0-1-llama3-<digest>`. That named no machine — and worse, it was
the *same* on every machine, so two hosts published byte-identical identifiers
and a consumer merging both catalogues silently kept one and dropped the other.

The host part is now the machine's own name, so the identifier reads
`helixllm-anton-llama3-<digest>` and says where the model actually is.

Re-run discovery, or fetch the configuration again from
`/v1/config/helixcode` or `/v1/config/opencode`, and use what comes back.

This will not happen again for this reason. Both spellings of loopback now
resolve to the same machine name, where previously flipping
`HELIX_LLM_LOCAL_RPC_HOST` between `localhost` and `127.0.0.1` silently re-minted
every identifier. The naming *scheme* did not change — only the host value fed
into it.

---

## I asked for one model and got an error instead of a different model

That is deliberate, and it is new.

Naming a model is a choice, not a hint. If you name a model this server serves
and it cannot be served right now, you get an error saying so. You do not get a
different model answering in its place.

Previously the request fell through to whichever provider scored highest, which
meant the substitution happened *exactly* when the thing you named was gone —
the worst possible moment to quietly answer with something else, because the
reply looked normal and nothing told you the model had changed.

This applies to any named model, not only to published identifiers. A request
naming `gpt-4o` with an OpenAI provider registered no longer cross-falls-back to
another provider when that one fails.

If you want *any* available model, name none.

---

## The server will not start and says a credential is a placeholder

The value in your configuration is still `${SOMETHING}` because nothing
substituted it — the variable is unset, or misspelled, or the expansion step did
not run.

The error names both the field and the variable. Export the variable and start
again.

The server refuses rather than continuing, because continuing was worse. The
literal text `${HELIX_AUTH_JWT_SECRET}` would otherwise have been used *as* the
signing key: a fixed string, committed to a public repository, that anyone
reading it already knows. Every token would have verified, every health check
would have passed, and nothing would have looked wrong.

An unset secret is now a refusal you can act on in one command, instead of a
silent, shared password.

---

## An instance I discovered stopped answering after it restarted

Requests go to the address that proved it holds the shared secret — not to the
name you configured, resolved again at send time.

That is what stops a relay attack. An attacker who controls a DNS answer between
the moment we authenticate an instance and the moment we send it your prompt
could otherwise point the second lookup at a host of their choosing, and it
would receive your prompt, the contents of files you have open, and your
upstream credentials.

The cost is this: if an instance legitimately changes address — a DHCP lease
expiring, a container restarting — it will not be reached until it is discovered
again. Re-run discovery and it comes back.

Refusing to send is the safe failure here. Re-resolving the name is precisely
the hole.

---

## The agent lane refuses a model it used to run

The agent lane used to take its model from `AGENTGEN_MODEL_GGUF` and the amount
of VRAM to reserve from a second variable, `AGENTGEN_NEED_BYTES`. Those two had
to be kept in agreement by hand. If you pointed the first at a larger model and
forgot the second, the lane reserved the smaller amount and started anyway —
having never checked that the model fits.

Measured on a 12 GiB card: naming a 19.5 GiB model while leaving the byte count
alone gave `ADMIT-OK` and exit 0. The same binary, told that model's real size,
refused it. The only difference was whether someone remembered the second
variable.

The lane now chooses from the measured catalogue, and the requirement comes from
the entry it chose. There is nothing left to keep in agreement.

The cost is real and worth stating: a model that is not in the catalogue can no
longer be run here. If you were serving Mistral-Nemo-Instruct-2407,
GLM-4.7-Flash or DeepSeek-Coder-V2-Lite this way, you will now be refused —
those three were never in the catalogue, which is exactly why a fixed 9 GiB
placeholder had to stand in for their footprint.

To bring one back, measure it and add a catalogue entry recording where the
figure came from, as the existing entries do. Do not estimate it: a wrong figure
here is the defect above with extra steps.

To choose deliberately among models that ARE in the catalogue, use `--pin`. It
is validated against the catalogue and refuses with the resource you are short
of, rather than starting something that cannot load.
