# HelixLLM VRAM Residency Broker — Design Spike (DZ-04 / P1-T4)

| | |
|---|---|
| **Status** | DESIGN (spike before implementation, §11.4.6 — do not code the broker until this is agreed) |
| **Owns risk** | DZ-04 (new component), DZ-03 (32 GB VRAM contention), CX-02 |
| **Created** | 2026-07-06 · Revision 1 · Track `(T1/main)` |
| **Grounding** | `docs/research/07.2026/00_master/02_cross_cutting_foundations_ADR.md` Decision 3; streams 01/02/04; the router image capability `--models-max N --models-autoload` (observed in the built router image `CMD` — parent meta-repo `docs/qa/phase0_router_build/router_build.log:740`; not yet exercised at runtime against ≥2 concurrent models, §11.4.5/§11.4.6) |

> Anti-bluff (§11.4.6): every VRAM figure here is a **budget estimate** to be replaced by
> on-card measurement (`nvidia-smi` deltas) once each service runs. The broker's admission
> decisions MUST be driven by measured reservations, never static guesses.

## 1. Problem

One **RTX 5090 · 32 GB**. The programme runs ~10 GPU-capable services (coder fleet, VLM,
image-gen, video-gen, translation-quality, embeddings, reranker). Their aggregate resident
VRAM **far exceeds 32 GB**, so naive co-hosting OOMs. Yet the interactive coder fleet (6–12
agents) must stay hot. We need an **admission + residency broker** that keeps the hot path
resident, swaps mid-tier models on demand, runs heavy generators single-owner, and never OOMs.

## 2. Residency tiers (ADR Decision 3)

| Tier | Members | Policy | VRAM budget (est., re-measure) |
|------|---------|--------|-------------------------------|
| **Resident** | Qwen3-Coder-30B-A3B (fleet) | never evicted; the hot path | ~18 GB weights + ~10–12 GB KV (q8_0, 12×16k) |
| **Warm-swappable** | Qwen2.5-VL, TOWER+ translation, a 2nd coder lane | one active; others parked (vLLM **Sleep Mode** / Ollama `keep_alive:0` / llama-server stop) | shares the residual ~2–4 GB + evicts to make room |
| **Burst (single-owner §11.4.119)** | FLUX image-gen, WAN/LTX video-gen | started per-job, **never co-resident** with another burst; may require pausing warm tier | claims most of the card for the job, then releases |
| **CPU-only** | Qdrant, HelixMemory, NLLB (CTranslate2 int8 ~3.5 GB — can also be CPU), Tesseract | no GPU reservation | 0 GB GPU |

## 3. What already exists (reuse, don't reimplement — §11.4.74)

- **llama.cpp router mode** (`--models-max 3 --models-autoload`, in the built router image) already
  does **multi-model LRU management within the LLM tier**: it loads up to N models on demand and
  unloads the LRU. → The broker DELEGATES the LLM tier to the router and does NOT re-implement LLM
  swapping. The broker's job is the **cross-service** budget (LLM router ↔ VLM ↔ image ↔ video).
- **`containers` submodule** (`pkg/boot`/`pkg/compose`/`pkg/health`, §11.4.76) starts/stops the
  non-LLM GPU service containers. → The broker drives service lifecycle THROUGH the containers API.
- **HelixLLM `internal/brain`** manages the llama.cpp server process. → The broker is a NEW package
  `internal/vrambroker` that sits ABOVE brain + the containers layer.

## 4. Admission API (sketch — Go)

```go
package vrambroker

type Class string // "coder" | "vlm" | "image" | "video" | "translate" | "embed"

type Lease struct {
    ID        string
    Class     Class
    VRAMBytes int64       // measured reservation
    Owner     string      // single-owner tag for burst classes (§11.4.119)
    Release   func()      // idempotent; frees the reservation + may sleep/stop the service
}

type Broker interface {
    // Acquire blocks until the class can be admitted within the VRAM budget,
    // evicting warm-swappable LRU and/or pausing burst peers as needed.
    // ctx cancellation = give up (caller queues or degrades).
    Acquire(ctx context.Context, c Class) (*Lease, error)
    // Budget returns live total/used/free from nvidia-smi (NVML), never a static guess.
    Budget() (total, used, free int64)
}
```

- **Acquire(coder)** → resident, always granted (fleet is pinned).
- **Acquire(vlm|translate)** → warm tier; if free < need, sleep/stop the LRU warm member, then grant.
- **Acquire(image|video)** → burst; assert single-owner (§11.4.119): no other burst lease live; may
  pause warm tier; grant most of the card; on `Release`, resume warm tier.
- Admission gates on **measured** `Budget().free` (NVML query), plus a safety headroom (≥2 GB) and a
  §11.4.133 thermal/power check (refuse/queue if the card is thermally throttled).

## 5. Eviction & fairness

- Resident: never evicted.
- Warm-swappable: **LRU**, but a lease in active use is not evicted (ref-counted).
- Burst: strictly serialized (a queue); a burst job waits for the previous burst's `Release`.
- Starvation guard: warm/burst requests get a max-wait; on timeout the caller degrades (e.g. route
  vision to a cloud provider, or return `503 model warming`) — surfaced honestly, never a silent hang.

## 6. Anti-bluff acceptance (§11.4.108 runtime signature)

The broker is DONE only when, on the clean target, a scripted burst proves:
1. Coder fleet stays live (a concurrent `/v1/chat/completions` keeps answering) while a VLM lease is
   acquired+released — captured tok/s continuity + `nvidia-smi` timeline.
2. An image-gen burst lease pauses the warm tier, runs, and the warm tier resumes — captured VRAM
   timeline shows no OOM and correct hand-back.
3. Admission **refuses** a request that would exceed budget (returns queue/503, does NOT OOM) — the
   negative case, with a paired §1.1 mutation (disable the budget check → a scripted over-commit OOMs → gate FAILs).
Evidence under `docs/qa/<run-id>/vrambroker/`.

## 7. Open questions (resolve before coding)

- **Q1** VLM serving engine: vLLM (Sleep Mode, best swap latency 18–200×) vs llama.cpp+mmproj (simpler,
  one binary). Sleep Mode favors vLLM for the warm tier — verify vLLM sm_120 build (DZ-02) first.
- **Q2** Does llama.cpp router `--models-max` evict cleanly enough that the LLM tier needs no broker
  involvement at all? (Measure: load 3 models, request a 4th, confirm LRU unload + VRAM reclaim.)
- **Q3** NVML (go-nvml) vs shelling `nvidia-smi` for `Budget()` — go-nvml is lower-latency; confirm it
  works rootless in-container.
- **Q4** Where does the broker run — in the HelixLLM server process, or a sidecar coordinating all
  containers? (Leaning: in HelixLLM, since it already owns the llama.cpp lifecycle; it calls the
  containers API for the non-LLM services.)

## 8. Composition

§11.4.119 (single-resource-owner for burst) · §11.4.133 (thermal/power safety gates admission) ·
§11.4.76 (drives services via the containers submodule) · §11.4.74 (reuses router `--models-max` +
containers, doesn't reimplement) · §11.4.108 (runtime-signature acceptance) · ADR-0001 Decision 3.
