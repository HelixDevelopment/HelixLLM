package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Usage:
//
//	laneconfig-validate <config.json> [known-lanes.json]
//
// Exit codes:
//
//	0  — config admissible: all three D1/D4/D7 checks pass.
//	2  — the config file itself is malformed / missing required fields
//	     (a distinct failure class from a well-formed-but-failing config).
//	20 — one or more of the STATIC_FOOTPRINT_EXCEEDS_BUDGET /
//	     NGL_BELOW_FULL_RESIDENCY / PORT_COLLISION / CONTAINER_NAME_COLLISION
//	     checks failed — see the printed FAIL lines for the specific code(s).
func main() {
	if len(os.Args) < 2 {
		fatal("usage: laneconfig-validate <config.json> [known-lanes.json]")
	}

	cfg, err := loadConfig(os.Args[1])
	if err != nil {
		fatal("config: %v", err)
	}

	known := DefaultKnownLanes
	if len(os.Args) >= 3 {
		known, err = loadKnownLanes(os.Args[2])
		if err != nil {
			fatal("known-lanes: %v", err)
		}
	}

	res, err := Validate(cfg, known)
	if err != nil {
		fatal("validate: %v", err)
	}

	printResult(cfg, res)
	if !res.OK {
		os.Exit(20)
	}
	os.Exit(0)
}

func loadConfig(path string) (LaneConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return LaneConfig{}, fmt.Errorf("read %q: %w", path, err)
	}
	var cfg LaneConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return LaneConfig{}, fmt.Errorf("parse %q as JSON: %w", path, err)
	}
	return cfg, nil
}

func loadKnownLanes(path string) ([]KnownLane, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	var lanes []KnownLane
	if err := json.Unmarshal(raw, &lanes); err != nil {
		return nil, fmt.Errorf("parse %q as JSON: %w", path, err)
	}
	return lanes, nil
}

func printResult(cfg LaneConfig, res Result) {
	fmt.Printf("lane: port=%d container=%q model=%q\n", cfg.Port, cfg.ContainerName, cfg.ModelPath)
	fmt.Printf("  weight_bytes=%d (%s)\n", res.WeightBytes, humanBytes(res.WeightBytes))
	fmt.Printf("  kv_bytes_per_token=%d ctx_size=%d parallel=%d -> worst_case_kv_bytes=%d (%s)\n",
		res.KVBytesPerToken, cfg.CtxSize, cfg.Parallel, res.WorstCaseKVBytes, humanBytes(res.WorstCaseKVBytes))
	fmt.Printf("  total_static_bytes=%d (%s)\n", res.TotalStaticBytes, humanBytes(res.TotalStaticBytes))
	fmt.Printf("  budget_bytes=%d (%s) headroom_bytes=%d (%s) available_bytes=%d (%s)\n",
		res.BudgetBytes, humanBytes(res.BudgetBytes), res.HeadroomBytes, humanBytes(res.HeadroomBytes),
		res.AvailableBytes, humanBytes(res.AvailableBytes))
	fmt.Printf("  ngl=%d full_layers_target=%d\n", cfg.NGL, res.FullLayers)

	if res.OK {
		fmt.Println("OK: lane config admissible — all D1/D4/D7 pre-boot checks passed")
		return
	}
	for i, code := range res.Codes {
		fmt.Printf("FAIL: %s: %s\n", code, res.Reasons[i])
	}
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", a...)
	os.Exit(2)
}
