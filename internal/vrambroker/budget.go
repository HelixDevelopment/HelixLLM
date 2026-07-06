package vrambroker

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// MiB is one mebibyte in bytes. nvidia-smi reports memory in MiB (with
// --format=...,nounits); the broker works internally in bytes.
const MiB int64 = 1 << 20

// nvidiaSMIArgs is the exact query used to read the live VRAM budget. Design Q3
// resolution: shell `nvidia-smi` (works rootless in-container, simplest reliable
// source) rather than NVML/go-nvml.
var nvidiaSMIArgs = []string{
	"--query-gpu=memory.total,memory.used,memory.free",
	"--format=csv,noheader,nounits",
}

// budgetReader reads (total, used, free) VRAM in BYTES. It is the injection seam
// for unit tests (CONST-050(A): fakes live only in *_test.go). Production always
// resolves to readNvidiaSMI, which shells the real tool.
type budgetReader func(ctx context.Context) (total, used, free int64, err error)

// readNvidiaSMI queries the real nvidia-smi and returns live VRAM in bytes.
func readNvidiaSMI(ctx context.Context) (total, used, free int64, err error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cctx, "nvidia-smi", nvidiaSMIArgs...)
	out, err := cmd.Output()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("nvidia-smi query failed: %w", err)
	}
	return parseSMICSV(string(out))
}

// parseSMICSV parses the FIRST GPU row of `memory.total, memory.used,
// memory.free` (in MiB, nounits) and returns the values converted to BYTES.
func parseSMICSV(out string) (total, used, free int64, err error) {
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) != 3 {
			return 0, 0, 0, fmt.Errorf("unexpected nvidia-smi row %q: want 3 CSV fields, got %d", line, len(fields))
		}
		t, e1 := strconv.ParseInt(strings.TrimSpace(fields[0]), 10, 64)
		u, e2 := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
		f, e3 := strconv.ParseInt(strings.TrimSpace(fields[2]), 10, 64)
		if e1 != nil {
			return 0, 0, 0, fmt.Errorf("parse memory.total from %q: %w", line, e1)
		}
		if e2 != nil {
			return 0, 0, 0, fmt.Errorf("parse memory.used from %q: %w", line, e2)
		}
		if e3 != nil {
			return 0, 0, 0, fmt.Errorf("parse memory.free from %q: %w", line, e3)
		}
		if t <= 0 {
			return 0, 0, 0, fmt.Errorf("nvidia-smi reported non-positive total %d MiB", t)
		}
		return t * MiB, u * MiB, f * MiB, nil
	}
	return 0, 0, 0, fmt.Errorf("nvidia-smi returned no GPU rows")
}
