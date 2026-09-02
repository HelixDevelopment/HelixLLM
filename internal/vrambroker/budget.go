package vrambroker

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
)

// MiB is one mebibyte in bytes. nvidia-smi reports memory in MiB (with
// --format=...,nounits); the broker works internally in bytes.
const MiB int64 = 1 << 20

// nvidiaSMIArgs is the exact query used to read the live VRAM budget. Design Q3
// resolution: shell `nvidia-smi` (works rootless in-container, simplest reliable
// source) rather than NVML/go-nvml.
//
// The two identity columns come first and are not decoration. nvidia-smi emits
// one row per GPU and that row ORDER is assigned at discovery time: it moves
// when a card is added, when the driver reloads, and when boot order differs.
// Asking for uuid and pci.bus_id is what lets a row be resolved by the device
// it describes rather than by the position it happens to occupy (§11.4.111).
var nvidiaSMIArgs = []string{
	"--query-gpu=uuid,pci.bus_id,memory.total,memory.used,memory.free",
	"--format=csv,noheader,nounits",
}

// deviceBudget is one GPU's measured VRAM alongside the stable identities it can
// be addressed by. Both identities are carried because operators reach for
// different ones: a UUID is what nvidia-smi -L prints, a PCI address is what
// lspci and the kernel report, and either names the same physical card on every
// enumeration.
type deviceBudget struct {
	UUID   capability.DeviceIdentity
	PCIBus capability.DeviceIdentity
	Total  int64
	Used   int64
	Free   int64
}

// budgetReader reads (total, used, free) VRAM in BYTES for the device the broker
// is bound to. It is the injection seam for unit tests (CONST-050(A): fakes live
// only in *_test.go). Production always resolves to the nvidia-smi reader.
type budgetReader func(ctx context.Context) (total, used, free int64, err error)

// nvidiaSMIReader returns a reader bound to one device by its stable identity.
//
// An empty want means no device was configured. That is honest on a single-GPU
// host — one row is the only device, not a position — and refused on a
// multi-GPU one, where choosing for the caller would be a guess about where the
// work will land (§11.4.6).
func nvidiaSMIReader(want capability.DeviceIdentity) budgetReader {
	return func(ctx context.Context) (total, used, free int64, err error) {
		devices, err := readNvidiaSMI(ctx)
		if err != nil {
			return 0, 0, 0, err
		}
		d, err := selectDevice(devices, want)
		if err != nil {
			return 0, 0, 0, err
		}
		return d.Total, d.Used, d.Free, nil
	}
}

// readNvidiaSMI queries the real nvidia-smi and returns every GPU it reports.
func readNvidiaSMI(ctx context.Context) ([]deviceBudget, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cctx, "nvidia-smi", nvidiaSMIArgs...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("nvidia-smi query failed: %w", err)
	}
	return parseSMICSV(string(out))
}

// parseSMICSV parses EVERY GPU row of `uuid, pci.bus_id, memory.total,
// memory.used, memory.free` (memory in MiB, nounits), converting memory to
// BYTES.
//
// Every row is returned, in the order read, and the order is not load-bearing:
// selectDevice resolves by identity. Returning only the first row is the defect
// this replaces — it silently attributed one card's free memory to whichever
// card the work actually loaded onto.
func parseSMICSV(out string) ([]deviceBudget, error) {
	var devices []deviceBudget

	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) != 5 {
			return nil, fmt.Errorf("unexpected nvidia-smi row %q: want 5 CSV fields, got %d", line, len(fields))
		}

		uuid := strings.TrimSpace(fields[0])
		pci := strings.TrimSpace(fields[1])
		if uuid == "" && pci == "" {
			return nil, fmt.Errorf("nvidia-smi row %q reports no device identity", line)
		}

		t, err := parseMiB(fields[2], "memory.total", line)
		if err != nil {
			return nil, err
		}
		u, err := parseMiB(fields[3], "memory.used", line)
		if err != nil {
			return nil, err
		}
		f, err := parseMiB(fields[4], "memory.free", line)
		if err != nil {
			return nil, err
		}
		if t <= 0 {
			return nil, fmt.Errorf("nvidia-smi reported non-positive total %d MiB for %q", t, uuid)
		}

		devices = append(devices, deviceBudget{
			UUID:   capability.DeviceIdentity(uuid),
			PCIBus: capability.DeviceIdentity(pci),
			Total:  t * MiB,
			Used:   u * MiB,
			Free:   f * MiB,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading nvidia-smi output: %w", err)
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("nvidia-smi returned no GPU rows")
	}
	return devices, nil
}

func parseMiB(field, name, line string) (int64, error) {
	v, err := strconv.ParseInt(strings.TrimSpace(field), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s from %q: %w", name, line, err)
	}
	return v, nil
}

// selectDevice resolves the device the budget is read from.
//
// want is matched against BOTH stable identities, never against a position. An
// empty want is only answerable when there is exactly one device: with several
// present, picking one would attribute a budget to a card the caller never
// named, which is the whole failure this guards (§11.4.111).
func selectDevice(devices []deviceBudget, want capability.DeviceIdentity) (deviceBudget, error) {
	if len(devices) == 0 {
		return deviceBudget{}, fmt.Errorf("%w: no devices were reported", ErrDeviceNotFound)
	}

	if want == "" {
		if len(devices) == 1 {
			return devices[0], nil
		}
		return deviceBudget{}, fmt.Errorf("%w: %d devices reported and none named; %s",
			ErrDeviceAmbiguous, len(devices), identityList(devices))
	}

	for _, d := range devices {
		if d.UUID == want || d.PCIBus == want {
			return d, nil
		}
	}
	return deviceBudget{}, fmt.Errorf("%w: %q is not among the measured devices; %s",
		ErrDeviceNotFound, want, identityList(devices))
}

// identityList names what WAS measured, so a refusal tells the operator which
// identities they may configure instead of only that theirs did not match.
func identityList(devices []deviceBudget) string {
	names := make([]string, 0, len(devices))
	for _, d := range devices {
		names = append(names, fmt.Sprintf("%s (%s)", d.UUID, d.PCIBus))
	}
	return "measured: " + strings.Join(names, ", ")
}
