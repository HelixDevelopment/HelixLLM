// Command laneconfig-validate is the PURE-Go, GPU-touch-free, container-boot-
// free pre-boot config validator for a NEW HelixLLM serving lane (Serving-plan
// Task 1.5, master plan §6.3, danger zones D1/D4/D7).
//
// Given a proposed lane's launch config (model architecture, llama-server
// launch flags, port, container name — cmd-line arg 1, a JSON file) it
// computes the worst-case static VRAM footprint via the §4 KV-cache-per-token
// formula (docs/research/07.2026/01_local_models_serving/
// 01_local_models_serving.md §4: "2 (K+V) × n_layers × n_kv_heads × head_dim ×
// bytes_per_elem") and refuses (non-zero exit + a specific machine-parseable
// error code) when ANY of the following holds:
//
//	(a) STATIC_FOOTPRINT_EXCEEDS_BUDGET — the worst-case weights+KV sum for the
//	    proposed lane exceeds (budget_bytes - headroom_bytes). D1 mitigation:
//	    a burst where the coder + the new lane simultaneously ramp KV-cache
//	    usage to their configured ceilings must never exceed what the card
//	    can actually hold — this is a CONFIG-TIME, not admission-time,
//	    guarantee (the vrambroker's Acquire remains the admission-time
//	    fail-closed guarantee at ACTUAL boot time, §11.4.108 SOURCE vs
//	    RUNTIME layers).
//	(b) NGL_BELOW_FULL_RESIDENCY — the declared -ngl is below the model's full
//	    transformer layer count. D7 mitigation: an -ngl too low silently
//	    spills layers to CPU RAM instead of failing outright — a
//	    working-but-catastrophically-slow lane that is very hard to debug
//	    without this check.
//	(c) PORT_COLLISION / CONTAINER_NAME_COLLISION — the proposed port or
//	    container name matches an already-registered lane. D4 mitigation:
//	    every lane MUST be its own container + port; a naive "share the
//	    process" shortcut silently reduces effective concurrency without any
//	    error.
//
// This tool NEVER boots a container, NEVER touches the live coder (:18434,
// container helixllm-coder — §11.4.122 no-silent-removal / no unauthorized
// disturbance of a shipped capability), and NEVER allocates GPU memory. When
// `budget_bytes` is omitted from the config it reads the LIVE measured free
// VRAM via `vrambroker.New().Budget()` (a READ-ONLY nvidia-smi shell-out,
// §11.4.174 process-ownership-safe — this package issues no writes/Acquire
// calls of its own) — the exact same live-free-VRAM source the real
// vrambroker.Acquire admission gate uses, so a config that passes this
// validator is expected to admit at real boot time too (barring the DZ-23
// volatility the master plan documents: free VRAM MUST be re-read immediately
// before the real Acquire, never trusted from an earlier validator run).
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/HelixDevelopment/HelixLLM/internal/vrambroker"
)

// LaneConfig is the declared launch config for a proposed serving lane —
// everything the D1/D4/D7 pre-boot checks need. Every numeric field is
// EXPLICIT (§11.4.6 no-guessing): nothing is inferred except the two
// documented budget overrides and the weight-size auto-stat fallback.
type LaneConfig struct {
	// --- identity (D4 collision checks) ---
	Port          int    `json:"port"`
	ContainerName string `json:"container_name"`

	// --- model / weights ---
	ModelPath   string `json:"model_path"`             // informational + weight-size fallback source
	WeightBytes int64  `json:"weight_bytes,omitempty"` // measured resident weight size; 0 => os.Stat(ModelPath)

	// --- architecture (drives the §4 KV-cache-per-token formula) ---
	NLayers    int `json:"n_layers"`
	NKVHeads   int `json:"n_kv_heads"`
	HeadDim    int `json:"head_dim"`
	FullLayers int `json:"full_layers,omitempty"` // total transformer layers = the -ngl full-residency target; 0 => defaults to NLayers

	// --- llama-server launch flags under validation ---
	KVQuant  string `json:"kv_quant"`  // "fp16" | "q8_0" | "q4_0" | "q4_1"
	CtxSize  int64  `json:"ctx_size"`  // -c
	Parallel int    `json:"parallel"`  // --parallel
	NGL      int    `json:"ngl"`       // -ngl

	// --- budget (optional overrides; 0/omitted => live vrambroker source) ---
	BudgetBytes   int64 `json:"budget_bytes,omitempty"`   // 0 => vrambroker.New().Budget() (live free VRAM)
	HeadroomBytes int64 `json:"headroom_bytes,omitempty"` // 0 => vrambroker.HeadroomBytes
}

// KnownLane is one already-registered serving lane's port + container name,
// checked for D4 (model-swap-thrash) collisions.
type KnownLane struct {
	Port int    `json:"port"`
	Name string `json:"name"`
}

// DefaultKnownLanes mirrors the port map documented in
// docs/research/07.2026/00_master/RESUME.md (coder :18434; translate/
// whisper/tesseract/vision/rag/acp/imagegen/videogen :18436-18443 — :18435 is
// deliberately ABSENT from the known set per RESUME.md/the task brief, not an
// oversight). Honest boundary (§11.4.6): the container-name entries are
// DESCRIPTIVE SLUGS, not verified production podman-compose project names
// (those vary by the compose --project-name given at real boot time) — pass
// -known-lanes to load an accurate registry for a specific deployment.
var DefaultKnownLanes = []KnownLane{
	{18434, "helixllm-coder"},
	{18436, "helixllm-translate"},
	{18437, "helixllm-whisper"},
	{18438, "helixllm-tesseract"},
	{18439, "helixllm-visiongen"},
	{18440, "helixllm-rag"},
	{18441, "helixllm-a2a"},
	{18442, "helixllm-imagegen"},
	{18443, "helixllm-videogen"},
}

// Result is the machine-checkable verdict of Validate — every FAIL cites the
// exact numbers that produced it (§11.4.5 captured evidence, never a bare
// boolean).
type Result struct {
	OK      bool
	Codes   []string // stable machine-parseable failure codes; empty on OK
	Reasons []string // one human-readable reason per code, same order

	WeightBytes      int64
	KVBytesPerToken  int64
	WorstCaseKVBytes int64
	TotalStaticBytes int64
	BudgetBytes      int64
	HeadroomBytes    int64
	AvailableBytes   int64 // BudgetBytes - HeadroomBytes

	FullLayers int
}

// kvUnitsPerElem is bytesPerElem expressed in HALF-byte integer units so the
// whole §4 formula stays EXACT int64 arithmetic (no float rounding on
// multi-gigabyte sums): fp16 = 2.0 B = 4 halves; q8_0 = 1.0 B = 2 halves;
// q4_0/q4_1 = 0.5 B = 1 half — see 01_local_models_serving.md §4.
func kvUnitsPerElem(quant string) (int64, error) {
	switch strings.ToLower(strings.TrimSpace(quant)) {
	case "fp16", "f16":
		return 4, nil
	case "q8_0":
		return 2, nil
	case "q4_0", "q4_1":
		return 1, nil
	default:
		return 0, fmt.Errorf("unknown kv_quant %q (want one of fp16|q8_0|q4_0|q4_1)", quant)
	}
}

// kvBytesPerToken implements the §4 formula:
//
//	2 (K+V) × n_layers × n_kv_heads × head_dim × bytes_per_elem
//
// Substituting bytesPerElem = units/2 (units in half-bytes) cancels the
// leading 2 exactly, leaving pure integer arithmetic:
//
//	kvBytesPerToken = n_layers × n_kv_heads × head_dim × units
func kvBytesPerToken(nLayers, nKVHeads, headDim int, units int64) int64 {
	return int64(nLayers) * int64(nKVHeads) * int64(headDim) * units
}

// liveBudget reads the LIVE measured VRAM budget via the vrambroker package
// (read-only nvidia-smi shell-out — §11.4.174 process-ownership-safe, no
// Acquire/write of any kind). Returns the TOTAL and FREE bytes.
func liveBudget() (total, free int64, err error) {
	b := vrambroker.New()
	t, _, f := b.Budget()
	if t == 0 {
		return 0, 0, fmt.Errorf("vrambroker: live VRAM budget unavailable (nvidia-smi read failed) — refusing fail-closed (§11.4.6)")
	}
	return t, f, nil
}

// Validate runs the D1/D4/D7 pre-boot checks against cfg. It NEVER boots a
// container and NEVER allocates GPU memory — it is a pure computation over
// the declared config plus (optionally) a single READ of the live VRAM
// budget. known is the collision registry (DefaultKnownLanes for production
// use; a test-injected list for deterministic Challenge fixtures).
//
// A non-nil error return means the CONFIG ITSELF is malformed (missing/
// invalid required fields) — a distinct failure class from a well-formed
// config that fails one of the three D1/D4/D7 checks (reported via
// Result.OK==false / Result.Codes, never an error).
func Validate(cfg LaneConfig, known []KnownLane) (Result, error) {
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return Result{}, fmt.Errorf("port must be a valid TCP port (1-65535), got %d", cfg.Port)
	}
	if strings.TrimSpace(cfg.ContainerName) == "" {
		return Result{}, fmt.Errorf("container_name must not be empty")
	}
	if cfg.NLayers <= 0 || cfg.NKVHeads <= 0 || cfg.HeadDim <= 0 {
		return Result{}, fmt.Errorf("n_layers/n_kv_heads/head_dim must all be > 0 (got %d/%d/%d)", cfg.NLayers, cfg.NKVHeads, cfg.HeadDim)
	}
	if cfg.CtxSize <= 0 {
		return Result{}, fmt.Errorf("ctx_size must be > 0 (got %d)", cfg.CtxSize)
	}
	if cfg.Parallel <= 0 {
		return Result{}, fmt.Errorf("parallel must be > 0 (got %d)", cfg.Parallel)
	}
	if cfg.NGL < 0 {
		return Result{}, fmt.Errorf("ngl must be >= 0 (got %d)", cfg.NGL)
	}

	units, err := kvUnitsPerElem(cfg.KVQuant)
	if err != nil {
		return Result{}, err
	}

	weightBytes := cfg.WeightBytes
	if weightBytes <= 0 {
		if cfg.ModelPath == "" {
			return Result{}, fmt.Errorf("weight_bytes not provided and model_path is empty — cannot determine weight size (§11.4.6 no-guessing)")
		}
		fi, statErr := os.Stat(cfg.ModelPath)
		if statErr != nil {
			return Result{}, fmt.Errorf("weight_bytes not provided and model_path %q is not stat-able: %w", cfg.ModelPath, statErr)
		}
		weightBytes = fi.Size()
	}

	fullLayers := cfg.FullLayers
	if fullLayers <= 0 {
		fullLayers = cfg.NLayers
	}

	headroom := cfg.HeadroomBytes
	if headroom <= 0 {
		headroom = vrambroker.HeadroomBytes
	}

	budget := cfg.BudgetBytes
	if budget <= 0 {
		_, free, lerr := liveBudget()
		if lerr != nil {
			return Result{}, fmt.Errorf("budget_bytes not provided and %w", lerr)
		}
		budget = free
	}

	perToken := kvBytesPerToken(cfg.NLayers, cfg.NKVHeads, cfg.HeadDim, units)
	worstCaseKV := perToken * cfg.CtxSize * int64(cfg.Parallel)
	totalStatic := weightBytes + worstCaseKV
	available := budget - headroom

	res := Result{
		WeightBytes:      weightBytes,
		KVBytesPerToken:  perToken,
		WorstCaseKVBytes: worstCaseKV,
		TotalStaticBytes: totalStatic,
		BudgetBytes:      budget,
		HeadroomBytes:    headroom,
		AvailableBytes:   available,
		FullLayers:       fullLayers,
	}

	// (a) STATIC_FOOTPRINT_EXCEEDS_BUDGET — D1 mitigation.
	if totalStatic > available {
		res.Codes = append(res.Codes, "STATIC_FOOTPRINT_EXCEEDS_BUDGET")
		res.Reasons = append(res.Reasons, fmt.Sprintf(
			"worst-case static weights+KV sum %d bytes (%s) > available budget %d bytes (%s) [budget=%d (%s) - headroom=%d (%s)]",
			totalStatic, humanBytes(totalStatic), available, humanBytes(available),
			budget, humanBytes(budget), headroom, humanBytes(headroom)))
	}

	// (b) NGL_BELOW_FULL_RESIDENCY — D7 mitigation.
	if cfg.NGL < fullLayers {
		res.Codes = append(res.Codes, "NGL_BELOW_FULL_RESIDENCY")
		res.Reasons = append(res.Reasons, fmt.Sprintf(
			"-ngl %d is below the model's full residency target of %d layers — full-GPU-residency is NOT achievable; llama.cpp would silently spill layers to CPU RAM",
			cfg.NGL, fullLayers))
	}

	// (c) PORT_COLLISION / CONTAINER_NAME_COLLISION — D4 mitigation.
	for _, k := range known {
		if k.Port == cfg.Port {
			res.Codes = append(res.Codes, "PORT_COLLISION")
			res.Reasons = append(res.Reasons, fmt.Sprintf(
				"proposed port %d collides with existing lane %q", cfg.Port, k.Name))
		}
		if strings.EqualFold(k.Name, cfg.ContainerName) {
			res.Codes = append(res.Codes, "CONTAINER_NAME_COLLISION")
			res.Reasons = append(res.Reasons, fmt.Sprintf(
				"proposed container name %q collides with existing lane on port %d", cfg.ContainerName, k.Port))
		}
	}

	res.OK = len(res.Codes) == 0
	return res, nil
}

// humanBytes renders n bytes as a human-readable GiB string for error
// messages/logs (bytes stay the source of truth in every field above).
func humanBytes(n int64) string {
	const gib = 1024 * 1024 * 1024
	return fmt.Sprintf("%.2f GiB", float64(n)/float64(gib))
}
