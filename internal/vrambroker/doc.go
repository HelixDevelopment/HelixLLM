// Package vrambroker implements the CORE of the HelixLLM VRAM residency broker:
// a cross-service admission gate that keeps the resident coder fleet hot, runs
// heavy generators single-owner, and REFUSES (fail-closed) any request that
// would exceed the measured VRAM budget rather than risk an OOM on the single
// 32 GB card.
//
// Design authority: docs/VRAM_BROKER.md (DZ-04 / P1-T4). This package implements
// the approved design's Admission API (§4) and the residency tiers (§2):
//
//   - Resident  (coder)          — the hot path; Acquire always grants (pinned,
//     never counted against admission because the fleet is already loaded).
//   - Burst     (image | video)  — single-owner per §11.4.119: at most one burst
//     lease may be live; admission gates on measured free VRAM + a safety
//     headroom.
//   - Warm      (vlm | translate)— admission-gated on measured free VRAM.
//   - CPU-only  (embed)          — no GPU reservation.
//
// Budget() reads the LIVE numbers from `nvidia-smi` (design Q3 resolution: shell
// nvidia-smi rather than NVML/go-nvml — it works rootless in-container and is the
// simplest reliable source). Every admission decision is driven by these MEASURED
// numbers, never a static guess (design §4, §11.4.6).
//
// Runtime-signature acceptance (§11.4.108): the broker is DONE only when, against
// the live card with the coder resident, (a) Budget() returns the real nvidia-smi
// numbers, (b) an over-budget request is REFUSED with ErrBudgetExceeded and no
// OOM, and (c) a second concurrent burst lease is refused with ErrBurstInUse
// while the coder keeps answering. See internal/vrambroker/integration_test.go
// and docs/qa/phase3_vrambroker_20260706/.
package vrambroker
